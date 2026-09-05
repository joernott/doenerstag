package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// notImplementedError marks a verb whose behaviour a later sprint delivers. It
// carries the task number so that the message points at the plan rather than
// leaving the operator wondering whether they used the command wrongly.
type notImplementedError struct {
	verb string
	task string
}

func (e *notImplementedError) Error() string {
	return fmt.Sprintf("%s is not implemented yet (task %s in docs/14_implementation_plan.md)",
		e.verb, e.task)
}

// notImplemented returns a RunE that fails with a clear message.
func notImplemented(verb, task string) func(*cobra.Command, []string) error {
	return func(*cobra.Command, []string) error {
		return &notImplementedError{verb: verb, task: task}
	}
}

// newRootCommand builds the whole command tree.
func newRootCommand() *cobra.Command {
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			return cmd.Help()
		},
	}

	root.AddCommand(
		newServerCommand(),
		newInstallCommand(),
		newUpdateCommand(),
		newCleanupCommand(),
		newVersionCommand(),
	)

	return root
}

func newServerCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "server",
		Short: "Run the web server",
		Long: `Run the HTTPS web server, serving the frontend and the REST API.

Use --no-https to serve plain HTTP, which is intended for development and for
deployments behind a TLS-terminating reverse proxy.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          notImplemented("server", "4.8"),
	}
}

func newInstallCommand() *cobra.Command {
	return &cobra.Command{
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
		RunE:          notImplemented("install", "3.1"),
	}
}

func newUpdateCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Update an existing installation",
		Long: `Apply outstanding database migrations and bring the configuration file
forward, adding settings introduced by the new version while preserving every
value already set.

Updating is not possible before the first release has shipped.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          notImplemented("update", "14.4"),
	}
}

func newCleanupCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "cleanup",
		Short: "Remove expired orders and unreferenced data",
		Long: `Delete orders whose deadline is older than the retention period, together
with the menu data and images that become unreferenced as a result.

Intended to be run from cron. Safe to run while the server is running, and safe
to run twice. Use --dry-run to see what a run would remove.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          notImplemented("cleanup", "9.7"),
	}
}

func newVersionCommand() *cobra.Command {
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
		RunE:          notImplemented("version", "3.12"),
	}
}
