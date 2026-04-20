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
