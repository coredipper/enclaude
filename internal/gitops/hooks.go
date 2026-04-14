package gitops

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// hookMarker is a shell comment appended to every enclaude hook command.
// Detection uses a simple strings.Contains on this marker, avoiding
// any need to parse shell quoting or tokenization.
const hookMarker = "# enclaude-managed"

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

// sealHookFull builds a complete hook command with the marker comment.
func sealHookFull(action string) string {
	return sealHookCommand() + " " + action + "  " + hookMarker
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

	// Upgrade any pre-marker hooks to current format
	migrateLegacyHooks(hooks)

	// Add SessionStart hook
	if err := addHookEntry(hooks, "SessionStart", hookEntry{
		Hooks: []hookDef{{
			Type:    "command",
			Command: sealHookFull("session-start"),
			Timeout: 30,
		}},
	}); err != nil {
		return fmt.Errorf("adding SessionStart hook: %w", err)
	}

	// Add SessionEnd hook (async to avoid blocking Claude shutdown)
	if err := addHookEntry(hooks, "SessionEnd", hookEntry{
		Hooks: []hookDef{{
			Type:    "command",
			Command: sealHookFull("session-end"),
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

	// Check if seal hook already exists (marker-only — legacy hooks
	// are migrated before addHookEntry is called)
	for _, e := range entries {
		for _, h := range e.Hooks {
			if hasMarker(h.Command) {
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
		isOurs := false
		for _, h := range e.Hooks {
			if hasMarker(h.Command) || isLegacyHook(h.Command) {
				isOurs = true
				break
			}
		}
		if !isOurs {
			filtered = append(filtered, e)
		}
	}

	data, _ := json.Marshal(filtered)
	hooks[event] = data
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
			if hasMarker(h.Command) {
				return true
			}
		}
	}
	return false
}

// hasMarker checks whether a command carries the enclaude marker comment.
// This is the sole detection mechanism for current-format hooks.
func hasMarker(cmd string) bool {
	return strings.Contains(cmd, hookMarker)
}

// isLegacyHook detects pre-marker enclaude hooks. Requires "enclaude"
// followed by "hook-handler" in the command, matching all legacy forms:
// bare ("enclaude hook-handler ..."), absolute path
// ("/path/to/enclaude hook-handler ..."), and quoted
// ("'/path/to/enclaude' hook-handler ...").
// Used only by migration and removal, never for idempotency checks.
func isLegacyHook(cmd string) bool {
	i := strings.Index(cmd, "enclaude")
	if i < 0 {
		return false
	}
	return strings.Contains(cmd[i:], "hook-handler")
}

// extractAction extracts the hook action (e.g. "session-start") from a
// legacy hook command. Finds "hook-handler" and returns the next token.
func extractAction(cmd string) string {
	const sentinel = "hook-handler"
	idx := strings.Index(cmd, sentinel)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(cmd[idx+len(sentinel):])
	if rest == "" {
		return ""
	}
	if i := strings.IndexByte(rest, ' '); i >= 0 {
		return rest[:i]
	}
	return rest
}

// migrateLegacyHooks upgrades pre-marker hook commands to the current
// marker format. Returns the number of hooks migrated.
func migrateLegacyHooks(hooks map[string]json.RawMessage) int {
	migrated := 0
	for _, event := range []string{"SessionStart", "SessionEnd"} {
		raw, ok := hooks[event]
		if !ok {
			continue
		}
		var entries []hookEntry
		if err := json.Unmarshal(raw, &entries); err != nil {
			continue
		}

		changed := false
		for i := range entries {
			for j := range entries[i].Hooks {
				h := &entries[i].Hooks[j]
				if hasMarker(h.Command) {
					continue
				}
				if !isLegacyHook(h.Command) {
					continue
				}
				action := extractAction(h.Command)
				if action == "" {
					continue
				}
				h.Command = sealHookFull(action)
				changed = true
				migrated++
			}
		}

		if changed {
			data, err := json.Marshal(entries)
			if err != nil {
				continue
			}
			hooks[event] = data
		}
	}
	return migrated
}
