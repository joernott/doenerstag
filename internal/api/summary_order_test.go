package api

import "testing"

// F4.6 as a rule rather than as an example.
//
// The end-to-end test in summary_test.go proves the common case, 1 before 2 before 10.
// The rule has corners the fixture cannot reach cheaply: an item that has left
// the menu and so has no number at all, a restaurant whose item numbers are not
// numbers, and two items sharing a number. Getting one of those backwards puts
// a line in the wrong place on a sheet somebody reads down a telephone.
func TestMenuOrderRule(t *testing.T) {
	line := func(externalID, name string) aggregatedLine {
		return aggregatedLine{ExternalID: externalID, ItemName: name}
	}

	cases := []struct {
		name  string
		a, b  aggregatedLine
		first bool // whether a comes before b
	}{
		{"numbers compare numerically", line("2", "b"), line("10", "a"), true},
		{"the larger number comes second", line("10", "a"), line("2", "b"), false},
		{"a numbered item precedes an unnumbered one",
			line("1", "z"), line("", "a"), true},
		{"an unnumbered item follows a numbered one",
			line("", "a"), line("1", "z"), false},
		{"a numeric item number precedes a non-numeric one",
			line("7", "z"), line("A3", "a"), true},
		{"a non-numeric item number follows a numeric one",
			line("A3", "a"), line("7", "z"), false},
		{"two non-numeric numbers compare as text",
			line("A3", "z"), line("B1", "a"), true},
		{"the same number falls back to the name",
			line("5", "Ayran"), line("5", "Börek"), true},
		{"the name comparison ignores case",
			line("5", "ayran"), line("5", "Börek"), true},
		{"two unnumbered items compare by name",
			line("", "Ayran"), line("", "Börek"), true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := lessByMenuOrder(c.a, c.b); got != c.first {
				t.Errorf("lessByMenuOrder(%q/%q, %q/%q) = %v, want %v",
					c.a.ExternalID, c.a.ItemName,
					c.b.ExternalID, c.b.ItemName, got, c.first)
			}
		})
	}
}

// An item number is only a number when all of it is one. "12a" is a label the
// restaurant chose, and reading it as twelve would sort it among the numbers
// and lose the a.
func TestOnlyWholeNumbersAreItemNumbers(t *testing.T) {
	cases := map[string]struct {
		value   int64
		numeric bool
	}{
		"":    {0, false},
		"0":   {0, true},
		"7":   {7, true},
		"10":  {10, true},
		"012": {12, true},
		"12a": {0, false},
		"a12": {0, false},
		"1 2": {0, false},
		"-1":  {0, false},
		"1.5": {0, false},
		"١٢":  {0, false}, // Arabic-Indic digits are digits to a human, not here
	}

	for input, want := range cases {
		value, numeric := numericID(input)
		if numeric != want.numeric || value != want.value {
			t.Errorf("numericID(%q) = (%d, %v), want (%d, %v)",
				input, value, numeric, want.value, want.numeric)
		}
	}
}
