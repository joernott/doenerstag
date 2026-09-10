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
	"server":     config.ScopeServer,
	"install":    config.ScopeInstall,
	"update":     config.ScopeInstall,
	"cleanup":    config.ScopeCleanup,
	"version":    config.ScopeGlobal,
	"user":       config.ScopeGlobal,
	"restaurant": config.ScopeGlobal,
	"order":      config.ScopeGlobal,
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

	logOptions := logging.Options{
		Level: cfg.Log.Level,
		File:  cfg.Log.File,
	}
	if cfg.Log.File == "" {
		// Write to the command's own output stream rather than reaching for
		// os.Stdout directly. In production cobra hands back os.Stdout, so the
		// behaviour is identical, and it lets a test capture the log.
		//
		// Unless the verb's own output is data. `restaurant export` writes a
		// document to standard output, and a log line in the middle of it is not
		// a cosmetic problem: it is the first line of the file, and the file no
		// longer parses. For those verbs the log goes to standard error, which is
		// what standard error is for.
		if writesDataToStdout(cmd) {
			logOptions.Output = cmd.ErrOrStderr()
		} else {
			logOptions.Output = cmd.OutOrStdout()
		}
	}

	logger, err := logging.New(logOptions)
	if err != nil {
		logger, err = logToStderrInstead(cmd, logOptions, err)
		if err != nil {
			reportFatal(cmd, err)
			return errAlreadyReported
		}
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

// topLevelVerb names the verb a command belongs to.
//
// A subcommand's own name is not enough: `user list` and `restaurant list` are
// both called "list", and keying the scope table on that would make the two
// share an entry and collide the moment they wanted different settings. The
// scope belongs to the verb, so the lookup walks up to it.
func topLevelVerb(cmd *cobra.Command) string {
	for cmd.HasParent() && cmd.Parent().HasParent() {
		cmd = cmd.Parent()
	}
	return cmd.Name()
}

// writesDataToStdout reports whether this command's standard output is a
// document rather than a report for a person.
//
// Only `restaurant export` without --output, today. It is asked as a question
// about the command rather than answered by a flag on appContext so that adding
// another such verb is one line here rather than a new mechanism.
func writesDataToStdout(cmd *cobra.Command) bool {
	if topLevelVerb(cmd) != "restaurant" || cmd.Name() != "export" {
		return false
	}
	// With --output the document goes to a file and standard output is free
	// for the log again.
	return cmd.Flags().Lookup("output") == nil || cmd.Flags().Lookup("output").Value.String() == ""
}

// logToStderrInstead salvages a run whose log file cannot be opened.
//
// The configuration file names one log destination, and it is the one the
// server writes to: /var/log/doenerstag/doenerstag.log, owned by the service
// user and readable by nobody else. Every administrative verb reads the same
// file to find the database, so `doenerstag update` run by a person at a
// terminal tried to open that log and exited FATAL before doing anything —
// "permission denied" on a file the operator never meant to write. Reported by
// a user, who had to pass -L to get anywhere.
//
// So for the verbs a person runs, a log destination that cannot be opened is a
// reason to log somewhere else and say so, not a reason to refuse to work. The
// server is the exception and stays fatal: it is a daemon started by systemd,
// it is meant to run as the user that owns that file, and one that could not
// write its log would run for months with nobody noticing it had never
// recorded anything.
//
// Standard error rather than standard output, because the verb's own output may
// be a document (`restaurant export`) and because a diagnostic belongs there.
func logToStderrInstead(
	cmd *cobra.Command,
	opts logging.Options,
	cause error,
) (*logging.Logger, error) {
	if opts.File == "" || topLevelVerb(cmd) == "server" {
		return nil, cause
	}

	out := cmd.ErrOrStderr()
	if out == nil {
		out = os.Stderr
	}

	logger, err := logging.New(logging.Options{Level: opts.Level, Output: out})
	if err != nil {
		// The fallback failed too. Report the original problem, which is the
		// one worth reading.
		return nil, cause
	}

	logger.Component("config").Warn().
		Str("log_file", opts.File).
		Err(cause).
		Msg("cannot write the configured log file; logging to standard error instead")

	return logger, nil
}
