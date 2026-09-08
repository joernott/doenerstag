package main

import (
	"github.com/spf13/cobra"

	"github.com/joernott/doenerstag/internal/config"
	"github.com/joernott/doenerstag/internal/version"
)

// newRootCommand builds the whole command tree.
func newRootCommand() *cobra.Command {
	app := &appContext{}

	root := &cobra.Command{
		Use:   "doenerstag",
		Short: "Coordinate food orders for a group",
		Long: `doenerstag coordinates food orders for a group.

One person opens an order for a restaurant and a pickup time, everyone else
adds what they want, and a summary page says what to order and who owes what.

It does not take payments and does not place orders with restaurants.`,

		// A verb is required; running the bare command shows help.
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,

		Version: version.String(),

		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	// --version reports what the binary is, without touching the database.
	// The `version` verb reports what the database says is installed. The two
	// can disagree, and noticing that is the point of having both.
	root.SetVersionTemplate("{{.Version}}\n")

	config.RegisterGlobalFlags(root.PersistentFlags())

	root.AddCommand(
		newServerCommand(app),
		newInstallCommand(app),
		newUpdateCommand(app),
		newCleanupCommand(app),
		newVersionCommand(app),
		newUserCommand(app),
	)

	// Resolve configuration and start logging before any verb runs. The root
	// command itself is skipped: it only prints help, and failing that on a
	// malformed configuration file would be unhelpful.
	root.PersistentPreRunE = func(cmd *cobra.Command, _ []string) error {
		if cmd == root {
			return nil
		}
		return app.setup(cmd, verbScopes[topLevelVerb(cmd)])
	}
	root.PersistentPostRun = func(*cobra.Command, []string) {
		app.Close()
	}

	return root
}

func newServerCommand(app *appContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run the web server",
		Long: `Run the HTTPS web server, serving the frontend and the REST API.

Use --no-https to serve plain HTTP, which is intended for development and for
deployments behind a TLS-terminating reverse proxy.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runServer(app, cmd)
		},
	}
	config.RegisterScopeFlags(cmd.Flags(), config.ScopeServer)
	return cmd
}

func newInstallCommand(app *appContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Set up the database and write a configuration file",
		Long: `Provision the database and write a configuration file, interactively.

install creates the database and its runtime user, applies every migration
including the seed data, creates the root administrator, generates the session
signing secret and writes a configuration file containing every setting.

The privileged database credentials it asks for are used once and never
stored.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runInstall(app, cmd)
		},
	}
	config.RegisterScopeFlags(cmd.Flags(), config.ScopeInstall)
	return cmd
}

func newUpdateCommand(app *appContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update an existing installation",
		Long: `Apply outstanding database migrations and bring the configuration file
forward, adding settings introduced by the new version while preserving every
value already set.

Updating is not possible before the first release has shipped.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUpdate(app, cmd)
		},
	}
	// update takes the same flags as install.
	config.RegisterScopeFlags(cmd.Flags(), config.ScopeInstall)
	// ...and one of its own. Resetting the administrator's password is the
	// documented recovery path, and doing it during an update saves an
	// operator a second run of install.
	cmd.Flags().Bool("reset-root-password", false,
		"ask for a new password for the root administrator")
	return cmd
}

func newCleanupCommand(app *appContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Remove expired orders and unreferenced data",
		Long: `Delete orders whose deadline is older than the retention period, together
with the menu data and images that become unreferenced as a result.

Intended to be run from cron. Safe to run while the server is running, and safe
to run twice. Use --dry-run to see what a run would remove.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          func(cmd *cobra.Command, _ []string) error { return runCleanup(app, cmd) },
	}
	config.RegisterScopeFlags(cmd.Flags(), config.ScopeCleanup)
	return cmd
}

func newVersionCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show the installed application and schema version",
		Long: `Report the newest entry of the app_version table: the application version,
the schema version applied with it, and when that happened.

This connects to the database. Use the --version flag instead to report what
the binary itself is, without touching the database.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runVersion(app, cmd)
		},
	}
}
