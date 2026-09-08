package transfer_test

import (
	"strings"
	"testing"

	"github.com/joernott/doenerstag/internal/transfer"
)

// The format is decided by the content, because a suffix is a claim somebody
// made about a file and the two disagree often enough to matter.
func TestTheFormatIsDecidedByTheContent(t *testing.T) {
	cases := map[string]struct {
		content string
		want    transfer.Format
	}{
		"a plain object":              {`{"version": 1}`, transfer.FormatJSON},
		"an object after blank lines": {"\n\n  {\"version\": 1}", transfer.FormatJSON},
		"a mapping":                   {"version: 1\nrestaurants: []", transfer.FormatYAML},
		"a mapping after a comment":   {"# written by export\nversion: 1", transfer.FormatYAML},
		"a document marker":           {"---\nversion: 1", transfer.FormatYAML},
	}

	for name, c := range cases {
		got, err := transfer.DetectFormat([]byte(c.content))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: detected %q, want %q", name, got, c.want)
		}
	}

	if _, err := transfer.DetectFormat([]byte("   \n\n  ")); err == nil {
		t.Error("an empty file was given a format")
	}
}

// A round trip through each format has to give back what went in. This is the
// property the whole feature rests on: an export that loses a field is a menu
// somebody has to retype.
func TestADocumentSurvivesBothFormats(t *testing.T) {
	minimum := int64(2000)
	original := &transfer.Document{
		Version: transfer.Version,
		Restaurants: []transfer.Restaurant{{
			ID:                 "018f0000-0000-7000-8000-000000000001",
			Name:               "Döner Palast",
			Currency:           "EUR",
			MinOrderValueCents: &minimum,
			Notes:              "Ruft vor 11 Uhr an.",
			Contacts: []transfer.Contact{
				{Type: "phone", Value: "+49 30 123456", Label: "Theke", SortOrder: 1},
				{Type: "address", Value: "Hauptstr. 1", SortOrder: 2},
			},
			OpeningHours: []transfer.OpeningHours{
				{Day: 5, Start: "22:00", End: "02:00"},
			},
			Categories: []transfer.Category{
				{ID: "018f0000-0000-7000-8000-000000000002", Name: "Vom Grill", SortOrder: 1},
			},
			Items: []transfer.Item{{
				ID:          "018f0000-0000-7000-8000-000000000003",
				Category:    "018f0000-0000-7000-8000-000000000002",
				ExternalID:  "A3",
				Name:        "Döner Kebab",
				Description: "Mit allem",
				PriceCents:  550,
				Available:   true,
				Tags:        []string{"spicy"},
				Allergens:   []string{"A"},
				Additives:   []string{"1"},
				Modifications: []transfer.Modification{
					{Name: "Extra Käse", PriceDeltaCents: 50, SortOrder: 1},
				},
			}},
		}},
	}

	for _, format := range []transfer.Format{transfer.FormatYAML, transfer.FormatJSON} {
		encoded, err := transfer.Encode(original, format)
		if err != nil {
			t.Fatalf("%s: encoding: %v", format, err)
		}

		// The format it was written in is the format that is detected.
		detected, err := transfer.DetectFormat(encoded)
		if err != nil {
			t.Fatalf("%s: detecting: %v", format, err)
		}
		if detected != format {
			t.Errorf("%s was detected as %s", format, detected)
		}

		back, err := transfer.Decode(encoded, detected)
		if err != nil {
			t.Fatalf("%s: decoding: %v", format, err)
		}

		got := back.Restaurants[0]
		want := original.Restaurants[0]

		if got.Name != want.Name || got.Currency != want.Currency || got.Notes != want.Notes {
			t.Errorf("%s: the restaurant changed: %+v", format, got)
		}
		if got.MinOrderValueCents == nil || *got.MinOrderValueCents != minimum {
			t.Errorf("%s: the minimum order value did not survive", format)
		}
		if len(got.Contacts) != 2 || got.Contacts[0].Label != "Theke" {
			t.Errorf("%s: the contacts did not survive: %+v", format, got.Contacts)
		}
		if len(got.OpeningHours) != 1 || got.OpeningHours[0].End != "02:00" {
			t.Errorf("%s: the opening hours did not survive: %+v", format, got.OpeningHours)
		}
		if len(got.Items) != 1 {
			t.Fatalf("%s: the menu did not survive", format)
		}
		item := got.Items[0]
		if item.PriceCents != 550 || item.ExternalID != "A3" || !item.Available {
			t.Errorf("%s: the item changed: %+v", format, item)
		}
		if len(item.Tags) != 1 || item.Tags[0] != "spicy" {
			t.Errorf("%s: the tags did not survive: %v", format, item.Tags)
		}
		if len(item.Modifications) != 1 || item.Modifications[0].PriceDeltaCents != 50 {
			t.Errorf("%s: the modifications did not survive: %+v", format, item.Modifications)
		}
	}
}

// A time like 02:00 must not come back as a number of seconds, and 11:00 must
// not become 660. YAML's sexagesimal parsing has eaten times in other projects,
// and the quoting the encoder produces is what prevents it here.
func TestOpeningTimesSurviveAsStrings(t *testing.T) {
	doc := &transfer.Document{
		Version: transfer.Version,
		Restaurants: []transfer.Restaurant{{
			ID: "018f0000-0000-7000-8000-000000000001", Name: "x", Currency: "EUR",
			OpeningHours: []transfer.OpeningHours{{Day: 1, Start: "11:00", End: "22:30"}},
		}},
	}

	encoded, err := transfer.Encode(doc, transfer.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"11:00"`) {
		t.Errorf("the start time was not quoted, which risks it being read as a number:\n%s", encoded)
	}

	back, err := transfer.Decode(encoded, transfer.FormatYAML)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Restaurants[0].OpeningHours[0]; got.Start != "11:00" || got.End != "22:30" {
		t.Errorf("the times came back as %q and %q", got.Start, got.End)
	}
}

// A mistyped field is refused rather than imported as an empty value: the
// restaurant would arrive looking wrong with nothing to say why.
func TestAMistypedFieldIsRefused(t *testing.T) {
	for _, c := range []struct {
		format  transfer.Format
		content string
	}{
		{transfer.FormatYAML, "version: 1\nrestaurants:\n  - id: x\n    nme: Typo\n"},
		{transfer.FormatJSON, `{"version":1,"restaurants":[{"id":"x","nme":"Typo"}]}`},
	} {
		if _, err := transfer.Decode([]byte(c.content), c.format); err == nil {
			t.Errorf("%s: a mistyped field was accepted", c.format)
		}
	}
}

func TestADocumentWithoutAVersionIsRefused(t *testing.T) {
	_, err := transfer.Decode([]byte("restaurants:\n  - id: x\n"), transfer.FormatYAML)
	if err == nil {
		t.Fatal("a file with no version was accepted")
	}
	if !strings.Contains(err.Error(), "version") {
		t.Errorf("the error does not mention the version: %v", err)
	}
}

// A file from a future version is refused with a sentence rather than a type
// error, so that somebody meeting it knows to fetch a newer doenerstag.
func TestANewerDocumentIsRefusedClearly(t *testing.T) {
	content := "version: 99\nrestaurants:\n  - id: x\n    name: y\n    currency: EUR\n"

	_, err := transfer.Decode([]byte(content), transfer.FormatYAML)
	if err == nil {
		t.Fatal("a newer document was accepted")
	}
	if !strings.Contains(err.Error(), "newer") {
		t.Errorf("the error does not say what to do: %v", err)
	}
}

func TestParseFormat(t *testing.T) {
	// A slice rather than a map, because one of the cases is a value with
	// surrounding whitespace -- which is the case worth having, and which a map
	// literal makes look like a mistake.
	cases := []struct {
		input string
		want  transfer.Format
	}{
		{"yaml", transfer.FormatYAML},
		{"YAML", transfer.FormatYAML},
		{"yml", transfer.FormatYAML},
		{"json", transfer.FormatJSON},
		{"  json  ", transfer.FormatJSON},
	}

	for _, c := range cases {
		input, want := c.input, c.want
		got, err := transfer.ParseFormat(input)
		if err != nil {
			t.Errorf("%q: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("%q became %q, want %q", input, got, want)
		}
	}

	if _, err := transfer.ParseFormat("xml"); err == nil {
		t.Error("an unknown format was accepted")
	}
}
