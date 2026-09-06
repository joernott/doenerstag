package install

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/config"
	"github.com/joernott/doenerstag/internal/db"
)

// Options are what the install verb needs to do its work.
type Options struct {
	// Config is the resolved configuration: built-in defaults overlaid with any
	// existing configuration file, which is what supplies the answers offered
	// back to the operator.
	Config *config.Config

	// OutputPath is the configuration file to write.
	OutputPath string

	// AppVersion is the binary's own version, recorded in app_version.
	AppVersion string

	Prompter *Prompter
	Logger   *zerolog.Logger

	// Now is injected by tests. Nil means time.Now.
	Now func() time.Time
}

// Result reports what an install run did, for the caller to summarise.
type Result struct {
	Database      string
	ConfigPath    string
	SchemaVersion uint
	Administrator string
}

// Run performs a complete installation.
//
// The order matters. Writability is proven before a single question is asked,
// because half an hour of answers followed by "permission denied" is a bad
// trade. The configuration file is written last, after everything it describes
// actually exists, so a file on disk always corresponds to a working database.
func Run(ctx context.Context, opts Options) (Result, error) {
	if opts.Config == nil {
		return Result{}, errors.New("install: no configuration was resolved")
	}
	if opts.Prompter == nil {
		return Result{}, errors.New("install: no prompter was supplied")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	// 1. Prove we can write the answer before asking for it.
	if err := config.CheckWritable(opts.OutputPath); err != nil {
		return Result{}, err
	}

	// The version is a property of the binary, so it is knowable before
	// anything happens. Parsing it here rather than at the point of use means a
	// binary stamped with something unusable fails immediately, instead of
	// after it has asked for three passwords, created the roles and applied
	// every migration.
	appVersion, err := ParseSemanticVersion(opts.AppVersion)
	if err != nil {
		return Result{}, err
	}

	p := opts.Prompter
	p.Say("Installing doenerstag. Press return to accept the value in brackets.")
	p.Say("")

	// 2. The interview.
	answers, err := interview(p, opts)
	if err != nil {
		return Result{}, err
	}

	// 3. Create the database, the roles and the privileges.
	provisioner := &Provisioner{
		Server:   answers.server,
		Root:     answers.root,
		Admin:    answers.admin,
		Runtime:  Identity{User: answers.runtimeUser, Password: answers.runtimePassword},
		Database: answers.database,
		Logger:   opts.Logger,
	}

	p.Say("")
	p.Say("Creating the database and its roles...")
	if err := provisioner.Provision(ctx); err != nil {
		return Result{}, err
	}

	p.Say("Applying the schema...")
	if err := provisioner.Migrate(); err != nil {
		return Result{}, err
	}

	schemaVersion, err := db.LatestVersion()
	if err != nil {
		return Result{}, err
	}

	// 4. Everything from here runs as the runtime user, which proves in passing
	//    that the privileges granted above are actually sufficient.
	runtimeOpts := answers.server.
		WithUser(answers.runtimeUser, answers.runtimePassword).
		WithDatabase(answers.database)

	pool, err := db.Connect(ctx, runtimeOpts, opts.Logger)
	if err != nil {
		return Result{}, fmt.Errorf("connecting as the runtime user: %w", err)
	}
	defer pool.Close()

	if err := db.VerifyServerVersion(ctx, pool); err != nil {
		return Result{}, err
	}

	p.Say("Creating the %s administrator...", AdministratorName)
	if err := EnsureAdministrator(ctx, pool, answers.adminPassword); err != nil {
		return Result{}, err
	}

	for _, key := range ContentPageKeys {
		if err := LoadContentPage(ctx, pool, key, answers.contentPages[key]); err != nil {
			return Result{}, err
		}
	}

	if err := RecordVersion(ctx, pool, appVersion, schemaVersion); err != nil {
		return Result{}, err
	}

	// 5. The configuration file, written last so that its existence means the
	//    database it describes is ready.
	if err := writeConfig(opts, answers, now()); err != nil {
		return Result{}, err
	}

	return Result{
		Database:      answers.database,
		ConfigPath:    opts.OutputPath,
		SchemaVersion: schemaVersion,
		Administrator: AdministratorName,
	}, nil
}

// answers holds everything the interview collected.
type answers struct {
	server          db.Options
	database        string
	root            Identity
	admin           Identity
	runtimeUser     string
	runtimePassword string
	adminPassword   string // the root account's password, not a database one
	jwtSecret       string
	contentPages    map[string]string
}

func interview(p *Prompter, opts Options) (answers, error) {
	cfg := opts.Config
	a := answers{contentPages: map[string]string{}}

	ask := func(prompt, def, flag string) (string, error) {
		return p.Ask(Question{Prompt: prompt, Default: def, Flag: flag,
			Env: envFor(flag)})
	}

	host, err := ask("Database server", cfg.Database.Server, "database-server")
	if err != nil {
		return a, err
	}

	port, err := p.AskInt(Question{
		Prompt: "Database port", Default: itoa(cfg.Database.Port),
		Flag: "database-port", Env: envFor("database-port"),
		Validate: func(s string) error {
			n, _ := parseInt(s)
			if n < 1 || n > 65535 {
				return fmt.Errorf("%q is not a port number", s)
			}
			return nil
		},
	})
	if err != nil {
		return a, err
	}

	sslMode, err := p.Ask(Question{
		Prompt: "Database TLS mode", Default: cfg.Database.SSLMode,
		Flag: "database-sslmode", Env: envFor("database-sslmode"),
		Validate: func(s string) error {
			if !slices.Contains(db.SSLModes, s) {
				return fmt.Errorf("%q is not one of %v", s, db.SSLModes)
			}
			return nil
		},
	})
	if err != nil {
		return a, err
	}

	a.server = db.Options{
		Host: host, Port: port, SSLMode: sslMode,
		Database: MaintenanceDatabase, User: "placeholder",
		MaxConnections: cfg.Database.MaxConnectionPool,
	}
	if a.server.MaxConnections < 1 {
		a.server.MaxConnections = 10
	}

	if a.database, err = ask("Database name", cfg.Database.Name, "database-name"); err != nil {
		return a, err
	}

	p.Say("")
	p.Say("The next two accounts are used once and never written to the configuration file.")

	if a.root.User, err = ask("Database user permitted to create databases and roles",
		orDefault(cfg.Install.RootUser, "postgres"), "database-root-user"); err != nil {
		return a, err
	}
	if a.root.Password, err = p.Ask(Question{
		Prompt: "Password for " + a.root.User, Secret: true, AllowEmpty: true,
		Default: cfg.Install.RootPassword,
		Flag:    "", Env: "DOENER_DATABASE_ROOT_PASSWORD",
	}); err != nil {
		return a, err
	}

	if a.admin.User, err = ask("Database user that will own the schema",
		orDefault(cfg.Install.AdminUser, a.database+"_admin"), "database-admin-user"); err != nil {
		return a, err
	}
	if a.admin.Password, err = p.Ask(Question{
		Prompt: "Password for " + a.admin.User, Secret: true, AllowEmpty: true,
		Default: cfg.Install.AdminPassword,
		Env:     "DOENER_DATABASE_ADMIN_PASSWORD",
	}); err != nil {
		return a, err
	}

	p.Say("")
	p.Say("The runtime account is what the server uses, and is stored in the configuration file.")

	if a.runtimeUser, err = ask("Database user for the running server",
		cfg.Database.User, "database-user"); err != nil {
		return a, err
	}
	if a.runtimePassword, err = p.AskSecretTwice(Question{
		Prompt:  "Password for " + a.runtimeUser,
		Default: cfg.Database.Password,
		Env:     "DOENER_DATABASE_PASSWORD",
	}); err != nil {
		return a, err
	}

	p.Say("")
	if a.adminPassword, err = p.AskSecretTwice(Question{
		Prompt:   "Password for the doenerstag " + AdministratorName + " account",
		Default:  cfg.Install.RootAccountPassword,
		Flag:     "root-password",
		Env:      envFor("root-password"),
		Validate: auth.ValidateComplexity,
	}); err != nil {
		return a, err
	}

	// An existing usable secret is kept, so a re-run does not log everyone out.
	a.jwtSecret = cfg.Session.JWTSecret
	if !JWTSecretIsUsable(a.jwtSecret) {
		if a.jwtSecret, err = GenerateJWTSecret(); err != nil {
			return a, err
		}
		p.Say("Generated a new session signing secret.")
	} else {
		p.Say("Keeping the existing session signing secret, so current sessions survive.")
	}

	p.Say("")
	for _, key := range ContentPageKeys {
		prompt := "HTML snippet for the " + key + " page (optional)"
		path, err := p.Ask(Question{
			Prompt: prompt, Default: cfg.Install.PageFile(key),
			AllowEmpty: true, Flag: pageFlag(key), Env: envFor(pageFlag(key)),
		})
		if err != nil {
			return a, err
		}
		a.contentPages[key] = path
	}

	return a, nil
}

func writeConfig(opts Options, a answers, at time.Time) error {
	cfg := *opts.Config
	cfg.Database.Server = a.server.Host
	cfg.Database.Port = a.server.Port
	cfg.Database.Name = a.database
	cfg.Database.User = a.runtimeUser
	cfg.Database.Password = a.runtimePassword
	cfg.Database.SSLMode = a.server.SSLMode
	cfg.Session.JWTSecret = a.jwtSecret

	body, err := config.Render(config.ValuesFrom(&cfg), at)
	if err != nil {
		return err
	}
	return config.WriteFile(opts.OutputPath, body)
}

func envFor(flag string) string {
	if flag == "" {
		return ""
	}
	setting, ok := config.SettingByFlag(flag)
	if !ok {
		return ""
	}
	return setting.Env()
}

func pageFlag(key string) string {
	if key == "imprint" {
		return "imprint-file"
	}
	return "legal-notes-file"
}

func orDefault(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}
