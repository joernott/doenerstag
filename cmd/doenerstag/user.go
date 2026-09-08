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

	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/config"
	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/install"
	"github.com/joernott/doenerstag/internal/mail"
	"github.com/joernott/doenerstag/internal/model"
)

// newUserCommand builds the user verb and its subcommands.
//
// It exists because the two ways of administering accounts -- the browser and
// the machine -- serve different people. The browser is for the administrator
// who is already logged in; this is for the one who is not, or who has a
// hundred accounts to create, or who is repairing an installation from a
// terminal because nobody can log into it.
func newUserCommand(app *appContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "Manage user accounts",
		Long: `List, create, delete and reset the password of user accounts.

Everything here can also be done in the browser by the root administrator. This
exists for the cases where that is not possible: an installation nobody can log
into, or an account that has to be created before anyone has a password.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		// Without a RunE, cobra prints help for `user unexpected` and exits 0:
		// it only validates arguments for a command that does something. A verb
		// that needs a subcommand and was not given one has not done what was
		// asked, and a script should be able to tell.
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = cmd.Help()
			return errors.New("user needs a subcommand: list, add, delete or password")
		},
	}

	cmd.AddCommand(
		newUserListCommand(app),
		newUserAddCommand(app),
		newUserDeleteCommand(app),
		newUserPasswordCommand(app),
	)
	return cmd
}

func newUserListCommand(app *appContext) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List every account with its id",
		Long: `List every account: id, login name, display name, e-mail and whether it
is the administrator.

The id is the first column because it is what the other subcommands take, and
what the API paths use.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withUserPool(app, func(ctx context.Context, pool *pgxpool.Pool) error {
				users, err := db.ListUsers(ctx, pool)
				if err != nil {
					return err
				}
				writeUserTable(cmd.OutOrStdout(), users)
				return nil
			})
		},
	}
}

func newUserAddCommand(app *appContext) *cobra.Command {
	var name, displayName, email string

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Create an account with a generated password",
		Long: `Create an account and print the password that was generated for it.

The password is shown once, here, and is not recoverable afterwards: it is
stored as an Argon2id hash like every other. Hand it over by whatever means you
would hand over any password, and expect the person to change it.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if strings.TrimSpace(name) == "" {
				return errors.New("--username is required")
			}
			return withUserPool(app, func(ctx context.Context, pool *pgxpool.Pool) error {
				return addUser(ctx, cmd.OutOrStdout(), pool, name, displayName, email)
			})
		},
	}

	cmd.Flags().StringVarP(&name, "username", "u", "", "login name for the new account")
	cmd.Flags().StringVarP(&displayName, "displayname", "d", "", "name shown to other people; defaults to the login name")
	cmd.Flags().StringVarP(&email, "email", "e", "", "e-mail address, needed for a password reset by mail")
	return cmd
}

func newUserDeleteCommand(app *appContext) *cobra.Command {
	var id, name string

	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete an account",
		Long: `Delete an account, named either by --id or by --username.

What happens to the orders and items that account left behind is the rule in
docs/02_features.md F2.6, which this does not repeat or override: the database
decides, and it refuses to remove the administrator or the placeholder.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withUserPool(app, func(ctx context.Context, pool *pgxpool.Pool) error {
				user, err := findUser(ctx, pool, id, name)
				if err != nil {
					return err
				}
				if err := db.DeleteUser(ctx, pool, user.ID); err != nil {
					return fmt.Errorf("deleting %s: %w", user.Name, err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Deleted %s (%s).\n", user.Name, user.ID)
				return nil
			})
		},
	}

	cmd.Flags().StringVarP(&id, "id", "i", "", "id of the account to delete")
	cmd.Flags().StringVarP(&name, "username", "u", "", "login name of the account to delete")
	return cmd
}

func newUserPasswordCommand(app *appContext) *cobra.Command {
	var id, name string
	var setPassword bool

	cmd := &cobra.Command{
		Use:   "password",
		Short: "Reset an account's password",
		Long: `Reset a password, either by setting one or by issuing a reset link.

Without --set-password this prints a link that lets the account holder choose
their own password, valid for an hour, and mails it to them if the account has
an address and this installation can send mail. That is the better of the two:
an administrator who sets a password knows it, and a password two people know is
not a password.

With --set-password the new password is asked for on the terminal. Never on the
command line -- see docs/09_configuration.md for why.`,
		Args:          cobra.NoArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return withUserPool(app, func(ctx context.Context, pool *pgxpool.Pool) error {
				user, err := findUser(ctx, pool, id, name)
				if err != nil {
					return err
				}
				if setPassword {
					return setUserPassword(ctx, cmd, pool, user)
				}
				return issueResetLink(ctx, cmd, app.Config, user)
			})
		},
	}

	cmd.Flags().StringVarP(&id, "id", "i", "", "id of the account")
	cmd.Flags().StringVarP(&name, "username", "u", "", "login name of the account")
	cmd.Flags().BoolVar(&setPassword, "set-password", false,
		"ask for a new password on the terminal instead of issuing a reset link")
	return cmd
}

// withUserPool opens the database, runs fn and closes it.
func withUserPool(app *appContext, fn func(context.Context, *pgxpool.Pool) error) error {
	ctx := context.Background()

	pool, err := db.Connect(ctx, db.OptionsFromConfig(app.Config.Database), app.Logger.Component("db"))
	if err != nil {
		return err
	}
	defer pool.Close()

	return fn(ctx, pool)
}

// findUser resolves the --id or --username a subcommand was given.
//
// Exactly one is required. Accepting both and preferring one would let a
// mistyped id silently act on the account the name happens to match, which for
// `user delete` is the wrong account deleted without a word.
func findUser(ctx context.Context, pool *pgxpool.Pool, id, name string) (model.User, error) {
	id = strings.TrimSpace(id)
	name = strings.TrimSpace(name)

	switch {
	case id == "" && name == "":
		return model.User{}, errors.New("either --id or --username is required")
	case id != "" && name != "":
		return model.User{}, errors.New("--id and --username name two different things; give one")
	}

	if id != "" {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return model.User{}, fmt.Errorf("%q is not a user id: %w", id, err)
		}
		user, err := db.UserByID(ctx, pool, parsed)
		if errors.Is(err, db.ErrNotFound) {
			return model.User{}, fmt.Errorf("no account has the id %s", parsed)
		}
		return user, err
	}

	user, err := db.UserByName(ctx, pool, name)
	if errors.Is(err, db.ErrNotFound) {
		return model.User{}, fmt.Errorf("no account is called %q", name)
	}
	return user, err
}

// writeUserTable prints the accounts, aligned.
func writeUserTable(out io.Writer, users []model.User) {
	if len(users) == 0 {
		fmt.Fprintln(out, "No accounts.")
		return
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tDISPLAY NAME\tE-MAIL\tADMIN")
	for _, u := range users {
		admin := ""
		if u.IsAdmin {
			admin = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", u.ID, u.Name, u.DisplayName, u.Email, admin)
	}
	_ = w.Flush()
}

// addUser creates the account and prints its password.
func addUser(ctx context.Context, out io.Writer, pool *pgxpool.Pool, name, displayName, email string) error {
	if displayName == "" {
		displayName = name
	}

	password, err := auth.GeneratePassword()
	if err != nil {
		return fmt.Errorf("generating a password: %w", err)
	}
	hash, err := auth.Hash(password)
	if err != nil {
		return fmt.Errorf("hashing the password: %w", err)
	}

	user, err := db.CreateUser(ctx, pool, db.NewUser{
		Name:         name,
		DisplayName:  displayName,
		Email:        email,
		PasswordHash: hash,
	})
	if errors.Is(err, db.ErrNameTaken) {
		return fmt.Errorf("an account called %q already exists", name)
	}
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Created %s.\n", user.Name)
	fmt.Fprintf(out, "  Id:       %s\n", user.ID)
	fmt.Fprintf(out, "  Password: %s\n", password)
	fmt.Fprintln(out)
	fmt.Fprintln(out, "This is the only time the password is shown. It is stored as a hash.")
	return nil
}

// setUserPassword asks for a password and stores it.
func setUserPassword(ctx context.Context, cmd *cobra.Command, pool *pgxpool.Pool, user model.User) error {
	prompter := install.NewPrompter(cmd.InOrStdin(), cmd.OutOrStdout(), false)

	password, err := prompter.AskSecretTwice(install.Question{
		Prompt:   fmt.Sprintf("New password for %s", user.Name),
		Validate: func(value string) error { return auth.ValidateComplexity(value) },
	})
	if err != nil {
		return err
	}
	if err := auth.ValidateComplexity(password); err != nil {
		return err
	}

	hash, err := auth.Hash(password)
	if err != nil {
		return fmt.Errorf("hashing the password: %w", err)
	}
	if _, err := db.UpdateUser(ctx, pool, user.ID, user.ID, db.UserUpdate{PasswordHash: &hash}); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\nThe password for %s has been changed.\n", user.Name)
	return nil
}

// issueResetLink prints a reset link, and mails it if it can.
func issueResetLink(ctx context.Context, cmd *cobra.Command, cfg *config.Config, user model.User) error {
	signer, err := auth.NewResetSigner(cfg.Session.JWTSecret)
	if err != nil {
		return fmt.Errorf("this installation cannot issue reset links: %w", err)
	}

	base := strings.TrimRight(cfg.Server.BaseURL, "/")
	if base == "" {
		return errors.New(
			"base-url is not configured, so a reset link would not name this server.\n" +
				"Set it in the configuration file, or use --set-password instead")
	}

	token := signer.IssueReset(user.ID, time.Now())
	link := base + "/reset-password/" + token
	out := cmd.OutOrStdout()

	fmt.Fprintf(out, "A password reset link for %s, valid for %s:\n\n  %s\n\n",
		user.Name, auth.ResetLifetime, link)

	if user.Email == "" {
		fmt.Fprintf(out, "%s has no e-mail address on file, so nothing was sent.\n", user.Name)
		return nil
	}
	if !cfg.Mail.Enabled() {
		fmt.Fprintln(out, "No mail server is configured, so nothing was sent.")
		return nil
	}

	sender := mail.New(mailOptions(cfg))
	body := resetMailBody(user.DisplayName, link)
	if err := sender.Send(ctx, mail.Message{
		To:      user.Email,
		Subject: resetMailSubject,
		Body:    body,
	}); err != nil {
		// Not a failure of the command. The link above is valid whether or not
		// the message went anywhere, and an administrator who can read this can
		// send it themselves.
		fmt.Fprintf(out, "The mail could not be sent (%v).\nThe link above still works.\n", err)
		return nil
	}

	fmt.Fprintf(out, "Sent to %s.\n", user.Email)
	return nil
}

// mailOptions turns the configuration into what the mail package wants.
func mailOptions(cfg *config.Config) mail.Options {
	encryption, err := mail.ParseEncryption(cfg.Mail.Encryption)
	if err != nil {
		// Already validated at startup; an unparseable value here means the
		// safest of the three rather than a panic in a verb that is otherwise
		// about to succeed.
		encryption = mail.EncryptionSTARTTLS
	}
	return mail.Options{
		Host:       cfg.Mail.Host,
		Port:       cfg.Mail.Port,
		Username:   cfg.Mail.Username,
		Password:   cfg.Mail.Password,
		From:       cfg.Mail.From,
		Encryption: encryption,
		Timeout:    cfg.Mail.Timeout,
	}
}

const resetMailSubject = "Reset your doenerstag password"

// resetMailBody is the one message this application sends.
//
// English only, and deliberately so: the server does not know which language
// the recipient reads. The interface language is a cookie in a browser, and
// there is no browser here. Adding a language column to app_user would make
// this translatable and is the obvious next step if anybody asks.
func resetMailBody(displayName, link string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Hello %s,\n\n", displayName)
	b.WriteString("somebody asked to reset your doenerstag password.\n\n")
	b.WriteString("Open this link to choose a new one:\n\n")
	fmt.Fprintf(&b, "  %s\n\n", link)
	fmt.Fprintf(&b, "The link stops working after %s.\n\n", auth.ResetLifetime)
	b.WriteString("If you did not ask for this, you can ignore this message.\n")
	b.WriteString("Your password has not changed.\n")
	return b.String()
}
