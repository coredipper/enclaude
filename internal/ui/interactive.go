package ui

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"
)

// PromptFunc writes a prompt to stderr and reads one line of input. isTTY is
// false when stdin is not a terminal, so callers fall back to a default instead
// of blocking — important for the session-start hook and CI. Injectable so the
// interactive helpers can be tested without a real terminal, mirroring the
// crypto.DefaultPassphraseFunc seam.
type PromptFunc func(prompt string) (line string, isTTY bool)

// DefaultPrompt is the production line reader.
var DefaultPrompt PromptFunc = readLine

// IsInteractive reports whether stdin is a terminal, so callers can choose an
// interactive flow versus a non-blocking default.
func IsInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func readLine(prompt string) (string, bool) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", false
	}
	fmt.Fprint(os.Stderr, prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", true
	}
	return strings.TrimSpace(line), true
}

// Confirm asks a yes/no question. Off a terminal, or on blank input, it returns
// def.
func Confirm(question string, def bool) bool {
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	line, isTTY := DefaultPrompt(fmt.Sprintf("%s %s ", question, hint))
	if !isTTY {
		return def
	}
	switch strings.ToLower(line) {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	default:
		return def
	}
}

// Choose presents a numbered menu and returns the 0-based index of the chosen
// option. Off a terminal, or on blank/invalid input, it returns 0 (the first
// option).
func Choose(question string, options []string) int {
	fmt.Fprintln(os.Stderr, question)
	for i, opt := range options {
		fmt.Fprintf(os.Stderr, "  %d) %s\n", i+1, opt)
	}
	line, isTTY := DefaultPrompt("Choice: ")
	if !isTTY {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(options) {
		return 0
	}
	return n - 1
}

// EditString prompts for a replacement value, showing the current one. Off a
// terminal, or on blank input (just Enter), it keeps current.
func EditString(question, current string) string {
	line, isTTY := DefaultPrompt(fmt.Sprintf("%s [%s]: ", question, current))
	if !isTTY || line == "" {
		return current
	}
	return line
}
