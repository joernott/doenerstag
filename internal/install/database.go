package install

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/db"
)

// MaintenanceDatabase is the database the root identity connects to in order to
// create the application's own. It always exists.
const MaintenanceDatabase = "postgres"

// Identity is a database user and its password.
type Identity struct {
	User     string
	Password string `log:"-"`
}

// Provisioner creates the database, the roles and the privileges.
//
// Three identities are deliberately distinct, per docs/09_configuration.md:
//
//   - root creates the database and the roles, and is never stored.
//   - admin owns the database and its objects, and is never stored.
//   - runtime is what the running server uses, and is the only one written to
//     the configuration file.
//
// Separating them means a compromise of the running application yields an
// account that cannot create or drop anything.
type Provisioner struct {
	// Server describes how to reach the database server. Its User, Password and
	// Database are ignored; the identities below supply those.
	Server db.Options

	Root     Identity
	Admin    Identity
	Runtime  Identity
	Database string

	Logger *zerolog.Logger
}

// Provision brings the server to a state where the application can run.
//
// It is idempotent: an existing role or database is adopted rather than
// recreated, so re-running install after a failure part way through works.
func (p *Provisioner) Provision(ctx context.Context) error {
	if err := p.validate(); err != nil {
		return err
	}

	rootConn, err := p.connect(ctx, p.Root, MaintenanceDatabase)
	if err != nil {
		return fmt.Errorf("connecting as the root database user: %w", err)
	}
	defer func() { _ = rootConn.Close(ctx) }()

	if err := p.ensureRole(ctx, rootConn, p.Admin, true); err != nil {
		return err
	}
	if err := p.ensureRole(ctx, rootConn, p.Runtime, false); err != nil {
		return err
	}
	if err := p.ensureDatabase(ctx, rootConn); err != nil {
		return err
	}

	// The remaining work happens inside the application database, as its owner.
	adminConn, err := p.connect(ctx, p.Admin, p.Database)
	if err != nil {
		return fmt.Errorf("connecting as the admin database user: %w", err)
	}
	defer func() { _ = adminConn.Close(ctx) }()

	return p.grantRuntimePrivileges(ctx, adminConn)
}

func (p *Provisioner) validate() error {
	switch {
	case p.Database == "":
		return fmt.Errorf("no database name was given")
	case p.Root.User == "":
		return fmt.Errorf("no root database user was given")
	case p.Admin.User == "":
		return fmt.Errorf("no admin database user was given")
	case p.Runtime.User == "":
		return fmt.Errorf("no runtime database user was given")
	case p.Admin.User == p.Runtime.User:
		return fmt.Errorf(
			"the admin and runtime database users must differ, both are %q: "+
				"the runtime user exists precisely so that the running server cannot "+
				"create or drop anything", p.Admin.User)
	}
	return nil
}

func (p *Provisioner) connect(ctx context.Context, as Identity, database string) (*pgx.Conn, error) {
	opts := p.Server.WithUser(as.User, as.Password).WithDatabase(database)
	if err := opts.Validate(); err != nil {
		return nil, err
	}
	// A single connection rather than a pool: this is a handful of DDL
	// statements, and CREATE DATABASE cannot run in a transaction anyway.
	return pgx.Connect(ctx, opts.DSN())
}

// ensureRole creates a login role, or updates the password of an existing one.
//
// Updating rather than skipping matters when install is re-run: the operator
// may be correcting a password they have since forgotten.
func (p *Provisioner) ensureRole(ctx context.Context, conn *pgx.Conn, role Identity, createDB bool) error {
	var exists bool
	err := conn.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = $1)", role.User).Scan(&exists)
	if err != nil {
		return fmt.Errorf("checking for the role %s: %w", role.User, err)
	}

	verb := "CREATE"
	if exists {
		verb = "ALTER"
	}

	// Role names and passwords cannot be bind parameters in DDL, so they are
	// quoted explicitly: the name as an identifier, the password as a literal.
	stmt := fmt.Sprintf("%s ROLE %s LOGIN PASSWORD %s",
		verb, quoteIdentifier(role.User), quoteLiteral(role.Password))
	if createDB && !exists {
		// The admin identity owns the database, which means it needs to be able
		// to be given ownership of one.
		stmt += " NOCREATEDB NOSUPERUSER NOCREATEROLE"
	}

	if _, err := conn.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("%s role %s: %w", strings.ToLower(verb), role.User, err)
	}

	p.log("role ready", "role", role.User, "created", !exists)
	return nil
}

func (p *Provisioner) ensureDatabase(ctx context.Context, conn *pgx.Conn) error {
	var exists bool
	err := conn.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = $1)", p.Database).Scan(&exists)
	if err != nil {
		return fmt.Errorf("checking for the database %s: %w", p.Database, err)
	}

	if exists {
		// Adopting an existing database is what makes a re-run after a partial
		// install work. Its owner is corrected in case it was created by hand.
		stmt := fmt.Sprintf("ALTER DATABASE %s OWNER TO %s",
			quoteIdentifier(p.Database), quoteIdentifier(p.Admin.User))
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("taking ownership of the existing database %s: %w", p.Database, err)
		}
		p.log("database ready", "database", p.Database, "created", false)
		return nil
	}

	// CREATE DATABASE cannot run inside a transaction, which is one reason this
	// uses a plain connection rather than a pool with implicit transactions.
	stmt := fmt.Sprintf("CREATE DATABASE %s OWNER %s ENCODING 'UTF8'",
		quoteIdentifier(p.Database), quoteIdentifier(p.Admin.User))
	if _, err := conn.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("creating the database %s: %w", p.Database, err)
	}

	p.log("database ready", "database", p.Database, "created", true)
	return nil
}

// grantRuntimePrivileges gives the runtime user exactly what the running server
// needs and nothing else: connect, use the schema, and read and write rows.
//
// No CREATE, no DROP, no ownership. docs/09_configuration.md is explicit that
// the runtime user gets SELECT, INSERT, UPDATE and DELETE on the application
// tables and nothing more.
func (p *Provisioner) grantRuntimePrivileges(ctx context.Context, conn *pgx.Conn) error {
	runtime := quoteIdentifier(p.Runtime.User)
	database := quoteIdentifier(p.Database)

	statements := []string{
		fmt.Sprintf("GRANT CONNECT ON DATABASE %s TO %s", database, runtime),
		fmt.Sprintf("GRANT USAGE ON SCHEMA public TO %s", runtime),

		// Tables that already exist, for a re-run against a migrated database.
		fmt.Sprintf("GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %s", runtime),

		// And tables the migrations are about to create. Default privileges are
		// attached to the creating role, which is why this runs as admin.
		fmt.Sprintf("ALTER DEFAULT PRIVILEGES IN SCHEMA public "+
			"GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO %s", runtime),

		// The schema itself stays owned by admin: the runtime user must not be
		// able to create anything in it.
		fmt.Sprintf("REVOKE CREATE ON SCHEMA public FROM %s", runtime),
	}

	for _, stmt := range statements {
		if _, err := conn.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("granting privileges to %s: %w", p.Runtime.User, err)
		}
	}

	p.log("runtime privileges granted", "role", p.Runtime.User, "database", p.Database)
	return nil
}

// Migrate applies every outstanding migration as the admin identity.
//
// Migrations run as the schema owner rather than as the runtime user, which is
// the whole reason the two are separate: the runtime user has no privilege to
// create a table, so it could not apply them even by accident.
func (p *Provisioner) Migrate() error {
	opts := p.Server.WithUser(p.Admin.User, p.Admin.Password).WithDatabase(p.Database)

	migrator, err := db.NewMigrator(opts.DSN(), p.Logger)
	if err != nil {
		return fmt.Errorf("preparing the migrations: %w", err)
	}
	defer func() { _ = migrator.Close() }()

	if err := migrator.Up(); err != nil {
		return err
	}

	version, dirty, err := migrator.Version()
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf(
			"the schema is at version %d and marked dirty, meaning a migration "+
				"failed part way and was not rolled back; the database needs "+
				"attention before it can be used", version)
	}

	p.log("schema migrated", "schema_version", version)
	return nil
}

func (p *Provisioner) log(message string, pairs ...any) {
	if p.Logger == nil {
		return
	}
	// The component field comes from the logger the caller scoped, so setting
	// it again here would emit it twice.
	event := p.Logger.Info()
	for i := 0; i+1 < len(pairs); i += 2 {
		key, _ := pairs[i].(string)
		event = event.Interface(key, pairs[i+1])
	}
	event.Msg(message)
}

// quoteIdentifier renders a SQL identifier.
//
// Role and database names cannot be bind parameters in DDL, so they are
// interpolated. Every one here comes from the operator, which is not the same
// as being trustworthy: a database named `x"; DROP DATABASE y; --` should
// produce a badly named database, not a disaster.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// quoteLiteral renders a SQL string literal.
//
// Passwords go through here. With standard_conforming_strings on, which has
// been the default for many years, doubling single quotes is sufficient and
// backslashes are ordinary characters. A password containing a backslash is
// therefore stored as typed rather than as an escape.
func quoteLiteral(value string) string {
	return `'` + strings.ReplaceAll(value, `'`, `''`) + `'`
}
