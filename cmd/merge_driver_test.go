package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
)

// TestMergeManifests_CorruptAncestorFails guards that an unparseable
// ancestor manifest aborts the merge instead of being treated as empty,
// which would turn the three-way merge into a two-way one and could
// resurrect files deleted on one side.
func TestMergeManifests_CorruptAncestorFails(t *testing.T) {
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

	if err := config.DefaultConfig(claudeDir, sealDir).Save(sealDir); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := t.TempDir()
	ancestor := filepath.Join(dir, "ancestor.json")
	ours := filepath.Join(dir, "ours.json")
	theirs := filepath.Join(dir, "theirs.json")
	for path, content := range map[string]string{
		ancestor: "not json",
		ours:     "{}",
		theirs:   "{}",
	} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	err = mergeManifests(ancestor, ours, theirs)
	if err == nil || !strings.Contains(err.Error(), "parsing ancestor manifest") {
		t.Fatalf("mergeManifests() error = %v, want parsing ancestor manifest error", err)
	}
}
