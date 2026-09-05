package main

import (
	"context"
	"errors"
	"os"

	"github.com/spf13/cobra"

	"github.com/joernott/doenerstag/internal/config"
	"github.com/joernott/doenerstag/internal/logging"
)

// errAlreadyReported marks an error that has already been written to the log,
// so that main does not report it a second time in a different format.
var errAlreadyReported = errors.New("already reported")

// appContext carries what every verb needs: the resolved configuration and a
// configured logger. It is built by the root command's PersistentPreRunE and
// handed to the verb.
type appContext struct {
	Config *config.Config
	Logger *logging.Logger

	// cancel stops the log reopen handler on shutdown.
	cancel context.CancelFunc
}

// Close releases the logger. Safe on a partially built context.
func (a *appContext) Close() {
	if a.cancel != nil {
		a.cancel()
	}
	if a.Logger != nil {
		_ = a.Logger.Close()
	}
}

// verbScopes maps each verb to the settings it understands. update takes the
// same flags as install.
var verbScopes = map[string]config.Scope{
	"server":  config.ScopeServer,
	"install": config.ScopeInstall,
	"update":  config.ScopeInstall,
	"cleanup": config.ScopeCleanup,
	"version": config.ScopeGlobal,
}

// setup resolves the configuration and starts logging for the verb being run.
//
// A failure here is reported as FATAL on a bootstrap logger and returned as
// errAlreadyReported, because the two rules it enforces — no secret on the
// command line, and a configuration file no one else can read — are documented
// as fatal security errors rather than as ordinary usage problems.
func (a *appContext) setup(cmd *cobra.Command, scope config.Scope) error {
	cfg, err := config.Load(config.LoadOptions{
		Flags: cmd.Flags(),
		Scope: scope,
	})
	if err != nil {
		reportFatal(cmd, err)
		return errAlreadyReported
	}

	logger, err := logging.New(logging.Options{
		Level: cfg.Log.Level,
		File:  cfg.Log.File,
	})
	if err != nil {
		reportFatal(cmd, err)
		return errAlreadyReported
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	logger.HandleReopenSignal(ctx)

	a.Config = cfg
	a.Logger = logger
	a.cancel = cancel

	startup := logger.Component("startup")
	event := startup.Debug()
	if event.Enabled() {
		event.
			Str("verb", cmd.Name()).
			Str("config_file", configFileForLog(cfg)).
			Str("log_level", cfg.Log.Level.String()).
			Msg("configuration resolved")
	}
	if config.SkipPermissionCheck() {
		startup.Debug().Msg(
			"configuration file permission check skipped: not a POSIX platform")
	}

	return nil
}

func configFileForLog(cfg *config.Config) string {
	if cfg.File == "" {
		return "(none)"
	}
	return cfg.File
}

// reportFatal writes a FATAL line for an error that occurred before the real
// logger exists.
//
// It uses a minimal logger writing JSON to stderr, so that a configuration
// failure is still machine-readable and consistent with everything else the
// application emits.
func reportFatal(cmd *cobra.Command, err error) {
	out := cmd.ErrOrStderr()
	if out == nil {
		out = os.Stderr
	}

	bootstrap, newErr := logging.New(logging.Options{
		Level:  logging.LevelFatal,
		Output: out,
	})
	if newErr != nil {
		// Nothing left to log with; say it plainly.
		cmd.PrintErrln("Error:", err)
		return
	}

	// Not zerolog's Fatal(), which calls os.Exit and would bypass the deferred
	// cleanup and the exit code main is responsible for.
	bootstrap.Zerolog().WithLevel(logging.LevelFatal.Zerolog()).
		Str(logging.FieldComponent, "config").
		Err(err).
		Msg("cannot start")
}
