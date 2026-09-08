package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
	"github.com/joernott/doenerstag/internal/testdb"
)

// newUser inserts an account with a valid hash and a name derived from the
// test, so that two tests sharing a database cannot collide.
func newUser(t *testing.T, pool *pgxpool.Pool, name string) model.User {
	t.Helper()

	hash, err := auth.HashWithoutValidation("test-password")
	if err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateUser(context.Background(), pool, db.NewUser{
		Name:         name,
		PasswordHash: hash,
	})
	if err != nil {
		t.Fatalf("creating user %q: %v", name, err)
	}
	return u
}

func TestCreateAndReadUser(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()

	hash, err := auth.HashWithoutValidation("test-password")
	if err != nil {
		t.Fatal(err)
	}

	created, err := db.CreateUser(ctx, pool, db.NewUser{
		Name:         "Anna",
		DisplayName:  "Anna B.",
		Email:        "anna@example.invalid",
		PasswordHash: hash,
	})
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	if created.ID.Version() != 7 {
		t.Errorf("the id is UUIDv%d, want v7", created.ID.Version())
	}
	if created.IsAdmin {
		t.Error("a registered account was created as an administrator")
	}

	byID, err := db.UserByID(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("reading by id: %v", err)
	}
	if byID.Name != "Anna" || byID.DisplayName != "Anna B." {
		t.Errorf("read back %+v", byID)
	}

	// "Anna" logs in as "anna": the lookup matches the unique index, which is
	// on lower(name).
	byName, err := db.UserByName(ctx, pool, "ANNA")
	if err != nil {
		t.Fatalf("reading by name: %v", err)
	}
	if byName.ID != created.ID {
		t.Error("a differently-cased name found a different account")
	}
}

// Registration must reject a name that differs only in case, because "Anna"
// and "anna" are one person registering twice.
func TestUserNamesAreUniqueRegardlessOfCase(t *testing.T) {
	pool := testdb.Migrated(t)
	newUser(t, pool, "Berta")

	hash, err := auth.HashWithoutValidation("test-password")
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CreateUser(context.Background(), pool, db.NewUser{
		Name:         "BERTA",
		PasswordHash: hash,
	})
	if !errors.Is(err, db.ErrNameTaken) {
		t.Errorf("got %v, want ErrNameTaken", err)
	}
}

func TestMissingUserIsNotFound(t *testing.T) {
	pool := testdb.Migrated(t)

	if _, err := db.UserByName(context.Background(), pool, "nobody"); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

// The optional fields must round-trip as empty strings rather than surfacing
// SQL NULL, so that no handler has to think about three-valued logic.
func TestOptionalFieldsComeBackEmptyNotNull(t *testing.T) {
	pool := testdb.Migrated(t)
	u := newUser(t, pool, "Clara")

	if u.DisplayName != "" || u.Email != "" {
		t.Errorf("unset optional fields came back as %q/%q", u.DisplayName, u.Email)
	}
	if u.Label() != "Clara" {
		t.Errorf("Label is %q, want the user name as the fallback", u.Label())
	}
}

func TestPasswordHashByNameReturnsTheStoredHash(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()

	hash, err := auth.Hash("Correct-Horse9")
	if err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateUser(ctx, pool, db.NewUser{
		Name: "Dora", PasswordHash: hash,
	})
	if err != nil {
		t.Fatal(err)
	}

	user, stored, err := db.PasswordHashByName(ctx, pool, "dora")
	if err != nil {
		t.Fatalf("reading the hash: %v", err)
	}
	if user.ID != created.ID {
		t.Error("the hash came back with the wrong account")
	}
	if err := auth.Verify("Correct-Horse9", stored); err != nil {
		t.Errorf("the stored hash does not verify the password: %v", err)
	}
}

// A PATCH sends only what it changes, so an absent field and an empty one must
// mean different things.
func TestUpdateChangesOnlyTheFieldsThatAreSet(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()

	hash, err := auth.HashWithoutValidation("test-password")
	if err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateUser(ctx, pool, db.NewUser{
		Name: "Emil", DisplayName: "Emil E.", Email: "emil@example.invalid",
		PasswordHash: hash,
	})
	if err != nil {
		t.Fatal(err)
	}

	display := "Emilia"
	updated, err := db.UpdateUser(ctx, pool, u.ID, u.ID, db.UserUpdate{DisplayName: &display})
	if err != nil {
		t.Fatalf("updating: %v", err)
	}
	if updated.DisplayName != "Emilia" {
		t.Errorf("display name is %q, want Emilia", updated.DisplayName)
	}
	if updated.Email != "emil@example.invalid" {
		t.Errorf("an omitted field was changed: email is now %q", updated.Email)
	}
	if updated.Name != "Emil" {
		t.Errorf("an omitted field was changed: name is now %q", updated.Name)
	}

	// An explicit empty value clears the column, which is how a user removes
	// their display name.
	empty := ""
	cleared, err := db.UpdateUser(ctx, pool, u.ID, u.ID, db.UserUpdate{DisplayName: &empty})
	if err != nil {
		t.Fatalf("clearing: %v", err)
	}
	if cleared.DisplayName != "" {
		t.Errorf("display name is %q after being cleared", cleared.DisplayName)
	}
	if cleared.Label() != "Emil" {
		t.Errorf("Label is %q, want the fallback to the user name", cleared.Label())
	}
}

func TestUpdateWithNothingSetIsNotAnError(t *testing.T) {
	pool := testdb.Migrated(t)
	u := newUser(t, pool, "Frida")

	got, err := db.UpdateUser(context.Background(), pool, u.ID, u.ID, db.UserUpdate{})
	if err != nil {
		t.Fatalf("an empty patch failed: %v", err)
	}
	if got.ID != u.ID || got.Name != u.Name {
		t.Error("an empty patch changed something")
	}
}

// Renaming onto somebody else's name is the same collision as registering with
// it, and must be reported the same way.
func TestRenamingOntoATakenNameIsRejected(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	newUser(t, pool, "Gustav")
	other := newUser(t, pool, "Heidi")

	taken := "gustav"
	_, err := db.UpdateUser(ctx, pool, other.ID, other.ID, db.UserUpdate{Name: &taken})
	if !errors.Is(err, db.ErrNameTaken) {
		t.Errorf("got %v, want ErrNameTaken", err)
	}
}

// updated_by records who acted, which for an administrator editing somebody
// else is not the edited account.
func TestUpdateRecordsTheActingUser(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	subject := newUser(t, pool, "Ida")
	actor := newUser(t, pool, "Jonas")

	display := "Ida I."
	if _, err := db.UpdateUser(ctx, pool, subject.ID, actor.ID,
		db.UserUpdate{DisplayName: &display}); err != nil {
		t.Fatal(err)
	}

	var updatedBy string
	if err := pool.QueryRow(ctx,
		`SELECT updated_by::text FROM app_user WHERE id = $1`, subject.ID).
		Scan(&updatedBy); err != nil {
		t.Fatal(err)
	}
	if updatedBy != actor.ID.String() {
		t.Errorf("updated_by is %s, want the acting user %s", updatedBy, actor.ID)
	}
}

func TestRecordLoginStampsTheTime(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	u := newUser(t, pool, "Klara")

	if u.LastLoginAt != nil {
		t.Error("a new account already has a last login")
	}

	at := time.Now().UTC().Truncate(time.Second)
	if err := db.RecordLogin(ctx, pool, u.ID, at); err != nil {
		t.Fatal(err)
	}

	reread, err := db.UserByID(ctx, pool, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reread.LastLoginAt == nil || !reread.LastLoginAt.Equal(at) {
		t.Errorf("last login is %v, want %v", reread.LastLoginAt, at)
	}
}

func TestListUsersIncludesEveryAccount(t *testing.T) {
	pool := testdb.Migrated(t)
	newUser(t, pool, "Lena")
	newUser(t, pool, "Malte")

	users, err := db.ListUsers(context.Background(), pool)
	if err != nil {
		t.Fatal(err)
	}

	names := make(map[string]bool, len(users))
	for _, u := range users {
		names[u.Name] = true
	}
	for _, want := range []string{"Lena", "Malte", "deleted"} {
		if !names[want] {
			t.Errorf("the list is missing %q", want)
		}
	}
}

// The two protected rows are protected by a trigger, so no code path can
// remove them -- including a query written by hand at three in the morning.
func TestProtectedAccountsCannotBeDeleted(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()

	if err := db.DeleteUser(ctx, pool, model.DeletedUserID); err == nil {
		t.Error("the deleted-user placeholder was deleted")
	}

	admin, err := db.UserByName(ctx, pool, model.AdministratorName)
	if errors.Is(err, db.ErrNotFound) {
		t.Skip("this database has no administrator; install creates it")
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteUser(ctx, pool, admin.ID); err == nil {
		t.Error("the administrator was deleted")
	}
}

func TestDeleteUserRemovesTheAccount(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()
	u := newUser(t, pool, "Nils")

	if err := db.DeleteUser(ctx, pool, u.ID); err != nil {
		t.Fatalf("deleting: %v", err)
	}
	if _, err := db.UserByID(ctx, pool, u.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("the account is still readable: %v", err)
	}
	// Deleting it again reports not found rather than succeeding silently.
	if err := db.DeleteUser(ctx, pool, u.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("deleting twice gave %v, want ErrNotFound", err)
	}
}
