package cmd

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/store"
)

// TestCommitSealStore_RescuesIgnoredMetadataWithoutContentChanges guards the
// heal path: a store whose manifest.json was kept out of history by a global
// gitignore must get committed by the next seal/sync/push even when the seal
// itself saw no content changes — gating the commit on seal stats leaves the
// clone-breaking gap open until the next unrelated edit.
func TestCommitSealStore_RescuesIgnoredMetadataWithoutContentChanges(t *testing.T) {
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "Test User")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "test@example.com")
	}

	dir := t.TempDir()
	g := initTestGitRepo(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "seal.toml"), []byte("config_version = 2\n"), 0600); err != nil {
		t.Fatal(err)
	}
	committed, err := commitSealStore(dir, "initial")
	if err != nil {
		t.Fatalf("commitSealStore(initial): %v", err)
	}
	if !committed {
		t.Fatal("initial commitSealStore reported nothing to commit")
	}

	// A hostile global rule (same precedence as core.excludesFile) kept
	// manifest.json untracked; no other changes exist.
	exclude := filepath.Join(dir, ".git", "info", "exclude")
	if err := os.WriteFile(exclude, []byte("manifest.json\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	committed, err = commitSealStore(dir, "heal")
	if err != nil {
		t.Fatalf("commitSealStore(heal): %v", err)
	}
	if !committed {
		t.Error("commitSealStore did not commit the rescued manifest.json")
	}
	if _, err := g.ShowFileAtRef("HEAD", "manifest.json"); err != nil {
		t.Errorf("manifest.json not in HEAD after heal commit: %v", err)
	}

	committed, err = commitSealStore(dir, "noop")
	if err != nil {
		t.Fatalf("commitSealStore(noop): %v", err)
	}
	if committed {
		t.Error("commitSealStore committed with nothing staged")
	}
}

// TestCommitSealStore_NoOpSealCreatesNoCommit exercises the seal→commit flow
// end to end on an unchanged store: the second seal must produce nothing to
// commit. Guards the staged-diff gate against manifest timestamp churn —
// without it every session-end hook run would add a commit.
func TestCommitSealStore_NoOpSealCreatesNoCommit(t *testing.T) {
	for _, k := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(k, "Test User")
	}
	for _, k := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(k, "test@example.com")
	}

	claudeDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(claudeDir, "CLAUDE.md"), []byte("# hi\n"), 0644); err != nil {
		t.Fatal(err)
	}
	sealDir := t.TempDir()
	initTestGitRepo(t, sealDir)
	identity, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}
	cfg := config.DefaultConfig(claudeDir, sealDir)

	if _, err := store.Seal(cfg, identity.Recipient(), false, nil); err != nil {
		t.Fatalf("first Seal() error: %v", err)
	}
	committed, err := commitSealStore(sealDir, "initial seal")
	if err != nil {
		t.Fatalf("commitSealStore(initial): %v", err)
	}
	if !committed {
		t.Fatal("initial seal produced nothing to commit")
	}

	// sealed_at has second resolution: without crossing a boundary, a
	// rewritten manifest is byte-identical and git can't see the churn this
	// test exists to catch.
	time.Sleep(1100 * time.Millisecond)

	if _, err := store.Seal(cfg, identity.Recipient(), false, nil); err != nil {
		t.Fatalf("second Seal() error: %v", err)
	}
	committed, err = commitSealStore(sealDir, "no-op seal")
	if err != nil {
		t.Fatalf("commitSealStore(no-op): %v", err)
	}
	if committed {
		t.Error("no-op seal created a commit (manifest timestamp churn)")
	}
}
