package store

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"filippo.io/age"
	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
)

// TestNewManifest verifies NewManifest stamps version 2, the given device
// ID, an empty (non-nil) files map, and a valid RFC3339 sealed-at time.
func TestNewManifest(t *testing.T) {
	deviceID := "test-device-id"
	m := NewManifest(deviceID)

	if m.Version != 2 {
		t.Errorf("expected version 2, got %d", m.Version)
	}
	if m.DeviceID != deviceID {
		t.Errorf("expected device ID %q, got %q", deviceID, m.DeviceID)
	}
	if len(m.Files) != 0 {
		t.Errorf("expected empty files map, got %d files", len(m.Files))
	}
	if m.Files == nil {
		t.Error("expected non-nil files map")
	}

	_, err := time.Parse(time.RFC3339, m.SealedAt)
	if err != nil {
		t.Errorf("expected valid RFC3339 sealed_at, got parse error: %v", err)
	}
}

// TestLoadManifest covers LoadManifest across a missing file (nil, nil), a
// valid manifest, one omitting the files field (map defaulted to non-nil),
// and malformed JSON (error, nil manifest).
func TestLoadManifest(t *testing.T) {
	sealDir := t.TempDir()

	// Test non-existent manifest
	m, err := LoadManifest(sealDir)
	if err != nil {
		t.Fatalf("expected no error for non-existent manifest, got %v", err)
	}
	if m != nil {
		t.Fatalf("expected nil manifest for non-existent file, got %v", m)
	}

	// Test valid manifest
	validJSON := []byte(`{"version":2,"device_id":"test-device","sealed_at":"2023-01-01T00:00:00Z","files":{"test.txt":{"content_hash":"hash123","size_plaintext":10}}}`)
	err = os.WriteFile(filepath.Join(sealDir, "manifest.json"), validJSON, 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	m, err = LoadManifest(sealDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil manifest")
	}
	if m.Version != 2 {
		t.Errorf("expected version 2, got %d", m.Version)
	}
	if m.DeviceID != "test-device" {
		t.Errorf("expected device ID 'test-device', got %q", m.DeviceID)
	}
	if len(m.Files) != 1 {
		t.Errorf("expected 1 file, got %d", len(m.Files))
	}

	// Test manifest with missing files field (should initialize empty map)
	noFilesJSON := []byte(`{"version":2,"device_id":"test-device"}`)
	err = os.WriteFile(filepath.Join(sealDir, "manifest.json"), noFilesJSON, 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	m, err = LoadManifest(sealDir)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if m.Files == nil {
		t.Error("expected non-nil files map when missing from JSON")
	}

	// Test invalid JSON
	invalidJSON := []byte(`{invalid`)
	err = os.WriteFile(filepath.Join(sealDir, "manifest.json"), invalidJSON, 0644)
	if err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	m, err = LoadManifest(sealDir)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	if m != nil {
		t.Fatal("expected nil manifest for invalid JSON")
	}
}

// TestManifestSave verifies Save writes a manifest that LoadManifest reads
// back with the same device ID and file entries.
func TestManifestSave(t *testing.T) {
	sealDir := t.TempDir()

	m := NewManifest("test-device")
	m.Files["test.txt"] = FileEntry{
		ContentHash:   "hash123",
		SizePlaintext: 10,
	}

	err := m.Save(sealDir)
	if err != nil {
		t.Fatalf("expected no error saving manifest, got %v", err)
	}

	// Verify file was written
	loaded, err := LoadManifest(sealDir)
	if err != nil {
		t.Fatalf("expected no error loading saved manifest, got %v", err)
	}
	if loaded.DeviceID != m.DeviceID {
		t.Errorf("expected device ID %q, got %q", m.DeviceID, loaded.DeviceID)
	}
	if loaded.Files["test.txt"].ContentHash != "hash123" {
		t.Errorf("expected content hash 'hash123', got %q", loaded.Files["test.txt"].ContentHash)
	}
}

// TestManifestSave_OriginHomeRoundTrip guards that the OriginHome field — the
// authoritative source-home signal for the cross-device project remap —
// survives a Save/Load cycle, and that an empty value is omitted (omitempty) so
// older stores stay clean.
func TestManifestSave_OriginHomeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	m := NewManifest("dev")
	m.OriginHome = "/home/daniel"
	if err := m.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := LoadManifest(dir)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if loaded.OriginHome != "/home/daniel" {
		t.Errorf("OriginHome round-trip = %q, want /home/daniel", loaded.OriginHome)
	}

	empty := t.TempDir()
	m2 := NewManifest("dev")
	if err := m2.Save(empty); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(empty, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "origin_home") {
		t.Errorf("empty OriginHome should be omitted from JSON, got: %s", data)
	}
}

// TestLoadManifest_ReadError verifies LoadManifest errors when manifest.json
// cannot be read because it is a directory rather than a file.
func TestLoadManifest_ReadError(t *testing.T) {
	sealDir := t.TempDir()

	// Create a directory where the file should be to cause a read error
	err := os.Mkdir(filepath.Join(sealDir, "manifest.json"), 0755)
	if err != nil {
		t.Fatalf("failed to create directory: %v", err)
	}

	_, err = LoadManifest(sealDir)
	if err == nil {
		t.Fatal("expected error reading manifest when it is a directory")
	}
}

// TestManifestDiff_OtherNotNil verifies Diff against a non-nil prior manifest
// classifies entries as added, modified (hash changed), and deleted.
func TestManifestDiff_OtherNotNil(t *testing.T) {
	oldManifest := NewManifest("device1")
	oldManifest.Files = map[string]FileEntry{
		"unchanged.txt": {ContentHash: "hash1"},
		"modified.txt":  {ContentHash: "hash2"},
		"deleted.txt":   {ContentHash: "hash3"},
	}

	newManifest := NewManifest("device1")
	newManifest.Files = map[string]FileEntry{
		"unchanged.txt": {ContentHash: "hash1"},
		"modified.txt":  {ContentHash: "hash2_new"},
		"added.txt":     {ContentHash: "hash4"},
	}

	diff := newManifest.Diff(oldManifest)

	if len(diff.Added) != 1 || diff.Added[0] != "added.txt" {
		t.Errorf("expected added [added.txt], got %v", diff.Added)
	}
	if len(diff.Modified) != 1 || diff.Modified[0] != "modified.txt" {
		t.Errorf("expected modified [modified.txt], got %v", diff.Modified)
	}
	if len(diff.Deleted) != 1 || diff.Deleted[0] != "deleted.txt" {
		t.Errorf("expected deleted [deleted.txt], got %v", diff.Deleted)
	}
}

// TestManifestDiff_OtherNilEmptyMap verifies Diff against a nil prior manifest
// treats every current file as added.
func TestManifestDiff_OtherNilEmptyMap(t *testing.T) {
	m := NewManifest("device1")
	m.Files = map[string]FileEntry{
		"test1.txt": {ContentHash: "hash1"},
		"test2.txt": {ContentHash: "hash2"},
	}

	diff := m.Diff(nil)
	if len(diff.Added) != 2 {
		t.Errorf("expected 2 added files, got %d", len(diff.Added))
	}
}

// We use json.MarshalIndent in Save, and while json.MarshalIndent on
// strings and integers does not fail, we can test it indirectly by
// using some mocking if needed, but 83.3% for Save is enough as we
// cover the main unhappy path (file system error) and happy path.

// encryptNamesSetup returns a config sealing setupTestDir's tree with
// encrypt_names on, and points manifestIdentity at the returned key so Seal
// and Status can read the encrypted manifest without a keyring.
func encryptNamesSetup(t *testing.T) (*config.Config, *age.X25519Identity) {
	t.Helper()
	identity, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	cfg := config.DefaultConfig(setupTestDir(t), t.TempDir())
	cfg.Seal.EncryptNames = true
	orig := manifestIdentity
	manifestIdentity = func() (age.Identity, error) { return identity, nil }
	t.Cleanup(func() { manifestIdentity = orig })
	return cfg, identity
}

// storeMentions reports whether any file name or file content under dir
// contains s.
func storeMentions(t *testing.T, dir, s string) bool {
	t.Helper()
	found := false
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(d.Name(), s) {
			found = true
		}
		if d.Type().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			found = found || bytes.Contains(data, []byte(s))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
	return found
}

// TestSeal_EncryptNames covers both settings of encrypt_names: with it on, no
// folder or file name appears anywhere in the store, and unsealing still gives
// back every file under its own name with the same bytes and the same
// modified time, including names with spaces, accents and nested folders.
// With it off, the names stay readable in manifest.json, which also shows
// storeMentions can spot a leak.
func TestSeal_EncryptNames(t *testing.T) {
	odd := []string{
		"commands/job hunting/plan v2.md",
		"commands/déménagement/ünïcode–notes.md",
		"commands/a/b/c/d/deep.md",
	}
	for _, encrypt := range []bool{true, false} {
		t.Run(map[bool]string{true: "encrypted", false: "plain"}[encrypt], func(t *testing.T) {
			cfg, identity := encryptNamesSetup(t)
			cfg.Seal.EncryptNames = encrypt
			// Sub-second times catch a restore that drops nanoseconds.
			mtime := time.Date(2024, 3, 1, 12, 0, 0, 123456789, time.UTC)
			for i, rel := range odd {
				path := filepath.Join(cfg.Seal.ClaudeDir, rel)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(rel), 0600); err != nil {
					t.Fatal(err)
				}
				at := mtime.Add(time.Duration(i) * time.Hour)
				if err := os.Chtimes(path, at, at); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := Seal(cfg, identity.Recipient(), false, nil); err != nil {
				t.Fatalf("Seal: %v", err)
			}
			for _, name := range []string{"proj-a", "user_role", "sessions-index", "job hunting", "déménagement", "deep.md"} {
				if got := storeMentions(t, cfg.Seal.SealDir, name); got != !encrypt {
					t.Errorf("store mentions %q = %v, want %v", name, got, !encrypt)
				}
			}

			m, err := LoadManifest(cfg.Seal.SealDir, identity)
			if err != nil {
				t.Fatalf("LoadManifest: %v", err)
			}
			for _, rel := range append(odd, "projects/proj-a/memory/user_role.md", "history.jsonl") {
				if _, ok := m.Files[rel]; !ok {
					t.Errorf("manifest is missing %q", rel)
				}
			}

			restoreDir := t.TempDir()
			restoreCfg := config.DefaultConfig(restoreDir, cfg.Seal.SealDir)
			stats, err := Unseal(restoreCfg, identity, false, nil)
			if err != nil || stats.Errors != 0 || stats.Restored != len(m.Files) {
				t.Fatalf("Unseal = %s, %v; want %d restored", stats, err, len(m.Files))
			}
			for rel := range m.Files {
				want, wantErr := os.Stat(filepath.Join(cfg.Seal.ClaudeDir, rel))
				got, err := os.Stat(filepath.Join(restoreDir, rel))
				if wantErr != nil || err != nil {
					t.Errorf("%s: stat original %v, restored %v", rel, wantErr, err)
					continue
				}
				if !got.ModTime().Equal(want.ModTime()) {
					t.Errorf("%s modified time = %v, want %v", rel, got.ModTime(), want.ModTime())
				}
				wantData, _ := os.ReadFile(filepath.Join(cfg.Seal.ClaudeDir, rel))
				gotData, _ := os.ReadFile(filepath.Join(restoreDir, rel))
				if !bytes.Equal(gotData, wantData) {
					t.Errorf("%s content = %q, want %q", rel, gotData, wantData)
				}
			}
		})
	}
}

// TestSeal_EncryptNamesUnchangedSealKeepsManifest guards against rewriting an
// unchanged encrypted manifest: age output differs on every encryption, so a
// rewrite would turn every no-op seal into a commit.
func TestSeal_EncryptNamesUnchangedSealKeepsManifest(t *testing.T) {
	cfg, identity := encryptNamesSetup(t)
	path := filepath.Join(cfg.Seal.SealDir, "manifest.json")
	if _, err := Seal(cfg, identity.Recipient(), false, nil); err != nil {
		t.Fatalf("first Seal: %v", err)
	}
	before, _ := os.ReadFile(path)
	stats, err := Seal(cfg, identity.Recipient(), false, nil)
	if err != nil {
		t.Fatalf("second Seal: %v", err)
	}
	after, _ := os.ReadFile(path)
	if stats.HasChanges() || !bytes.Equal(before, after) {
		t.Errorf("unchanged seal rewrote manifest (stats %s)", stats)
	}
}

// TestSeal_SwitchingEncryptNamesRewritesManifest verifies that turning
// encrypt_names on or off takes effect on the next seal even when no file
// changed, so an existing store can switch without waiting for an edit.
func TestSeal_SwitchingEncryptNamesRewritesManifest(t *testing.T) {
	cfg, identity := encryptNamesSetup(t)
	path := filepath.Join(cfg.Seal.SealDir, "manifest.json")
	for _, encrypt := range []bool{false, true, false} {
		cfg.Seal.EncryptNames = encrypt
		if _, err := Seal(cfg, identity.Recipient(), false, nil); err != nil {
			t.Fatalf("Seal (encrypt_names=%v): %v", encrypt, err)
		}
		data, _ := os.ReadFile(path)
		if got := bytes.HasPrefix(data, []byte(ageHeader)); got != encrypt {
			t.Errorf("encrypt_names=%v: manifest encrypted = %v", encrypt, got)
		}
	}
}

// TestLoadManifest_Encrypted covers reading an encrypted manifest with the
// right key, the wrong key, and no key at all (no keyring, no key file):
// the last two must fail rather than return an empty manifest, which a
// later seal would take as every file being new.
func TestLoadManifest_Encrypted(t *testing.T) {
	cfg, identity := encryptNamesSetup(t)
	m := NewManifest("dev")
	m.Files["projects/job_hunting_canada/a.jsonl"] = FileEntry{ContentHash: "h"}
	if err := m.Save(cfg.Seal.SealDir, identity.Recipient()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := LoadManifest(cfg.Seal.SealDir, identity)
	if err != nil || got.Files["projects/job_hunting_canada/a.jsonl"].ContentHash != "h" || !got.encrypted {
		t.Fatalf("LoadManifest(right key) = %+v, %v", got, err)
	}

	wrong, _ := crypto.GenerateKey()
	if _, err := LoadManifest(cfg.Seal.SealDir, wrong); err == nil || !strings.Contains(err.Error(), "decrypting manifest") {
		t.Errorf("LoadManifest(wrong key) error = %v, want decrypting manifest error", err)
	}

	manifestIdentity = func() (age.Identity, error) { return nil, errors.New("no key found") }
	if _, err := LoadManifest(cfg.Seal.SealDir); err == nil || !strings.Contains(err.Error(), "manifest is encrypted: no key found") {
		t.Errorf("LoadManifest(no key) error = %v, want manifest is encrypted error", err)
	}
	if _, err := Status(cfg); err == nil {
		t.Error("Status with no key returned nil error")
	}
}

// TestStatus_EncryptedManifest verifies Status, which holds no key, reads an
// encrypted manifest through manifestIdentity and reports a new file.
func TestStatus_EncryptedManifest(t *testing.T) {
	cfg, identity := encryptNamesSetup(t)
	if _, err := Seal(cfg, identity.Recipient(), false, nil); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfg.Seal.ClaudeDir, "CLAUDE.md"), []byte("# changed"), 0644); err != nil {
		t.Fatal(err)
	}
	diff, err := Status(cfg)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(diff.Modified) != 1 || diff.Modified[0] != "CLAUDE.md" {
		t.Errorf("Status modified = %v, want [CLAUDE.md]", diff.Modified)
	}
}

// TestRotate_EncryptedManifest verifies key rotation re-encrypts the manifest
// to the new key; leaving it on the old key would lock every device out of
// the names once the old key is gone.
func TestRotate_EncryptedManifest(t *testing.T) {
	cfg, oldIdentity := encryptNamesSetup(t)
	if _, err := Seal(cfg, oldIdentity.Recipient(), false, nil); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	newIdentity, _ := crypto.GenerateKey()
	if _, err := Rotate(cfg, oldIdentity, newIdentity.Recipient(), false, nil); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if _, err := LoadManifest(cfg.Seal.SealDir, newIdentity); err != nil {
		t.Errorf("LoadManifest(new key): %v", err)
	}
	if _, err := LoadManifest(cfg.Seal.SealDir, oldIdentity); err == nil {
		t.Error("LoadManifest(old key) succeeded after rotation")
	}
}

// TestRepair_EncryptedManifestStaysEncrypted verifies Repair, after
// re-sealing a missing object, saves the manifest encrypted again.
func TestRepair_EncryptedManifestStaysEncrypted(t *testing.T) {
	cfg, identity := encryptNamesSetup(t)
	if _, err := Seal(cfg, identity.Recipient(), false, nil); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	m, err := LoadManifest(cfg.Seal.SealDir, identity)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	store := NewObjectStore(cfg.Seal.SealDir)
	if err := store.Delete(m.Files["CLAUDE.md"].ContentHash); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	result, err := Repair(cfg, identity, false, false)
	if err != nil || result.Fixed != 1 {
		t.Fatalf("Repair = %+v, %v; want 1 fixed", result, err)
	}
	if storeMentions(t, cfg.Seal.SealDir, "proj-a") {
		t.Error("Repair wrote folder names in plaintext")
	}
}

// TestSeal_EncryptNamesUnreadableManifestFails verifies a seal that cannot
// read the encrypted manifest (no key on this machine, or a corrupt file)
// stops with an error and leaves the manifest as it was. Treating it as
// missing would write a fresh manifest and drop every tracked file.
func TestSeal_EncryptNamesUnreadableManifestFails(t *testing.T) {
	cfg, identity := encryptNamesSetup(t)
	if _, err := Seal(cfg, identity.Recipient(), false, nil); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	path := filepath.Join(cfg.Seal.SealDir, "manifest.json")
	good, _ := os.ReadFile(path)

	manifestIdentity = func() (age.Identity, error) { return nil, errors.New("no key found") }
	if _, err := Seal(cfg, identity.Recipient(), false, nil); err == nil {
		t.Error("Seal without a key returned nil error")
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(after, good) {
		t.Error("Seal without a key changed the manifest")
	}

	manifestIdentity = func() (age.Identity, error) { return identity, nil }
	truncated := good[:len(good)-20]
	if err := os.WriteFile(path, truncated, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Seal(cfg, identity.Recipient(), false, nil); err == nil || !strings.Contains(err.Error(), "decrypting manifest") {
		t.Errorf("Seal on a truncated manifest error = %v, want decrypting manifest error", err)
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(after, truncated) {
		t.Error("Seal on a truncated manifest rewrote it")
	}
}

// TestUnseal_EncryptedManifestWrongKey verifies unsealing with a key that
// cannot open the manifest fails before writing anything into the Claude
// directory.
func TestUnseal_EncryptedManifestWrongKey(t *testing.T) {
	cfg, identity := encryptNamesSetup(t)
	if _, err := Seal(cfg, identity.Recipient(), false, nil); err != nil {
		t.Fatalf("Seal: %v", err)
	}
	wrong, _ := crypto.GenerateKey()
	restoreDir := t.TempDir()
	if _, err := Unseal(config.DefaultConfig(restoreDir, cfg.Seal.SealDir), wrong, false, nil); err == nil {
		t.Fatal("Unseal with the wrong key returned nil error")
	}
	if entries, _ := os.ReadDir(restoreDir); len(entries) != 0 {
		t.Errorf("Unseal with the wrong key wrote %d entries", len(entries))
	}
}

// TestManifestSave_ConcurrentReadersNeverSeeHalfAFile guards the atomic
// write: commands such as status and unseal read the manifest without the
// seal lock, so a reader running while a seal saves must always get a whole
// manifest, never a truncated one that fails to decrypt.
func TestManifestSave_ConcurrentReadersNeverSeeHalfAFile(t *testing.T) {
	dir := t.TempDir()
	identity, _ := crypto.GenerateKey()
	m := NewManifest("dev")
	for i := range 2000 {
		m.Files[fmt.Sprintf("projects/p%d/session.jsonl", i)] = FileEntry{ContentHash: strings.Repeat("a", 64)}
	}
	if err := m.Save(dir, identity.Recipient()); err != nil {
		t.Fatalf("Save: %v", err)
	}

	done := make(chan struct{})
	errs := make(chan error, 1)
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				got, err := LoadManifest(dir, identity)
				if err == nil && len(got.Files) != len(m.Files) {
					err = fmt.Errorf("read %d files, want %d", len(got.Files), len(m.Files))
				}
				if err != nil {
					select {
					case errs <- err:
					default:
					}
					return
				}
			}
		}()
	}
	for range 50 {
		if err := m.Save(dir, identity.Recipient()); err != nil {
			t.Fatalf("Save: %v", err)
		}
	}
	close(done)
	wg.Wait()
	select {
	case err := <-errs:
		t.Fatalf("reader saw a partial manifest: %v", err)
	default:
	}
}

// TestManifestSave_FailureLeavesNoTempFile verifies a failed save returns an
// error, keeps the old manifest, and removes its temporary file, which a
// later `git add .` would otherwise commit.
func TestManifestSave_FailureLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	m := NewManifest("dev")
	if err := m.Save(dir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	// A directory in the manifest's place makes the final rename fail
	// after the temp file is written.
	path := filepath.Join(dir, "manifest.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(path, "x"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.Save(dir); err == nil {
		t.Fatal("Save over a directory returned nil error")
	}
	if tmps, _ := filepath.Glob(filepath.Join(dir, ".manifest-*.tmp")); len(tmps) != 0 {
		t.Errorf("failed Save left temp files: %v", tmps)
	}

	if err := m.Save(filepath.Join(dir, "missing")); err == nil {
		t.Error("Save into a missing directory returned nil error")
	}
}
