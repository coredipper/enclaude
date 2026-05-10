package crypto

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zalando/go-keyring"
)

// withTestEnv isolates each test: temp key file path, in-memory keyring mock,
// stub passphrase prompter, restored keyring func vars on cleanup.
func withTestEnv(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("ENCLAUDE_KEY_FILE", filepath.Join(dir, "key.age.enc"))
	t.Setenv(envKeyVar, "")

	keyring.MockInit()

	origSet, origGet, origDel := keyringSet, keyringGet, keyringDelete
	origPP := DefaultPassphraseFunc
	t.Cleanup(func() {
		keyringSet, keyringGet, keyringDelete = origSet, origGet, origDel
		DefaultPassphraseFunc = origPP
	})

	DefaultPassphraseFunc = func(prompt string, confirm bool) (string, error) {
		return "test-passphrase", nil
	}
}

// TestStoreKey_FallbackClearsStaleKeyringEntry exercises the rotate scenario:
// keyring already holds K_old, keyring.Set(K_new) fails (D-Bus blip), fallback
// must wipe stale entry so LoadKey returns K_new from the file rather than the
// stale K_old from the keyring.
func TestStoreKey_FallbackClearsStaleKeyringEntry(t *testing.T) {
	withTestEnv(t)

	kOld, err := GenerateKey()
	if err != nil {
		t.Fatalf("generate K_old: %v", err)
	}
	if err := keyring.Set(keychainService, keychainAccount, kOld.String()); err != nil {
		t.Fatalf("seed keyring with K_old: %v", err)
	}

	keyringSet = func(service, user, pass string) error {
		return errors.New("simulated dbus blip")
	}

	kNew, err := GenerateKey()
	if err != nil {
		t.Fatalf("generate K_new: %v", err)
	}

	source, err := StoreKey(kNew)
	if err != nil {
		t.Fatalf("StoreKey returned error: %v", err)
	}
	if source != SourceFile {
		t.Fatalf("expected SourceFile fallback, got %q", source)
	}

	// LoadKey precedence is env → keyring → file. The fallback path must
	// have cleared the stale K_old keyring entry, otherwise LoadKey returns
	// it before reaching the file.
	got, src, err := LoadKey()
	if err != nil {
		t.Fatalf("LoadKey: %v", err)
	}
	if src != SourceFile {
		t.Fatalf("LoadKey source = %q, want %q (keyring should be empty)", src, SourceFile)
	}
	if got.String() != kNew.String() {
		t.Fatal("LoadKey returned stale K_old; fallback failed to clear keyring")
	}
}

// TestStoreKey_FallbackAbortsWhenKeyringDeleteFails verifies StoreKey refuses
// to write the file fallback when it cannot clear a stale keyring entry —
// committing the file in that state would leave LoadKey returning the
// shadowed stale key.
func TestStoreKey_FallbackAbortsWhenKeyringDeleteFails(t *testing.T) {
	withTestEnv(t)

	kOld, _ := GenerateKey()
	if err := keyring.Set(keychainService, keychainAccount, kOld.String()); err != nil {
		t.Fatalf("seed keyring: %v", err)
	}

	keyringSet = func(service, user, pass string) error {
		return errors.New("set blip")
	}
	keyringDelete = func(service, user string) error {
		return errors.New("delete blip")
	}

	kNew, _ := GenerateKey()
	source, err := StoreKey(kNew)
	if err == nil {
		t.Fatalf("StoreKey unexpectedly succeeded (source=%q); should refuse to commit half-state", source)
	}

	// Ensure no key file was written behind the user's back.
	if KeyFileExists() {
		t.Fatal("key file written despite StoreKey error; half-state committed")
	}
}

// TestStoreKey_KeyringSuccessClearsStaleFile covers the inverse case: file
// holds an old key from a prior fallback, then keyring becomes available and
// StoreKey writes a new key to keyring. The stale file must be cleared so a
// future keyring.Get failure does not return the obsolete file key.
func TestStoreKey_KeyringSuccessClearsStaleFile(t *testing.T) {
	withTestEnv(t)

	kOld, _ := GenerateKey()
	if err := StoreKeyFile(kOld, "test-passphrase"); err != nil {
		t.Fatalf("seed file with K_old: %v", err)
	}
	if !KeyFileExists() {
		t.Fatal("seed precondition: key file missing")
	}

	kNew, _ := GenerateKey()
	source, err := StoreKey(kNew)
	if err != nil {
		t.Fatalf("StoreKey: %v", err)
	}
	if source != SourceKeyring {
		t.Fatalf("expected SourceKeyring, got %q", source)
	}
	if KeyFileExists() {
		t.Fatal("stale key file not cleared after keyring write succeeded")
	}
}

// TestDeleteKey_FileOnlyHostIgnoresKeyringBackendError covers headless Linux:
// keyring backend is unreachable (D-Bus error, not ErrNotFound), file is the
// only real backend. DeleteKey must clear the file and return nil — the
// keyring "missing entry" is whole-backend missing, semantically identical
// to ErrNotFound for delete purposes.
func TestDeleteKey_FileOnlyHostIgnoresKeyringBackendError(t *testing.T) {
	withTestEnv(t)

	// Simulate D-Bus / Secret Service unavailable. Anything that is not
	// keyring.ErrNotFound exercises the regression path.
	backendDown := errors.New("dbus: org.freedesktop.secrets not provided")
	keyringGet = func(service, user string) (string, error) { return "", backendDown }

	deleteCalled := false
	keyringDelete = func(service, user string) error {
		deleteCalled = true
		return backendDown
	}

	// Seed file fallback (the only real backend on this host).
	id, _ := GenerateKey()
	if err := StoreKeyFile(id, "test-passphrase"); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	if err := DeleteKey(); err != nil {
		t.Fatalf("DeleteKey returned error on file-only host: %v", err)
	}
	if KeyFileExists() {
		t.Fatal("file fallback not deleted")
	}
	if deleteCalled {
		t.Fatal("keyringDelete should not be called when Get reports backend unavailable")
	}
}

// TestDeleteKey_RealKeyringErrorStillSurfaces ensures the relaxed contract
// does not mask genuine failures: if the entry exists (Get succeeds) but
// Delete fails, the error must propagate.
func TestDeleteKey_RealKeyringErrorStillSurfaces(t *testing.T) {
	withTestEnv(t)

	id, _ := GenerateKey()
	if err := keyring.Set(keychainService, keychainAccount, id.String()); err != nil {
		t.Fatalf("seed keyring: %v", err)
	}

	keyringDelete = func(service, user string) error {
		return errors.New("keyring locked")
	}

	if err := DeleteKey(); err == nil {
		t.Fatal("expected error when keyring entry exists and Delete fails")
	}
}

func TestLoadKey_NoKeySources(t *testing.T) {
	withTestEnv(t)

	if _, _, err := LoadKey(); err == nil {
		t.Fatal("expected error when no key in env, keyring, or file")
	} else if !contains(err.Error(), "no key found") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Sanity: file path used in error message points into temp dir.
	want := os.Getenv("ENCLAUDE_KEY_FILE")
	if want == "" {
		t.Fatal("ENCLAUDE_KEY_FILE not set by test harness")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
