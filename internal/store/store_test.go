package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
)

func TestSealUnsealRoundTrip(t *testing.T) {
	// Create source directory (simulated ~/.claude/)
	claudeDir := setupTestDir(t)

	// Create seal directory
	sealDir := t.TempDir()

	// Generate key
	identity, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}

	cfg := config.DefaultConfig(claudeDir, sealDir)

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
	manifest, err := LoadManifest(sealDir)
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
	store := NewObjectStore(sealDir)
	for path, entry := range manifest.Files {
		if !store.Exists(entry.ContentHash) {
			t.Errorf("object missing for %s (hash: %s)", path, entry.ContentHash)
		}
	}

	// Unseal to a different directory
	restoreDir := t.TempDir()
	cfg2 := config.DefaultConfig(restoreDir, sealDir)

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
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

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
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

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

// TestSeal_ReSealsWhenObjectMissing guards the fast path's store.Exists
// check: when the manifest still records a file as unchanged (matching size
// and mtime) but its content blob has vanished from the object store,
// re-sealing must rewrite the object instead of trusting the stale manifest.
// Otherwise a lost object can never be recovered by an ordinary seal.
func TestSeal_ReSealsWhenObjectMissing(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	// Initial seal writes the object for history.jsonl (unique content,
	// unlike abc123.jsonl / agent-abc.jsonl which share a hash).
	if _, err := Seal(cfg, identity.Recipient(), false, nil); err != nil {
		t.Fatalf("first Seal() error: %v", err)
	}

	// Delete the stored object, leaving the file (and its mtime) untouched.
	manifest, _ := LoadManifest(sealDir)
	store := NewObjectStore(sealDir)
	hash := manifest.Files["history.jsonl"].ContentHash
	if err := os.Remove(store.ObjectPath(hash)); err != nil {
		t.Fatalf("removing object: %v", err)
	}

	// Re-seal: the file is byte-identical, but its object is gone.
	if _, err := Seal(cfg, identity.Recipient(), false, nil); err != nil {
		t.Fatalf("second Seal() error: %v", err)
	}

	if !store.Exists(hash) {
		t.Errorf("object for history.jsonl was not restored after re-seal: " +
			"the fast path's store.Exists guard fell through to the slow-path " +
			"hash check (store.go:207), which also skips writing unchanged content")
	}
}

// TestSeal_FastPathMissesSameSizeSameMtimeContentChange pins an accepted
// limitation of the metadata fast path: a file edited in place to new
// content of identical byte length, with its mtime rolled back to the
// previously sealed value, is reported Unchanged. This pathological
// content-swap-plus-mtime-rollback case is the inherent trade-off of
// size+mtime change detection; the test makes any future change to the
// heuristic deliberate rather than silent.
func TestSeal_FastPathMissesSameSizeSameMtimeContentChange(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	if _, err := Seal(cfg, identity.Recipient(), false, nil); err != nil {
		t.Fatalf("first Seal() error: %v", err)
	}

	manifest, _ := LoadManifest(sealDir)
	sealedMtime := time.Unix(0, manifest.Files["history.jsonl"].ModTimeNs)

	// Overwrite with different content of the SAME byte length, then roll the
	// mtime back to the sealed value so both fast-path predicates still hold.
	historyPath := filepath.Join(claudeDir, "history.jsonl")
	orig, _ := os.ReadFile(historyPath)
	next := []byte(`{"display":"XXXX","timestamp":9}`)
	if len(next) != len(orig) {
		t.Fatalf("test setup: replacement len %d != original len %d", len(next), len(orig))
	}
	if err := os.WriteFile(historyPath, next, 0644); err != nil {
		t.Fatalf("overwriting file: %v", err)
	}
	if err := os.Chtimes(historyPath, sealedMtime, sealedMtime); err != nil {
		t.Fatalf("resetting mtime: %v", err)
	}

	stats, err := Seal(cfg, identity.Recipient(), false, nil)
	if err != nil {
		t.Fatalf("second Seal() error: %v", err)
	}

	if stats.Modified != 0 {
		t.Errorf("fast path unexpectedly detected the same-size, same-mtime "+
			"content change (Modified=%d); the heuristic's behavior changed", stats.Modified)
	}
}

func TestUnsealDeletesRemovedFiles(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	// Seal all files
	Seal(cfg, identity.Recipient(), false, nil)

	// Remove one file from the manifest (simulate another device deleting it)
	manifest, _ := LoadManifest(sealDir)
	var removedPath string
	for path := range manifest.Files {
		removedPath = path
		delete(manifest.Files, path)
		break
	}
	manifest.Save(sealDir)

	// Unseal should delete the stale file
	stats, err := Unseal(cfg, identity, false, nil)
	if err != nil {
		t.Fatalf("Unseal() error: %v", err)
	}

	if stats.Deleted != 1 {
		t.Errorf("expected 1 deleted, got %d", stats.Deleted)
	}

	// Verify the file is gone
	absPath := filepath.Join(claudeDir, removedPath)
	if _, err := os.Stat(absPath); !os.IsNotExist(err) {
		t.Errorf("file %s should have been deleted but still exists", removedPath)
	}
}

// TestSealPopulatesByteCountersAndSessionTracked verifies the new
// fields surfaced in the sync output: bytes processed and session
// JSONL counts. Both depend on the per-file FileEntry data we already
// gather — these assertions pin that they reach SealStats.
func TestSealPopulatesByteCountersAndSessionTracked(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	stats, err := Seal(cfg, identity.Recipient(), false, nil)
	if err != nil {
		t.Fatalf("Seal() error: %v", err)
	}
	if stats.BytesPlaintext <= 0 {
		t.Errorf("BytesPlaintext should be > 0 after first seal, got %d", stats.BytesPlaintext)
	}
	if stats.BytesEncrypted <= 0 {
		t.Errorf("BytesEncrypted should be > 0 after first seal, got %d", stats.BytesEncrypted)
	}
	// setupTestDir creates two session JSONLs under projects/proj-a/.
	if stats.Sessions.Tracked < 2 {
		t.Errorf("Sessions.Tracked: want >= 2 from test fixture, got %d", stats.Sessions.Tracked)
	}
	if stats.Sessions.New != stats.Sessions.Tracked {
		t.Errorf("Sessions.New on first seal should equal Tracked (%d), got %d",
			stats.Sessions.Tracked, stats.Sessions.New)
	}
	if stats.Elapsed <= 0 {
		t.Errorf("Elapsed should be set, got %v", stats.Elapsed)
	}
}

// TestSealCountsUpdatedSessions exercises the "Sessions.Updated" counter:
// growing an existing session JSONL between two seals should show up as
// exactly one update.
func TestSealCountsUpdatedSessions(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	if _, err := Seal(cfg, identity.Recipient(), false, nil); err != nil {
		t.Fatalf("initial seal: %v", err)
	}

	// Append a second JSONL record to an existing session transcript.
	sessionPath := filepath.Join(claudeDir, "projects", "proj-a", "abc123.jsonl")
	f, err := os.OpenFile(sessionPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("open session for append: %v", err)
	}
	f.WriteString("\n" + `{"type":"assistant","content":"hi"}` + "\n")
	f.Close()

	stats, err := Seal(cfg, identity.Recipient(), false, nil)
	if err != nil {
		t.Fatalf("second seal: %v", err)
	}
	if stats.Sessions.New != 0 {
		t.Errorf("Sessions.New: want 0 on incremental seal, got %d", stats.Sessions.New)
	}
	if stats.Sessions.Updated != 1 {
		t.Errorf("Sessions.Updated: want 1 (the appended file), got %d", stats.Sessions.Updated)
	}
}

func TestUnsealDoesNotDeleteUnmanagedFiles(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	// Add a file that doesn't match include patterns (unmanaged)
	unmanagedPath := filepath.Join(claudeDir, "custom-script.sh")
	os.WriteFile(unmanagedPath, []byte("#!/bin/bash\necho hi"), 0755)

	// Seal (won't include custom-script.sh)
	Seal(cfg, identity.Recipient(), false, nil)

	// Unseal should NOT delete the unmanaged file
	_, err := Unseal(cfg, identity, false, nil)
	if err != nil {
		t.Fatalf("Unseal() error: %v", err)
	}

	if _, err := os.Stat(unmanagedPath); os.IsNotExist(err) {
		t.Error("unmanaged file was deleted — Unseal should only delete managed files")
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
	sealDir := t.TempDir()
	store := NewObjectStore(sealDir)
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

// TestStatus exercises Status across an uninitialized store (everything
// Added), a freshly sealed store (no changes), and a mixed working tree with a
// modified, an added, and a deleted file, verifying each lands in the right
// bucket.
func TestStatus(t *testing.T) {
	claudeDir := setupTestDir(t)
	sealDir := t.TempDir()

	identity, _ := crypto.GenerateKey()
	cfg := config.DefaultConfig(claudeDir, sealDir)

	// Scenario 1: Uninitialized seal store
	// Status should report all managed files as Added
	initialStatus, err := Status(cfg)
	if err != nil {
		t.Fatalf("initial Status() error: %v", err)
	}
	if len(initialStatus.Added) == 0 {
		t.Errorf("initial Status() reported 0 Added files, expected > 0")
	}
	if len(initialStatus.Modified) > 0 {
		t.Errorf("initial Status() reported %d Modified files, expected 0", len(initialStatus.Modified))
	}
	if len(initialStatus.Deleted) > 0 {
		t.Errorf("initial Status() reported %d Deleted files, expected 0", len(initialStatus.Deleted))
	}

	// Seal to sync state
	if _, err := Seal(cfg, identity.Recipient(), false, nil); err != nil {
		t.Fatalf("Seal() error: %v", err)
	}

	// Scenario 2: Synced state
	// Status should report no changes
	syncedStatus, err := Status(cfg)
	if err != nil {
		t.Fatalf("synced Status() error: %v", err)
	}
	if len(syncedStatus.Added) > 0 || len(syncedStatus.Modified) > 0 || len(syncedStatus.Deleted) > 0 {
		t.Errorf("synced Status() reported changes: %+v, expected none", syncedStatus)
	}

	// Scenario 3: Mixed changes
	// Modify a file
	historyPath := filepath.Join(claudeDir, "history.jsonl")
	f, _ := os.OpenFile(historyPath, os.O_APPEND|os.O_WRONLY, 0644)
	f.WriteString(`{"display":"new status entry","timestamp":3}` + "\n")
	f.Close()

	// Add a new file
	newSessionPath := filepath.Join(claudeDir, "projects", "proj-a", "status-new.jsonl")
	os.WriteFile(newSessionPath, []byte(`{"type":"user","message":"status"}`+"\n"), 0644)

	// Delete a file
	deletePath := filepath.Join(claudeDir, "CLAUDE.md")
	os.Remove(deletePath)

	mixedStatus, err := Status(cfg)
	if err != nil {
		t.Fatalf("mixed Status() error: %v", err)
	}

	// Verify modified
	if len(mixedStatus.Modified) != 1 || mixedStatus.Modified[0] != "history.jsonl" {
		t.Errorf("mixed Status() Modified = %v, expected [history.jsonl]", mixedStatus.Modified)
	}

	// Verify added
	foundAdded := false
	for _, added := range mixedStatus.Added {
		if added == "projects/proj-a/status-new.jsonl" {
			foundAdded = true
			break
		}
	}
	if !foundAdded {
		t.Errorf("mixed Status() Added = %v, expected it to contain projects/proj-a/status-new.jsonl", mixedStatus.Added)
	}
	if len(mixedStatus.Added) != 1 {
		t.Errorf("mixed Status() expected 1 Added file, got %d", len(mixedStatus.Added))
	}

	// Verify deleted
	if len(mixedStatus.Deleted) != 1 || mixedStatus.Deleted[0] != "CLAUDE.md" {
		t.Errorf("mixed Status() Deleted = %v, expected [CLAUDE.md]", mixedStatus.Deleted)
	}
}

// TestUnseal_ObjectsButNoManifest verifies the actionable error when a cloned seal
// store has encrypted objects but no manifest.json — the manifest was excluded by a
// gitignore on the pushing device (issue #94). A truly empty store keeps the
// original "is the seal store initialized?" wording.
func TestUnseal_ObjectsButNoManifest(t *testing.T) {
	identity, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey() error: %v", err)
	}

	t.Run("objects present yields recovery guidance", func(t *testing.T) {
		sealDir := t.TempDir()
		objStore := NewObjectStore(sealDir)
		if err := objStore.Init(); err != nil {
			t.Fatal(err)
		}
		hash := ContentHash([]byte("ciphertext"))
		if err := objStore.Write(hash, []byte("ciphertext")); err != nil {
			t.Fatal(err)
		}
		cfg := config.DefaultConfig(t.TempDir(), sealDir)

		_, err := Unseal(cfg, identity, false, nil)
		if err == nil {
			t.Fatal("Unseal() with objects but no manifest returned nil error")
		}
		for _, want := range []string{"manifest.json", "add -f", "encrypted objects"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error %q is missing %q", err.Error(), want)
			}
		}
	})

	t.Run("empty store reports not initialized", func(t *testing.T) {
		sealDir := t.TempDir()
		cfg := config.DefaultConfig(t.TempDir(), sealDir)

		_, err := Unseal(cfg, identity, false, nil)
		if err == nil {
			t.Fatal("Unseal() on empty store returned nil error")
		}
		if !strings.Contains(err.Error(), "is the seal store initialized?") {
			t.Errorf("error %q is missing the initialized hint", err.Error())
		}
	})
}
