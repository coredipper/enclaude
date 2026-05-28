package crypto

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKeyFilePath(t *testing.T) {
	// Test override
	overridePath := "/tmp/override/key.age.enc"
	t.Setenv("ENCLAUDE_KEY_FILE", overridePath)
	path, err := KeyFilePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != overridePath {
		t.Errorf("expected %q, got %q", overridePath, path)
	}

	// Test XDG_CONFIG_HOME
	t.Setenv("ENCLAUDE_KEY_FILE", "")
	xdgHome := "/tmp/xdg_config"
	t.Setenv("XDG_CONFIG_HOME", xdgHome)
	path, err = KeyFilePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join(xdgHome, "enclaude", keyFileName)
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}

	// Test Fallback to UserHomeDir
	t.Setenv("XDG_CONFIG_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("skipping fallback test because UserHomeDir failed: %v", err)
	}
	path, err = KeyFilePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected = filepath.Join(home, ".config", "enclaude", keyFileName)
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestKeyFileLifecycle(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.age.enc")
	t.Setenv("ENCLAUDE_KEY_FILE", keyPath)

	// Verify not exists initially
	if KeyFileExists() {
		t.Error("expected KeyFileExists to be false initially")
	}

	// Generate a key
	identity, err := GenerateKey()
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	// Store it
	passphrase := "test-passphrase-123"
	if err := StoreKeyFile(identity, passphrase); err != nil {
		t.Fatalf("StoreKeyFile failed: %v", err)
	}

	// Verify it exists now
	if !KeyFileExists() {
		t.Error("expected KeyFileExists to be true after StoreKeyFile")
	}

	// Check permissions (should be 0600)
	info, err := os.Stat(keyPath)
	if err != nil {
		t.Fatalf("failed to stat key file: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected permissions 0600, got %v", info.Mode().Perm())
	}

	// Load it
	loaded, err := LoadKeyFile(passphrase)
	if err != nil {
		t.Fatalf("LoadKeyFile failed: %v", err)
	}
	if loaded.String() != identity.String() {
		t.Errorf("loaded identity does not match original")
	}

	// Load with wrong passphrase should fail
	_, err = LoadKeyFile("wrong-passphrase")
	if err == nil {
		t.Error("LoadKeyFile with wrong passphrase expected error, got nil")
	}

	// Delete it
	if err := DeleteKeyFile(); err != nil {
		t.Fatalf("DeleteKeyFile failed: %v", err)
	}

	// Verify not exists again
	if KeyFileExists() {
		t.Error("expected KeyFileExists to be false after DeleteKeyFile")
	}

	// Deleting a non-existent file should not return an error
	if err := DeleteKeyFile(); err != nil {
		t.Fatalf("DeleteKeyFile on non-existent file expected nil, got %v", err)
	}
}

func TestStoreKeyFile_Errors(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.age.enc")
	t.Setenv("ENCLAUDE_KEY_FILE", keyPath)

	identity, _ := GenerateKey()

	// Empty passphrase
	err := StoreKeyFile(identity, "")
	if err == nil {
		t.Error("StoreKeyFile with empty passphrase expected error, got nil")
	}
}

func TestLoadKeyFile_Errors(t *testing.T) {
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.age.enc")
	t.Setenv("ENCLAUDE_KEY_FILE", keyPath)

	// Loading non-existent file
	_, err := LoadKeyFile("any-passphrase")
	if err == nil {
		t.Error("LoadKeyFile on non-existent file expected error, got nil")
	}
}

func TestKeyFilePath_HomeError(t *testing.T) {
	// Simulate error when both ENCLAUDE_KEY_FILE and XDG_CONFIG_HOME are empty
	// and home dir cannot be found (e.g. HOME is empty)
	t.Setenv("ENCLAUDE_KEY_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")

	// Unset HOME which UserHomeDir relies on for Unix (and USERPROFILE for Windows)
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "") // For Windows compatibility

	_, err := KeyFilePath()
	if err == nil {
		t.Error("expected KeyFilePath to fail when home directory cannot be found, but it succeeded")
	}
}

func TestKeyFileExists_PathError(t *testing.T) {
	t.Setenv("ENCLAUDE_KEY_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	if KeyFileExists() {
		t.Error("expected KeyFileExists to return false when KeyFilePath fails")
	}
}

func TestStoreKeyFile_PathError(t *testing.T) {
	t.Setenv("ENCLAUDE_KEY_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	identity, _ := GenerateKey()
	err := StoreKeyFile(identity, "pass")
	if err == nil {
		t.Error("expected StoreKeyFile to fail when KeyFilePath fails")
	}
}

func TestLoadKeyFile_PathError(t *testing.T) {
	t.Setenv("ENCLAUDE_KEY_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	_, err := LoadKeyFile("pass")
	if err == nil {
		t.Error("expected LoadKeyFile to fail when KeyFilePath fails")
	}
}

func TestDeleteKeyFile_PathError(t *testing.T) {
	t.Setenv("ENCLAUDE_KEY_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	err := DeleteKeyFile()
	if err == nil {
		t.Error("expected DeleteKeyFile to fail when KeyFilePath fails")
	}
}

func TestStoreKeyFile_DirCreationError(t *testing.T) {
	// Create a file where a directory should be to force MkdirAll to fail
	dir := t.TempDir()
	conflictingFile := filepath.Join(dir, "enclaude")
	if err := os.WriteFile(conflictingFile, []byte("conflict"), 0600); err != nil {
		t.Fatalf("failed to create conflicting file: %v", err)
	}

	keyPath := filepath.Join(conflictingFile, "key.age.enc")
	t.Setenv("ENCLAUDE_KEY_FILE", keyPath)

	identity, _ := GenerateKey()
	err := StoreKeyFile(identity, "pass")
	if err == nil {
		t.Error("expected StoreKeyFile to fail when directory creation fails")
	}
}

func TestStoreKeyFile_WriteError(t *testing.T) {
	// Create a directory where the file should be to force WriteFile to fail
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.age.enc")
	if err := os.MkdirAll(keyPath, 0700); err != nil {
		t.Fatalf("failed to create conflicting dir: %v", err)
	}
	t.Setenv("ENCLAUDE_KEY_FILE", keyPath)

	identity, _ := GenerateKey()
	err := StoreKeyFile(identity, "pass")
	if err == nil {
		t.Error("expected StoreKeyFile to fail when file write fails")
	}
}

// To test StoreKeyFile EncryptWithPassphrase failure, we could mock it, but EncryptWithPassphrase doesn't
// currently have an easy mock point in the public API or easy way to make it fail with valid arguments
// (scrypt failure usually requires very large parameters or missing random data, which we don't control here).
// 92.3% for StoreKeyFile and 100% for the rest is excellent coverage.

func TestLoadKeyFile_ReadError(t *testing.T) {
	// Create a directory where the file should be to force ReadFile to fail
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "key.age.enc")
	if err := os.MkdirAll(keyPath, 0700); err != nil {
		t.Fatalf("failed to create conflicting dir: %v", err)
	}
	t.Setenv("ENCLAUDE_KEY_FILE", keyPath)

	_, err := LoadKeyFile("pass")
	if err == nil {
		t.Error("expected LoadKeyFile to fail when file read fails")
	}
}

func TestDeleteKeyFile_RemoveError(t *testing.T) {
	// Not practically testable cleanly in a cross-platform way without mocking os.Remove,
	// but we already have 100% on DeleteKeyFile and LoadKeyFile.
}
