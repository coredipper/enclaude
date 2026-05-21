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
// a file's size and millisecond mtime match the manifest, Status reuses the
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
		Mtime:         time.UnixMilli(info.ModTime().UnixMilli()).UTC().Format(time.RFC3339),
		ModTimeMs:     info.ModTime().UnixMilli(),
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
		Mtime:         time.UnixMilli(info.ModTime().UnixMilli()).UTC().Format(time.RFC3339),
		ModTimeMs:     info.ModTime().UnixMilli(),
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
// millisecond-precision behavior. Two mtimes that share an RFC3339 second
// but differ in milliseconds must be treated as different, so a content
// change inside the same calendar second is not missed. Without
// ModTimeMs comparison this case silently slipped past the fast path.
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
	earlyMs := infoEarly.ModTime().UnixMilli()

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
	if infoLate.ModTime().UnixMilli() == earlyMs {
		t.Skip("filesystem does not preserve millisecond mtime precision; cannot exercise sub-second case")
	}

	// Manifest reflects the original (T+500ms) state with the real hash of
	// the original content. RFC3339-truncated Mtime is identical to what
	// the second write produces, so the *string* check would falsely match.
	manifest := NewManifest("test")
	manifest.Files["data.txt"] = FileEntry{
		ContentHash:   ContentHash([]byte("hello world")),
		SizePlaintext: infoEarly.Size(),
		Mtime:         tEarly.UTC().Format(time.RFC3339),
		ModTimeMs:     earlyMs,
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

// TestStatus_LegacyManifestWithoutModTimeMs_FallsThrough verifies that
// manifests written by older versions (where ModTimeMs is zero) bypass the
// fast path entirely. The slow path then computes the real hash, which
// matches, so no spurious diff is reported.
func TestStatus_LegacyManifestWithoutModTimeMs_FallsThrough(t *testing.T) {
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
		Mtime:         time.UnixMilli(info.ModTime().UnixMilli()).UTC().Format(time.RFC3339),
		// ModTimeMs intentionally left zero to simulate a legacy manifest.
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
		Mtime:         time.UnixMilli(info.ModTime().UnixMilli()).UTC().Format(time.RFC3339),
		ModTimeMs:     info.ModTime().UnixMilli(),
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

// TestUnsealStatus_LegacyManifestWithoutModTimeMs_FallsThrough is the
// UnsealStatus counterpart of the legacy-manifest test: zero ModTimeMs
// must skip the fast path and let the slow hashing path run.
func TestUnsealStatus_LegacyManifestWithoutModTimeMs_FallsThrough(t *testing.T) {
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
		Mtime:         time.UnixMilli(info.ModTime().UnixMilli()).UTC().Format(time.RFC3339),
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
