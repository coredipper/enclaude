package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/coredipper/enclaude/internal/config"
)

// fastPathFixture builds a minimal claude/seal directory pair with one file
// "data.txt" and returns the config plus the file path. Tests use this to
// drive Status / UnsealStatus through controlled manifest states.
func fastPathFixture(t *testing.T, content []byte) (*config.Config, string) {
	t.Helper()
	claudeDir := t.TempDir()
	sealDir := t.TempDir()

	filePath := filepath.Join(claudeDir, "data.txt")
	if err := os.WriteFile(filePath, content, 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Seal:    config.SealSection{ClaudeDir: claudeDir, SealDir: sealDir, DeviceID: "test"},
		Include: config.PatternSection{Patterns: []string{"*"}},
	}
	return cfg, filePath
}

// TestStatus_NoManifest_ReportsAllAsAdded guards against a panic that the
// fast path would otherwise raise on an uninitialized seal store: LoadManifest
// returns (nil, nil) when manifest.json is absent, and the fast path
// dereferences manifest.Files. Status must coerce nil to an empty manifest
// and report every on-disk file as Added.
func TestStatus_NoManifest_ReportsAllAsAdded(t *testing.T) {
	cfg, _ := fastPathFixture(t, []byte("hello world"))
	// No manifest is written to cfg.Seal.SealDir — simulate "init never ran".

	diff, err := Status(cfg)
	if err != nil {
		t.Fatalf("Status returned error on missing manifest: %v", err)
	}
	if len(diff.Added) != 1 || diff.Added[0] != "data.txt" {
		t.Fatalf("expected [data.txt] as Added, got %+v", diff)
	}
	if len(diff.Modified)+len(diff.Deleted) != 0 {
		t.Fatalf("expected no Modified/Deleted entries, got %+v", diff)
	}
}

// TestStatus_FastPath_SizeAndMtimeMatch_ReusesManifestHash verifies that when
// a file's size and nanosecond mtime match the manifest, Status reuses the
// stored hash without reading the file. We prove the file wasn't read by
// seeding the manifest with a sentinel hash that does NOT match the file's
// real content — a clean Status result means the fast path served the
// sentinel.
func TestStatus_FastPath_SizeAndMtimeMatch_ReusesManifestHash(t *testing.T) {
	cfg, filePath := fastPathFixture(t, []byte("hello world"))

	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}

	manifest := NewManifest("test")
	manifest.Files["data.txt"] = FileEntry{
		ContentHash:   "sentinel-not-the-real-hash",
		SizePlaintext: info.Size(),
		Mtime:         info.ModTime().UTC().Format(time.RFC3339),
		ModTimeNs:     info.ModTime().UnixNano(),
	}
	if err := manifest.Save(cfg.Seal.SealDir); err != nil {
		t.Fatal(err)
	}

	diff, err := Status(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Modified)+len(diff.Added)+len(diff.Deleted) != 0 {
		t.Fatalf("expected no diff, got %+v — fast path did not reuse manifest hash", diff)
	}
}

// TestStatus_FastPath_SizeDiffers_FallsThrough guards against a regression
// where the fast path would short-circuit on stale entries. With size
// mismatch, Status must hash the file and report it as Modified.
func TestStatus_FastPath_SizeDiffers_FallsThrough(t *testing.T) {
	cfg, filePath := fastPathFixture(t, []byte("hello world"))

	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}

	manifest := NewManifest("test")
	manifest.Files["data.txt"] = FileEntry{
		ContentHash:   "some-old-hash",
		SizePlaintext: info.Size() + 1, // deliberately wrong
		Mtime:         info.ModTime().UTC().Format(time.RFC3339),
		ModTimeNs:     info.ModTime().UnixNano(),
	}
	if err := manifest.Save(cfg.Seal.SealDir); err != nil {
		t.Fatal(err)
	}

	diff, err := Status(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Modified) != 1 || diff.Modified[0] != "data.txt" {
		t.Fatalf("expected data.txt modified, got %+v", diff)
	}
}

// TestStatus_FastPath_SubSecondMtimeDiffers_DetectsChange pins the
// nanosecond-precision behavior against the original same-RFC3339-second
// race. Two mtimes that share a calendar second but differ in
// sub-second nanoseconds must not be treated as equal by the fast path.
func TestStatus_FastPath_SubSecondMtimeDiffers_DetectsChange(t *testing.T) {
	cfg, filePath := fastPathFixture(t, []byte("hello world"))

	// Seed: place the file's mtime at T+500ms.
	tEarly := time.Date(2026, 5, 21, 12, 0, 0, 500_000_000, time.UTC)
	if err := os.Chtimes(filePath, tEarly, tEarly); err != nil {
		t.Fatal(err)
	}
	infoEarly, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	earlyNs := infoEarly.ModTime().UnixNano()

	// Rewrite with same-size but different content, then bump mtime to
	// T+999ms (same RFC3339 second).
	if err := os.WriteFile(filePath, []byte("HELLO WORLD"), 0644); err != nil {
		t.Fatal(err)
	}
	tLate := time.Date(2026, 5, 21, 12, 0, 0, 999_000_000, time.UTC)
	if err := os.Chtimes(filePath, tLate, tLate); err != nil {
		t.Fatal(err)
	}
	infoLate, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if infoLate.ModTime().UnixNano() == earlyNs {
		t.Skip("filesystem does not preserve sub-second mtime precision; cannot exercise sub-second case")
	}

	// Manifest reflects the original (T+500ms) state with the real hash of
	// the original content. RFC3339-truncated Mtime is identical to what
	// the second write produces, so the *string* check would falsely match.
	manifest := NewManifest("test")
	manifest.Files["data.txt"] = FileEntry{
		ContentHash:   ContentHash([]byte("hello world")),
		SizePlaintext: infoEarly.Size(),
		Mtime:         tEarly.UTC().Format(time.RFC3339),
		ModTimeNs:     earlyNs,
	}
	if err := manifest.Save(cfg.Seal.SealDir); err != nil {
		t.Fatal(err)
	}

	diff, err := Status(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Modified) != 1 || diff.Modified[0] != "data.txt" {
		t.Fatalf("expected sub-second change to be detected; got %+v", diff)
	}
}

// TestStatus_FastPath_SubMillisecondMtimeDiffers_DetectsChange pins the
// reason ModTimeNs (rather than ModTimeMs) is the right precision: two
// mtimes that share a UnixMilli value but differ in sub-millisecond
// nanoseconds must still register as different, otherwise a same-size
// content rewrite within the same millisecond would silently slip past
// the fast path on filesystems with sub-millisecond timestamp resolution.
func TestStatus_FastPath_SubMillisecondMtimeDiffers_DetectsChange(t *testing.T) {
	cfg, filePath := fastPathFixture(t, []byte("hello world"))

	// Two times inside the same millisecond (500ms) but at different nanos.
	tEarly := time.Date(2026, 5, 21, 12, 0, 0, 500_000_001, time.UTC)
	tLate := time.Date(2026, 5, 21, 12, 0, 0, 500_999_999, time.UTC)

	// Sanity: both round to the same UnixMilli, so a millisecond-only
	// comparison would have aliased them.
	if tEarly.UnixMilli() != tLate.UnixMilli() {
		t.Fatalf("test invariant broken: tEarly/tLate must share a UnixMilli")
	}
	if tEarly.UnixNano() == tLate.UnixNano() {
		t.Fatalf("test invariant broken: tEarly/tLate must differ in UnixNano")
	}

	if err := os.Chtimes(filePath, tEarly, tEarly); err != nil {
		t.Fatal(err)
	}
	infoEarly, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	earlyNs := infoEarly.ModTime().UnixNano()

	if err := os.WriteFile(filePath, []byte("HELLO WORLD"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filePath, tLate, tLate); err != nil {
		t.Fatal(err)
	}
	infoLate, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if infoLate.ModTime().UnixNano() == earlyNs {
		t.Skip("filesystem does not preserve sub-millisecond mtime precision; cannot exercise sub-ms case")
	}

	manifest := NewManifest("test")
	manifest.Files["data.txt"] = FileEntry{
		ContentHash:   ContentHash([]byte("hello world")),
		SizePlaintext: infoEarly.Size(),
		Mtime:         tEarly.UTC().Format(time.RFC3339),
		ModTimeNs:     earlyNs,
	}
	if err := manifest.Save(cfg.Seal.SealDir); err != nil {
		t.Fatal(err)
	}

	diff, err := Status(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Modified) != 1 || diff.Modified[0] != "data.txt" {
		t.Fatalf("expected sub-millisecond change to be detected; got %+v", diff)
	}
}

// TestStatus_LegacyManifestWithoutModTimeNs_FallsThrough verifies that
// manifests written by older versions (where ModTimeNs is zero) bypass the
// fast path entirely. The slow path then computes the real hash, which
// matches, so no spurious diff is reported.
func TestStatus_LegacyManifestWithoutModTimeNs_FallsThrough(t *testing.T) {
	content := []byte("hello world")
	cfg, filePath := fastPathFixture(t, content)

	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}

	manifest := NewManifest("test")
	manifest.Files["data.txt"] = FileEntry{
		ContentHash:   ContentHash(content),
		SizePlaintext: info.Size(),
		Mtime:         info.ModTime().UTC().Format(time.RFC3339),
		// ModTimeNs intentionally left zero to simulate a legacy manifest.
	}
	if err := manifest.Save(cfg.Seal.SealDir); err != nil {
		t.Fatal(err)
	}

	diff, err := Status(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Modified)+len(diff.Added)+len(diff.Deleted) != 0 {
		t.Fatalf("expected clean diff via slow-path fallback, got %+v", diff)
	}
}

// TestUnsealStatus_FastPath_SizeAndMtimeMatch_ReusesManifestHash mirrors
// the Status fast-path test for UnsealStatus. UnsealStatus computes a
// disk-hash map; when size+mtime match, that map should contain the
// manifest's stored hash, producing no diff.
func TestUnsealStatus_FastPath_SizeAndMtimeMatch_ReusesManifestHash(t *testing.T) {
	cfg, filePath := fastPathFixture(t, []byte("payload"))

	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}

	manifest := NewManifest("test")
	manifest.Files["data.txt"] = FileEntry{
		ContentHash:   "sentinel-not-the-real-hash",
		SizePlaintext: info.Size(),
		Mtime:         info.ModTime().UTC().Format(time.RFC3339),
		ModTimeNs:     info.ModTime().UnixNano(),
	}
	if err := manifest.Save(cfg.Seal.SealDir); err != nil {
		t.Fatal(err)
	}

	diff, err := UnsealStatus(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Modified)+len(diff.Added)+len(diff.Deleted) != 0 {
		t.Fatalf("expected no diff, got %+v — UnsealStatus fast path did not reuse manifest hash", diff)
	}
}

// TestUnsealStatus_LegacyManifestWithoutModTimeNs_FallsThrough is the
// UnsealStatus counterpart of the legacy-manifest test: zero ModTimeNs
// must skip the fast path and let the slow hashing path run.
func TestUnsealStatus_LegacyManifestWithoutModTimeNs_FallsThrough(t *testing.T) {
	content := []byte("payload")
	cfg, filePath := fastPathFixture(t, content)

	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}

	manifest := NewManifest("test")
	manifest.Files["data.txt"] = FileEntry{
		ContentHash:   ContentHash(content),
		SizePlaintext: info.Size(),
		Mtime:         info.ModTime().UTC().Format(time.RFC3339),
	}
	if err := manifest.Save(cfg.Seal.SealDir); err != nil {
		t.Fatal(err)
	}

	diff, err := UnsealStatus(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(diff.Modified)+len(diff.Added)+len(diff.Deleted) != 0 {
		t.Fatalf("expected clean diff via slow-path fallback, got %+v", diff)
	}
}

// TestStatus_LoadManifestError verifies that an invalid manifest JSON causes Status to return an error.
func TestStatus_LoadManifestError(t *testing.T) {
	claudeDir := t.TempDir()
	sealDir := t.TempDir()

	// Write an invalid manifest to trigger a LoadManifest error
	if err := os.WriteFile(filepath.Join(sealDir, "manifest.json"), []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Seal: config.SealSection{ClaudeDir: claudeDir, SealDir: sealDir, DeviceID: "test"},
	}

	_, err := Status(cfg)
	if err == nil {
		t.Fatal("expected an error when LoadManifest fails, got nil")
	}
}

// TestStatus_ScanFilesError verifies that file scanning errors (e.g., inaccessible directory) are returned by Status.
func TestStatus_ScanFilesError(t *testing.T) {
	claudeDir := t.TempDir()
	sealDir := t.TempDir()

	subDir := filepath.Join(claudeDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "file.txt"), []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}
	// Make subdirectory inaccessible to trigger an error during WalkDir
	if err := os.Chmod(subDir, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.Chmod(subDir, 0755) // Ensure cleanup works
	})

	cfg := &config.Config{
		Seal:    config.SealSection{ClaudeDir: claudeDir, SealDir: sealDir, DeviceID: "test"},
		Include: config.PatternSection{Patterns: []string{"sub/file.txt"}},
	}

	_, err := Status(cfg)
	if err == nil {
		t.Fatal("expected an error when ScanFiles fails, got nil")
	}
}

// TestStatus_ReadFileError verifies that if a file cannot be read, it is handled gracefully (omitted from current manifest).
func TestStatus_ReadFileError(t *testing.T) {
	claudeDir := t.TempDir()
	sealDir := t.TempDir()

	filePath := filepath.Join(claudeDir, "data.txt")
	// Make file completely unreadable but accessible by WalkDir
	if err := os.WriteFile(filePath, []byte("test"), 0000); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Seal:    config.SealSection{ClaudeDir: claudeDir, SealDir: sealDir, DeviceID: "test"},
		Include: config.PatternSection{Patterns: []string{"*"}},
	}

	// Create a dummy manifest where data.txt is present to ensure diff doesn't panic
	manifest := NewManifest("test")
	manifest.Files["data.txt"] = FileEntry{
		ContentHash:   ContentHash([]byte("old_test")),
		SizePlaintext: 8,
		ModTimeNs:     1,
	}
	if err := manifest.Save(sealDir); err != nil {
		t.Fatal(err)
	}

	diff, err := Status(cfg)
	if err != nil {
		t.Fatalf("unexpected error when a file fails to read: %v", err)
	}

	// Since data.txt couldn't be read, it won't be in the 'current' manifest,
	// so diff will mark it as deleted relative to the initial manifest.
	if len(diff.Deleted) != 1 {
		t.Fatalf("expected 1 deleted file (could not be read), got %d", len(diff.Deleted))
	}
}
