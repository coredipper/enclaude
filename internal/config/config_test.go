package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(t.TempDir())
	if err == nil {
		t.Fatal("expected error for missing seal.toml, got nil")
	}
}

func TestLoadOverlaysAllDefaultsWhenMergeSectionAbsent(t *testing.T) {
	sealDir := t.TempDir()
	content := `config_version = 2
[seal]
claude_dir = "/tmp/claude"
seal_dir = "/tmp/seal"
device_id = "test-device"
`
	if err := os.WriteFile(filepath.Join(sealDir, "seal.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(sealDir)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Merge == nil {
		t.Fatal("expected Merge map after overlay, got nil")
	}
	if _, ok := cfg.Merge["history.jsonl"]; !ok {
		t.Error("expected history.jsonl to be injected from defaults")
	}
	if _, ok := cfg.Merge["projects/*/sessions-index.json"]; !ok {
		t.Error("expected projects/*/sessions-index.json to be injected")
	}
}

func TestLoadPreservesUserOverride(t *testing.T) {
	sealDir := t.TempDir()
	content := `config_version = 2
[seal]
claude_dir = "/tmp/claude"
seal_dir = "/tmp/seal"
device_id = "test-device"

[merge_strategies]
"history.jsonl" = "last_write_wins"
`
	if err := os.WriteFile(filepath.Join(sealDir, "seal.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(sealDir)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Merge["history.jsonl"] != "last_write_wins" {
		t.Errorf("user override should be preserved, got %q", cfg.Merge["history.jsonl"])
	}
}

func TestLoadOverlaysOnlyMissingKeys(t *testing.T) {
	sealDir := t.TempDir()
	content := `config_version = 2
[seal]
claude_dir = "/tmp/claude"
seal_dir = "/tmp/seal"
device_id = "test-device"

[merge_strategies]
"history.jsonl" = "last_write_wins"
`
	if err := os.WriteFile(filepath.Join(sealDir, "seal.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(sealDir)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Merge["history.jsonl"] != "last_write_wins" {
		t.Errorf("user key should be preserved, got %q", cfg.Merge["history.jsonl"])
	}
	if _, ok := cfg.Merge["projects/*/sessions-index.json"]; !ok {
		t.Error("missing default key should be injected alongside user key")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	sealDir := t.TempDir()
	orig := DefaultConfig("/tmp/claude", sealDir)
	orig.Seal.DeviceID = "roundtrip-test-device"

	if err := orig.Save(sealDir); err != nil {
		t.Fatalf("Save() error: %v", err)
	}
	loaded, err := Load(sealDir)
	if err != nil {
		t.Fatalf("Load() after Save() error: %v", err)
	}
	if loaded.Seal.DeviceID != orig.Seal.DeviceID {
		t.Errorf("DeviceID: got %q, want %q", loaded.Seal.DeviceID, orig.Seal.DeviceID)
	}
	if loaded.Version != orig.Version {
		t.Errorf("Version: got %d, want %d", loaded.Version, orig.Version)
	}
	if len(loaded.Merge) < len(orig.Merge) {
		t.Errorf("merge strategies lost: got %d, want %d", len(loaded.Merge), len(orig.Merge))
	}
}

func TestDefaultConfig(t *testing.T) {
	claudeDir := "/mock/claude"
	sealDir := "/mock/seal"

	cfg := DefaultConfig(claudeDir, sealDir)

	if cfg.Version != ConfigVersion {
		t.Errorf("expected Version %d, got %d", ConfigVersion, cfg.Version)
	}

	if cfg.Seal.ClaudeDir != claudeDir {
		t.Errorf("expected ClaudeDir %q, got %q", claudeDir, cfg.Seal.ClaudeDir)
	}

	if cfg.Seal.SealDir != sealDir {
		t.Errorf("expected SealDir %q, got %q", sealDir, cfg.Seal.SealDir)
	}

	if cfg.Seal.DeviceID == "" {
		t.Error("expected non-empty DeviceID")
	}

	if !cfg.Sync.AutoSealOnSessionEnd {
		t.Error("expected AutoSealOnSessionEnd to be true")
	}

	if !cfg.Sync.AutoUnsealOnSessionStart {
		t.Error("expected AutoUnsealOnSessionStart to be true")
	}

	if cfg.Sync.AutoPush {
		t.Error("expected AutoPush to be false")
	}

	if cfg.Sync.AutoPull {
		t.Error("expected AutoPull to be false")
	}

	if len(cfg.Include.Patterns) != 12 {
		t.Errorf("expected 12 include patterns, got %d", len(cfg.Include.Patterns))
	}

	if len(cfg.Exclude.Patterns) != 17 {
		t.Errorf("expected 17 exclude patterns, got %d", len(cfg.Exclude.Patterns))
	}

	expectedMerges := map[string]string{
		"history.jsonl":                   "jsonl_dedup",
		"projects/*/sessions-index.json":  "sessions_index",
		"stats-cache.json":                "last_write_wins",
		"settings.json":                   "last_write_wins",
		"projects/*/*.jsonl":              "immutable",
		"projects/*/subagents/**/*.jsonl": "immutable",
		"projects/*/subagents/**/*.json":  "immutable",
		"projects/*/memory/**":            "text_merge",
		"**/*.md":                         "text_merge",
	}

	if len(cfg.Merge) != len(expectedMerges) {
		t.Errorf("expected %d merge strategies, got %d", len(expectedMerges), len(cfg.Merge))
	}

	for k, v := range expectedMerges {
		if got, ok := cfg.Merge[k]; !ok || got != v {
			t.Errorf("expected merge strategy %q for %q, got %q", v, k, got)
		}
	}
}
