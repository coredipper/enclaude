package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	sealstore "github.com/coredipper/enclaude/internal/store"
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

// TestMergeManifests_EncryptedNames verifies the merge driver decrypts
// encrypted manifests from both sides and writes the merged result encrypted
// again, both to git's output file and to the seal store, keeping the files
// each side added.
func TestMergeManifests_EncryptedNames(t *testing.T) {
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

	dir := t.TempDir()
	ancestor := filepath.Join(dir, "ancestor.json")
	if err := os.WriteFile(ancestor, nil, 0600); err != nil {
		t.Fatal(err)
	}
	sides := map[string]string{"ours.json": "projects/a/x.jsonl", "theirs.json": "projects/b/y.jsonl"}
	for name, rel := range sides {
		m := sealstore.NewManifest("dev")
		m.Files[rel] = sealstore.FileEntry{ContentHash: strings.Repeat("a", 64)}
		data, err := m.Marshal(identity.Recipient())
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}

	ours := filepath.Join(dir, "ours.json")
	if err := mergeManifests(ancestor, ours, filepath.Join(dir, "theirs.json")); err != nil {
		t.Fatalf("mergeManifests: %v", err)
	}
	for _, path := range []string{ours, filepath.Join(sealDir, "manifest.json")} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if strings.Contains(string(data), "projects/") {
			t.Errorf("%s holds plaintext names", path)
		}
		m, err := sealstore.ParseManifest(data, identity)
		if err != nil {
			t.Fatalf("ParseManifest(%s): %v", path, err)
		}
		for _, rel := range sides {
			if _, ok := m.Files[rel]; !ok {
				t.Errorf("%s lost %s", path, rel)
			}
		}
	}
}

// TestMergeManifests_EncryptedNamesWrongKey verifies the merge driver fails
// when the local key cannot open the encrypted manifests, so git reports a
// conflict instead of taking an empty or one-sided result.
func TestMergeManifests_EncryptedNamesWrongKey(t *testing.T) {
	sealDir := t.TempDir()
	flagSealDir = sealDir
	t.Cleanup(func() { flagSealDir = "" })

	owner, _ := crypto.GenerateKey()
	stranger, _ := crypto.GenerateKey()
	t.Setenv("ENCLAUDE_KEY", stranger.String())
	cfg := config.DefaultConfig(t.TempDir(), sealDir)
	cfg.Seal.EncryptNames = true
	if err := cfg.Save(sealDir); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := t.TempDir()
	data, err := sealstore.NewManifest("dev").Marshal(owner.Recipient())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, name := range []string{"ancestor.json", "ours.json", "theirs.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	err = mergeManifests(filepath.Join(dir, "ancestor.json"), filepath.Join(dir, "ours.json"), filepath.Join(dir, "theirs.json"))
	if err == nil || !strings.Contains(err.Error(), "decrypting manifest") {
		t.Fatalf("mergeManifests() error = %v, want decrypting manifest error", err)
	}
	if _, err := os.Stat(filepath.Join(sealDir, "manifest.json")); !os.IsNotExist(err) {
		t.Errorf("failed merge wrote the seal manifest (stat err %v)", err)
	}
}
