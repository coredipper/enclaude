package crypto

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestShredFile_Small(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "small.txt")

	content := []byte("hello world")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := ShredFile(path); err != nil {
		t.Fatalf("ShredFile failed: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still exists after shredding: %v", err)
	}
}

func TestShredFile_NonExistent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.txt")

	err := ShredFile(path)
	if err == nil {
		t.Fatal("expected error when shredding non-existent file, got nil")
	}
}

func TestShredFile_ReadOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "readonly.txt")

	content := []byte("hello world")
	if err := os.WriteFile(path, content, 0400); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	err := ShredFile(path)
	if err == nil {
		t.Fatal("expected error when shredding read-only file, got nil")
	}
}

func TestShredFile_Undeletable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "undeletable.txt")

	content := []byte("hello world")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Make directory read-only so the file can't be deleted
	if err := os.Chmod(dir, 0500); err != nil {
		t.Fatalf("failed to make directory read-only: %v", err)
	}
	t.Cleanup(func() {
		// Restore permissions so TempDir cleanup doesn't fail
		os.Chmod(dir, 0755)
	})

	err := ShredFile(path)
	if err == nil {
		t.Fatal("expected error when removing file in read-only directory, got nil")
	}

	// But the file should have been overwritten
	newContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read file back: %v", err)
	}

	if len(newContent) != len(content) {
		t.Errorf("expected size %d, got %d", len(content), len(newContent))
	}

	if bytes.Equal(newContent, content) {
		t.Error("file content was not overwritten")
	}
}

func TestShredFile_Large(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.txt")

	// Create a file larger than 64KB to test chunked writing
	size := 100 * 1024 // 100KB
	content := bytes.Repeat([]byte("A"), size)
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	if err := ShredFile(path); err != nil {
		t.Fatalf("ShredFile failed: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still exists after shredding: %v", err)
	}
}
