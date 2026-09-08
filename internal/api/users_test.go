package api_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/joernott/doenerstag/internal/api"
	"github.com/joernott/doenerstag/internal/auth"
	"github.com/joernott/doenerstag/internal/db"
	"github.com/joernott/doenerstag/internal/model"
)

type userResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
	Email       string `json:"email"`
}

// The public profile is exactly id, name and display name, as docs/04_api.md
// says. Anything else here would be published to every passer-by on the
// network.
func TestPublicProfileCarriesNothingPrivate(t *testing.T) {
	f := newAPIFixture(t)

	rec := f.post("/auth/register", map[string]string{
		"name": "anna", "email": "anna@example.invalid", "password": validPassword,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("registering: %s", rec.Body.String())
	}

	// Anonymously, which is what "public" means.
	profile := f.get("/users/" + f.userID("anna"))
	if profile.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", profile.Code, profile.Body.String())
	}

	body := profile.Body.String()
	if strings.Contains(body, "anna@example.invalid") {
		t.Error("the public profile carries the e-mail address")
	}
	if strings.Contains(body, "$argon2id$") {
		t.Error("the public profile carries the password hash")
	}
	for _, field := range []string{"last_login_at", "created_at"} {
		if strings.Contains(body, field) {
			t.Errorf("the public profile carries %s", field)
		}
	}

	var user userResponse
	decode(t, profile, &user)
	if user.Name != "anna" {
		t.Errorf("returned %+v", user)
	}
}

// The same, for the owner: the shape does not change with who is asking,
// because a field that appears for some callers and not others eventually
// appears for the wrong one.
func TestTheProfileShapeDoesNotChangeForTheOwner(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("bertil")

	anonymous := f.get("/users/" + f.userID("bertil"))
	owner := f.get("/users/"+f.userID("bertil"), cookies...)

	if anonymous.Body.String() != owner.Body.String() {
		t.Errorf("the profile differs by caller:\n anonymous: %s\n     owner: %s",
			anonymous.Body.String(), owner.Body.String())
	}
}

func TestUnknownUserIsNotFound(t *testing.T) {
	f := newAPIFixture(t)

	// A well-formed id that does not exist, and one that is not an id at all,
	// answer the same: from the caller's side there is no such user either way.
	for _, id := range []string{
		"018f0000-0000-7000-8000-00000000dead",
		"not-a-uuid",
	} {
		rec := f.get("/users/" + id)
		expectError(t, rec, http.StatusNotFound, api.CodeNotFound)
	}
}

// The full list is administrator-only.
func TestListUsersRequiresTheAdministrator(t *testing.T) {
	f := newAPIFixture(t)
	ordinary := f.register("carla")

	anonymous := f.get("/users")
	expectError(t, anonymous, http.StatusUnauthorized, api.CodeNotAuthenticated)

	asUser := f.get("/users", ordinary...)
	expectError(t, asUser, http.StatusForbidden, api.CodeAdminRequired)
}

func TestTheAdministratorCanListUsers(t *testing.T) {
	f := newAPIFixture(t)
	f.register("dora")
	admin := f.loginAsAdmin("chief")

	rec := f.get("/users", admin...)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Users []userResponse `json:"users"`
	}
	decode(t, rec, &body)

	names := make(map[string]bool, len(body.Users))
	for _, u := range body.Users {
		names[u.Name] = true
	}
	for _, want := range []string{"dora", "chief", "deleted"} {
		if !names[want] {
			t.Errorf("the list is missing %q", want)
		}
	}
}

func TestOwnerCanChangeTheirProfile(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("erik")

	rec := f.patch("/users/"+f.userID("erik"), map[string]any{
		"display_name": "Erik E.",
		"email":        "erik@example.invalid",
	}, cookies...)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var user userResponse
	decode(t, rec, &user)
	if user.DisplayName != "Erik E." {
		t.Errorf("display name is %q", user.DisplayName)
	}

	stored, err := db.UserByName(context.Background(), f.pool, "erik")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Email != "erik@example.invalid" {
		t.Errorf("the e-mail was not stored: %q", stored.Email)
	}
}

func TestOneUserCannotEditAnother(t *testing.T) {
	f := newAPIFixture(t)
	f.register("frieda")
	intruder := f.register("gustav")

	rec := f.patch("/users/"+f.userID("frieda"), map[string]any{
		"display_name": "not theirs to set",
	}, intruder...)
	expectError(t, rec, http.StatusForbidden, api.CodeNotOwner)

	anonymous := f.patch("/users/"+f.userID("frieda"), map[string]any{
		"display_name": "nor mine",
	})
	expectError(t, anonymous, http.StatusUnauthorized, api.CodeNotAuthenticated)
}

// Wherever the owner column is ticked, the administrator can act as well.
func TestTheAdministratorCanEditAnyone(t *testing.T) {
	f := newAPIFixture(t)
	f.register("hanna")
	admin := f.loginAsAdmin("chief")

	rec := f.patch("/users/"+f.userID("hanna"), map[string]any{
		"display_name": "Hanna H.",
	}, admin...)
	if rec.Code != http.StatusOK {
		t.Errorf("the administrator could not edit an account: %d %s",
			rec.Code, rec.Body.String())
	}
}

// Changing your own password requires the current one. This is what stops a
// borrowed unlocked browser from becoming a permanent account takeover: the
// session is enough to order lunch, and must not be enough to lock the owner
// out.
func TestChangingYourOwnPasswordNeedsTheCurrentOne(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("ida")
	path := "/users/" + f.userID("ida")

	missing := f.patch(path, map[string]any{
		"password": "New-Password7",
	}, cookies...)
	expectError(t, missing, http.StatusBadRequest, api.CodeMissingField)

	wrong := f.patch(path, map[string]any{
		"password":         "New-Password7",
		"current_password": "not the current one",
	}, cookies...)
	expectError(t, wrong, http.StatusUnauthorized, api.CodeInvalidLogin)

	// The password must not have changed on either failure.
	_, stored, err := db.PasswordHashByName(context.Background(), f.pool, "ida")
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.Verify(validPassword, stored); err != nil {
		t.Error("a refused change altered the password anyway")
	}

	ok := f.patch(path, map[string]any{
		"password":         "New-Password7",
		"current_password": validPassword,
	}, cookies...)
	if ok.Code != http.StatusOK {
		t.Fatalf("the change was refused: %s", ok.Body.String())
	}

	// And the new password works.
	if rec := f.post("/auth/login", map[string]string{
		"name": "ida", "password": "New-Password7",
	}); rec.Code != http.StatusOK {
		t.Errorf("the new password does not work: %s", rec.Body.String())
	}
}

// The administrator resetting somebody else's password does not supply the old
// one, because they do not have it. That is the recovery path.
func TestTheAdministratorResetsAPasswordWithoutTheOldOne(t *testing.T) {
	f := newAPIFixture(t)
	f.register("jonas")
	admin := f.loginAsAdmin("chief")

	rec := f.patch("/users/"+f.userID("jonas"), map[string]any{
		"password": "Reset-Password7",
	}, admin...)
	if rec.Code != http.StatusOK {
		t.Fatalf("the reset was refused: %s", rec.Body.String())
	}

	if login := f.post("/auth/login", map[string]string{
		"name": "jonas", "password": "Reset-Password7",
	}); login.Code != http.StatusOK {
		t.Errorf("the reset password does not work: %s", login.Body.String())
	}
}

// A new password is held to the same complexity rules as registration.
func TestAChangedPasswordMustStillBeStrong(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("klara")

	rec := f.patch("/users/"+f.userID("klara"), map[string]any{
		"password":         "weak",
		"current_password": validPassword,
	}, cookies...)
	expectError(t, rec, http.StatusBadRequest, api.CodePasswordTooWeak)
}

func TestRenamingOntoATakenNameIsRejected(t *testing.T) {
	f := newAPIFixture(t)
	f.register("lena")
	cookies := f.register("malte")

	rec := f.patch("/users/"+f.userID("malte"), map[string]any{
		"name": "LENA",
	}, cookies...)
	expectError(t, rec, http.StatusBadRequest, api.CodeUserNameTaken)
}

// root cannot be renamed: the account is referred to by name throughout, and
// docs/05_auth_and_permissions.md says so outright.
func TestTheAdministratorCannotBeRenamed(t *testing.T) {
	f := newAPIFixture(t)
	admin := f.loginAsAdmin(model.AdministratorName)

	rec := f.patch("/users/"+f.userID(model.AdministratorName), map[string]any{
		"name": "chief",
	}, admin...)
	expectError(t, rec, http.StatusBadRequest, api.CodeInvalidField)
}

// The placeholder owns the order items of deleted accounts. Editing it would
// corrupt the history it exists to preserve.
func TestThePlaceholderCannotBeEdited(t *testing.T) {
	f := newAPIFixture(t)
	admin := f.loginAsAdmin("chief")

	rec := f.patch("/users/"+model.DeletedUserID.String(), map[string]any{
		"display_name": "renamed",
	}, admin...)
	expectError(t, rec, http.StatusForbidden, api.CodePlaceholderReadOnly)
}

// An omitted field is left alone; an empty one is cleared.
func TestPatchDistinguishesAbsentFromEmpty(t *testing.T) {
	f := newAPIFixture(t)
	cookies := f.register("nils")
	path := "/users/" + f.userID("nils")

	if rec := f.patch(path, map[string]any{
		"display_name": "Nils N.", "email": "nils@example.invalid",
	}, cookies...); rec.Code != http.StatusOK {
		t.Fatalf("setting: %s", rec.Body.String())
	}

	// Patching only the display name must leave the address alone.
	if rec := f.patch(path, map[string]any{
		"display_name": "Nils",
	}, cookies...); rec.Code != http.StatusOK {
		t.Fatalf("updating: %s", rec.Body.String())
	}
	stored, err := db.UserByName(context.Background(), f.pool, "nils")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Email != "nils@example.invalid" {
		t.Errorf("an omitted field was changed: e-mail is now %q", stored.Email)
	}

	// An explicit empty value clears it.
	if rec := f.patch(path, map[string]any{"email": ""}, cookies...); rec.Code != http.StatusOK {
		t.Fatalf("clearing: %s", rec.Body.String())
	}
	stored, err = db.UserByName(context.Background(), f.pool, "nils")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Email != "" {
		t.Errorf("the e-mail was not cleared: %q", stored.Email)
	}
}

// updated_by records who acted, which for the administrator is not the edited
// account.
func TestPatchRecordsTheActingUser(t *testing.T) {
	f := newAPIFixture(t)
	f.register("olga")
	admin := f.loginAsAdmin("chief")

	if rec := f.patch("/users/"+f.userID("olga"), map[string]any{
		"display_name": "Olga O.",
	}, admin...); rec.Code != http.StatusOK {
		t.Fatalf("editing: %s", rec.Body.String())
	}

	var updatedBy string
	if err := f.pool.QueryRow(context.Background(),
		`SELECT updated_by::text FROM app_user WHERE lower(name) = 'olga'`).
		Scan(&updatedBy); err != nil {
		t.Fatal(err)
	}
	if updatedBy != f.userID("chief") {
		t.Errorf("updated_by is %s, want the administrator", updatedBy)
	}
}
