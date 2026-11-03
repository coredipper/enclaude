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

// TestLoadPinsSealDirToLoadLocation verifies Load overrides the stored
// seal_dir with the directory it actually read seal.toml from. The store is
// synced across machines, so a seal_dir baked in by `init` on another device
// (with a different home) is stale — the manifest and objects always live next
// to the seal.toml we just loaded, so that location is authoritative.
func TestLoadPinsSealDirToLoadLocation(t *testing.T) {
	sealDir := t.TempDir()
	content := `config_version = 2
[seal]
claude_dir = "/home/daniel/.claude"
seal_dir = "/home/daniel/.enclaude"
device_id = "test-device"
`
	if err := os.WriteFile(filepath.Join(sealDir, "seal.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(sealDir)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Seal.SealDir != sealDir {
		t.Errorf("SealDir should be pinned to load location %q, got %q", sealDir, cfg.Seal.SealDir)
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

// TestDefaultConfig verifies DefaultConfig wires the given Claude and seal
// dirs, sets a non-empty DeviceID and the current ConfigVersion, applies the
// expected auto-seal/unseal defaults (push/pull off), and ships the full set
// of include, exclude, and per-path merge-strategy defaults.
func TestDefaultConfig(t *testing.T) {
	claudeDir := "/mock/claude"
	sealDir := "/mock/seal"

	cfg := DefaultConfig(claudeDir, sealDir)

	if cfg.Version != ConfigVersion {
		t.Errorf("expected Version %d, got %d", ConfigVersion, cfg.Version)
	}

	if cfg.Seal.SourceDir != claudeDir {
		t.Errorf("expected SourceDir %q, got %q", claudeDir, cfg.Seal.SourceDir)
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

// TestDefaultConfigForSourcePi verifies the pi profile selects the pi include,
// exclude and merge defaults and does not leak Claude's.
func TestDefaultConfigForSourcePi(t *testing.T) {
	cfg := DefaultConfigForSource("pi", "/tmp/pi-agent", "/tmp/seal")
	if cfg.Source() != SourcePi {
		t.Fatalf("Source() = %q, want pi", cfg.Source())
	}
	if cfg.Seal.SourceDir != "/tmp/pi-agent" {
		t.Errorf("SourceDir = %q", cfg.Seal.SourceDir)
	}
	if _, ok := cfg.Merge["sessions/**/*.jsonl"]; !ok {
		t.Error("pi defaults missing sessions/**/*.jsonl merge strategy")
	}
	if _, ok := cfg.Merge["history.jsonl"]; ok {
		t.Error("claude history strategy leaked into pi config")
	}
}

// TestLoadMigratesLegacyClaudeDir guards the v2 -> v3 rename: the legacy
// claude_dir key becomes source_dir and is cleared so it is never written back.
func TestLoadMigratesLegacyClaudeDir(t *testing.T) {
	sealDir := t.TempDir()
	content := "config_version = 2\n[seal]\nclaude_dir = \"/tmp/legacy\"\nseal_dir = \"/tmp/seal\"\ndevice_id = \"d\"\n"
	if err := os.WriteFile(filepath.Join(sealDir, "seal.toml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(sealDir)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.Seal.SourceDir != "/tmp/legacy" {
		t.Errorf("SourceDir = %q, want /tmp/legacy", cfg.Seal.SourceDir)
	}
	if cfg.Seal.LegacyClaudeDir != "" {
		t.Error("legacy key should be cleared")
	}
}
