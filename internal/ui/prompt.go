package ui

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// ReadPassphrase reads a passphrase from the controlling terminal without
// echo. When confirm is true it prompts a second time and verifies the two
// inputs match (used when setting a new passphrase).
func ReadPassphrase(prompt string, confirm bool) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("cannot prompt for passphrase: stdin is not a terminal")
	}

	fmt.Fprint(os.Stderr, prompt)
	pw, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("reading passphrase: %w", err)
	}
	pp := strings.TrimSpace(string(pw))
	if pp == "" {
		return "", fmt.Errorf("empty passphrase")
	}

	if confirm {
		fmt.Fprint(os.Stderr, "Confirm passphrase: ")
		pw2, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("reading confirmation: %w", err)
		}
		if strings.TrimSpace(string(pw2)) != pp {
			return "", fmt.Errorf("passphrases do not match")
		}
	}
	return pp, nil
}

// ReadPassphraseOptional reads a passphrase without echo. Empty input is
// returned as "" without error (caller treats as "skip"). Use for optional
// backup-passphrase prompts.
func ReadPassphraseOptional(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("cannot prompt for passphrase: stdin is not a terminal")
	}
	fmt.Fprint(os.Stderr, prompt)
	pw, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return "", fmt.Errorf("reading passphrase: %w", err)
	}
	return strings.TrimSpace(string(pw)), nil
}
