package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/gitops"
	"github.com/coredipper/enclaude/internal/store"
	"github.com/spf13/cobra"
)

func TestRollbackE2E(t *testing.T) {
	// Setup test environment
	claudeDir := t.TempDir()
	sealDir := t.TempDir()

	// Override global flags
	flagClaudeDir = claudeDir
	flagSealDir = sealDir
	t.Cleanup(func() {
		flagClaudeDir = ""
		flagSealDir = ""
	})

	// Setup crypto
	origPassphraseFunc := crypto.DefaultPassphraseFunc
	crypto.DefaultPassphraseFunc = func(prompt string, confirm bool) (string, error) {
		return "test-passphrase", nil
	}
	t.Cleanup(func() {
		crypto.DefaultPassphraseFunc = origPassphraseFunc
	})

	// Create identity
	identity, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	// Make sure we store key properly, we just set env var so LoadKey finds it
	t.Setenv("ENCLAUDE_KEY", identity.String())

	// Create config
	cfg := config.DefaultConfig(claudeDir, sealDir)
	if err := os.MkdirAll(sealDir, 0700); err != nil {
		t.Fatalf("mkdir sealDir: %v", err)
	}
	if err := cfg.Save(sealDir); err != nil {
		t.Fatalf("save config: %v", err)
	}

	git := gitops.New(sealDir)
	if err := git.Init(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	runGit(t, sealDir, "config", "user.name", "Test User")
	runGit(t, sealDir, "config", "user.email", "test@example.com")

	// Commit A (Initial)
	// Create required directories first
	os.MkdirAll(filepath.Join(claudeDir, "commands"), 0700)
	file1 := filepath.Join(claudeDir, "commands", "file1.txt")
	os.WriteFile(file1, []byte("A"), 0644)

	stats1, err := store.Seal(cfg, identity.Recipient(), false, nil)
	if err != nil {
		t.Fatalf("seal 1: %v", err)
	}
	if stats1.Added != 1 {
		t.Fatalf("expected 1 file sealed, got %v (stats1)", stats1)
	}
	if err := git.AddAll(); err != nil {
		t.Fatalf("git add 1: %v", err)
	}
	if err := git.Commit("commit A"); err != nil {
		t.Fatalf("git commit 1: %v", err)
	}
	// We don't really need commitA ref for this test, but good to know it's there.

	// Commit B (Modify file1, add file2)
	os.WriteFile(file1, []byte("B"), 0644)
	file2 := filepath.Join(claudeDir, "commands", "file2.txt")
	os.WriteFile(file2, []byte("B"), 0644)

	stats2, err := store.Seal(cfg, identity.Recipient(), false, nil)
	if err != nil {
		t.Fatalf("seal 2: %v", err)
	}
	if stats2.Modified != 1 || stats2.Added != 1 {
		t.Fatalf("expected 1 modified, 1 added, got %v", stats2)
	}
	if err := git.AddAll(); err != nil {
		t.Fatalf("git add 2: %v", err)
	}
	if err := git.Commit("commit B"); err != nil {
		t.Fatalf("git commit 2: %v", err)
	}

	refB, _ := git.Log(1)
	refBHash := strings.Split(refB, " ")[0]

	// Commit C (Modify file1, add file3)
	os.WriteFile(file1, []byte("C"), 0644)
	file3 := filepath.Join(claudeDir, "commands", "file3.txt")
	os.WriteFile(file3, []byte("C"), 0644)

	stats3, err := store.Seal(cfg, identity.Recipient(), false, nil)
	if err != nil {
		t.Fatalf("seal 3: %v", err)
	}
	if stats3.Modified != 1 || stats3.Added != 1 {
		t.Fatalf("expected 1 modified, 1 added, got %v", stats3)
	}
	if err := git.AddAll(); err != nil {
		t.Fatalf("git add 3: %v", err)
	}
	if err := git.Commit("commit C"); err != nil {
		t.Fatalf("git commit 3: %v", err)
	}

	// Prepare rollback using --force
	rollbackForce = true
	t.Cleanup(func() { rollbackForce = false })

	cmd := &cobra.Command{}

	// Execute rollback to B
	if err := runRollback(cmd, []string{refBHash}); err != nil {
		t.Fatalf("runRollback: %v", err)
	}

	// Verify State at B
	content1, _ := os.ReadFile(file1)
	if string(content1) != "B" {
		t.Errorf("expected file1 to be 'B', got %q", content1)
	}

	content2, _ := os.ReadFile(file2)
	if string(content2) != "B" {
		t.Errorf("expected file2 to be 'B', got %q", content2)
	}

	if _, err := os.Stat(file3); !os.IsNotExist(err) {
		t.Errorf("expected file3 to be deleted (did not exist in B)")
	}

	// Verify Git Log has safety seal and rollback commits
	// Note: We won't test for "safety seal" because we didn't add any changes
	// before calling rollback, so `stats.HasChanges()` is false, and safety seal isn't committed.
	logFull, _ := git.LogFull(3)
	if !strings.Contains(logFull, "rollback to "+refBHash) {
		t.Errorf("expected rollback commit in git log, got:\n%s", logFull)
	}
}

func TestRollbackAbort(t *testing.T) {
	claudeDir := t.TempDir()
	sealDir := t.TempDir()

	flagClaudeDir = claudeDir
	flagSealDir = sealDir
	t.Cleanup(func() {
		flagClaudeDir = ""
		flagSealDir = ""
	})

	identity, _ := crypto.GenerateKey()
	t.Setenv("ENCLAUDE_KEY", identity.String())

	cfg := config.DefaultConfig(claudeDir, sealDir)
	os.MkdirAll(sealDir, 0700)
	cfg.Save(sealDir)

	git := gitops.New(sealDir)
	if err := git.Init(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	runGit(t, sealDir, "config", "user.name", "Test User")
	runGit(t, sealDir, "config", "user.email", "test@example.com")

	// Initial commit
	if err := os.MkdirAll(filepath.Join(claudeDir, "commands"), 0700); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(claudeDir, "commands", "f.txt"), []byte("X"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := store.Seal(cfg, identity.Recipient(), false, nil); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := git.AddAll(); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := git.Commit("Initial"); err != nil {
		t.Fatalf("setup: %v", err)
	}
	ref, _ := git.Log(1)
	refHash := strings.Split(ref, " ")[0]

	// Redirect stdin to simulate user input "n\n"
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = oldStdin
	})

	go func() {
		w.Write([]byte("n\n"))
		w.Close()
	}()

	// Capture stdout to prevent noisy output
	oldStdout := os.Stdout
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0666)
	if err != nil {
		t.Fatalf("failed to open devNull: %v", err)
	}
	os.Stdout = devNull
	t.Cleanup(func() {
		os.Stdout = oldStdout
		devNull.Close()
	})

	rollbackForce = false // default
	errRollback := runRollback(&cobra.Command{}, []string{refHash})
	if errRollback != nil {
		t.Fatalf("expected nil error on abort, got %v", errRollback)
	}
}
