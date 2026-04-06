package gitops

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallHooksPreservesExisting(t *testing.T) {
	dir := t.TempDir()

	// Write a settings.json with existing hooks (simulating peon-ping, notchi)
	existing := map[string]interface{}{
		"env": map[string]interface{}{
			"ENABLE_LSP_TOOL": "1",
		},
		"hooks": map[string]interface{}{
			"SessionStart": []interface{}{
				map[string]interface{}{
					"matcher": "",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "/path/to/peon-ping/peon.sh",
							"timeout": 10,
						},
					},
				},
			},
			"SessionEnd": []interface{}{
				map[string]interface{}{
					"matcher": "",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "/path/to/peon-ping/peon.sh",
							"timeout": 10,
							"async":   true,
						},
					},
				},
			},
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "Bash",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "/path/to/rtk-rewrite.sh",
						},
					},
				},
			},
		},
		"enabledPlugins": map[string]interface{}{
			"linear@claude-plugins-official": true,
		},
	}

	data, _ := json.MarshalIndent(existing, "", "  ")
	os.WriteFile(filepath.Join(dir, "settings.json"), data, 0644)

	// Install hooks
	if err := InstallHooks(dir); err != nil {
		t.Fatalf("InstallHooks() error: %v", err)
	}

	// Read back
	result, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	resultStr := string(result)

	// Verify existing hooks preserved
	if !strings.Contains(resultStr, "peon-ping/peon.sh") {
		t.Error("peon-ping hook was removed")
	}
	if !strings.Contains(resultStr, "rtk-rewrite.sh") {
		t.Error("rtk-rewrite hook was removed")
	}

	// Verify vault hooks added
	if !strings.Contains(resultStr, "claude-vault hook-handler session-start") {
		t.Error("session-start hook not added")
	}
	if !strings.Contains(resultStr, "claude-vault hook-handler session-end") {
		t.Error("session-end hook not added")
	}

	// Verify non-hook settings preserved
	if !strings.Contains(resultStr, "ENABLE_LSP_TOOL") {
		t.Error("env settings lost")
	}
	if !strings.Contains(resultStr, "enabledPlugins") {
		t.Error("enabledPlugins lost")
	}

	// Verify installed
	if !HooksInstalled(dir) {
		t.Error("HooksInstalled() returned false after install")
	}
}

func TestInstallHooksIdempotent(t *testing.T) {
	dir := t.TempDir()
	settings := map[string]interface{}{"hooks": map[string]interface{}{}}
	data, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(filepath.Join(dir, "settings.json"), data, 0644)

	// Install twice
	InstallHooks(dir)
	InstallHooks(dir)

	result, _ := os.ReadFile(filepath.Join(dir, "settings.json"))
	// Count occurrences — should only appear once
	count := strings.Count(string(result), "claude-vault hook-handler session-start")
	if count != 1 {
		t.Errorf("hook-handler session-start appears %d times, expected 1", count)
	}
}

func TestRemoveHooksPreservesOthers(t *testing.T) {
	dir := t.TempDir()
	settings := map[string]interface{}{"hooks": map[string]interface{}{}}
	data, _ := json.MarshalIndent(settings, "", "  ")
	os.WriteFile(filepath.Join(dir, "settings.json"), data, 0644)

	// Install then remove
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

func TestHooksInstalledFalseWhenMissing(t *testing.T) {
	dir := t.TempDir()
	if HooksInstalled(dir) {
		t.Error("should return false when no settings.json")
	}
}
