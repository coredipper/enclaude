package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/gitops"
	"github.com/coredipper/enclaude/internal/store"
	"github.com/spf13/cobra"
)

// encryptedStore sets up a git-backed seal store with encrypt_names on, keyed
// through ENCLAUDE_KEY so no keyring is touched, and returns its config.
func encryptedStore(t *testing.T) (*config.Config, *age.X25519Identity, *gitops.Git) {
	t.Helper()
	claudeDir := t.TempDir()
	sealDir := t.TempDir()
	flagClaudeDir = claudeDir
	flagSealDir = sealDir
	t.Cleanup(func() {
		flagClaudeDir = ""
		flagSealDir = ""
	})
	identity, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	t.Setenv("ENCLAUDE_KEY", identity.String())

	cfg := config.DefaultConfig(claudeDir, sealDir)
	cfg.Seal.EncryptNames = true
	if err := cfg.Save(sealDir); err != nil {
		t.Fatalf("save config: %v", err)
	}
	git := initTestGitRepo(t, sealDir)
	runGit(t, sealDir, "config", "user.name", "Test User")
	runGit(t, sealDir, "config", "user.email", "test@example.com")
	writeTestFile(t, filepath.Join(sealDir, ".gitattributes"), "manifest.json merge=enclaude-manifest\n")
	return cfg, identity, git
}

// writeAt writes a file under dir and sets its modified time.
func writeAt(t *testing.T, dir, rel, content string, mtime time.Time) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, path, content)
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// sealAndCommit seals cfg's Claude dir into the store and commits it.
func sealAndCommit(t *testing.T, cfg *config.Config, identity *age.X25519Identity, git *gitops.Git, msg string) {
	t.Helper()
	if _, err := store.Seal(cfg, identity.Recipient(), false, nil); err != nil {
		t.Fatalf("seal (%s): %v", msg, err)
	}
	if err := git.AddAll(); err != nil {
		t.Fatalf("git add (%s): %v", msg, err)
	}
	if err := git.Commit(msg); err != nil {
		t.Fatalf("git commit (%s): %v", msg, err)
	}
}

// assertFile checks a restored file's content and modified time.
func assertFile(t *testing.T, path, want string, mtime time.Time) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil || string(got) != want {
		t.Errorf("%s = %q (%v), want %q", path, got, err, want)
		return
	}
	info, _ := os.Stat(path)
	if !info.ModTime().Equal(mtime) {
		t.Errorf("%s modified time = %v, want %v", path, info.ModTime(), mtime)
	}
}

// TestEncryptNamesE2E_DiffAndRollback runs diff and rollback against a
// store whose manifests are encrypted in git history, checking that the old
// manifest read back from git decrypts, that rollback restores each file's
// content and modified time, and that the rolled-back manifest stays
// encrypted.
func TestEncryptNamesE2E_DiffAndRollback(t *testing.T) {
	cfg, identity, git := encryptedStore(t)
	t1 := time.Date(2024, 1, 2, 3, 4, 5, 600000000, time.UTC)
	t2 := t1.Add(48 * time.Hour)
	plan := "commands/job_hunting_canada/plan.md"
	writeAt(t, cfg.Seal.ClaudeDir, plan, "A", t1)
	sealAndCommit(t, cfg, identity, git, "commit A")
	logA, err := git.Log(1)
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	refA := strings.Fields(logA)[0]

	writeAt(t, cfg.Seal.ClaudeDir, plan, "B", t2)
	writeAt(t, cfg.Seal.ClaudeDir, "commands/new.md", "new", t2)
	sealAndCommit(t, cfg, identity, git, "commit B")

	if err := runDiff(&cobra.Command{}, []string{"HEAD~1"}); err != nil {
		t.Fatalf("runDiff: %v", err)
	}
	if out := runGit(t, cfg.Seal.SealDir, "log", "-p", "--all"); strings.Contains(out, "job_hunting_canada") {
		t.Error("git history mentions a folder name")
	}

	rollbackForce = true
	t.Cleanup(func() { rollbackForce = false })
	if err := runRollback(&cobra.Command{}, []string{refA}); err != nil {
		t.Fatalf("runRollback: %v", err)
	}
	assertFile(t, filepath.Join(cfg.Seal.ClaudeDir, plan), "A", t1)
	if _, err := os.Stat(filepath.Join(cfg.Seal.ClaudeDir, "commands/new.md")); !os.IsNotExist(err) {
		t.Errorf("commands/new.md survived rollback (stat err %v)", err)
	}
	data, _ := os.ReadFile(filepath.Join(cfg.Seal.SealDir, "manifest.json"))
	if !strings.HasPrefix(string(data), "age-encryption.org/") {
		t.Error("manifest is plaintext after rollback")
	}
}

// TestEncryptNamesE2E_DiffWrongKey verifies diff fails cleanly when the
// encrypted manifest cannot be opened with the local key.
func TestEncryptNamesE2E_DiffWrongKey(t *testing.T) {
	cfg, identity, git := encryptedStore(t)
	writeAt(t, cfg.Seal.ClaudeDir, "commands/a.md", "A", time.Now())
	sealAndCommit(t, cfg, identity, git, "commit A")
	writeAt(t, cfg.Seal.ClaudeDir, "commands/a.md", "B", time.Now())
	sealAndCommit(t, cfg, identity, git, "commit B")

	stranger, _ := crypto.GenerateKey()
	t.Setenv("ENCLAUDE_KEY", stranger.String())
	if err := runDiff(&cobra.Command{}, []string{"HEAD~1"}); err == nil || !strings.Contains(err.Error(), "decrypting manifest") {
		t.Fatalf("runDiff with the wrong key error = %v, want decrypting manifest error", err)
	}
}

// TestEncryptNamesE2E_GitMergeRunsDriver has real git merge two branches
// that each added a file. Git runs this test binary as the merge driver in
// a child process and waits for it, so the test checks the result git keeps
// once the process exits: an encrypted manifest holding both sides' files,
// which unseal restores with their content and modified times.
func TestEncryptNamesE2E_GitMergeRunsDriver(t *testing.T) {
	cfg, identity, git := encryptedStore(t)
	sealDir := cfg.Seal.SealDir
	base := time.Date(2024, 5, 6, 7, 8, 9, 987654321, time.UTC)
	writeAt(t, cfg.Seal.ClaudeDir, "commands/base.md", "base", base)
	sealAndCommit(t, cfg, identity, git, "base")
	runGit(t, sealDir, "branch", "other")

	// Each branch seals from its own Claude dir, as two devices would.
	oursAt, theirsAt := base.Add(time.Hour), base.Add(2*time.Hour)
	writeAt(t, cfg.Seal.ClaudeDir, "commands/ours dir/ours.md", "ours", oursAt)
	sealAndCommit(t, cfg, identity, git, "ours")

	runGit(t, sealDir, "checkout", "-q", "other")
	theirsCfg := *cfg
	theirsCfg.Seal.ClaudeDir = t.TempDir()
	writeAt(t, theirsCfg.Seal.ClaudeDir, "commands/base.md", "base", base)
	writeAt(t, theirsCfg.Seal.ClaudeDir, "commands/théirs/theirs.md", "theirs", theirsAt)
	sealAndCommit(t, &theirsCfg, identity, git, "theirs")
	runGit(t, sealDir, "checkout", "-q", "-")

	if err := git.ConfigMergeDriver("enclaude-manifest",
		[]string{"--seal-dir", sealDir, "merge-driver", "manifest", "%O", "%A", "%B"}); err != nil {
		t.Fatalf("ConfigMergeDriver: %v", err)
	}
	t.Setenv("ENCLAUDE_TEST_AS_CLI", "1")
	runGit(t, sealDir, "merge", "--no-edit", "other")

	data, err := os.ReadFile(filepath.Join(sealDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := store.ParseManifest(data, identity)
	if err != nil {
		t.Fatalf("merged manifest: %v", err)
	}
	if !strings.HasPrefix(string(data), "age-encryption.org/") || len(m.Files) != 3 {
		t.Fatalf("merged manifest encrypted=%v with %d files, want encrypted with 3", strings.HasPrefix(string(data), "age-encryption.org/"), len(m.Files))
	}

	restoreDir := t.TempDir()
	stats, err := store.Unseal(config.DefaultConfig(restoreDir, sealDir), identity, false, nil)
	if err != nil || stats.Errors != 0 {
		t.Fatalf("Unseal = %s, %v", stats, err)
	}
	assertFile(t, filepath.Join(restoreDir, "commands/base.md"), "base", base)
	assertFile(t, filepath.Join(restoreDir, "commands/ours dir/ours.md"), "ours", oursAt)
	assertFile(t, filepath.Join(restoreDir, "commands/théirs/theirs.md"), "theirs", theirsAt)
}
