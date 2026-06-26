package cmd

import (
	"errors"
	"testing"

	"filippo.io/age"
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
			return 0, rotateErr
		},
	}

	_, _, _, err := rotateKeyCore(deps)
	if err == nil {
		t.Fatal("expected rotation error")
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
