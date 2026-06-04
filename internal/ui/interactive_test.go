package ui

import "testing"

// withPrompt swaps DefaultPrompt for the duration of a test.
func withPrompt(t *testing.T, line string, isTTY bool) {
	t.Helper()
	prev := DefaultPrompt
	t.Cleanup(func() { DefaultPrompt = prev })
	DefaultPrompt = func(string) (string, bool) { return line, isTTY }
}

// TestConfirm_NonTTYReturnsDefault verifies Confirm never blocks off a terminal:
// it returns the supplied default without consulting input.
func TestConfirm_NonTTYReturnsDefault(t *testing.T) {
	withPrompt(t, "", false)
	if !Confirm("ok?", true) {
		t.Error("non-TTY Confirm should return the true default")
	}
	if Confirm("ok?", false) {
		t.Error("non-TTY Confirm should return the false default")
	}
}

// TestConfirm_ParsesInput covers the yes/no/blank parsing on a TTY.
func TestConfirm_ParsesInput(t *testing.T) {
	cases := []struct {
		line string
		def  bool
		want bool
	}{
		{"y", false, true},
		{"yes", false, true},
		{"n", true, false},
		{"no", true, false},
		{"", true, true},   // blank keeps default
		{"", false, false}, // blank keeps default
	}
	for _, c := range cases {
		withPrompt(t, c.line, true)
		if got := Confirm("ok?", c.def); got != c.want {
			t.Errorf("Confirm(line=%q def=%v) = %v, want %v", c.line, c.def, got, c.want)
		}
	}
}

// TestChoose_NonTTYReturnsZero verifies Choose defaults to the first option off
// a terminal, and parses a 1-based selection on a TTY.
func TestChoose_NonTTYReturnsZero(t *testing.T) {
	withPrompt(t, "", false)
	if got := Choose("pick", []string{"a", "b", "c"}); got != 0 {
		t.Errorf("non-TTY Choose = %d, want 0", got)
	}
	withPrompt(t, "2", true)
	if got := Choose("pick", []string{"a", "b", "c"}); got != 1 {
		t.Errorf("Choose(\"2\") = %d, want 1 (0-based)", got)
	}
	withPrompt(t, "9", true) // out of range falls back to 0
	if got := Choose("pick", []string{"a", "b"}); got != 0 {
		t.Errorf("out-of-range Choose = %d, want 0", got)
	}
}

// TestEditString_KeepsCurrentOnBlank verifies EditString returns the current
// value when input is blank or off a terminal, and the typed value otherwise.
func TestEditString_KeepsCurrentOnBlank(t *testing.T) {
	withPrompt(t, "", false)
	if got := EditString("edit", "cur"); got != "cur" {
		t.Errorf("non-TTY EditString = %q, want cur", got)
	}
	withPrompt(t, "", true)
	if got := EditString("edit", "cur"); got != "cur" {
		t.Errorf("blank EditString = %q, want cur", got)
	}
	withPrompt(t, "new", true)
	if got := EditString("edit", "cur"); got != "new" {
		t.Errorf("EditString = %q, want new", got)
	}
}
