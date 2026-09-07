// Package update brings an existing installation forward.
//
// It is the smaller half of install: the database and its roles already exist,
// so there is nothing to create. What is left is applying the migrations this
// binary carries and that the database has not seen, bringing the configuration
// file forward, and recording that it happened.
//
// docs/09_configuration.md is the contract.
package update

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/config"
	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/install"
)

// ErrPreRelease is returned by a binary that was never released.
//
// docs/09_configuration.md: "Until the first release ships, update prints a
// message that updating is not yet possible and exits with status 1." The
// machinery below is written and tested regardless -- a verb whose code is
// written on the day it is first needed is a verb nobody has ever run.
//
// The test is 0.0.0 and not "below 1.0.0". The first release is 0.1.0, and a
// major-version test would refuse every upgrade inside the whole 0.x line
// while telling the operator to run install instead -- which would try to
// create a database and roles that already exist.
var ErrPreRelease = errors.New(
	"updating is not possible from an unreleased build.\n" +
		"This binary reports 0.0.0, the version an untagged development build\n" +
		"carries, so there is no released version it could be updating from.\n" +
		"Use doenerstag install instead")

// Options are what the update verb needs.
type Options struct {
	// Config is the resolved configuration: the existing file overlaid with
	// this binary's defaults, which is exactly what the new file should say.
	Config *config.Config

	// OutputPath is the configuration file to write. Normally the file that was
	// read, which is what makes update the in-place operation it should be.
	OutputPath string

	// ExistingPath is the file to read the retired settings out of. Empty means
	// OutputPath, which is the usual case.
	ExistingPath string

	// AppVersion is this binary's version, recorded in app_version.
	AppVersion string

	// ResetRootPassword asks for a new password for the root administrator.
	ResetRootPassword bool

	Prompter *install.Prompter
	Logger   *zerolog.Logger

	// Now is injected by the tests. Nil means time.Now.
	Now func() time.Time
}

// Result reports what an update run did.
type Result struct {
	ConfigPath string

	// SchemaBefore and SchemaAfter are the migration versions. Equal means
	// there was nothing to apply, which is a normal outcome and not a failure.
	SchemaBefore uint
	SchemaAfter  uint

	AppVersion install.SemanticVersion

	// Retired lists the configuration keys the new file comments out.
	Retired []string

	// RootPasswordReset reports whether the administrator's password changed.
	RootPasswordReset bool
}

// Run applies the migrations and rewrites the configuration file.
//
// The order is the same as install's, and for the same reason: prove the file
// can be written before doing anything that a failure would leave half-done,
// and write it last, once what it describes is true.
func Run(ctx context.Context, opts Options) (Result, error) {
	if opts.Config == nil {
		return Result{}, errors.New("update: no configuration was resolved")
	}
	if opts.Prompter == nil {
		return Result{}, errors.New("update: no prompter was supplied")
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	appVersion, err := install.ParseSemanticVersion(opts.AppVersion)
	if err != nil {
		return Result{}, err
	}
	if appVersion == (install.SemanticVersion{}) {
		return Result{}, ErrPreRelease
	}

	if err := config.CheckWritable(opts.OutputPath); err != nil {
		return Result{}, err
	}

	p := opts.Prompter
	p.Say("Updating doenerstag to %s.", opts.AppVersion)
	p.Say("")

	// The migrations run as the schema's owner, not as the runtime user, which
	// has no DDL rights at all -- that is the whole point of the three
	// identities in docs/09_configuration.md.
	admin, err := askAdmin(p, opts.Config)
	if err != nil {
		return Result{}, err
	}

	server := db.OptionsFromConfig(opts.Config.Database)
	migrator := &install.Provisioner{
		Server:   server,
		Admin:    admin,
		Database: opts.Config.Database.Name,
		Logger:   opts.Logger,
	}

	before, err := migrator.SchemaVersion()
	if err != nil {
		return Result{}, err
	}

	expected, err := db.LatestVersion()
	if err != nil {
		return Result{}, err
	}

	// A schema newer than the binary means somebody is running an old binary
	// against a database a newer one has already migrated. Applying nothing is
	// right, but so is refusing: the old binary's queries may not match the new
	// schema, and starting it would be the actual damage.
	if before > expected {
		return Result{}, fmt.Errorf(
			"the database schema is at version %d and this binary only knows %d.\n"+
				"It has been updated by a newer doenerstag. Use that one",
			before, expected)
	}

	if before == expected {
		p.Say("The schema is already at version %d.", before)
	} else {
		p.Say("Applying migrations %d to %d...", before, expected)
		if err := migrator.Migrate(); err != nil {
			return Result{}, err
		}
	}

	// Everything from here runs as the runtime user, which proves in passing
	// that it can still do its job against the new schema.
	pool, err := db.Connect(ctx, server, opts.Logger)
	if err != nil {
		return Result{}, fmt.Errorf("connecting as the runtime user: %w", err)
	}
	defer pool.Close()

	if err := db.VerifyServerVersion(ctx, pool); err != nil {
		return Result{}, err
	}

	result := Result{
		ConfigPath:   opts.OutputPath,
		SchemaBefore: before,
		SchemaAfter:  expected,
		AppVersion:   appVersion,
	}

	if opts.ResetRootPassword {
		// The default is what --root-password or DOENER_ROOT_PASSWORD resolved
		// to, which is what makes the reset work unattended -- the same route
		// install offers, because an operator scripting an update should not
		// have to do it differently.
		password, err := p.AskSecretTwice(install.Question{
			Prompt:  "New password for " + install.AdministratorName,
			Default: opts.Config.Install.RootAccountPassword,
			Env:     "DOENER_ROOT_PASSWORD",
		})
		if err != nil {
			return Result{}, err
		}
		if err := install.EnsureAdministrator(ctx, pool, password); err != nil {
			return Result{}, err
		}
		result.RootPasswordReset = true
	}

	if err := install.RecordVersion(ctx, pool, appVersion, expected); err != nil {
		return Result{}, err
	}

	retired, err := writeConfig(opts, now())
	if err != nil {
		return Result{}, err
	}
	result.Retired = retired

	return result, nil
}

// askAdmin collects the identity that owns the schema.
//
// Not stored, exactly as at install time: it is used for this run's migrations
// and forgotten.
func askAdmin(p *install.Prompter, cfg *config.Config) (install.Identity, error) {
	user, err := p.Ask(install.Question{
		Prompt:  "Database user that owns the schema",
		Default: adminDefault(cfg),
		Flag:    "database-admin-user",
		Env:     "DOENER_DATABASE_ADMIN_USER",
	})
	if err != nil {
		return install.Identity{}, err
	}

	password, err := p.Ask(install.Question{
		Prompt: "Password for " + user, Secret: true, AllowEmpty: true,
		Default: cfg.Install.AdminPassword,
		Env:     "DOENER_DATABASE_ADMIN_PASSWORD",
	})
	if err != nil {
		return install.Identity{}, err
	}

	return install.Identity{User: user, Password: password}, nil
}

func adminDefault(cfg *config.Config) string {
	if cfg.Install.AdminUser != "" {
		return cfg.Install.AdminUser
	}
	return cfg.Database.Name + "_admin"
}

/*
writeConfig rewrites the configuration file.

Every setting this binary knows about is written with its explanatory comment
and the value currently in force -- which is the operator's, because the
configuration was resolved from their file, or this version's default for a
setting their file did not have. Anything their file had that this binary does
not know about is written back as a comment rather than dropped.
*/
func writeConfig(opts Options, at time.Time) ([]string, error) {
	existingPath := opts.ExistingPath
	if existingPath == "" {
		existingPath = opts.OutputPath
	}

	var retired []config.RetiredSetting
	if existing, err := os.ReadFile(existingPath); err == nil { //nolint:gosec // an operator-supplied path, which is the point
		retired = config.RetiredIn(string(existing))
	}

	body, err := config.RenderFile(config.ValuesFrom(opts.Config), config.RenderOptions{
		GeneratedAt: at,
		Verb:        "update",
		Retired:     retired,
	})
	if err != nil {
		return nil, err
	}
	if err := config.WriteFile(opts.OutputPath, body); err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(retired))
	for _, setting := range retired {
		keys = append(keys, setting.Key)
	}
	return keys, nil
}
