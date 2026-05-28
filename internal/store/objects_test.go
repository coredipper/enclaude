package store

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestNewObjectStore(t *testing.T) {
	sealDir := "/path/to/seal"
	store := NewObjectStore(sealDir)
	expectedDir := filepath.Join(sealDir, "objects")

	if store.dir != expectedDir {
		t.Errorf("NewObjectStore().dir = %q, want %q", store.dir, expectedDir)
	}
}

func TestObjectPath(t *testing.T) {
	store := &ObjectStore{dir: "/objects"}
	hash := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	expectedPath := filepath.Join("/objects", "e3", "b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855.age")

	path := store.ObjectPath(hash)
	if path != expectedPath {
		t.Errorf("ObjectPath() = %q, want %q", path, expectedPath)
	}
}

func TestObjectStore_Init(t *testing.T) {
	tmpDir := t.TempDir()
	sealDir := filepath.Join(tmpDir, "seal")
	store := NewObjectStore(sealDir)

	// Check it doesn't exist yet
	if _, err := os.Stat(store.dir); !os.IsNotExist(err) {
		t.Fatalf("expected store.dir to not exist before Init")
	}

	if err := store.Init(); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	// Check it exists now
	if info, err := os.Stat(store.dir); err != nil {
		t.Fatalf("expected store.dir to exist after Init: %v", err)
	} else if !info.IsDir() {
		t.Errorf("expected store.dir to be a directory")
	}
}

func TestObjectStore_WriteReadExists(t *testing.T) {
	store := NewObjectStore(t.TempDir())

	data := []byte("encrypted data")
	hash := ContentHash(data) // in real usage it's hash of plaintext, but for test it's just an id

	// Should not exist initially
	if store.Exists(hash) {
		t.Errorf("Exists() returned true for non-existent object")
	}

	// Read should fail
	if _, err := store.Read(hash); err == nil {
		t.Errorf("Read() should fail for non-existent object")
	}

	// Write
	if err := store.Write(hash, data); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Should exist now
	if !store.Exists(hash) {
		t.Errorf("Exists() returned false for existing object")
	}

	// Read should succeed and match
	readData, err := store.Read(hash)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if string(readData) != string(data) {
		t.Errorf("Read() data = %q, want %q", readData, data)
	}
}

func TestObjectStore_Delete(t *testing.T) {
	store := NewObjectStore(t.TempDir())

	data := []byte("encrypted data")
	hash := ContentHash(data)

	// Write
	if err := store.Write(hash, data); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if !store.Exists(hash) {
		t.Fatalf("Exists() returned false, object not written")
	}

	// Delete
	if err := store.Delete(hash); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Should not exist
	if store.Exists(hash) {
		t.Errorf("Exists() returned true after Delete")
	}
}

func TestObjectStore_ListAll(t *testing.T) {
	store := NewObjectStore(t.TempDir())

	// Initially empty
	hashes, err := store.ListAll()
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}
	if len(hashes) != 0 {
		t.Errorf("ListAll() returned %d hashes, want 0", len(hashes))
	}

	// Write some objects
	hashesToWrite := []string{
		"11223344556677889900aabbccddeeff",
		"11aabbccddeeff001122334455667788",
		"223344556677889900aabbccddeeff11",
	}

	for _, hash := range hashesToWrite {
		if err := store.Write(hash, []byte("data")); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}

	// Add some garbage files/dirs to ensure they are skipped
	_ = os.WriteFile(filepath.Join(store.dir, "notadir.txt"), []byte("garbage"), 0600)
	_ = os.MkdirAll(filepath.Join(store.dir, "3"), 0700)   // too short prefix
	_ = os.MkdirAll(filepath.Join(store.dir, "333"), 0700) // too long prefix
	_ = os.WriteFile(filepath.Join(store.dir, "11", "notanagefile.txt"), []byte("garbage"), 0600)
	_ = os.MkdirAll(filepath.Join(store.dir, "11", "isdir.age"), 0700)

	// ListAll again
	hashes, err = store.ListAll()
	if err != nil {
		t.Fatalf("ListAll() error = %v", err)
	}

	if len(hashes) != len(hashesToWrite) {
		t.Fatalf("ListAll() returned %d hashes, want %d: %v", len(hashes), len(hashesToWrite), hashes)
	}

	sort.Strings(hashesToWrite)
	sort.Strings(hashes)

	for i := range hashes {
		if hashes[i] != hashesToWrite[i] {
			t.Errorf("ListAll() hash at %d = %q, want %q", i, hashes[i], hashesToWrite[i])
		}
	}
}
