package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/coredipper/claude-seal/internal/config"
	"github.com/coredipper/claude-seal/internal/crypto"
)

func TestListAllObjects(t *testing.T) {
	sealDir := t.TempDir()
	store := NewObjectStore(sealDir)
	store.Init()

	// Write some objects
	hashes := []string{
		"abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		"1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
	}
	for _, h := range hashes {
		store.Write(h, []byte("test"))
	}

	listed, err := store.ListAll()
	if err != nil {
		t.Fatalf("ListAll() error: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected 2 objects, got %d", len(listed))
	}

	listedSet := make(map[string]bool)
	for _, h := range listed {
		listedSet[h] = true
	}
	for _, h := range hashes {
		if !listedSet[h] {
			t.Errorf("missing hash %s", h[:16])
		}
	}
}

func TestVerifyHealthySeal(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	Seal(cfg, identity.Recipient(), false, nil)

	result, err := Verify(cfg, identity, false)
	if err != nil {
		t.Fatalf("Verify() error: %v", err)
	}

	if len(result.MissingObjects) > 0 {
		t.Errorf("expected 0 missing, got %d", len(result.MissingObjects))
	}
	if len(result.CorruptObjects) > 0 {
		t.Errorf("expected 0 corrupt, got %d", len(result.CorruptObjects))
	}
	if len(result.OrphanObjects) > 0 {
		t.Errorf("expected 0 orphans, got %d", len(result.OrphanObjects))
	}
}

func TestVerifyDetectsMissingObject(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	Seal(cfg, identity.Recipient(), false, nil)

	// Delete one object
	manifest, _ := LoadManifest(sealDir)
	store := NewObjectStore(sealDir)
	for _, entry := range manifest.Files {
		os.Remove(store.ObjectPath(entry.ContentHash))
		break // delete just one
	}

	result, err := Verify(cfg, identity, false)
	if err != nil {
		t.Fatalf("Verify() error: %v", err)
	}

	if len(result.MissingObjects) != 1 {
		t.Errorf("expected 1 missing, got %d", len(result.MissingObjects))
	}
}

func TestVerifyDetectsOrphan(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	Seal(cfg, identity.Recipient(), false, nil)

	// Add a fake orphan object
	store := NewObjectStore(sealDir)
	fakeHash := "deadbeef12345678deadbeef12345678deadbeef12345678deadbeef12345678"
	store.Write(fakeHash, []byte("orphan data"))

	result, err := Verify(cfg, identity, false)
	if err != nil {
		t.Fatalf("Verify() error: %v", err)
	}

	if len(result.OrphanObjects) != 1 {
		t.Errorf("expected 1 orphan, got %d", len(result.OrphanObjects))
	}
}

func TestRepairFixesMissing(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	Seal(cfg, identity.Recipient(), false, nil)

	// Delete one object
	manifest, _ := LoadManifest(sealDir)
	store := NewObjectStore(sealDir)
	var deletedPath string
	for path, entry := range manifest.Files {
		os.Remove(store.ObjectPath(entry.ContentHash))
		deletedPath = path
		break
	}

	// Repair should re-seal from plaintext
	result, err := Repair(cfg, identity, false, false)
	if err != nil {
		t.Fatalf("Repair() error: %v", err)
	}

	if result.Fixed != 1 {
		t.Errorf("expected 1 fixed, got %d", result.Fixed)
	}

	// Verify the object exists again
	manifest2, _ := LoadManifest(sealDir)
	entry := manifest2.Files[deletedPath]
	if !store.Exists(entry.ContentHash) {
		t.Error("object still missing after repair")
	}
}

func TestRotateReEncrypts(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	oldIdentity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	Seal(cfg, oldIdentity.Recipient(), false, nil)

	// Generate new key and rotate
	newIdentity, _ := crypto.GenerateKey()
	rotated, err := Rotate(cfg, oldIdentity, newIdentity.Recipient(), false, nil)
	if err != nil {
		t.Fatalf("Rotate() error: %v", err)
	}

	if rotated == 0 {
		t.Fatal("rotated 0 objects")
	}

	// Verify old key can't decrypt
	manifest, _ := LoadManifest(sealDir)
	store := NewObjectStore(sealDir)
	for _, entry := range manifest.Files {
		encrypted, _ := store.Read(entry.ContentHash)
		_, err := crypto.Decrypt(encrypted, oldIdentity)
		if err == nil {
			t.Error("old key should not decrypt after rotation")
		}
		break
	}

	// Verify new key can decrypt
	for path, entry := range manifest.Files {
		encrypted, _ := store.Read(entry.ContentHash)
		plaintext, err := crypto.Decrypt(encrypted, newIdentity)
		if err != nil {
			t.Errorf("new key failed to decrypt %s: %v", path, err)
			continue
		}
		if ContentHash(plaintext) != entry.ContentHash {
			t.Errorf("hash mismatch after rotation for %s", path)
		}
		break // spot check one
	}

	// Verify full round-trip: unseal with new key to fresh dir
	restoreDir := t.TempDir()
	cfg2 := config.DefaultConfig(restoreDir, sealDir)
	stats, err := Unseal(cfg2, newIdentity, false, nil)
	if err != nil {
		t.Fatalf("Unseal with new key error: %v", err)
	}
	if stats.Errors > 0 {
		t.Errorf("unseal had %d errors", stats.Errors)
	}

	// Verify restored files match originals
	for path, entry := range manifest.Files {
		origPath := filepath.Join(claudeDir, path)
		restoredPath := filepath.Join(restoreDir, path)
		origData, err := os.ReadFile(origPath)
		if err != nil {
			continue
		}
		restoredData, err := os.ReadFile(restoredPath)
		if err != nil {
			t.Errorf("restored file missing: %s", path)
			continue
		}
		if ContentHash(origData) != entry.ContentHash || ContentHash(restoredData) != entry.ContentHash {
			t.Errorf("content mismatch for %s after rotation", path)
		}
	}
}
