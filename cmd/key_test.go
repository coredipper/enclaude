package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"
	"github.com/coredipper/enclaude/internal/store"
)

// trace records the order of side-effects so tests can assert that storeKey
// runs strictly before rotate, and that rotate is skipped when storeKey fails.
type rotateTrace struct {
	calls []string
}

func (t *rotateTrace) record(name string) { t.calls = append(t.calls, name) }

func mustGen(t *testing.T) *age.X25519Identity {
	t.Helper()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("age.GenerateX25519Identity: %v", err)
	}
	return id
}

// TestRotateKeyCore_PersistsBeforeRotate verifies the happy-path ordering:
// loadKey → genKey → storeKey → rotate. If storeKey ever runs after rotate,
// a storeKey failure would leave the store re-encrypted to a key that was
// never persisted (data loss).
func TestRotateKeyCore_PersistsBeforeRotate(t *testing.T) {
	tr := &rotateTrace{}
	oldID := mustGen(t)
	newID := mustGen(t)

	deps := keyRotateDeps{
		loadKey: func() (*age.X25519Identity, error) {
			tr.record("loadKey")
			return oldID, nil
		},
		genKey: func() (*age.X25519Identity, error) {
			tr.record("genKey")
			return newID, nil
		},
		storeKey: func(id *age.X25519Identity) error {
			tr.record("storeKey")
			if id != newID {
				t.Fatalf("storeKey called with wrong identity")
			}
			return nil
		},
		rotate: func(old *age.X25519Identity, newR *age.X25519Recipient) (int, error) {
			tr.record("rotate")
			if old != oldID {
				t.Fatal("rotate called with wrong old identity")
			}
			if newR.String() != newID.Recipient().String() {
				t.Fatal("rotate called with wrong new recipient")
			}
			return 7, nil
		},
	}

	gotOld, gotNew, n, err := rotateKeyCore(deps)
	if err != nil {
		t.Fatalf("rotateKeyCore: %v", err)
	}
	if gotOld != oldID || gotNew != newID || n != 7 {
		t.Fatalf("unexpected return values: old=%v new=%v n=%d", gotOld, gotNew, n)
	}

	want := []string{"loadKey", "genKey", "storeKey", "rotate"}
	if len(tr.calls) != len(want) {
		t.Fatalf("call sequence = %v, want %v", tr.calls, want)
	}
	for i, c := range want {
		if tr.calls[i] != c {
			t.Fatalf("call[%d] = %q, want %q (full: %v)", i, tr.calls[i], c, tr.calls)
		}
	}
}

// TestRotateKeyCore_StoreFailureSkipsRotate is the regression guard for the
// reported total-loss bug: when storeKey fails (e.g. user cancels passphrase
// prompt on file-fallback hosts), rotate must NOT be called — otherwise the
// seal store would be re-encrypted to a key that was never persisted.
func TestRotateKeyCore_StoreFailureSkipsRotate(t *testing.T) {
	tr := &rotateTrace{}
	storeErr := errors.New("user cancelled passphrase prompt")

	deps := keyRotateDeps{
		loadKey: func() (*age.X25519Identity, error) {
			tr.record("loadKey")
			return mustGen(t), nil
		},
		genKey: func() (*age.X25519Identity, error) {
			tr.record("genKey")
			return mustGen(t), nil
		},
		storeKey: func(*age.X25519Identity) error {
			tr.record("storeKey")
			return storeErr
		},
		rotate: func(*age.X25519Identity, *age.X25519Recipient) (int, error) {
			tr.record("rotate")
			t.Fatal("rotate must not be called when storeKey fails")
			return 0, nil
		},
	}

	_, _, _, err := rotateKeyCore(deps)
	if err == nil {
		t.Fatal("expected error when storeKey fails")
	}
	if !errors.Is(err, storeErr) {
		t.Fatalf("error chain missing storeErr: %v", err)
	}

	want := []string{"loadKey", "genKey", "storeKey"}
	if len(tr.calls) != len(want) {
		t.Fatalf("call sequence = %v, want %v (rotate must be absent)", tr.calls, want)
	}
}

// TestRotateKeyCore_RotationFailureRestoresOldKey guards the failure mode
// introduced by fail-fast rotation preflight: if the new key was persisted but
// rotation then fails before rewriting objects, key storage must be put back to
// the old key so existing objects remain decryptable.
func TestRotateKeyCore_RotationFailureRestoresOldKey(t *testing.T) {
	tr := &rotateTrace{}
	oldID := mustGen(t)
	newID := mustGen(t)
	rotateErr := errors.New("missing object")
	var stored []*age.X25519Identity

	deps := keyRotateDeps{
		loadKey: func() (*age.X25519Identity, error) {
			tr.record("loadKey")
			return oldID, nil
		},
		genKey: func() (*age.X25519Identity, error) {
			tr.record("genKey")
			return newID, nil
		},
		storeKey: func(id *age.X25519Identity) error {
			tr.record("storeKey:" + id.Recipient().String())
			stored = append(stored, id)
			return nil
		},
		rotate: func(*age.X25519Identity, *age.X25519Recipient) (int, error) {
			tr.record("rotate")
			return 0, errors.Join(store.ErrRotationStoreUnchanged, rotateErr)
		},
	}

	_, _, _, err := rotateKeyCore(deps)
	if err == nil {
		t.Fatal("expected rotation error")
	}
	if !errors.Is(err, store.ErrRotationStoreUnchanged) {
		t.Fatalf("error chain missing ErrRotationStoreUnchanged: %v", err)
	}
	if !errors.Is(err, rotateErr) {
		t.Fatalf("error chain missing rotateErr: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("stored %d keys, want new then old", len(stored))
	}
	if stored[0] != newID || stored[1] != oldID {
		t.Fatalf("store order = %v, want new identity then old identity", stored)
	}
	want := []string{
		"loadKey",
		"genKey",
		"storeKey:" + newID.Recipient().String(),
		"rotate",
		"storeKey:" + oldID.Recipient().String(),
	}
	if len(tr.calls) != len(want) {
		t.Fatalf("call sequence = %v, want %v", tr.calls, want)
	}
	for i, c := range want {
		if tr.calls[i] != c {
			t.Fatalf("call[%d] = %q, want %q (full: %v)", i, tr.calls[i], c, tr.calls)
		}
	}
}

// TestRotateKeyCore_RotationFailureKeepsNewKeyWhenStoreMayHaveChanged
// verifies late or ambiguous rotation errors do not restore the old key. If
// object writes may have survived, the newly persisted key is the safer state.
func TestRotateKeyCore_RotationFailureKeepsNewKeyWhenStoreMayHaveChanged(t *testing.T) {
	tr := &rotateTrace{}
	oldID := mustGen(t)
	newID := mustGen(t)
	rotateErr := errors.New("saving manifest")
	var stored []*age.X25519Identity

	deps := keyRotateDeps{
		loadKey: func() (*age.X25519Identity, error) {
			tr.record("loadKey")
			return oldID, nil
		},
		genKey: func() (*age.X25519Identity, error) {
			tr.record("genKey")
			return newID, nil
		},
		storeKey: func(id *age.X25519Identity) error {
			tr.record("storeKey:" + id.Recipient().String())
			stored = append(stored, id)
			return nil
		},
		rotate: func(*age.X25519Identity, *age.X25519Recipient) (int, error) {
			tr.record("rotate")
			return 2, rotateErr
		},
	}

	_, _, _, err := rotateKeyCore(deps)
	if err == nil {
		t.Fatal("expected rotation error")
	}
	if !errors.Is(err, rotateErr) {
		t.Fatalf("error chain missing rotateErr: %v", err)
	}
	if len(stored) != 1 || stored[0] != newID {
		t.Fatalf("stored = %v, want only new identity", stored)
	}
	want := []string{
		"loadKey",
		"genKey",
		"storeKey:" + newID.Recipient().String(),
		"rotate",
	}
	if len(tr.calls) != len(want) {
		t.Fatalf("call sequence = %v, want %v", tr.calls, want)
	}
	for i, c := range want {
		if tr.calls[i] != c {
			t.Fatalf("call[%d] = %q, want %q (full: %v)", i, tr.calls[i], c, tr.calls)
		}
	}
}

// TestRotateKeyCore_AmbiguousRotationPreservesRecoveryKeys verifies that when
// rotation reports an ambiguous object-store state, the core invokes the
// recovery-key hook with both identities and surfaces the recovery file path in
// the error, rather than discarding either key.
func TestRotateKeyCore_AmbiguousRotationPreservesRecoveryKeys(t *testing.T) {
	tr := &rotateTrace{}
	oldID := mustGen(t)
	newID := mustGen(t)
	rotateErr := errors.New("new-key recovery failed")
	var stored []*age.X25519Identity
	var preservedOld, preservedNew *age.X25519Identity

	deps := keyRotateDeps{
		loadKey: func() (*age.X25519Identity, error) {
			tr.record("loadKey")
			return oldID, nil
		},
		genKey: func() (*age.X25519Identity, error) {
			tr.record("genKey")
			return newID, nil
		},
		storeKey: func(id *age.X25519Identity) error {
			tr.record("storeKey:" + id.Recipient().String())
			stored = append(stored, id)
			return nil
		},
		rotate: func(*age.X25519Identity, *age.X25519Recipient) (int, error) {
			tr.record("rotate")
			return 1, errors.Join(store.ErrRotationStoreAmbiguous, rotateErr)
		},
		preserveRecoveryKeys: func(old, new *age.X25519Identity) (string, error) {
			tr.record("preserveRecoveryKeys")
			preservedOld, preservedNew = old, new
			return "/tmp/recovery.txt", nil
		},
	}

	_, _, _, err := rotateKeyCore(deps)
	if err == nil {
		t.Fatal("expected ambiguous rotation error")
	}
	if !errors.Is(err, store.ErrRotationStoreAmbiguous) {
		t.Fatalf("error chain missing ErrRotationStoreAmbiguous: %v", err)
	}
	if !errors.Is(err, rotateErr) {
		t.Fatalf("error chain missing rotateErr: %v", err)
	}
	if !strings.Contains(err.Error(), "/tmp/recovery.txt") {
		t.Fatalf("error should name recovery path: %v", err)
	}
	if len(stored) != 1 || stored[0] != newID {
		t.Fatalf("stored = %v, want only new identity", stored)
	}
	if preservedOld != oldID || preservedNew != newID {
		t.Fatalf("preserved keys = old %v new %v, want original old/new", preservedOld, preservedNew)
	}
	want := []string{
		"loadKey",
		"genKey",
		"storeKey:" + newID.Recipient().String(),
		"rotate",
		"preserveRecoveryKeys",
	}
	if len(tr.calls) != len(want) {
		t.Fatalf("call sequence = %v, want %v", tr.calls, want)
	}
	for i, c := range want {
		if tr.calls[i] != c {
			t.Fatalf("call[%d] = %q, want %q (full: %v)", i, tr.calls[i], c, tr.calls)
		}
	}
}

// TestWriteRotationRecoveryKeysFileCreatesExclusive0600File verifies the
// emergency recovery file is created with 0600 permissions and contains both
// the old and new private keys.
func TestWriteRotationRecoveryKeysFileCreatesExclusive0600File(t *testing.T) {
	oldID := mustGen(t)
	newID := mustGen(t)
	path := filepath.Join(t.TempDir(), "recovery.txt")

	if err := writeRotationRecoveryKeysFile(path, oldID, newID); err != nil {
		t.Fatalf("writeRotationRecoveryKeysFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat recovery file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("recovery file mode = %v, want 0600", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recovery file: %v", err)
	}
	text := string(data)
	if !strings.Contains(text, "old="+oldID.String()) || !strings.Contains(text, "new="+newID.String()) {
		t.Fatalf("recovery file missing expected keys: %q", text)
	}
}

// TestWriteRotationRecoveryKeysFileRefusesExistingPath guards the O_EXCL create:
// an existing path is left untouched rather than overwritten with key material.
func TestWriteRotationRecoveryKeysFileRefusesExistingPath(t *testing.T) {
	oldID := mustGen(t)
	newID := mustGen(t)
	path := filepath.Join(t.TempDir(), "recovery.txt")
	original := []byte("do not replace")
	if err := os.WriteFile(path, original, 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeRotationRecoveryKeysFile(path, oldID, newID); err == nil {
		t.Fatal("expected existing recovery path to fail")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read existing file: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("existing file was modified: got %q", got)
	}
}

// TestWriteRotationRecoveryKeysFileRefusesSymlinkPath guards against following a
// symlink at the recovery path, which would write key material to an
// attacker-chosen target outside the config dir.
func TestWriteRotationRecoveryKeysFileRefusesSymlinkPath(t *testing.T) {
	oldID := mustGen(t)
	newID := mustGen(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	link := filepath.Join(dir, "recovery.txt")
	original := []byte("outside target")
	if err := os.WriteFile(target, original, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	if err := writeRotationRecoveryKeysFile(link, oldID, newID); err == nil {
		t.Fatal("expected symlink recovery path to fail")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read symlink target: %v", err)
	}
	if string(got) != string(original) {
		t.Fatalf("symlink target was modified: got %q", got)
	}
}

// TestRotateKeyCore_LoadFailureSkipsEverything is a sanity guard: if loadKey
// fails there is nothing to rotate and no new key to persist.
func TestRotateKeyCore_LoadFailureSkipsEverything(t *testing.T) {
	tr := &rotateTrace{}
	loadErr := errors.New("no key found")

	deps := keyRotateDeps{
		loadKey: func() (*age.X25519Identity, error) {
			tr.record("loadKey")
			return nil, loadErr
		},
		genKey: func() (*age.X25519Identity, error) {
			tr.record("genKey")
			return nil, nil
		},
		storeKey: func(*age.X25519Identity) error {
			tr.record("storeKey")
			return nil
		},
		rotate: func(*age.X25519Identity, *age.X25519Recipient) (int, error) {
			tr.record("rotate")
			return 0, nil
		},
	}

	if _, _, _, err := rotateKeyCore(deps); err == nil {
		t.Fatal("expected error when loadKey fails")
	}
	if len(tr.calls) != 1 || tr.calls[0] != "loadKey" {
		t.Fatalf("call sequence = %v, want only [loadKey]", tr.calls)
	}
}
