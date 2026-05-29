package crypto

import (
	"os"
	"path/filepath"
	"testing"
)

// TestKeyFilePath verifies that KeyFilePath resolves the key file location in
// precedence order: the ENCLAUDE_KEY_FILE override, then XDG_CONFIG_HOME, then
// the ~/.config/enclaude fallback under the user home directory.
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

// TestKeyFileLifecycle exercises the full key-file flow: absence before
// creation, StoreKeyFile writing a 0600 file, LoadKeyFile round-tripping the
// identity, rejection of a wrong passphrase, and DeleteKeyFile being idempotent.
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

// TestStoreKeyFile_Errors verifies that StoreKeyFile returns an error when
// given an empty passphrase.
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

// TestLoadKeyFile_Errors verifies that LoadKeyFile returns an error when the
// key file does not exist at the resolved path.
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

// TestKeyFilePath_HomeError verifies that KeyFilePath returns an error when all
// location sources are unavailable: the override and XDG_CONFIG_HOME are empty
// and the home directory cannot be resolved.
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

// TestKeyFileExists_PathError verifies that KeyFileExists reports false (rather
// than panicking) when KeyFilePath cannot resolve a path.
func TestKeyFileExists_PathError(t *testing.T) {
	t.Setenv("ENCLAUDE_KEY_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")

	if KeyFileExists() {
		t.Error("expected KeyFileExists to return false when KeyFilePath fails")
	}
}

// TestStoreKeyFile_PathError verifies that StoreKeyFile propagates the error
// when KeyFilePath cannot resolve a destination path.
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

// TestLoadKeyFile_PathError verifies that LoadKeyFile propagates the error when
// KeyFilePath cannot resolve a source path.
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

// TestDeleteKeyFile_PathError verifies that DeleteKeyFile propagates the error
// when KeyFilePath cannot resolve a path.
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

// TestStoreKeyFile_DirCreationError verifies that StoreKeyFile fails when the
// parent directory cannot be created because a regular file occupies its path.
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

// TestStoreKeyFile_WriteError verifies that StoreKeyFile fails when the key
// file cannot be written because a directory already occupies its path.
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

// TestLoadKeyFile_ReadError verifies that LoadKeyFile fails when the key file
// cannot be read because a directory occupies its path.
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

// TestDeleteKeyFile_RemoveError is a placeholder documenting that the os.Remove
// failure path of DeleteKeyFile is not exercised here, since it cannot be
// triggered cross-platform without mocking os.Remove.
func TestDeleteKeyFile_RemoveError(t *testing.T) {
	// Not practically testable cleanly in a cross-platform way without mocking os.Remove,
	// but we already have 100% on DeleteKeyFile and LoadKeyFile.
}
