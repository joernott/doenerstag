package db

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/golang-migrate/migrate/v4"
	// Registers the "pgx5" driver scheme used by pgxScheme below.
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/rs/zerolog"

	root "github.com/joernott/doenerstag"
)

// NoSchema is the version reported for a database with no migrations applied.
const NoSchema uint = 0

// Migrator applies and rolls back the schema migrations.
//
// The application uses golang-migrate as a library rather than shelling out to
// its CLI, so that install and update carry their migrations with them and an
// installation needs nothing but the binary.
type Migrator struct {
	m      *migrate.Migrate
	logger *zerolog.Logger
}

// NewMigrator builds a migrator for one database, using the embedded SQL.
//
// dsn must be a connection string for the database being migrated. Migrations
// run as the admin identity, which owns the schema; see
// docs/09_configuration.md.
//
// The caller must Close the migrator, which releases its own connection.
func NewMigrator(dsn string, logger *zerolog.Logger) (*Migrator, error) {
	return newMigratorFromFS(root.MigrationsFS, root.MigrationsDir, dsn, logger)
}

func newMigratorFromFS(fsys fs.FS, dir, dsn string, logger *zerolog.Logger) (*Migrator, error) {
	source, err := iofs.New(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("reading embedded migrations: %w", err)
	}

	// golang-migrate wants the pgx/v5 scheme rather than the plain postgres
	// one, and otherwise takes the same connection string.
	m, err := migrate.NewWithSourceInstance("iofs", source, pgxScheme(dsn))
	if err != nil {
		return nil, fmt.Errorf("preparing migrations: %w", err)
	}

	return &Migrator{m: m, logger: logger}, nil
}

// pgxScheme rewrites a postgres:// connection string to the pgx5:// scheme
// golang-migrate's driver registers itself under.
func pgxScheme(dsn string) string {
	const (
		postgres = "postgres://"
		postgre  = "postgresql://"
		pgx      = "pgx5://"
	)
	switch {
	case len(dsn) >= len(postgres) && dsn[:len(postgres)] == postgres:
		return pgx + dsn[len(postgres):]
	case len(dsn) >= len(postgre) && dsn[:len(postgre)] == postgre:
		return pgx + dsn[len(postgre):]
	default:
		return dsn
	}
}

// Up applies every outstanding migration. Applying nothing is not an error.
func (m *Migrator) Up() error {
	before, _, _ := m.Version()

	err := m.m.Up()
	if errors.Is(err, migrate.ErrNoChange) {
		m.log("schema already up to date", before, before)
		return nil
	}
	if err != nil {
		return fmt.Errorf("applying migrations: %w", err)
	}

	after, _, _ := m.Version()
	m.log("schema migrated", before, after)
	return nil
}

// Down rolls every migration back, leaving an empty database. It exists for the
// migration tests; nothing in the application calls it.
func (m *Migrator) Down() error {
	err := m.m.Down()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("rolling back migrations: %w", err)
	}
	return nil
}

// Migrate moves the schema to an exact version, in either direction.
func (m *Migrator) Migrate(version uint) error {
	err := m.m.Migrate(version)
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrating to version %d: %w", version, err)
	}
	return nil
}

// Steps applies n migrations forward, or -n backwards.
func (m *Migrator) Steps(n int) error {
	err := m.m.Steps(n)
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("stepping %d migrations: %w", n, err)
	}
	return nil
}

// Version reports the applied schema version and whether the database is in a
// dirty state, meaning a migration failed part way and was not rolled back.
//
// A database with no migrations applied reports NoSchema and no error.
func (m *Migrator) Version() (version uint, dirty bool, err error) {
	version, dirty, err = m.m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return NoSchema, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("reading schema version: %w", err)
	}
	return version, dirty, nil
}

// Close releases the migrator's connection and its source.
func (m *Migrator) Close() error {
	sourceErr, dbErr := m.m.Close()
	return errors.Join(sourceErr, dbErr)
}

func (m *Migrator) log(message string, from, to uint) {
	if m.logger == nil {
		return
	}
	// No component field here: the caller passes a logger already scoped to
	// whichever part of the application is migrating.
	m.logger.Info().
		Uint("from_version", from).
		Uint("to_version", to).
		Msg(message)
}

// LatestVersion reports the highest migration version the binary carries.
//
// The server refuses to start when the database does not match this, so a
// forgotten `doenerstag update` fails loudly rather than producing subtle
// errors later. See docs/09_configuration.md.
func LatestVersion() (uint, error) {
	source, err := iofs.New(root.MigrationsFS, root.MigrationsDir)
	if err != nil {
		return 0, fmt.Errorf("reading embedded migrations: %w", err)
	}
	defer func() { _ = source.Close() }()

	first, err := source.First()
	if err != nil {
		return 0, fmt.Errorf("no migrations are embedded: %w", err)
	}

	latest := first
	for {
		next, err := source.Next(latest)
		if err != nil {
			// Next reports an error once there is nothing after latest.
			return latest, nil
		}
		latest = next
	}
}
