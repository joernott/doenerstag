package main

import (
	"github.com/spf13/cobra"

	"github.com/joernott/doenerstag/internal/config"
	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/install"
	"github.com/joernott/doenerstag/internal/version"
)

// runInstall provisions the database and writes a configuration file.
func runInstall(app *appContext, cmd *cobra.Command) error {
	cfg := app.Config

	output := cfg.Install.Output
	if output == "" {
		// Defaults to the file that was read, which is what makes re-running
		// install against an existing installation the normal case.
		output = cfg.File
	}
	if output == "" {
		output = config.DefaultConfigFile
	}

	result, err := install.Run(cmd.Context(), install.Options{
		Config:     cfg,
		OutputPath: output,
		AppVersion: version.Version(),
		Prompter: install.NewPrompter(
			cmd.InOrStdin(), cmd.OutOrStdout(), cfg.Install.NonInteractive),
		Logger: app.Logger.Component("install"),
	})
	if err != nil {
		return err
	}

	// cmd.Printf writes to the command's own stream. Terminal output that
	// cannot usefully fail does not need its error threaded back to the caller.
	cmd.Printf("\nInstalled doenerstag %s.\n", version.Version())
	cmd.Printf("  Database:      %s\n", result.Database)
	cmd.Printf("  Schema:        version %d\n", result.SchemaVersion)
	cmd.Printf("  Administrator: %s\n", result.Administrator)
	cmd.Printf("  Configuration: %s\n", result.ConfigPath)
	cmd.Printf("\nStart the server with:\n  doenerstag server -c %s\n", result.ConfigPath)
	return nil
}

// runVersion reports what the database says is installed.
//
// Deliberately distinct from --version, which reports what the binary is
// without touching anything. The two disagreeing is exactly what a forgotten
// `doenerstag update` looks like.
func runVersion(app *appContext, cmd *cobra.Command) error {
	ctx := cmd.Context()

	pool, err := db.Connect(ctx, db.OptionsFromConfig(app.Config.Database),
		app.Logger.Component("db"))
	if err != nil {
		return err
	}
	defer pool.Close()

	installed, err := install.ReadVersion(ctx, pool)
	if err != nil {
		return err
	}

	cmd.Println(installed)

	if binary := version.Version(); !matchesBinary(installed, binary) {
		cmd.Printf(
			"\nThis binary is %s, which does not match what is installed.\n"+
				"Run doenerstag update to bring the database forward.\n", binary)
	}
	return nil
}

// matchesBinary compares the recorded version with the running binary's,
// tolerating a development build that reports no real version.
func matchesBinary(installed install.InstalledVersion, binary string) bool {
	if version.IsDevelopment() {
		return true
	}
	parsed, err := install.ParseSemanticVersion(binary)
	if err != nil {
		return true
	}
	return parsed == installed.SemanticVersion
}
