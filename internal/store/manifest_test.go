package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewManifest(t *testing.T) {
	deviceID := "test-device-123"
	m := NewManifest(deviceID)

	if m.DeviceID != deviceID {
		t.Errorf("expected DeviceID %q, got %q", deviceID, m.DeviceID)
	}
	if m.Version != 2 {
		t.Errorf("expected Version 2, got %d", m.Version)
	}
	if m.Files == nil {
		t.Error("expected Files map to be initialized, got nil")
	}
	if m.SealedAt == "" {
		t.Error("expected SealedAt to be set, got empty string")
	}

	// Validate SealedAt parses as RFC3339
	if _, err := time.Parse(time.RFC3339, m.SealedAt); err != nil {
		t.Errorf("expected SealedAt to be valid RFC3339, got error: %v", err)
	}
}

func TestLoadManifest(t *testing.T) {
	t.Run("missing manifest", func(t *testing.T) {
		dir := t.TempDir()
		m, err := LoadManifest(dir)
		if err != nil {
			t.Errorf("expected no error for missing manifest, got %v", err)
		}
		if m != nil {
			t.Errorf("expected nil manifest for missing file, got %v", m)
		}
	})

	t.Run("valid manifest", func(t *testing.T) {
		dir := t.TempDir()
		m := NewManifest("test-device")
		m.Files["test.txt"] = FileEntry{ContentHash: "abc", SizePlaintext: 10}

		data, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("failed to marshal manifest: %v", err)
		}

		err = os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0644)
		if err != nil {
			t.Fatalf("failed to write manifest: %v", err)
		}

		loaded, err := LoadManifest(dir)
		if err != nil {
			t.Errorf("expected no error loading valid manifest, got %v", err)
		}
		if loaded == nil {
			t.Fatal("expected manifest to be loaded, got nil")
		}
		if loaded.DeviceID != "test-device" {
			t.Errorf("expected device ID %q, got %q", "test-device", loaded.DeviceID)
		}
		if len(loaded.Files) != 1 {
			t.Errorf("expected 1 file, got %d", len(loaded.Files))
		}
		if loaded.Files["test.txt"].ContentHash != "abc" {
			t.Errorf("expected hash %q, got %q", "abc", loaded.Files["test.txt"].ContentHash)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		dir := t.TempDir()
		err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{invalid json"), 0644)
		if err != nil {
			t.Fatalf("failed to write invalid manifest: %v", err)
		}

		_, err = LoadManifest(dir)
		if err == nil {
			t.Error("expected error loading invalid json, got nil")
		}
	})

	t.Run("missing files map initializes empty map", func(t *testing.T) {
		dir := t.TempDir()
		// Write valid JSON but without the "files" key
		err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"version":2, "device_id":"test"}`), 0644)
		if err != nil {
			t.Fatalf("failed to write manifest: %v", err)
		}

		loaded, err := LoadManifest(dir)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
		if loaded.Files == nil {
			t.Error("expected Files map to be initialized, got nil")
		}
	})
}

func TestManifestSave(t *testing.T) {
	dir := t.TempDir()
	m := NewManifest("test-device")

	// Modify SealedAt so we don't have to sleep a full second
	m.SealedAt = time.Now().UTC().Add(-2 * time.Second).Format(time.RFC3339)
	beforeSave, _ := time.Parse(time.RFC3339, m.SealedAt)
	// Wait briefly to ensure SealedAt changes

	err := m.Save(dir)
	if err != nil {
		t.Fatalf("expected no error saving manifest, got %v", err)
	}

	// Verify SealedAt was updated
	sealedAt, err := time.Parse(time.RFC3339, m.SealedAt)
	if err != nil {
		t.Fatalf("expected valid RFC3339 SealedAt, got error: %v", err)
	}
	if !sealedAt.After(beforeSave) {
		t.Errorf("expected SealedAt %v to be after %v", sealedAt, beforeSave)
	}

	// Verify file was written
	path := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected to read saved manifest, got error: %v", err)
	}

	var loaded Manifest
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("expected valid json in saved manifest, got error: %v", err)
	}
	if loaded.DeviceID != "test-device" {
		t.Errorf("expected loaded device ID %q, got %q", "test-device", loaded.DeviceID)
	}
}

func TestManifest_Diff(t *testing.T) {
	t.Run("diff with nil", func(t *testing.T) {
		m := NewManifest("test")
		m.Files["a.txt"] = FileEntry{ContentHash: "hash-a"}
		m.Files["b.txt"] = FileEntry{ContentHash: "hash-b"}

		diff := m.Diff(nil)
		if len(diff.Added) != 2 {
			t.Errorf("expected 2 added files, got %d", len(diff.Added))
		}
		if len(diff.Modified) != 0 || len(diff.Deleted) != 0 {
			t.Errorf("expected no modified or deleted files, got M:%d, D:%d", len(diff.Modified), len(diff.Deleted))
		}
	})

	t.Run("diff identical", func(t *testing.T) {
		m1 := NewManifest("test")
		m1.Files["a.txt"] = FileEntry{ContentHash: "hash-a"}

		m2 := NewManifest("test")
		m2.Files["a.txt"] = FileEntry{ContentHash: "hash-a"}

		diff := m1.Diff(m2)
		if len(diff.Added) != 0 || len(diff.Modified) != 0 || len(diff.Deleted) != 0 {
			t.Errorf("expected empty diff, got A:%d, M:%d, D:%d", len(diff.Added), len(diff.Modified), len(diff.Deleted))
		}
	})

	t.Run("diff changes", func(t *testing.T) {
		oldM := NewManifest("test")
		oldM.Files["kept.txt"] = FileEntry{ContentHash: "hash-kept"}
		oldM.Files["modified.txt"] = FileEntry{ContentHash: "hash-old"}
		oldM.Files["deleted.txt"] = FileEntry{ContentHash: "hash-deleted"}

		newM := NewManifest("test")
		newM.Files["kept.txt"] = FileEntry{ContentHash: "hash-kept"}
		newM.Files["modified.txt"] = FileEntry{ContentHash: "hash-new"}
		newM.Files["added.txt"] = FileEntry{ContentHash: "hash-added"}

		diff := newM.Diff(oldM)

		if len(diff.Added) != 1 || diff.Added[0] != "added.txt" {
			t.Errorf("expected added.txt in Added, got %v", diff.Added)
		}
		if len(diff.Modified) != 1 || diff.Modified[0] != "modified.txt" {
			t.Errorf("expected modified.txt in Modified, got %v", diff.Modified)
		}
		if len(diff.Deleted) != 1 || diff.Deleted[0] != "deleted.txt" {
			t.Errorf("expected deleted.txt in Deleted, got %v", diff.Deleted)
		}
	})
}
