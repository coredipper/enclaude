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
