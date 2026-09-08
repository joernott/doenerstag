package db

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/joernott/doenerstag/internal/model"
)

// ErrNotFound is returned when a row does not exist.
//
// One sentinel for the whole package rather than one per table: every caller
// turns it into the same 4000, and distinguishing "no such user" from "no such
// order" at this layer would only invite an error message that tells an
// anonymous caller which identifiers are real.
var ErrNotFound = errors.New("db: not found")

// ErrNameTaken is returned when a unique name constraint rejects a write.
var ErrNameTaken = errors.New("db: name already taken")

// userColumns is the projection every user query returns, so that scanUser can
// be shared. Written once rather than repeated in each query, because a column
// added to one list and not the other is a scan error at runtime.
const userColumns = `id, name, coalesce(display_name, ''), coalesce(email, ''),
	is_admin, last_login_at, created_at, updated_at`

func scanUser(row pgx.Row) (model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.Name, &u.DisplayName, &u.Email,
		&u.IsAdmin, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.User{}, ErrNotFound
		}
		return model.User{}, err
	}
	return u, nil
}

// Querier is the subset of the pgx pool the queries here need.
//
// Taking an interface rather than *pgxpool.Pool lets the same function run
// inside a transaction, which the account deletion in F2.6 needs: its remap and
// its delete have to succeed or fail together.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// NewUser is the set of values registration supplies.
type NewUser struct {
	Name         string
	DisplayName  string
	Email        string
	PasswordHash string
}

// CreateUser inserts an account and returns it.
//
// The caller is responsible for having validated the password and hashed it:
// this layer never sees a plaintext password, which is what keeps it out of a
// query log.
func CreateUser(ctx context.Context, q Querier, in NewUser) (model.User, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return model.User{}, fmt.Errorf("generating a user id: %w", err)
	}

	row := q.QueryRow(ctx, `
		INSERT INTO app_user (id, name, display_name, email, password_hash, created_by, updated_by)
		VALUES ($1, $2, nullif($3, ''), nullif($4, ''), $5, $1, $1)
		RETURNING `+userColumns,
		id, in.Name, in.DisplayName, in.Email, in.PasswordHash)

	u, err := scanUser(row)
	if isUniqueViolation(err) {
		// The unique index is on lower(name), so this is the only unique
		// constraint an insert here can breach.
		return model.User{}, ErrNameTaken
	}
	return u, err
}

// UserByID reads one account.
func UserByID(ctx context.Context, q Querier, id uuid.UUID) (model.User, error) {
	return scanUser(q.QueryRow(ctx,
		`SELECT `+userColumns+` FROM app_user WHERE id = $1`, id))
}

// UserByName reads one account by login name.
//
// The comparison is on lower(name), matching the unique index, so that "Anna"
// logs in as "anna".
func UserByName(ctx context.Context, q Querier, name string) (model.User, error) {
	return scanUser(q.QueryRow(ctx,
		`SELECT `+userColumns+` FROM app_user WHERE lower(name) = lower($1)`, name))
}

// PasswordHashByName reads the stored hash for a login attempt.
//
// It returns the user as well, so a successful verification does not need a
// second query. The hash is a separate return value rather than a field on
// model.User: a hash on the domain type would eventually be rendered into a
// response by accident, and the type system is a better guard against that
// than a comment would be.
func PasswordHashByName(ctx context.Context, q Querier, name string) (model.User, string, error) {
	var (
		u    model.User
		hash string
	)
	err := q.QueryRow(ctx,
		`SELECT `+userColumns+`, password_hash FROM app_user WHERE lower(name) = lower($1)`,
		name).
		Scan(&u.ID, &u.Name, &u.DisplayName, &u.Email, &u.IsAdmin,
			&u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt, &hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return model.User{}, "", ErrNotFound
		}
		return model.User{}, "", err
	}
	return u, hash, nil
}

// PasswordHashByID is PasswordHashByName for a known account, used when
// changing a password requires confirming the current one.
func PasswordHashByID(ctx context.Context, q Querier, id uuid.UUID) (string, error) {
	var hash string
	err := q.QueryRow(ctx,
		`SELECT password_hash FROM app_user WHERE id = $1`, id).Scan(&hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return hash, nil
}

// UserUpdate carries the fields PATCH /users/{id} may change.
//
// Every field is a pointer so that "absent" and "set to empty" are different
// requests: sending an empty display name clears it, and omitting the field
// leaves it alone.
type UserUpdate struct {
	Name         *string
	DisplayName  *string
	Email        *string
	PasswordHash *string
}

// UpdateUser applies the fields that are set and returns the updated account.
//
// actor is recorded in updated_by. It is the acting user, which for an
// administrator editing somebody else is not the row's own id.
func UpdateUser(ctx context.Context, q Querier, id, actor uuid.UUID, in UserUpdate) (model.User, error) {
	// The statement is built rather than written out, because the alternative
	// -- one UPDATE per combination of fields -- is sixteen statements for
	// four columns.
	var (
		sets []string
		args []any
	)
	add := func(clause string, value any) {
		args = append(args, value)
		sets = append(sets, fmt.Sprintf(clause, len(args)))
	}

	if in.Name != nil {
		add("name = $%d", *in.Name)
	}
	if in.DisplayName != nil {
		add("display_name = nullif($%d, '')", *in.DisplayName)
	}
	if in.Email != nil {
		add("email = nullif($%d, '')", *in.Email)
	}
	if in.PasswordHash != nil {
		add("password_hash = $%d", *in.PasswordHash)
	}
	if len(sets) == 0 {
		// A PATCH with nothing in it is not an error; it simply has no effect.
		return UserByID(ctx, q, id)
	}

	add("updated_by = $%d", actor)
	args = append(args, id)

	row := q.QueryRow(ctx,
		`UPDATE app_user SET `+strings.Join(sets, ", ")+
			fmt.Sprintf(" WHERE id = $%d RETURNING ", len(args))+userColumns,
		args...)

	u, err := scanUser(row)
	if isUniqueViolation(err) {
		return model.User{}, ErrNameTaken
	}
	return u, err
}

// RecordLogin stamps last_login_at.
func RecordLogin(ctx context.Context, q Querier, id uuid.UUID, at time.Time) error {
	_, err := q.Exec(ctx,
		`UPDATE app_user SET last_login_at = $2 WHERE id = $1`, id, at)
	return err
}

// ListUsers returns every account, newest first. Administrator only.
func ListUsers(ctx context.Context, q Querier) ([]model.User, error) {
	rows, err := q.Query(ctx,
		`SELECT `+userColumns+` FROM app_user ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	users := make([]model.User, 0, 16)
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// DeleteUser removes an account.
//
// The database refuses to delete the administrator or the placeholder, through
// the trigger in migration 000002, so this does not repeat the check: a caller
// that tries gets the constraint error rather than a silent success.
func DeleteUser(ctx context.Context, q Querier, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `DELETE FROM app_user WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// isUniqueViolation reports whether err is PostgreSQL's 23505.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// isForeignKeyViolation reports whether err is PostgreSQL's 23503.
//
// It means a referenced row does not exist -- an unknown currency code, or an
// image id that was never uploaded -- which the API answers as 4000 rather than
// as a server error.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// IsForeignKeyViolation reports whether err is PostgreSQL's 23503.
//
// Exported for the api package, which turns a reference to a row that does not
// exist into a 1002 naming the field rather than a server error.
func IsForeignKeyViolation(err error) bool { return isForeignKeyViolation(err) }

// UserByEmail reads one account by its e-mail address.
//
// Case-insensitive, because an address is: somebody who registered as
// Anna@example.com and asks for a reset as anna@example.com is the same person,
// and telling them otherwise would be telling them their account does not
// exist.
func UserByEmail(ctx context.Context, q Querier, email string) (model.User, error) {
	return scanUser(q.QueryRow(ctx,
		`SELECT `+userColumns+` FROM app_user WHERE lower(email) = lower($1)`, email))
}
