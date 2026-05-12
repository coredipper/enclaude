package ui

import "testing"

// Passphrases must preserve every character the user actually typed, except
// for a trailing CR/LF artifact that some terminal backends may include. Any
// trimming beyond that silently mangles passphrases pasted from password
// managers (which frequently include spaces, tabs, or unicode whitespace),
// turning a correct passphrase into a "wrong passphrase?" decryption error.

func TestSanitizePassphrase_PreservesInternalSpaces(t *testing.T) {
	in := []byte("my long pass phrase")
	if got := sanitizePassphrase(in); got != "my long pass phrase" {
		t.Fatalf("got %q, want internal spaces preserved", got)
	}
}

func TestSanitizePassphrase_PreservesLeadingSpaces(t *testing.T) {
	in := []byte("   leading")
	if got := sanitizePassphrase(in); got != "   leading" {
		t.Fatalf("got %q, want leading spaces preserved", got)
	}
}

func TestSanitizePassphrase_PreservesTrailingSpaces(t *testing.T) {
	in := []byte("trailing   ")
	if got := sanitizePassphrase(in); got != "trailing   " {
		t.Fatalf("got %q, want trailing spaces preserved", got)
	}
}

func TestSanitizePassphrase_PreservesTabs(t *testing.T) {
	in := []byte("a\tb\tc")
	if got := sanitizePassphrase(in); got != "a\tb\tc" {
		t.Fatalf("got %q, want tabs preserved", got)
	}
}

func TestSanitizePassphrase_StripsTrailingNewline(t *testing.T) {
	in := []byte("pass\n")
	if got := sanitizePassphrase(in); got != "pass" {
		t.Fatalf("got %q, want %q", got, "pass")
	}
}

func TestSanitizePassphrase_StripsTrailingCRLF(t *testing.T) {
	in := []byte("pass\r\n")
	if got := sanitizePassphrase(in); got != "pass" {
		t.Fatalf("got %q, want %q", got, "pass")
	}
}

func TestSanitizePassphrase_DoesNotStripTrailingNewlineFollowedByContent(t *testing.T) {
	// Defensive: only trailing CR/LF stripped, not embedded.
	in := []byte("a\nb")
	if got := sanitizePassphrase(in); got != "a\nb" {
		t.Fatalf("got %q, want embedded newline preserved", got)
	}
}

func TestSanitizePassphrase_EmptyStaysEmpty(t *testing.T) {
	if got := sanitizePassphrase(nil); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
	if got := sanitizePassphrase([]byte{}); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestSanitizePassphrase_OnlyNewlineBecomesEmpty(t *testing.T) {
	if got := sanitizePassphrase([]byte("\n")); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
