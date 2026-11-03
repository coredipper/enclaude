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
	piDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(piDir, "tools"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(piDir, "tools", "run.sh"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}

	sealDir := t.TempDir()
	id, _ := crypto.GenerateKey()
	cfg := config.DefaultConfigForSource(config.SourcePi, piDir, sealDir)
	if _, err := Seal(cfg, id.Recipient(), false, nil); err != nil {
		t.Fatal(err)
	}

	restore := t.TempDir()
	cfg2 := config.DefaultConfigForSource(config.SourcePi, restore, sealDir)
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

// TestUnseal_BroadenedIncludeDoesNotDeleteUnmanaged guards that a synced
// [include] change cannot make unseal delete files this device never sealed:
// deletions are driven by the local managed-path ledger, not the include scan.
func TestUnseal_BroadenedIncludeDoesNotDeleteUnmanaged(t *testing.T) {
	src := t.TempDir()
	for _, f := range []string{"a.txt", "secret.json"} {
		if err := os.WriteFile(filepath.Join(src, f), []byte(f), 0644); err != nil {
			t.Fatal(err)
		}
	}

	sealDir := t.TempDir()
	id, _ := crypto.GenerateKey()
	cfg := config.DefaultConfigForSource(config.SourcePi, src, sealDir)
	cfg.Include.Patterns = []string{"a.txt"}
	if _, err := Seal(cfg, id.Recipient(), false, nil); err != nil {
		t.Fatal(err)
	}

	cfg.Include.Patterns = []string{"**"}
	if _, err := Unseal(cfg, id, false, nil); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"a.txt", "secret.json"} {
		if _, err := os.Stat(filepath.Join(src, f)); err != nil {
			t.Errorf("%s was deleted by a broadened include: %v", f, err)
		}
	}
}
