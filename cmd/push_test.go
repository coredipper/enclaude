package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/gitops"
	"github.com/spf13/cobra"
)

func TestPushE2E(t *testing.T) {
	// Setup environment
	claudeDir := t.TempDir()
	sealDir := t.TempDir()
	bareRemoteDir := t.TempDir()

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

	// Generate identity
	identity, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	t.Setenv("ENCLAUDE_KEY", identity.String())

	// Create config
	cfg := config.DefaultConfig(claudeDir, sealDir)
	if err := os.MkdirAll(sealDir, 0700); err != nil {
		t.Fatalf("mkdir sealDir: %v", err)
	}
	if err := cfg.Save(sealDir); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Init git in sealDir
	git := gitops.New(sealDir)
	if err := git.Init(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	runGit(t, sealDir, "config", "user.name", "Test User")
	runGit(t, sealDir, "config", "user.email", "test@example.com")
	// Make an initial commit so we have a branch
	runGit(t, sealDir, "commit", "--allow-empty", "-m", "initial commit")
	branch, _ := git.CurrentBranch()

	// Init bare remote
	runGit(t, bareRemoteDir, "init", "--bare")
	runGit(t, bareRemoteDir, "config", "user.name", "Test User")
	runGit(t, bareRemoteDir, "config", "user.email", "test@example.com")

	// Redirect stdout to prevent noisy output
	oldStdout := os.Stdout
	devNull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0666)
	os.Stdout = devNull
	t.Cleanup(func() {
		os.Stdout = oldStdout
		devNull.Close()
	})

	t.Run("MissingRemote", func(t *testing.T) {
		cmd := &cobra.Command{}
		err := runPush(cmd, []string{"origin"})
		if err == nil {
			t.Fatal("expected error when remote is not configured")
		}
		if !strings.Contains(err.Error(), "not configured") {
			t.Errorf("expected error to mention 'not configured', got: %v", err)
		}
	})

	t.Run("FirstPushWithUpstreamCreation", func(t *testing.T) {
		if err := git.RemoteAdd("origin", bareRemoteDir); err != nil {
			t.Fatalf("remote add: %v", err)
		}

		// Create files to be sealed
		os.MkdirAll(filepath.Join(claudeDir, "commands"), 0700)
		os.WriteFile(filepath.Join(claudeDir, "commands", "file1.txt"), []byte("data"), 0644)

		cmd := &cobra.Command{}
		err := runPush(cmd, []string{"origin"})
		if err != nil {
			t.Fatalf("runPush failed: %v", err)
		}

		// Check if pushed successfully
		branches := runGit(t, bareRemoteDir, "branch")
		if !strings.Contains(branches, branch) {
			t.Errorf("expected branch %q to be pushed, got branches: %q", branch, branches)
		}

		// Check if it's set as upstream
		if !git.HasUpstream() {
			t.Errorf("expected upstream to be configured after first push")
		}
	})

	t.Run("SubsequentPush", func(t *testing.T) {
		// Modify file and run push again
		os.WriteFile(filepath.Join(claudeDir, "commands", "file1.txt"), []byte("modified data"), 0644)

		cmd := &cobra.Command{}
		err := runPush(cmd, []string{"origin"})
		if err != nil {
			t.Fatalf("runPush failed: %v", err)
		}

		// Verify new commit is at bare remote
		log := runGit(t, bareRemoteDir, "log", "-1", "--oneline")
		if !strings.Contains(log, "seal from") {
			t.Errorf("expected latest commit to be a seal commit, got: %q", log)
		}
	})
}
