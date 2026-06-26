package store

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
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

	// Delete the object for history.jsonl specifically (has unique content,
	// unlike abc123.jsonl and agent-abc.jsonl which share the same hash).
	manifest, _ := LoadManifest(sealDir)
	store := NewObjectStore(sealDir)
	entry := manifest.Files["history.jsonl"]
	os.Remove(store.ObjectPath(entry.ContentHash))

	result, err := Verify(cfg, identity, true)
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

	result, err := Verify(cfg, identity, true)
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

	// Delete the object for history.jsonl (has unique content).
	manifest, _ := LoadManifest(sealDir)
	store := NewObjectStore(sealDir)
	deletedPath := "history.jsonl"
	os.Remove(store.ObjectPath(manifest.Files[deletedPath].ContentHash))

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

func TestRepairUpdatesManifestWhenPlaintextChanged(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	Seal(cfg, identity.Recipient(), false, nil)

	// Delete the object for history.jsonl (has unique content, unlike
	// abc123.jsonl and agent-abc.jsonl which share the same hash).
	manifest, _ := LoadManifest(sealDir)
	store := NewObjectStore(sealDir)
	deletedPath := "history.jsonl"
	oldHash := manifest.Files[deletedPath].ContentHash
	os.Remove(store.ObjectPath(oldHash))

	// Modify the plaintext so its hash will differ from manifest
	newContent := []byte("completely new content after modification")
	os.WriteFile(filepath.Join(claudeDir, deletedPath), newContent, 0644)
	expectedHash := ContentHash(newContent)

	// Repair should re-seal from modified plaintext and update manifest
	result, err := Repair(cfg, identity, false, false)
	if err != nil {
		t.Fatalf("Repair() error: %v", err)
	}

	if result.Fixed != 1 {
		t.Errorf("expected 1 fixed, got %d", result.Fixed)
	}

	// Manifest should now point to the new hash, not the old one
	manifest2, _ := LoadManifest(sealDir)
	entry := manifest2.Files[deletedPath]
	if entry.ContentHash == oldHash {
		t.Error("manifest still points to old hash after repair")
	}
	if entry.ContentHash != expectedHash {
		t.Errorf("manifest hash = %s, want %s", entry.ContentHash[:16], expectedHash[:16])
	}

	// New object should exist
	if !store.Exists(expectedHash) {
		t.Error("new object missing after repair")
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

// TestRotateMissingObjectFailsWithoutPartialOverwrite guards rotation's
// all-or-nothing preflight: if any referenced object is missing, no already
// readable object should be overwritten to the new key.
func TestRotateMissingObjectFailsWithoutPartialOverwrite(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	oldIdentity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)
	if _, err := Seal(cfg, oldIdentity.Recipient(), false, nil); err != nil {
		t.Fatalf("Seal() error: %v", err)
	}

	manifest, _ := LoadManifest(sealDir)
	store := NewObjectStore(sealDir)
	var keepHash, missingHash string
	for _, entry := range manifest.Files {
		if keepHash == "" {
			keepHash = entry.ContentHash
			continue
		}
		if entry.ContentHash != keepHash {
			missingHash = entry.ContentHash
			break
		}
	}
	if keepHash == "" || missingHash == "" {
		t.Fatal("test needs at least two unique objects")
	}
	keepBefore, err := store.Read(keepHash)
	if err != nil {
		t.Fatalf("reading keep object: %v", err)
	}
	if err := os.Remove(store.ObjectPath(missingHash)); err != nil {
		t.Fatalf("removing missing object: %v", err)
	}

	newIdentity, _ := crypto.GenerateKey()
	rotated, err := Rotate(cfg, oldIdentity, newIdentity.Recipient(), false, nil)
	if err == nil {
		t.Fatal("expected Rotate() to fail when an object is missing")
	}
	if rotated != 0 {
		t.Fatalf("rotated = %d, want 0 before preflight failure", rotated)
	}

	keepAfter, err := store.Read(keepHash)
	if err != nil {
		t.Fatalf("reading keep object after failed rotate: %v", err)
	}
	if !bytes.Equal(keepAfter, keepBefore) {
		t.Fatal("failed rotation overwrote an object before preflight completed")
	}
	if _, err := crypto.Decrypt(keepAfter, oldIdentity); err != nil {
		t.Fatalf("old key should still decrypt untouched object: %v", err)
	}
	if _, err := crypto.Decrypt(keepAfter, newIdentity); err == nil {
		t.Fatal("new key should not decrypt object after failed rotation")
	}
}

// TestVerifyNoManifest verifies Verify fails with "no manifest found" when
// the seal store has no manifest yet.
func TestVerifyNoManifest(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	// LoadManifest returns nil, nil when file doesn't exist
	// Verify should return an error "no manifest found"
	_, err := Verify(cfg, identity, false)
	if err == nil {
		t.Fatal("expected Verify() to fail when no manifest exists, got nil")
	}
	if !strings.Contains(err.Error(), "no manifest found") {
		t.Fatalf("expected error to mention 'no manifest found', got: %v", err)
	}
}

// TestVerifyInvalidManifest verifies Verify fails when the manifest file
// exists but contains malformed JSON.
func TestVerifyInvalidManifest(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	// Write an invalid manifest.json
	os.WriteFile(filepath.Join(sealDir, "manifest.json"), []byte("invalid json"), 0644)

	_, err := Verify(cfg, identity, false)
	if err == nil {
		t.Fatal("expected Verify() to fail with invalid manifest, got nil")
	}
}

// TestVerifyReadError verifies Verify flags an object as corrupt when its
// blob cannot be read (here the object path is replaced by a directory).
func TestVerifyReadError(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	Seal(cfg, identity.Recipient(), false, nil)

	manifest, _ := LoadManifest(sealDir)
	store := NewObjectStore(sealDir)

	// Get an entry
	var deletedPath string
	var hash string
	for p, entry := range manifest.Files {
		deletedPath = p
		hash = entry.ContentHash
		break
	}

	// Replace object with a directory to cause read error
	objPath := store.ObjectPath(hash)
	os.Remove(objPath)
	os.MkdirAll(objPath, 0755)

	result, err := Verify(cfg, identity, true)
	if err != nil {
		t.Fatalf("Verify() error: %v", err)
	}

	found := false
	for _, p := range result.CorruptObjects {
		if p == deletedPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected %s to be corrupt due to read error", deletedPath)
	}
}

// TestVerifyDecryptError verifies Verify flags an object as corrupt when its
// blob is not a valid age ciphertext and fails to decrypt.
func TestVerifyDecryptError(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	Seal(cfg, identity.Recipient(), false, nil)

	manifest, _ := LoadManifest(sealDir)
	store := NewObjectStore(sealDir)

	var corruptedPath string
	var hash string
	for p, entry := range manifest.Files {
		corruptedPath = p
		hash = entry.ContentHash
		break
	}

	// Write random data to object to cause decrypt error
	objPath := store.ObjectPath(hash)
	os.WriteFile(objPath, []byte("definitely not an age encrypted file"), 0644)

	result, err := Verify(cfg, identity, true)
	if err != nil {
		t.Fatalf("Verify() error: %v", err)
	}

	found := false
	for _, p := range result.CorruptObjects {
		if p == corruptedPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected %s to be corrupt due to decrypt error", corruptedPath)
	}
}

// TestVerifyHashMismatch verifies Verify flags an object as corrupt when it
// decrypts cleanly but its plaintext hash no longer matches the manifest entry.
func TestVerifyHashMismatch(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	Seal(cfg, identity.Recipient(), false, nil)

	manifest, _ := LoadManifest(sealDir)
	store := NewObjectStore(sealDir)

	var corruptedPath string
	var hash string
	for p, entry := range manifest.Files {
		corruptedPath = p
		hash = entry.ContentHash
		break // just pick the first one
	}

	// Create another encrypted object with wrong content
	wrongContent := []byte("wrong plaintext")
	wrongEncrypted, _ := crypto.Encrypt(wrongContent, identity.Recipient())
	objPath := store.ObjectPath(hash)
	os.WriteFile(objPath, wrongEncrypted, 0644)

	result, err := Verify(cfg, identity, true)
	if err != nil {
		t.Fatalf("Verify() error: %v", err)
	}

	found := false
	for _, p := range result.CorruptObjects {
		if p == corruptedPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected %s to be corrupt due to hash mismatch", corruptedPath)
	}
}

// TestVerifyListObjectsError verifies Verify fails when the object store
// cannot be enumerated (here the objects directory is replaced by a file).
func TestVerifyListObjectsError(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	Seal(cfg, identity.Recipient(), false, nil)

	// Replace objects directory with a file to cause ListAll() to fail
	objectsDir := filepath.Join(sealDir, "objects")
	os.RemoveAll(objectsDir)
	os.WriteFile(objectsDir, []byte("not a directory"), 0644)

	_, err := Verify(cfg, identity, true)
	if err == nil {
		t.Fatal("expected Verify() to fail due to ListAll error, got nil")
	}
}

// TestRepairDeletesOrphans verifies Repair removes orphan objects that are
// not referenced by the manifest.
func TestRepairDeletesOrphans(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	Seal(cfg, identity.Recipient(), false, nil)

	// Add a fake orphan object
	store := NewObjectStore(sealDir)
	fakeHash := "deadbeef12345678deadbeef12345678deadbeef12345678deadbeef12345678"
	store.Write(fakeHash, []byte("orphan data"))

	// Repair with deleteOrphans=true
	result, err := Repair(cfg, identity, true, false)
	if err != nil {
		t.Fatalf("Repair() error: %v", err)
	}

	if store.Exists(fakeHash) {
		t.Errorf("Orphan still exists")
	}

	if len(result.OrphanObjects) != 1 {
		t.Errorf("expected 1 orphan in result, got %d", len(result.OrphanObjects))
	}
}

// TestRepairSkipsMissingPlaintext verifies Repair cannot fix an entry whose
// object and source plaintext are both gone, leaving Fixed at 0.
func TestRepairSkipsMissingPlaintext(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	Seal(cfg, identity.Recipient(), false, nil)

	// Delete an object from the store AND from the plaintext dir
	manifest, _ := LoadManifest(sealDir)
	store := NewObjectStore(sealDir)
	deletedPath := "history.jsonl"
	os.Remove(store.ObjectPath(manifest.Files[deletedPath].ContentHash))
	os.Remove(filepath.Join(claudeDir, deletedPath))

	// Repair should attempt to fix but fail to read plaintext, returning without error but Fixed=0
	result, err := Repair(cfg, identity, false, false)
	if err != nil {
		t.Fatalf("Repair() error: %v", err)
	}

	if result.Fixed != 0 {
		t.Errorf("expected 0 fixed, got %d", result.Fixed)
	}
}

// TestRepairFailsWithCorruptManifest verifies Repair propagates an error
// when the manifest cannot be loaded.
func TestRepairFailsWithCorruptManifest(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	Seal(cfg, identity.Recipient(), false, nil)

	// Corrupt manifest
	os.WriteFile(filepath.Join(sealDir, "manifest.json"), []byte("{invalid json"), 0644)

	_, err := Repair(cfg, identity, false, false)
	if err == nil {
		t.Fatalf("expected error from Repair with corrupt manifest")
	}
}

// TestRepairFixesCorruptObject verifies Repair re-seals a corrupted object
// from its plaintext so the restored content matches the original.
func TestRepairFixesCorruptObject(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	Seal(cfg, identity.Recipient(), false, nil)

	// Corrupt an object
	manifest, _ := LoadManifest(sealDir)
	store := NewObjectStore(sealDir)
	corruptedPath := "history.jsonl"
	hash := manifest.Files[corruptedPath].ContentHash

	// Write wrong content to the object file
	store.Write(hash, []byte("corrupt data"))

	// Repair should fix it using the available plaintext
	result, err := Repair(cfg, identity, false, false)
	if err != nil {
		t.Fatalf("Repair() error: %v", err)
	}

	if result.Fixed != 1 {
		t.Errorf("expected 1 fixed, got %d", result.Fixed)
	}

	// Verify the object is properly restored
	manifest2, _ := LoadManifest(sealDir)
	entry := manifest2.Files[corruptedPath]

	// Since the plaintext hasn't changed, the hash should be the original one
	// But let's verify we can decrypt it and get original content
	encrypted, err := store.Read(entry.ContentHash)
	if err != nil {
		t.Fatalf("Failed to read repaired object: %v", err)
	}

	plaintext, err := crypto.Decrypt(encrypted, identity)
	if err != nil {
		t.Fatalf("Failed to decrypt repaired object: %v", err)
	}

	originalContent, err := os.ReadFile(filepath.Join(claudeDir, corruptedPath))
	if err != nil {
		t.Fatalf("Failed to read original plaintext: %v", err)
	}
	if !bytes.Equal(plaintext, originalContent) {
		t.Errorf("Repaired content does not match original plaintext")
	}
}
