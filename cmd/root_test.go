package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/coredipper/enclaude/internal/gitops"
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
	g := gitops.New(dir)
	if err := g.Init(); err != nil {
		t.Fatal(err)
	}
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
