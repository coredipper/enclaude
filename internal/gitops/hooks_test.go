package gitops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	resolveExecutable = func() (string, error) {
		return "/usr/local/bin/enclaude", nil
	}
	os.Exit(m.Run())
}

func TestInstallHooksPreservesExisting(t *testing.T) {
	dir := t.TempDir()

	existing := map[string]any{
		"env": map[string]any{"ENABLE_LSP_TOOL": "1"},
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "/path/to/peon-ping/peon.sh",
							"timeout": 10,
						},
					},
				},
			},
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Bash",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "/path/to/rtk-rewrite.sh",
						},
					},
				},
			},
		},
		"enabledPlugins": map[string]any{
			"linear@claude-plugins-official": true,
		},
	}

	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(dir, "settings.json"), data, 0644)

	if err := InstallHooks(dir); err != nil {
		t.Fatalf("InstallHooks() error: %v", err)
	}

	result, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	resultStr := string(result)

	if !strings.Contains(resultStr, "peon-ping/peon.sh") {
		t.Error("peon-ping hook was removed")
	}
	if !strings.Contains(resultStr, "rtk-rewrite.sh") {
		t.Error("rtk-rewrite hook was removed")
	}
	if strings.Count(resultStr, hookMarker) != 2 {
		t.Error("expected exactly 2 hook markers")
	}
	if !strings.Contains(resultStr, "ENABLE_LSP_TOOL") {
		t.Error("env settings lost")
	}
	if !HooksInstalled(dir) {
		t.Error("HooksInstalled() returned false after install")
	}
}

func TestInstallHooksIdempotent(t *testing.T) {
	dir := t.TempDir()
	settings := map[string]any{"hooks": map[string]any{}}
	data, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(filepath.Join(dir, "settings.json"), data, 0644)

	InstallHooks(dir)
	InstallHooks(dir)

	result, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	count := strings.Count(string(result), hookMarker)
	if count != 2 {
		t.Errorf("hook marker appears %d times, expected 2", count)
	}
}

func TestInstallHooksMigratesLegacy(t *testing.T) {
	dir := t.TempDir()

	// Simulate a settings.json with legacy (pre-marker) hooks
	existing := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "enclaude hook-handler session-start",
							"timeout": 30,
						},
					},
				},
			},
			"SessionEnd": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "/old/path/enclaude hook-handler session-end",
							"timeout": 60,
							"async":   true,
						},
					},
				},
			},
		},
	}

	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(dir, "settings.json"), data, 0644)

	if err := InstallHooks(dir); err != nil {
		t.Fatalf("InstallHooks() error: %v", err)
	}

	result, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	resultStr := string(result)

	// Legacy hooks should be migrated, not duplicated
	if strings.Count(resultStr, hookMarker) != 2 {
		t.Errorf("expected 2 markers after migration, got %d", strings.Count(resultStr, hookMarker))
	}
	// Should use current binary path, not old one
	if strings.Contains(resultStr, "/old/path/enclaude") {
		t.Error("legacy path was not replaced")
	}
	if !HooksInstalled(dir) {
		t.Error("HooksInstalled() returned false after migration")
	}
}

func TestRemoveHooksPreservesOthers(t *testing.T) {
	dir := t.TempDir()
	settings := map[string]any{"hooks": map[string]any{}}
	data, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(filepath.Join(dir, "settings.json"), data, 0644)

	InstallHooks(dir)

	if !HooksInstalled(dir) {
		t.Fatal("hooks should be installed")
	}

	if err := RemoveHooks(dir); err != nil {
		t.Fatalf("RemoveHooks() error: %v", err)
	}

	if HooksInstalled(dir) {
		t.Error("hooks should be removed")
	}
}

func TestRemoveHandlesBothFormats(t *testing.T) {
	dir := t.TempDir()

	// Mix of legacy and marker hooks
	existing := map[string]any{
		"hooks": map[string]any{
			"SessionStart": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "enclaude hook-handler session-start",
						},
					},
				},
			},
			"SessionEnd": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "'/usr/local/bin/enclaude' hook-handler session-end  " + hookMarker,
						},
					},
				},
			},
		},
	}

	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(dir, "settings.json"), data, 0644)

	if err := RemoveHooks(dir); err != nil {
		t.Fatalf("RemoveHooks() error: %v", err)
	}

	result, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	resultStr := string(result)

	if strings.Contains(resultStr, "enclaude") {
		t.Error("enclaude hooks should be fully removed")
	}
}

func TestShellQuoteHandlesSpaces(t *testing.T) {
	old := resolveExecutable
	resolveExecutable = func() (string, error) {
		return "/path with spaces/enclaude", nil
	}
	defer func() { resolveExecutable = old }()

	cmd := sealHookCommand()
	if cmd != "'/path with spaces/enclaude' hook-handler" {
		t.Errorf("unexpected command: %s", cmd)
	}
}

func TestShellQuoteHandlesSingleQuotes(t *testing.T) {
	old := resolveExecutable
	resolveExecutable = func() (string, error) {
		return "/it's/enclaude", nil
	}
	defer func() { resolveExecutable = old }()

	cmd := sealHookCommand()
	expected := `'/it'\''s/enclaude' hook-handler`
	if cmd != expected {
		t.Errorf("unexpected command: got %s, want %s", cmd, expected)
	}
}

func TestSymlinkNotResolved(t *testing.T) {
	old := resolveExecutable
	resolveExecutable = func() (string, error) {
		return "/opt/homebrew/bin/enclaude", nil
	}
	defer func() { resolveExecutable = old }()

	cmd := sealHookCommand()
	if cmd != "'/opt/homebrew/bin/enclaude' hook-handler" {
		t.Errorf("unexpected command (symlink may have been resolved): %s", cmd)
	}
}

func TestHasMarker(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"enclaude hook-handler session-start  " + hookMarker, true},
		{"'/usr/local/bin/enclaude' hook-handler session-end  " + hookMarker, true},
		// No marker
		{"enclaude hook-handler session-start", false},
		{"/path/to/peon-ping/peon.sh", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := hasMarker(tt.cmd); got != tt.want {
			t.Errorf("hasMarker(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

func TestIsLegacyHook(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{"enclaude hook-handler session-start", true},
		{"/usr/local/bin/enclaude hook-handler session-end", true},
		{"'/path/to/enclaude' hook-handler session-start", true},
		// Not legacy
		{"/path/to/peon-ping/peon.sh", false},
		{"some-random-script --foo", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isLegacyHook(tt.cmd); got != tt.want {
			t.Errorf("isLegacyHook(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

func TestExtractAction(t *testing.T) {
	tests := []struct {
		cmd  string
		want string
	}{
		{"enclaude hook-handler session-start", "session-start"},
		{"'/usr/local/bin/enclaude' hook-handler session-end", "session-end"},
		{"/path/enclaude hook-handler session-start  " + hookMarker, "session-start"},
		{"enclaude hook-handler", ""},
		{"peon.sh", ""},
	}
	for _, tt := range tests {
		if got := extractAction(tt.cmd); got != tt.want {
			t.Errorf("extractAction(%q) = %q, want %q", tt.cmd, got, tt.want)
		}
	}
}

func TestMigrateLegacyHooks(t *testing.T) {
	hooks := map[string]json.RawMessage{}

	// Legacy SessionStart
	startEntries := []hookEntry{
		{Hooks: []hookDef{{
			Type:    "command",
			Command: "enclaude hook-handler session-start",
			Timeout: 30,
		}}},
	}
	startData, _ := json.Marshal(startEntries)
	hooks["SessionStart"] = startData

	// Non-enclaude hook (should be untouched)
	otherEntries := []hookEntry{
		{Hooks: []hookDef{{
			Type:    "command",
			Command: "/path/to/peon.sh",
		}}},
	}
	otherData, _ := json.Marshal(otherEntries)
	hooks["PreToolUse"] = otherData

	n := migrateLegacyHooks(hooks)
	if n != 1 {
		t.Errorf("expected 1 migration, got %d", n)
	}

	// Verify migrated hook has marker
	var result []hookEntry
	json.Unmarshal(hooks["SessionStart"], &result)
	if !hasMarker(result[0].Hooks[0].Command) {
		t.Errorf("migrated hook missing marker: %s", result[0].Hooks[0].Command)
	}

	// Verify other hooks untouched
	var others []hookEntry
	json.Unmarshal(hooks["PreToolUse"], &others)
	if others[0].Hooks[0].Command != "/path/to/peon.sh" {
		t.Errorf("non-enclaude hook was modified: %s", others[0].Hooks[0].Command)
	}
}

func TestMigrateLegacySkipsAlreadyMigrated(t *testing.T) {
	hooks := map[string]json.RawMessage{}

	entries := []hookEntry{
		{Hooks: []hookDef{{
			Type:    "command",
			Command: sealHookFull("session-start"),
		}}},
	}
	data, _ := json.Marshal(entries)
	hooks["SessionStart"] = data

	n := migrateLegacyHooks(hooks)
	if n != 0 {
		t.Errorf("expected 0 migrations for already-migrated hooks, got %d", n)
	}
}

func TestSealHookFullIncludesMarker(t *testing.T) {
	cmd := sealHookFull("session-start")
	if !strings.Contains(cmd, hookMarker) {
		t.Errorf("sealHookFull missing marker: %s", cmd)
	}
	if !strings.Contains(cmd, "session-start") {
		t.Errorf("sealHookFull missing action: %s", cmd)
	}
	if !strings.Contains(cmd, "/usr/local/bin/enclaude") {
		t.Errorf("sealHookFull missing executable path: %s", cmd)
	}
}

func TestHooksInstalledFalseWhenMissing(t *testing.T) {
	dir := t.TempDir()
	if HooksInstalled(dir) {
		t.Error("should return false when no settings.json")
	}
}
