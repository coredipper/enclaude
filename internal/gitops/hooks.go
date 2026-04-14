package gitops

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// resolveExecutable returns the absolute path to the currently running binary.
// Package-level var so tests can override it.
var resolveExecutable = os.Executable

// sealHookCommand returns the shell-safe absolute path to the enclaude binary
// for use in hook commands. Hooks run via /bin/sh which may not have
// ~/go/bin or other user-specific paths in PATH.
// We intentionally do NOT resolve symlinks: symlinks (e.g. from Homebrew)
// are stable across upgrades, while their targets are versioned and ephemeral.
func sealHookCommand() string {
	exe, err := resolveExecutable()
	if err != nil {
		return "enclaude hook-handler"
	}
	return shellQuote(exe) + " hook-handler"
}

// shellQuote wraps a string in single quotes for safe use in /bin/sh commands.
// Single quotes prevent all shell expansion. Any literal single quotes in the
// input are handled by ending the quoted segment, inserting an escaped quote,
// and reopening.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// hookEntry matches Claude Code's hook config structure.
type hookEntry struct {
	Matcher string     `json:"matcher,omitempty"`
	Hooks   []hookDef  `json:"hooks"`
}

type hookDef struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
	Async   bool   `json:"async,omitempty"`
}

// InstallHooks adds enclaude hooks to settings.json without
// disturbing existing hooks.
func InstallHooks(claudeDir string) error {
	settingsPath := filepath.Join(claudeDir, "settings.json")

	// Read existing settings
	var settings map[string]json.RawMessage
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("reading settings.json: %w", err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("parsing settings.json: %w", err)
	}

	// Parse hooks section
	var hooks map[string]json.RawMessage
	if raw, ok := settings["hooks"]; ok {
		if err := json.Unmarshal(raw, &hooks); err != nil {
			return fmt.Errorf("parsing hooks: %w", err)
		}
	} else {
		hooks = make(map[string]json.RawMessage)
	}

	hookCmd := sealHookCommand()

	// Add SessionStart hook
	if err := addHookEntry(hooks, "SessionStart", hookEntry{
		Hooks: []hookDef{{
			Type:    "command",
			Command: hookCmd + " session-start",
			Timeout: 30,
		}},
	}); err != nil {
		return fmt.Errorf("adding SessionStart hook: %w", err)
	}

	// Add SessionEnd hook (async to avoid blocking Claude shutdown)
	if err := addHookEntry(hooks, "SessionEnd", hookEntry{
		Hooks: []hookDef{{
			Type:    "command",
			Command: hookCmd + " session-end",
			Timeout: 60,
			Async:   true,
		}},
	}); err != nil {
		return fmt.Errorf("adding SessionEnd hook: %w", err)
	}

	// Serialize hooks back
	hooksJSON, err := json.Marshal(hooks)
	if err != nil {
		return fmt.Errorf("marshaling hooks: %w", err)
	}
	settings["hooks"] = hooksJSON

	// Write back with indentation
	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling settings: %w", err)
	}

	return os.WriteFile(settingsPath, out, 0600)
}

// RemoveHooks removes enclaude hooks from settings.json.
func RemoveHooks(claudeDir string) error {
	settingsPath := filepath.Join(claudeDir, "settings.json")

	var settings map[string]json.RawMessage
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("reading settings.json: %w", err)
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("parsing settings.json: %w", err)
	}

	var hooks map[string]json.RawMessage
	if raw, ok := settings["hooks"]; ok {
		if err := json.Unmarshal(raw, &hooks); err != nil {
			return fmt.Errorf("parsing hooks: %w", err)
		}
	} else {
		return nil // no hooks to remove
	}

	for _, event := range []string{"SessionStart", "SessionEnd"} {
		removeHookEntries(hooks, event)
	}

	hooksJSON, err := json.Marshal(hooks)
	if err != nil {
		return err
	}
	settings["hooks"] = hooksJSON

	out, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(settingsPath, out, 0600)
}

// HooksInstalled checks if enclaude hooks are present in settings.json.
func HooksInstalled(claudeDir string) bool {
	settingsPath := filepath.Join(claudeDir, "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return false
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		return false
	}

	var hooks map[string]json.RawMessage
	raw, ok := settings["hooks"]
	if !ok {
		return false
	}
	if err := json.Unmarshal(raw, &hooks); err != nil {
		return false
	}

	return hasSealHook(hooks, "SessionStart") && hasSealHook(hooks, "SessionEnd")
}

// addHookEntry appends a hook entry to an event's array, skipping if already present.
func addHookEntry(hooks map[string]json.RawMessage, event string, entry hookEntry) error {
	var entries []hookEntry
	if raw, ok := hooks[event]; ok {
		if err := json.Unmarshal(raw, &entries); err != nil {
			return err
		}
	}

	// Check if seal hook already exists
	for _, e := range entries {
		for _, h := range e.Hooks {
			if containsMarker(h.Command) {
				return nil // already installed
			}
		}
	}

	entries = append(entries, entry)

	data, err := json.Marshal(entries)
	if err != nil {
		return err
	}
	hooks[event] = data
	return nil
}

// removeHookEntries removes seal hook entries from an event's array.
func removeHookEntries(hooks map[string]json.RawMessage, event string) {
	raw, ok := hooks[event]
	if !ok {
		return
	}

	var entries []hookEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return
	}

	var filtered []hookEntry
	for _, e := range entries {
		isSeal := false
		for _, h := range e.Hooks {
			if containsMarker(h.Command) {
				isSeal = true
				break
			}
		}
		if !isSeal {
			filtered = append(filtered, e)
		}
	}

	if len(filtered) == 0 {
		// Don't leave empty arrays; remove the key entirely
		// Actually, keep it as empty array to preserve the key
		data, _ := json.Marshal(filtered)
		hooks[event] = data
	} else {
		data, _ := json.Marshal(filtered)
		hooks[event] = data
	}
}

func hasSealHook(hooks map[string]json.RawMessage, event string) bool {
	raw, ok := hooks[event]
	if !ok {
		return false
	}
	var entries []hookEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return false
	}
	for _, e := range entries {
		for _, h := range e.Hooks {
			if containsMarker(h.Command) {
				return true
			}
		}
	}
	return false
}

func containsMarker(cmd string) bool {
	// Match both bare ("enclaude hook-handler ...") and shell-quoted
	// ("'/path with spaces/enclaude' hook-handler ...") forms.
	// Handles the '\'' escape sequence used by shellQuote for paths
	// containing apostrophes.
	arg0, rest := shellSplitFirst(cmd)
	if arg0 != "enclaude" && !strings.HasSuffix(arg0, "/enclaude") {
		return false
	}
	arg1, _ := shellSplitFirst(rest)
	return arg1 == "hook-handler"
}

// shellSplitFirst extracts the first shell token from cmd, handling
// single-quoted strings including the '\'' escape idiom. Returns the
// unquoted token value and the remaining string.
func shellSplitFirst(cmd string) (token, rest string) {
	cmd = strings.TrimSpace(cmd)
	if len(cmd) == 0 {
		return "", ""
	}

	// Accumulate token segments: quoted runs, '\'' escapes, and
	// adjacent unquoted characters are all part of one POSIX token
	// until we hit unquoted whitespace.
	var b strings.Builder
	i := 0
	for i < len(cmd) {
		switch cmd[i] {
		case '\'':
			end := strings.IndexByte(cmd[i+1:], '\'')
			if end < 0 {
				b.WriteString(cmd[i+1:])
				return b.String(), ""
			}
			b.WriteString(cmd[i+1 : i+1+end])
			i = i + 1 + end + 1
			// Handle '\'' escape (end quote, escaped quote, reopen)
			if strings.HasPrefix(cmd[i:], `\'`) {
				b.WriteByte('\'')
				i += 2
			}
		case '"':
			end := strings.IndexByte(cmd[i+1:], '"')
			if end < 0 {
				b.WriteString(cmd[i+1:])
				return b.String(), ""
			}
			b.WriteString(cmd[i+1 : i+1+end])
			i = i + 1 + end + 1
		case ' ', '\t', '\n':
			return b.String(), cmd[i:]
		default:
			b.WriteByte(cmd[i])
			i++
		}
	}
	return b.String(), ""
}
