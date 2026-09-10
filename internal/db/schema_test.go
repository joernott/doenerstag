package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/joernott/doenerstag/internal/testdb"
)

// DeletedUserID is the placeholder that owns orphaned historical order items.
// Application code references this exact value, so a change to it is a breaking
// change and is asserted here rather than assumed.
const DeletedUserID = "00000000-0000-7000-8000-000000000000"

// Every table the data model defines. A table missing here, or present in the
// database and missing from this list, means the schema and the specification
// have drifted apart.
var expectedTables = []string{
	"additive",
	"allergen",
	"api_token",
	"app_user",
	"app_version",
	"content_page",
	"contact_type",
	"currency",
	"food_order",
	"image",
	"menu_category",
	"menu_item",
	"menu_item_additive",
	"menu_item_allergen",
	"menu_item_modification",
	"menu_category_availability",
	"menu_item_availability",
	"availability_filter",
	"menu_item_tag",
	"opening_hours",
	"order_item",
	"order_item_modification",
	"restaurant",
	"restaurant_contact",
	"session",
	"tag",
}

func TestSchemaHasExactlyTheDocumentedTables(t *testing.T) {
	pool := testdb.Migrated(t)

	present := map[string]bool{}
	for _, name := range tableNames(t, pool) {
		if name == "schema_migrations" {
			continue // golang-migrate's own bookkeeping
		}
		present[name] = true
	}

	for _, want := range expectedTables {
		if !present[want] {
			t.Errorf("table %q is missing from the schema", want)
		}
		delete(present, want)
	}
	for extra := range present {
		t.Errorf("table %q exists but is not in docs/03_data_model.md", extra)
	}
}

// docs/03_data_model.md says every table carries the same four audit columns.
// One table quietly missing them would only show up much later, as a row nobody
// can attribute.
func TestEveryTableCarriesTheAuditColumns(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()

	for _, table := range expectedTables {
		for _, column := range []string{"created_at", "created_by", "updated_at", "updated_by"} {
			var exists bool
			err := pool.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
				)`, table, column).Scan(&exists)
			if err != nil {
				t.Fatalf("checking %s.%s: %v", table, column, err)
			}
			if !exists {
				t.Errorf("%s has no %s column", table, column)
			}
		}
	}
}

// The trigger is what makes updated_at trustworthy. A table that has the column
// but no trigger would report the row's creation time forever.
func TestEveryTableHasTheUpdatedAtTrigger(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()

	for _, table := range expectedTables {
		var count int
		err := pool.QueryRow(ctx, `
			SELECT count(*)
			FROM pg_trigger t
			JOIN pg_class c ON c.oid = t.tgrelid
			JOIN pg_proc p ON p.oid = t.tgfoid
			WHERE c.relname = $1
			  AND p.proname = 'set_updated_at'
			  AND NOT t.tgisinternal`, table).Scan(&count)
		if err != nil {
			t.Fatalf("checking the trigger on %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("%s has %d set_updated_at triggers, want 1", table, count)
		}
	}
}

func TestUpdatedAtTriggerActuallyFires(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()

	var before, after string
	err := pool.QueryRow(ctx,
		`SELECT updated_at::text FROM app_user WHERE id = $1`, DeletedUserID).Scan(&before)
	if err != nil {
		t.Fatalf("reading updated_at: %v", err)
	}

	if _, err := pool.Exec(ctx,
		`UPDATE app_user SET display_name = 'changed' WHERE id = $1`, DeletedUserID); err != nil {
		t.Fatalf("updating: %v", err)
	}

	err = pool.QueryRow(ctx,
		`SELECT updated_at::text FROM app_user WHERE id = $1`, DeletedUserID).Scan(&after)
	if err != nil {
		t.Fatalf("re-reading updated_at: %v", err)
	}

	if before == after {
		t.Errorf("updated_at did not change on UPDATE: still %s", before)
	}
}

// ---------------------------------------------------------------------------
// Seed data
// ---------------------------------------------------------------------------

func TestDeletedUserPlaceholderExistsWithItsFixedID(t *testing.T) {
	pool := testdb.Migrated(t)

	var name, hash string
	var isAdmin bool
	err := pool.QueryRow(context.Background(),
		`SELECT name, password_hash, is_admin FROM app_user WHERE id = $1`,
		DeletedUserID).Scan(&name, &hash, &isAdmin)
	if err != nil {
		t.Fatalf("the deleted-user placeholder is missing: %v", err)
	}

	if name != "deleted" {
		t.Errorf("placeholder name is %q, want deleted", name)
	}
	if isAdmin {
		t.Error("the placeholder is marked as an administrator")
	}
	// Not a PHC string, so no password can verify against it.
	if hash != "*" {
		t.Errorf("placeholder password hash is %q, want an unusable value", hash)
	}
}

func TestDeletedUserPlaceholderCannotBeDeleted(t *testing.T) {
	pool := testdb.Migrated(t)

	_, err := pool.Exec(context.Background(),
		`DELETE FROM app_user WHERE id = $1`, DeletedUserID)
	if err == nil {
		t.Fatal("the placeholder was deleted; order history would lose its owner")
	}
}

func TestAdministratorCannotBeDeleted(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `
		INSERT INTO app_user (id, name, password_hash, is_admin)
		VALUES ('00000000-0000-7000-8000-0000000000ff', 'root', 'x', true)`)
	if err != nil {
		t.Fatalf("creating an administrator: %v", err)
	}

	_, err = pool.Exec(ctx,
		`DELETE FROM app_user WHERE name = 'root'`)
	if err == nil {
		t.Error("the administrator account was deleted")
	}
}

func TestSeededReferenceData(t *testing.T) {
	pool := testdb.Migrated(t)

	cases := []struct {
		table string
		count int
		codes []string
	}{
		{
			table: "allergen", count: 14,
			codes: []string{
				"gluten", "crustaceans", "eggs", "fish", "peanuts", "soy", "milk",
				"nuts", "celery", "mustard", "sesame", "sulphites", "lupin", "molluscs",
			},
		},
		{
			table: "additive", count: 14,
			codes: []string{
				"colouring", "preservative", "antioxidant", "flavour_enh", "sulphured",
				"blackened", "waxed", "phosphate", "sweetener", "phenylalanine",
				"caffeine", "quinine", "taurine", "gmo",
			},
		},
		{
			table: "contact_type", count: 7,
			codes: []string{"phone", "mobile", "fax", "email", "website", "address", "other"},
		},
		{
			table: "tag", count: 10,
			codes: []string{
				"vegan", "vegetarian", "spicy", "very_spicy", "halal", "kosher",
				"gluten_free", "lactose_free", "new", "signature",
			},
		},
	}

	ctx := context.Background()
	for _, tc := range cases {
		var count int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+tc.table).Scan(&count); err != nil {
			t.Fatalf("counting %s: %v", tc.table, err)
		}
		if count != tc.count {
			t.Errorf("%s has %d rows, want %d", tc.table, count, tc.count)
		}

		// Codes are asserted individually: they are the only identifier these
		// rows carry now that the names live in the i18n catalogs, so renaming
		// one silently breaks its translation.
		for _, code := range tc.codes {
			var exists bool
			err := pool.QueryRow(ctx,
				"SELECT EXISTS (SELECT 1 FROM "+tc.table+" WHERE code = $1)", code).Scan(&exists)
			if err != nil {
				t.Fatalf("checking %s code %q: %v", tc.table, code, err)
			}
			if !exists {
				t.Errorf("%s is missing the code %q", tc.table, code)
			}
		}
	}
}

// The Annex II numbers are part of the legal declaration, not decoration.
func TestAllergensCarryTheirAnnexIINumbers(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()

	expected := map[string]string{
		"gluten": "1", "crustaceans": "2", "eggs": "3", "fish": "4",
		"peanuts": "5", "soy": "6", "milk": "7", "nuts": "8",
		"celery": "9", "mustard": "10", "sesame": "11", "sulphites": "12",
		"lupin": "13", "molluscs": "14",
	}

	for code, want := range expected {
		var reference string
		var sortOrder int
		err := pool.QueryRow(ctx,
			"SELECT reference, sort_order FROM allergen WHERE code = $1", code).
			Scan(&reference, &sortOrder)
		if err != nil {
			t.Errorf("reading allergen %q: %v", code, err)
			continue
		}
		if reference != want {
			t.Errorf("allergen %q has Annex II number %q, want %q", code, reference, want)
		}
		if got := itoa(sortOrder); got != want {
			t.Errorf("allergen %q sorts at %s, want %s", code, got, want)
		}
	}
}

func TestSeededCurrencies(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()

	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM currency").Scan(&count); err != nil {
		t.Fatalf("counting currencies: %v", err)
	}
	if count != 12 {
		t.Errorf("currency has %d rows, want 12", count)
	}

	// EUR is the default offered when creating a restaurant, and the minor unit
	// decides how every amount in that currency is formatted.
	var symbol string
	var minorUnit int
	err := pool.QueryRow(ctx,
		"SELECT symbol, minor_unit FROM currency WHERE code = 'EUR'").Scan(&symbol, &minorUnit)
	if err != nil {
		t.Fatalf("reading EUR: %v", err)
	}
	if symbol != "€" {
		t.Errorf("EUR symbol is %q, want €", symbol)
	}
	if minorUnit != 2 {
		t.Errorf("EUR minor unit is %d, want 2", minorUnit)
	}
}

// No seeded reference table stores a display name any more; the frontend
// translates the code. A name column reappearing would quietly reintroduce the
// migration-per-language problem that removing them solved.
func TestSeededReferenceTablesStoreNoNames(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()

	for _, table := range []string{"currency", "contact_type", "allergen", "additive"} {
		for _, column := range []string{"name", "name_en", "name_de"} {
			var exists bool
			err := pool.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = $1 AND column_name = $2
				)`, table, column).Scan(&exists)
			if err != nil {
				t.Fatalf("checking %s.%s: %v", table, column, err)
			}
			if exists {
				t.Errorf("%s has a %s column; names belong in the i18n catalogs", table, column)
			}
		}
	}

	// tag is the exception: a user-invented tag has no catalog entry.
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'tag' AND column_name = 'name'
		)`).Scan(&exists)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Error("tag has no name column, so user-created tags could not be labelled")
	}
}

func TestContentPagesArePresent(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()

	for _, key := range []string{"imprint", "legal_notes"} {
		var html string
		err := pool.QueryRow(ctx,
			"SELECT html FROM content_page WHERE key = $1", key).Scan(&html)
		if err != nil {
			t.Errorf("content page %q is missing: %v", key, err)
			continue
		}
		if html == "" {
			t.Errorf("content page %q is empty", key)
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}

// execErr runs a statement and returns its error, for the constraint tests.
func execErr(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) error {
	t.Helper()
	_, err := pool.Exec(context.Background(), sql, args...)
	return err
}

// The two currencies that do not divide by ten.
//
// The Malagasy ariary is five iraimbilanja and the Mauritanian ouguiya is five
// khoums, and they are the reason minor_per_major is a column rather than
// 10^minor_unit worked out at the point of use. Every other seeded currency
// must still satisfy the relation that used to be assumed, or migration 10's
// backfill did something other than preserve what was there.
func TestTheNonDecimalCurrencies(t *testing.T) {
	pool := testdb.Migrated(t)
	ctx := context.Background()

	rows, err := pool.Query(ctx,
		"SELECT code, minor_unit, minor_per_major FROM currency ORDER BY code")
	if err != nil {
		t.Fatalf("reading currencies: %v", err)
	}
	defer rows.Close()

	seen := map[string]int{}
	for rows.Next() {
		var code string
		var minorUnit, perMajor int
		if err := rows.Scan(&code, &minorUnit, &perMajor); err != nil {
			t.Fatal(err)
		}
		seen[code] = perMajor

		if code == "MGA" || code == "MRU" {
			if perMajor != 5 {
				t.Errorf("%s divides into %d, want 5", code, perMajor)
			}
			if minorUnit != 1 {
				t.Errorf("%s is written with %d places, want 1", code, minorUnit)
			}
			continue
		}

		want := 1
		for range minorUnit {
			want *= 10
		}
		if perMajor != want {
			t.Errorf("%s divides into %d with %d places, want %d",
				code, perMajor, minorUnit, want)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}

	for _, code := range []string{"MGA", "MRU", "EUR"} {
		if _, ok := seen[code]; !ok {
			t.Errorf("%s is not seeded", code)
		}
	}
}
