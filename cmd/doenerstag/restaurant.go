package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/install"
	"github.com/joernott/doenerstag/internal/model"
	"github.com/joernott/doenerstag/internal/transfer"
)

// newRestaurantCommand builds the restaurant verb and its subcommands.
//
// Carrying a restaurant between installations is the reason this exists. A menu
// is an hour of typing, and the second installation that needs one -- a test
// server, a colleague's machine, a replacement for a machine that died --
// should not be an hour of typing again.
func newRestaurantCommand(app *appContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restaurant",
		Short: "Manage restaurants from the command line",
		Long: `List, delete, export and import restaurants.

export and import carry a restaurant between installations, with its contacts,
opening hours, menu and the tags, allergens and additives its items are marked
with. Images are not carried; see docs/09_configuration.md.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = cmd.Help()
			return errors.New("restaurant needs a subcommand: list, delete, export or import")
		},
	}

	cmd.AddCommand(
		newRestaurantListCommand(app),
		newRestaurantDeleteCommand(app),
		newRestaurantExportCommand(app),
		newRestaurantImportCommand(app),
	)
	return cmd
}

func newRestaurantListCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every restaurant with its id",
		Long: `List every restaurant: id, name, currency and how many menu items it has.

The id is the first column because it is what the other subcommands take.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withUserPool(app, func(ctx context.Context, pool *pgxpool.Pool) error {
				restaurants, err := db.ListRestaurants(ctx, pool)
				if err != nil {
					return err
				}
				return writeRestaurantTable(ctx, cmd.OutOrStdout(), pool, restaurants)
			})
		},
	}
}

func newRestaurantDeleteCommand(app *appContext) *cobra.Command {
	var id string
	var force bool

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a restaurant and everything that depends on it",
		Long: `Delete a restaurant, its contacts, opening hours, menu -- and its orders.

This is not what the delete button in the browser does. That one refuses while
any order still points at the restaurant, because somebody clicking it must not
be able to destroy what other people ordered last Thursday. This removes those
orders too, so it asks first and prints what it is about to destroy.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			restaurantID, err := uuid.Parse(strings.TrimSpace(id))
			if err != nil {
				return errors.New("--id is required, and must be a restaurant id")
			}
			return withUserPool(app, func(ctx context.Context, pool *pgxpool.Pool) error {
				return deleteRestaurant(ctx, cmd, pool, restaurantID, force)
			})
		},
	}

	cmd.Flags().StringVarP(&id, "id", "i", "", "id of the restaurant to delete")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "do not ask for confirmation")
	return cmd
}

func newRestaurantExportCommand(app *appContext) *cobra.Command {
	var ids []string
	var format, output string
	var all bool

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export restaurants as YAML or JSON",
		Long: `Write one or more restaurants to a file, or to standard output.

Give --id once per restaurant, or --all for every one of them. The result is a
single document either way, so a file holding one restaurant and a file holding
forty are read by the same import.

What travels: the restaurant, its contacts, its opening hours, its menu with
categories, prices, modifications, and the tags, allergens and additives its
items carry. Reference data travels as codes rather than ids, because those ids
are seeded per installation and mean nothing anywhere else.

What does not: images, and the orders anybody placed. An imported restaurant
has no logo and no history.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if all && len(ids) > 0 {
				return errors.New("--all and --id say different things; give one")
			}
			if !all && len(ids) == 0 {
				return errors.New("give --id at least once, or --all")
			}
			chosen, err := transfer.ParseFormat(format)
			if err != nil {
				return err
			}
			return withUserPool(app, func(ctx context.Context, pool *pgxpool.Pool) error {
				return exportRestaurants(ctx, cmd, pool, ids, all, chosen, output)
			})
		},
	}

	cmd.Flags().StringArrayVarP(&ids, "id", "i", nil,
		"id of a restaurant to export; may be given more than once")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "export every restaurant")
	cmd.Flags().StringVarP(&format, "format", "F", "yaml", "yaml or json")
	cmd.Flags().StringVarP(&output, "output", "o", "",
		"file to write; the default is standard output")
	return cmd
}

func newRestaurantImportCommand(app *appContext) *cobra.Command {
	var file, format string
	var overwrite bool

	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import restaurants from a file",
		Long: `Read a file written by export and write what is in it.

The format is worked out by reading the file rather than by trusting its name,
because a suffix is a claim somebody made and the two disagree often enough to
matter. --format overrides that.

A file may hold several restaurants. A restaurant whose id is already here is
skipped, and the run continues with the rest; --overwrite deletes the existing
one first, orders and all.
`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(file) == "" {
				return errors.New("--file is required")
			}
			return withUserPool(app, func(ctx context.Context, pool *pgxpool.Pool) error {
				return importRestaurants(ctx, cmd, pool, file, format, overwrite)
			})
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "the file to read")
	cmd.Flags().StringVarP(&format, "format", "F", "",
		"yaml or json; the default is to work it out from the content")
	cmd.Flags().BoolVarP(&overwrite, "overwrite", "o", false,
		"replace a restaurant that already has this id, destroying its orders")
	return cmd
}

// writeRestaurantTable prints the restaurants, aligned.
func writeRestaurantTable(
	ctx context.Context, out io.Writer, pool *pgxpool.Pool, restaurants []model.Restaurant,
) error {
	if len(restaurants) == 0 {
		_, _ = fmt.Fprintln(out, "No restaurants.")
		return nil
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tNAME\tCURRENCY\tITEMS\tORDERS")
	for i := range restaurants {
		r := &restaurants[i]
		counts, err := db.CountRestaurantDependents(ctx, pool, r.ID)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\n",
			r.ID, r.Name, r.CurrencyCode, counts.MenuItems, counts.Orders)
	}
	return w.Flush()
}

// deleteRestaurant confirms and then destroys.
func deleteRestaurant(
	ctx context.Context, cmd *cobra.Command, pool *pgxpool.Pool, id uuid.UUID, force bool,
) error {
	restaurant, err := db.RestaurantByID(ctx, pool, id)
	if errors.Is(err, db.ErrNotFound) {
		return fmt.Errorf("no restaurant has the id %s", id)
	}
	if err != nil {
		return err
	}

	counts, err := db.CountRestaurantDependents(ctx, pool, id)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "%s (%s)\n", restaurant.Name, restaurant.ID)
	_, _ = fmt.Fprintf(out, "  %d menu items in %d categories\n", counts.MenuItems, counts.Categories)
	_, _ = fmt.Fprintf(out, "  %d contacts, %d opening periods\n", counts.Contacts, counts.Hours)
	_, _ = fmt.Fprintf(out, "  %d orders holding %d items\n", counts.Orders, counts.OrderItems)

	if !force {
		// The orders are the part worth stopping for. Everything else can be
		// typed again; what people ordered cannot.
		prompter := install.NewPrompter(cmd.InOrStdin(), out, false)
		_, _ = fmt.Fprintln(out)
		confirmed, err := prompter.AskBool(
			fmt.Sprintf("Delete %s and all of that, permanently", restaurant.Name), false)
		if err != nil {
			return err
		}
		if !confirmed {
			_, _ = fmt.Fprintln(out, "Nothing was deleted.")
			return nil
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := db.HardDeleteRestaurant(ctx, tx, id); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "Deleted %s.\n", restaurant.Name)
	return nil
}

// exportRestaurants writes the document.
func exportRestaurants(
	ctx context.Context, cmd *cobra.Command, pool *pgxpool.Pool,
	ids []string, all bool, format transfer.Format, output string,
) error {
	doc := transfer.Document{Version: transfer.Version}

	if all {
		exported, err := transfer.ExportAll(ctx, pool)
		if err != nil {
			return err
		}
		doc.Restaurants = exported
	} else {
		for _, raw := range ids {
			id, err := uuid.Parse(strings.TrimSpace(raw))
			if err != nil {
				return fmt.Errorf("%q is not a restaurant id: %w", raw, err)
			}
			exported, err := transfer.Export(ctx, pool, id)
			if errors.Is(err, db.ErrNotFound) {
				return fmt.Errorf("no restaurant has the id %s", id)
			}
			if err != nil {
				return err
			}
			doc.Restaurants = append(doc.Restaurants, exported)
		}
	}

	if len(doc.Restaurants) == 0 {
		return errors.New("there are no restaurants to export")
	}

	encoded, err := transfer.Encode(&doc, format)
	if err != nil {
		return err
	}

	if output == "" {
		_, err := cmd.OutOrStdout().Write(encoded)
		return err
	}

	// 0600: a menu is not a secret, but a file this writes should not be more
	// readable than the configuration next to it by default.
	if err := os.WriteFile(output, encoded, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", output, err)
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Wrote %d restaurant(s) to %s.\n",
		len(doc.Restaurants), output)
	return nil
}

// importRestaurants reads the document and writes what is in it.
func importRestaurants(
	ctx context.Context, cmd *cobra.Command, pool *pgxpool.Pool,
	file, format string, overwrite bool,
) error {
	content, err := os.ReadFile(file) //nolint:gosec // a path the operator typed, which is the point
	if err != nil {
		return fmt.Errorf("reading %s: %w", file, err)
	}

	var chosen transfer.Format
	if strings.TrimSpace(format) == "" {
		chosen, err = transfer.DetectFormat(content)
	} else {
		chosen, err = transfer.ParseFormat(format)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}

	doc, err := transfer.Decode(content, chosen)
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}

	// Everything is written as the root administrator: the account that created
	// it on the machine it came from may not exist here, and inventing a
	// reference to it would be a lie in the audit trail.
	actor, err := db.UserByName(ctx, pool, "root")
	if err != nil {
		return fmt.Errorf("finding the root administrator to record as the importer: %w", err)
	}

	out := cmd.OutOrStdout()
	var imported, skipped int

	for i := range doc.Restaurants {
		restaurant := &doc.Restaurants[i]
		err := transfer.Import(ctx, pool, *restaurant, actor.ID, overwrite)
		switch {
		case errors.Is(err, transfer.ErrAlreadyExists):
			// Skipped rather than fatal: a file of forty restaurants where one
			// is already here should import the other thirty-nine.
			_, _ = fmt.Fprintf(out, "skipped  %s (%s): already here; --overwrite replaces it\n",
				restaurant.Name, restaurant.ID)
			skipped++
		case err != nil:
			return fmt.Errorf("importing %s: %w", restaurant.Name, err)
		default:
			_, _ = fmt.Fprintf(out, "imported %s (%s): %d items in %d categories\n",
				restaurant.Name, restaurant.ID, len(restaurant.Items), len(restaurant.Categories))
			imported++
		}
	}

	_, _ = fmt.Fprintf(out, "\n%d imported, %d skipped, read as %s.\n", imported, skipped, chosen)
	return nil
}
