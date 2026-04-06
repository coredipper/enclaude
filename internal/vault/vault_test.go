package vault

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/coredipper/claude-vault/internal/config"
	"github.com/coredipper/claude-vault/internal/crypto"
)

func TestSealUnsealRoundTrip(t *testing.T) {
	// Create source directory (simulated ~/.claude/)
	claudeDir := setupTestDir(t)

	// Create vault directory
	vaultDir := t.TempDir()

	// Generate key
	identity, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}

	cfg := config.DefaultConfig(claudeDir, vaultDir)

	// Seal
	sealStats, err := Seal(cfg, identity.Recipient(), false, nil)
	if err != nil {
		t.Fatalf("Seal() error: %v", err)
	}

	if sealStats.Scanned == 0 {
		t.Fatal("Seal scanned 0 files")
	}
	if sealStats.Added == 0 {
		t.Fatal("Seal added 0 files")
	}
	t.Logf("Seal: %s", sealStats)

	// Verify manifest exists
	manifest, err := LoadManifest(vaultDir)
	if err != nil {
		t.Fatalf("LoadManifest() error: %v", err)
	}
	if manifest == nil {
		t.Fatal("manifest is nil after seal")
	}
	if len(manifest.Files) == 0 {
		t.Fatal("manifest has 0 files after seal")
	}

	// Verify objects exist
	store := NewObjectStore(vaultDir)
	for path, entry := range manifest.Files {
		if !store.Exists(entry.ContentHash) {
			t.Errorf("object missing for %s (hash: %s)", path, entry.ContentHash)
		}
	}

	// Unseal to a different directory
	restoreDir := t.TempDir()
	cfg2 := config.DefaultConfig(restoreDir, vaultDir)

	unsealStats, err := Unseal(cfg2, identity, false, nil)
	if err != nil {
		t.Fatalf("Unseal() error: %v", err)
	}

	if unsealStats.Restored == 0 {
		t.Fatal("Unseal restored 0 files")
	}
	t.Logf("Unseal: %s", unsealStats)

	// Verify round-trip: compare original and restored files
	for relPath, entry := range manifest.Files {
		origPath := filepath.Join(claudeDir, relPath)
		restoredPath := filepath.Join(restoreDir, relPath)

		origData, err := os.ReadFile(origPath)
		if err != nil {
			t.Errorf("cannot read original %s: %v", relPath, err)
			continue
		}
		restoredData, err := os.ReadFile(restoredPath)
		if err != nil {
			t.Errorf("cannot read restored %s: %v", relPath, err)
			continue
		}

		if ContentHash(origData) != entry.ContentHash {
			t.Errorf("original hash mismatch for %s", relPath)
		}
		if ContentHash(restoredData) != entry.ContentHash {
			t.Errorf("restored hash mismatch for %s", relPath)
		}
	}
}

func TestSealIdempotent(t *testing.T) {
	claudeDir := setupTestDir(t)
	vaultDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, vaultDir)

	// First seal
	stats1, err := Seal(cfg, identity.Recipient(), false, nil)
	if err != nil {
		t.Fatalf("first Seal() error: %v", err)
	}

	// Second seal (no changes)
	stats2, err := Seal(cfg, identity.Recipient(), false, nil)
	if err != nil {
		t.Fatalf("second Seal() error: %v", err)
	}

	if stats2.Added != 0 {
		t.Errorf("second seal added %d files, expected 0", stats2.Added)
	}
	if stats2.Modified != 0 {
		t.Errorf("second seal modified %d files, expected 0", stats2.Modified)
	}
	if stats2.Unchanged != stats1.Added {
		t.Errorf("second seal: %d unchanged, expected %d", stats2.Unchanged, stats1.Added)
	}
}

func TestSealIncrementalDetectsChanges(t *testing.T) {
	claudeDir := setupTestDir(t)
	vaultDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, vaultDir)

	// Initial seal
	Seal(cfg, identity.Recipient(), false, nil)

	// Modify a file
	historyPath := filepath.Join(claudeDir, "history.jsonl")
	f, _ := os.OpenFile(historyPath, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString(`{"display":"new entry","timestamp":2}` + "\n")
	f.Close()

	// Add a new file
	newSession := filepath.Join(claudeDir, "projects", "proj-a", "newsession.jsonl")
	os.WriteFile(newSession, []byte(`{"type":"user","message":"hello"}`+"\n"), 0644)

	// Second seal
	stats, err := Seal(cfg, identity.Recipient(), false, nil)
	if err != nil {
		t.Fatalf("incremental Seal() error: %v", err)
	}

	if stats.Added != 1 {
		t.Errorf("incremental seal: %d added, expected 1", stats.Added)
	}
	if stats.Modified != 1 {
		t.Errorf("incremental seal: %d modified, expected 1", stats.Modified)
	}
}

func TestManifestDiff(t *testing.T) {
	old := &Manifest{
		Files: map[string]FileEntry{
			"a.txt": {ContentHash: "hash1"},
			"b.txt": {ContentHash: "hash2"},
			"c.txt": {ContentHash: "hash3"},
		},
	}
	new := &Manifest{
		Files: map[string]FileEntry{
			"a.txt": {ContentHash: "hash1"},   // unchanged
			"b.txt": {ContentHash: "hash2b"},  // modified
			"d.txt": {ContentHash: "hash4"},   // added
		},
	}

	diff := new.Diff(old)

	if len(diff.Added) != 1 || diff.Added[0] != "d.txt" {
		t.Errorf("Added = %v, want [d.txt]", diff.Added)
	}
	if len(diff.Modified) != 1 || diff.Modified[0] != "b.txt" {
		t.Errorf("Modified = %v, want [b.txt]", diff.Modified)
	}
	if len(diff.Deleted) != 1 || diff.Deleted[0] != "c.txt" {
		t.Errorf("Deleted = %v, want [c.txt]", diff.Deleted)
	}
}

func TestContentHash(t *testing.T) {
	data := []byte("hello world")
	hash1 := ContentHash(data)
	hash2 := ContentHash(data)

	if hash1 != hash2 {
		t.Fatal("ContentHash is not deterministic")
	}

	hash3 := ContentHash([]byte("different data"))
	if hash1 == hash3 {
		t.Fatal("different data produced same hash")
	}

	if len(hash1) != 64 {
		t.Errorf("hash length = %d, want 64 (SHA-256 hex)", len(hash1))
	}
}

func TestObjectStoreWriteReadExists(t *testing.T) {
	vaultDir := t.TempDir()
	store := NewObjectStore(vaultDir)
	store.Init()

	data := []byte("encrypted data here")
	hash := "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890"

	if store.Exists(hash) {
		t.Fatal("object exists before write")
	}

	if err := store.Write(hash, data); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	if !store.Exists(hash) {
		t.Fatal("object does not exist after write")
	}

	readBack, err := store.Read(hash)
	if err != nil {
		t.Fatalf("Read() error: %v", err)
	}

	if string(readBack) != string(data) {
		t.Fatal("Read() returned different data")
	}
}
