package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
)

// TestUnseal_RestoresMode guards that the permission bits recorded at seal time
// are reapplied on unseal, so executable resources such as tools/run.sh keep +x.
func TestUnseal_RestoresMode(t *testing.T) {
	claudeDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(claudeDir, "tools"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "tools", "run.sh"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	sealDir := t.TempDir()
	id, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig(claudeDir, sealDir)
	cfg.Include.Patterns = []string{"**"}
	if _, err := Seal(cfg, id.Recipient(), false, nil); err != nil {
		t.Fatal(err)
	}

	restore := t.TempDir()
	cfg2 := config.DefaultConfig(restore, sealDir)
	if _, err := Unseal(cfg2, id, false, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(restore, "tools", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("restored mode = %o, want 755", info.Mode().Perm())
	}
}
