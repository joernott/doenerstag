package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/cobra"

	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/install"
	"github.com/joernott/doenerstag/internal/model"
)

// newOrderCommand builds the order verb and its subcommands.
//
// Orders are the one thing in this application that nobody can recreate: a
// restaurant can be typed again and an account can be made again, but what
// forty people asked for last Thursday exists once. So this verb is small --
// look, and remove -- and the removal says what it is about to destroy.
func newOrderCommand(app *appContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "order",
		Short: "Inspect and remove orders",
		Long: `List orders and delete them.

There is no subcommand for creating or editing an order: that is what the
application is for, and an order created from a terminal would have no
participants and no purpose.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = cmd.Help()
			return errors.New("order needs a subcommand: list or delete")
		},
	}

	cmd.AddCommand(newOrderListCommand(app), newOrderDeleteCommand(app))
	return cmd
}

func newOrderListCommand(app *appContext) *cobra.Command {
	var verbose bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List every order with its id",
		Long: `List every order.

By default: the id, the restaurant and the deadline, which is what somebody
looking for a particular order needs to recognise it.

--verbose adds the creator, the restaurant's own id, whether the order is a
pickup or a delivery and when, and how many items it holds. That last number is
the one worth reading before deleting anything.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withUserPool(app, func(ctx context.Context, pool *pgxpool.Pool) error {
				// The clock decides which orders count as expired, which this
				// listing does not filter on: an administrator asking what is in
				// the database wants all of it, expired included.
				orders, err := db.ListOrders(ctx, pool, time.Now())
				if err != nil {
					return err
				}
				writeOrderTable(cmd.OutOrStdout(), orders, verbose)
				return nil
			})
		},
	}

	cmd.Flags().BoolVarP(&verbose, "verbose", "V", false,
		"also show the creator, the restaurant id, the fulfilment and the item count")
	return cmd
}

func newOrderDeleteCommand(app *appContext) *cobra.Command {
	var id string
	var force bool

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete an order and everything in it",
		Long: `Delete the order named by --id, with the items people added to it.

An order is the one thing here nobody can recreate, so this prints what it is
about to destroy and asks. --force skips the question, for a script.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			orderID, err := uuid.Parse(strings.TrimSpace(id))
			if err != nil {
				return errors.New("--id is required, and must be an order id")
			}
			return withUserPool(app, func(ctx context.Context, pool *pgxpool.Pool) error {
				return deleteOrder(ctx, cmd, pool, orderID, force)
			})
		},
	}

	cmd.Flags().StringVarP(&id, "id", "i", "", "id of the order to delete")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "do not ask for confirmation")
	return cmd
}

// writeOrderTable prints the orders, aligned.
func writeOrderTable(out io.Writer, orders []model.Order, verbose bool) {
	if len(orders) == 0 {
		_, _ = fmt.Fprintln(out, "No orders.")
		return
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	if verbose {
		_, _ = fmt.Fprintln(w,
			"ID\tRESTAURANT\tRESTAURANT ID\tCREATOR\tDEADLINE\tFULFILMENT\tAT\tITEMS")
	} else {
		_, _ = fmt.Fprintln(w, "ID\tRESTAURANT\tDEADLINE")
	}

	for i := range orders {
		o := &orders[i]
		if verbose {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
				o.ID, o.RestaurantName, o.RestaurantID, o.CreatorName,
				formatOrderTime(o.DeadlineAt), o.Fulfilment,
				formatOrderTime(o.FulfilmentAt), o.ItemCount)
			continue
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\n",
			o.ID, o.RestaurantName, formatOrderTime(o.DeadlineAt))
	}
	_ = w.Flush()
}

// formatOrderTime prints a timestamp in the machine's own zone.
//
// RFC 3339 without the seconds: an order deadline is a wall-clock time somebody
// agreed with a restaurant, and the seconds have never meant anything.
func formatOrderTime(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Local().Format("2006-01-02 15:04")
}

// deleteOrder confirms and then removes.
func deleteOrder(
	ctx context.Context, cmd *cobra.Command, pool *pgxpool.Pool, id uuid.UUID, force bool,
) error {
	order, err := db.OrderByID(ctx, pool, id)
	if errors.Is(err, db.ErrNotFound) {
		return fmt.Errorf("no order has the id %s", id)
	}
	if err != nil {
		return err
	}

	items, err := db.CountOrderItems(ctx, pool, id)
	if err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	_, _ = fmt.Fprintf(out, "%s at %s\n", order.RestaurantName, formatOrderTime(order.DeadlineAt))
	_, _ = fmt.Fprintf(out, "  id:      %s\n", order.ID)
	_, _ = fmt.Fprintf(out, "  creator: %s\n", order.CreatorName)
	_, _ = fmt.Fprintf(out, "  items:   %d\n", items)

	if !force {
		prompter := install.NewPrompter(cmd.InOrStdin(), out, false)
		_, _ = fmt.Fprintln(out)
		confirmed, err := prompter.AskBool("Delete this order and everything in it", false)
		if err != nil {
			return err
		}
		if !confirmed {
			_, _ = fmt.Fprintln(out, "Nothing was deleted.")
			return nil
		}
	}

	if err := db.DeleteOrder(ctx, pool, id); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "Deleted the order at %s.\n", order.RestaurantName)
	return nil
}
