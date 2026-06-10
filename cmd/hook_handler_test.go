package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadHookConfig_PinsClaudeDirLocally guards the session-hook entry point
// against trusting the synced seal.toml: claude_dir written by the machine that
// ran init must be overridden with this machine's resolution. Otherwise a
// session-end hook on a freshly cloned store seals against a nonexistent
// foreign claude dir, scans zero files, and commits a manifest with every
// entry deleted.
func TestLoadHookConfig_PinsClaudeDirLocally(t *testing.T) {
	sealDir := t.TempDir()
	foreign := `config_version = 2

[seal]
claude_dir = "/home/someone-else/.claude"
seal_dir = "/home/someone-else/.enclaude"
device_id = "other-machine"

[sync]
auto_seal_on_session_end = true
auto_unseal_on_session_start = true
`
	if err := os.WriteFile(filepath.Join(sealDir, "seal.toml"), []byte(foreign), 0600); err != nil {
		t.Fatal(err)
	}

	localClaude := t.TempDir()
	oldSealFlag, oldClaudeFlag := flagSealDir, flagClaudeDir
	flagSealDir, flagClaudeDir = sealDir, localClaude
	defer func() { flagSealDir, flagClaudeDir = oldSealFlag, oldClaudeFlag }()

	cfg, gotSealDir, ok := loadHookConfig()
	if !ok {
		t.Fatal("loadHookConfig: ok=false for an initialized store")
	}
	if gotSealDir != sealDir {
		t.Errorf("sealDir = %q, want %q", gotSealDir, sealDir)
	}
	if cfg.Seal.ClaudeDir != localClaude {
		t.Errorf("ClaudeDir = %q, want local %q (synced foreign value must not be trusted)",
			cfg.Seal.ClaudeDir, localClaude)
	}
	if cfg.Seal.SealDir != sealDir {
		t.Errorf("SealDir = %q, want load location %q", cfg.Seal.SealDir, sealDir)
	}
	if !cfg.Sync.AutoSealOnSessionEnd {
		t.Error("loaded config lost sync settings")
	}
}

// TestLoadHookConfig_SkipsWhenStoreMissing covers the silent-skip contract:
// hooks run on every Claude session, so an uninitialized store must yield
// ok=false (no error, no output) rather than block or fail the session.
func TestLoadHookConfig_SkipsWhenStoreMissing(t *testing.T) {
	oldSealFlag := flagSealDir
	flagSealDir = t.TempDir() // exists, but has no seal.toml
	defer func() { flagSealDir = oldSealFlag }()

	if _, _, ok := loadHookConfig(); ok {
		t.Error("loadHookConfig: ok=true for a directory with no seal.toml")
	}
}
