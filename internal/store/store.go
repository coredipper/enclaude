package store

import (
	"bytes"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"filippo.io/age"
	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/merge"
	"github.com/coredipper/enclaude/internal/session"
)

// SessionStats counts session JSONL files under projects/ and how they
// changed relative to the previous manifest.
type SessionStats struct {
	Tracked int // session JSONLs in the new manifest
	New     int // session JSONLs not present in the prior manifest
	Updated int // session JSONLs whose JSONLLineCount grew since prior manifest
}

// SealStats tracks what happened during a seal operation.
type SealStats struct {
	Scanned        int
	Added          int
	Modified       int
	Deleted        int
	Unchanged      int
	Errors         int
	BytesPlaintext int64
	BytesEncrypted int64
	Sessions       SessionStats
	Elapsed        time.Duration
}

// HasChanges returns true if the seal produced any modifications worth committing.
func (s SealStats) HasChanges() bool {
	return s.Added > 0 || s.Modified > 0 || s.Deleted > 0
}

// String returns a compact single-line summary suitable for commit messages.
// Multiline() is the right choice for terminal display.
func (s SealStats) String() string {
	if s.Deleted > 0 {
		return fmt.Sprintf("scanned %d files: %d new, %d modified, %d deleted, %d unchanged",
			s.Scanned, s.Added, s.Modified, s.Deleted, s.Unchanged)
	}
	return fmt.Sprintf("scanned %d files: %d new, %d modified, %d unchanged",
		s.Scanned, s.Added, s.Modified, s.Unchanged)
}

// Multiline returns a 2–3 line summary for sync display: scan counters,
// data volume + elapsed, and session activity (when any sessions tracked).
// Each line is prefixed with the given indent.
func (s SealStats) Multiline(indent string) string {
	var b strings.Builder
	b.WriteString(indent)
	b.WriteString(s.String())
	if s.BytesPlaintext > 0 || s.BytesEncrypted > 0 {
		b.WriteByte('\n')
		b.WriteString(indent)
		fmt.Fprintf(&b, "%s plaintext → %s encrypted (%s)",
			FormatSize(s.BytesPlaintext), FormatSize(s.BytesEncrypted), formatElapsed(s.Elapsed))
	}
	if s.Sessions.Tracked > 0 {
		b.WriteByte('\n')
		b.WriteString(indent)
		fmt.Fprintf(&b, "%d sessions tracked, %d new, %d updated since last sync",
			s.Sessions.Tracked, s.Sessions.New, s.Sessions.Updated)
	}
	return b.String()
}

// UnsealStats tracks what happened during an unseal operation.
type UnsealStats struct {
	Total          int
	Restored       int
	Unchanged      int
	Deleted        int
	Errors         int
	BytesDecrypted int64
	Merges         merge.Aggregate
	Elapsed        time.Duration
}

// String returns a compact single-line summary.
func (s UnsealStats) String() string {
	if s.Deleted > 0 {
		return fmt.Sprintf("%d files: %d restored, %d unchanged, %d deleted",
			s.Total, s.Restored, s.Unchanged, s.Deleted)
	}
	return fmt.Sprintf("%d files: %d restored, %d unchanged",
		s.Total, s.Restored, s.Unchanged)
}

// Multiline returns the single-line summary plus an optional merge-activity
// line when the preceding pull invoked the merge driver.
func (s UnsealStats) Multiline(indent string) string {
	var b strings.Builder
	b.WriteString(indent)
	b.WriteString(s.String())
	if s.BytesDecrypted > 0 {
		fmt.Fprintf(&b, " (%s plaintext, %s)", FormatSize(s.BytesDecrypted), formatElapsed(s.Elapsed))
	}
	if s.Merges.FilesMerged > 0 {
		b.WriteByte('\n')
		b.WriteString(indent)
		fmt.Fprintf(&b, "merged %d files (%d dup lines removed, %d sessions deduped)",
			s.Merges.FilesMerged, s.Merges.LinesDeduped, s.Merges.SessionsDeduped)
	}
	return b.String()
}

// formatElapsed prints a duration with one significant fractional digit for
// sub-minute durations ("1.4s"), and a coarser format above that.
func formatElapsed(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return d.Round(time.Second).String()
}

// ProgressFunc is called during long operations to report progress.
type ProgressFunc func(current, total int, path string)

// Seal encrypts changed files from claudeDir into the seal store.
func Seal(cfg *config.Config, recipient age.Recipient, verbose bool, progress ProgressFunc) (SealStats, error) {
	start := time.Now()
	var stats SealStats
	sealDir := cfg.Seal.SealDir

	store := NewObjectStore(sealDir)
	if err := store.Init(); err != nil {
		return stats, fmt.Errorf("initializing object store: %w", err)
	}

	// Load existing manifest (may be nil on first seal). Snapshot the prior
	// file entries so we can compute session deltas without paying for a
	// second pass after the seal mutates the manifest in place.
	manifest, err := LoadManifest(sealDir)
	if err != nil {
		return stats, fmt.Errorf("loading manifest: %w", err)
	}
	prior := map[string]FileEntry{}
	existed := manifest != nil
	var priorDeviceID, priorOriginHome string
	if manifest == nil {
		manifest = NewManifest(cfg.Seal.DeviceID)
	} else {
		maps.Copy(prior, manifest.Files)
		priorDeviceID, priorOriginHome = manifest.DeviceID, manifest.OriginHome
	}
	manifest.DeviceID = cfg.Seal.DeviceID
	manifest.OriginHome = homeDir(cfg.Seal.ClaudeDir)

	// Active session IDs (PID-alive). Empty when the sessions dir is absent
	// (e.g. tests) — falls back to path-based completion semantics.
	activeSessions := activeSessionIDs(cfg.Seal.ClaudeDir)

	// Scan files
	files, err := ScanFiles(cfg.Seal.ClaudeDir, cfg.Include.Patterns, cfg.Exclude.Patterns)
	if err != nil {
		return stats, fmt.Errorf("scanning files: %w", err)
	}
	stats.Scanned = len(files)

	// A zero-file scan against a populated manifest is almost always a wrong
	// claude_dir (a synced foreign path, a typo'd --claude-dir) — sealing it
	// would track every entry as deleted, and a later unseal turns that into
	// real deletions on other machines. Refuse rather than wipe.
	if len(files) == 0 && len(manifest.Files) > 0 {
		return stats, fmt.Errorf("scanned 0 files under %s but the manifest tracks %d — refusing to seal what would delete every entry; check claude_dir (or --claude-dir), or delete the seal store and re-init if this is intentional",
			cfg.Seal.ClaudeDir, len(manifest.Files))
	}

	// Track which files still exist (for deletion detection)
	seen := make(map[string]bool)

	for i, f := range files {
		if progress != nil {
			progress(i+1, len(files), f.RelPath)
		}
		seen[f.RelPath] = true

		processFile(f, manifest, store, recipient, cfg, activeSessions, verbose, &stats)
	}

	trackDeleted(manifest, seen, verbose, &stats)
	tallySessions(manifest, prior, &stats)

	// Persist only when something real changed (content, first seal, or a
	// DeviceID/OriginHome refresh): Save rewrites sealed_at, and a
	// timestamp-only rewrite hands the staged-diff commit gate a manifest
	// change, turning every scheduled no-op seal into a commit.
	if stats.HasChanges() || !existed ||
		manifest.DeviceID != priorDeviceID || manifest.OriginHome != priorOriginHome {
		if err := manifest.Save(sealDir); err != nil {
			return stats, fmt.Errorf("saving manifest: %w", err)
		}
	}

	stats.Elapsed = time.Since(start)
	return stats, nil
}

func processFile(f ScanResult, manifest *Manifest, store *ObjectStore, recipient age.Recipient, cfg *config.Config, activeSessions map[string]bool, verbose bool, stats *SealStats) {
	// Fast path optimization for Seal
	if entry, ok := manifest.Files[f.RelPath]; ok && entry.ModTimeNs != 0 {
		if entry.SizePlaintext == f.Size && entry.ModTimeNs == f.ModTimeNs {
			if store.Exists(entry.ContentHash) {
				stats.Unchanged++
				return
			}
		}
	}

	plaintext, err := os.ReadFile(f.AbsPath)
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "  warning: cannot read %s: %v\n", f.RelPath, err)
		}
		stats.Errors++
		return
	}

	hash := ContentHash(plaintext)

	// Check if unchanged
	if existing, ok := manifest.Files[f.RelPath]; ok && existing.ContentHash == hash && store.Exists(hash) {
		stats.Unchanged++
		return
	}

	// Encrypt and store — unless the content already has an object under
	// another key (the same file synced from a machine with different project
	// keys, or duplicate content). age output is non-deterministic, so
	// re-encrypting identical plaintext would rewrite blob bytes git has
	// already committed and re-push the whole object. Rotate overwrites
	// ciphertext in place on purpose, so this check stays out of
	// ObjectStore.Write.
	encSize, exists := store.Size(hash)
	if !exists {
		encrypted, err := crypto.Encrypt(plaintext, recipient)
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "  warning: cannot encrypt %s: %v\n", f.RelPath, err)
			}
			stats.Errors++
			return
		}

		if err := store.Write(hash, encrypted); err != nil {
			stats.Errors++
			return
		}
		encSize = int64(len(encrypted))
	}

	// Determine if this is new or modified
	if _, existed := manifest.Files[f.RelPath]; existed {
		stats.Modified++
	} else {
		stats.Added++
	}
	stats.BytesPlaintext += f.Size
	stats.BytesEncrypted += encSize

	if verbose {
		action := "new"
		if _, existed := manifest.Files[f.RelPath]; existed {
			action = "mod"
		}
		fmt.Printf("  [%s] %s (%s)\n", action, f.RelPath, FormatSize(f.Size))
	}

	// Count JSONL lines if applicable
	lineCount := 0
	if strings.HasSuffix(f.RelPath, ".jsonl") {
		lineCount = bytes.Count(plaintext, []byte("\n"))
		if len(plaintext) > 0 && plaintext[len(plaintext)-1] != '\n' {
			lineCount++ // last line without trailing newline
		}
	}

	manifest.Files[f.RelPath] = FileEntry{
		ContentHash:     hash,
		SizePlaintext:   f.Size,
		SizeEncrypted:   encSize,
		Mtime:           time.Unix(0, f.ModTimeNs).UTC().Format(time.RFC3339),
		ModTimeNs:       f.ModTimeNs,
		MergeStrategy:   ResolveMergeStrategy(f.RelPath, cfg.Merge),
		JSONLLineCount:  lineCount,
		SessionComplete: isSessionCompleteFor(f.RelPath, activeSessions),
	}
}

func trackDeleted(manifest *Manifest, seen map[string]bool, verbose bool, stats *SealStats) {
	// Mark deleted files
	for path := range manifest.Files {
		if !seen[path] {
			if verbose {
				fmt.Printf("  [del] %s\n", path)
			}
			delete(manifest.Files, path)
			stats.Deleted++
		}
	}
}

func tallySessions(manifest *Manifest, prior map[string]FileEntry, stats *SealStats) {
	// Tally session activity against the prior manifest. "Updated" counts
	// session JSONLs whose JSONLLineCount grew since the previous seal —
	// the most reliable signal short of consulting live process state.
	for path, entry := range manifest.Files {
		if !isSessionPath(path) {
			continue
		}
		stats.Sessions.Tracked++
		priorEntry, ok := prior[path]
		if !ok {
			stats.Sessions.New++
			continue
		}
		if entry.JSONLLineCount > priorEntry.JSONLLineCount {
			stats.Sessions.Updated++
		}
	}
}

// activeSessionIDs returns the set of session UUIDs currently associated
// with live processes. Failure to enumerate returns an empty set so seal
// falls back to path-based completion (the historical behavior).
func activeSessionIDs(claudeDir string) map[string]bool {
	active := map[string]bool{}
	sessions, err := session.DetectActive(claudeDir)
	if err != nil {
		return active
	}
	for _, s := range sessions {
		if s.SessionID != "" {
			active[s.SessionID] = true
		}
	}
	return active
}

// isSessionPath matches the on-disk shape of a Claude session transcript.
func isSessionPath(relPath string) bool {
	return strings.HasPrefix(relPath, "projects/") && strings.HasSuffix(relPath, ".jsonl")
}

// isTopLevelSessionPath narrows isSessionPath to Claude transcript files at
// projects/<project>/<session>.jsonl. Nested JSONL files, such as subagent
// logs, are managed data but not purged by the completed-session default.
func isTopLevelSessionPath(relPath string) bool {
	_, sub, ok := splitProjectKey(relPath)
	if !ok || sub == "" || !strings.HasSuffix(sub, ".jsonl") {
		return false
	}
	name := strings.TrimPrefix(sub, "/")
	return name != "" && !strings.Contains(name, "/")
}

// unsealFile decrypts a single file and writes it to the claude directory.
func unsealFile(store *ObjectStore, identity age.Identity, entry FileEntry, absPath, relPath string, verbose bool) (int, error) {
	// Read encrypted object
	encrypted, err := store.Read(entry.ContentHash)
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "  warning: missing object for %s: %v\n", relPath, err)
		}
		return 0, err
	}

	// Decrypt
	plaintext, err := crypto.Decrypt(encrypted, identity)
	if err != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "  warning: cannot decrypt %s: %v\n", relPath, err)
		}
		return 0, err
	}

	// Write to claude directory
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return 0, err
	}
	if err := os.WriteFile(absPath, plaintext, 0600); err != nil {
		return 0, err
	}
	if entry.ModTimeNs != 0 {
		mtime := time.Unix(0, entry.ModTimeNs)
		os.Chtimes(absPath, mtime, mtime)
	}

	return len(plaintext), nil
}

// noManifestErr explains a missing manifest. A store with encrypted objects but
// no manifest.json is almost always a clone whose manifest was excluded by a
// gitignore on the pushing device — the blobs are unrecoverable without the
// relPath->hash mapping it holds, so point the user at the fix on that device.
func noManifestErr(sealDir string) error {
	if objs, _ := NewObjectStore(sealDir).ListAll(); len(objs) > 0 {
		return fmt.Errorf("seal store at %s has %d encrypted objects but no manifest.json — "+
			"it was likely excluded by a gitignore on the device that pushed it.\n"+
			"On that device run:\n"+
			"  git -C %s add -f manifest.json && git -C %s commit -m 'track manifest' && enclaude push\n"+
			"then run `enclaude pull` here.", sealDir, len(objs), sealDir, sealDir)
	}
	return fmt.Errorf("no manifest found — is the seal store initialized?")
}

// unsealConfig holds the optional knobs for Unseal. Defaults are set in Unseal.
type unsealConfig struct {
	remap RemapMode
}

// UnsealOption configures Unseal without changing its call-site signature —
// existing callers pass nothing and get the defaults. Mirrors the project's
// preference for stable signatures over threading a param through every caller.
type UnsealOption func(*unsealConfig)

// WithRemap sets how foreign project keys are handled (default RemapAuto).
func WithRemap(mode RemapMode) UnsealOption {
	return func(c *unsealConfig) { c.remap = mode }
}

// Unseal decrypts seal contents back to claudeDir and removes managed
// files not in the manifest. The manifest is the source of truth.
func Unseal(cfg *config.Config, identity age.Identity, verbose bool, progress ProgressFunc, opts ...UnsealOption) (stats UnsealStats, err error) {
	start := time.Now()
	defer func() { stats.Elapsed = time.Since(start) }()
	sealDir := cfg.Seal.SealDir

	opt := unsealConfig{remap: RemapAuto}
	for _, fn := range opts {
		fn(&opt)
	}

	store := NewObjectStore(sealDir)

	manifest, err := LoadManifest(sealDir)
	if err != nil {
		return stats, fmt.Errorf("loading manifest: %w", err)
	}
	if manifest == nil {
		return stats, noManifestErr(sealDir)
	}

	// Rewrite foreign project keys into an effective local manifest up front so
	// BOTH the restore loop and the delete-reconciliation pass below operate on
	// local keys — otherwise the latter would scan the freshly-restored file,
	// miss it among the raw (foreign) keys, and delete what was just written.
	manifest, err = remapManifest(manifest, cfg, opt.remap)
	if err != nil {
		return stats, err
	}

	stats.Total = len(manifest.Files)

	i := 0
	for relPath, entry := range manifest.Files {
		i++
		if progress != nil {
			progress(i, stats.Total, relPath)
		}
		absPath := filepath.Join(cfg.Seal.ClaudeDir, relPath)

		if !filepath.IsLocal(relPath) {
			stats.Errors++
			if verbose {
				fmt.Fprintf(os.Stderr, "  warning: skipping invalid path %s\n", relPath)
			}
			continue
		}

		// Fast path: check size and mtime before reading the entire file and hashing
		if info, err := os.Stat(absPath); err == nil && entry.ModTimeNs != 0 {
			if info.Size() == entry.SizePlaintext && info.ModTime().UnixNano() == entry.ModTimeNs {
				stats.Unchanged++
				continue
			}
		}

		// Check if file already exists and matches
		if existing, err := os.ReadFile(absPath); err == nil {
			if ContentHash(existing) == entry.ContentHash {
				stats.Unchanged++
				continue
			}
		}

		bytesDecrypted, err := unsealFile(store, identity, entry, absPath, relPath, verbose)
		if err != nil {
			stats.Errors++
			continue
		}

		if verbose {
			fmt.Printf("  [restore] %s (%s)\n", relPath, FormatSize(entry.SizePlaintext))
		}
		stats.Restored++
		stats.BytesDecrypted += int64(bytesDecrypted)
	}

	// Delete managed files not in the manifest. The manifest is the source
	// of truth — after git pull/merge, it reflects the intended state
	// including remote deletions. Skip if restore had errors (incomplete
	// unseal should not trigger deletions).
	if stats.Errors > 0 {
		return stats, nil
	}
	existingFiles, scanErr := ScanFiles(cfg.Seal.ClaudeDir, cfg.Include.Patterns, cfg.Exclude.Patterns)
	if scanErr != nil {
		if verbose {
			fmt.Fprintf(os.Stderr, "  warning: skipping deletion (scan incomplete: %v)\n", scanErr)
		}
		stats.Errors++
		return stats, nil
	}
	{
		manifestPaths := make(map[string]bool, len(manifest.Files))
		for relPath := range manifest.Files {
			manifestPaths[relPath] = true
		}
		for _, f := range existingFiles {
			if !manifestPaths[f.RelPath] {
				if err := os.Remove(f.AbsPath); err == nil {
					stats.Deleted++
					if verbose {
						fmt.Printf("  [delete] %s\n", f.RelPath)
					}
					dir := filepath.Dir(f.AbsPath)
					if dir != cfg.Seal.ClaudeDir {
						os.Remove(dir)
					}
				} else {
					stats.Errors++
					if verbose {
						fmt.Fprintf(os.Stderr, "  warning: cannot delete %s: %v\n", f.RelPath, err)
					}
				}
			}
		}
	}

	return stats, nil
}

// Status returns the diff between the current claude directory and the seal manifest.
func Status(cfg *config.Config) (*DiffResult, error) {
	manifest, err := LoadManifest(cfg.Seal.SealDir)
	if err != nil {
		return nil, fmt.Errorf("loading manifest: %w", err)
	}
	// Uninitialized seal store: treat as empty so the fast path can dereference
	// manifest.Files safely. Diff semantics are unchanged (everything on disk
	// reports as Added either way).
	if manifest == nil {
		manifest = NewManifest(cfg.Seal.DeviceID)
	}

	files, err := ScanFiles(cfg.Seal.ClaudeDir, cfg.Include.Patterns, cfg.Exclude.Patterns)
	if err != nil {
		return nil, fmt.Errorf("scanning files: %w", err)
	}

	// Build a "current" manifest from disk
	current := NewManifest(cfg.Seal.DeviceID)

	var mu sync.Mutex
	var wg sync.WaitGroup
	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers > 16 {
		numWorkers = 16
	}
	if numWorkers < 1 {
		numWorkers = 1
	}
	sem := make(chan struct{}, numWorkers)

	for _, f := range files {
		// Fast path: if size and nanosecond mtime match manifest, assume
		// unchanged and reuse the stored hash. ModTimeNs==0 means the
		// manifest was written by a version that didn't store nanosecond
		// precision — fall through to hashing in that case rather than
		// trust an unset value.
		if entry, ok := manifest.Files[f.RelPath]; ok && entry.ModTimeNs != 0 {
			if entry.SizePlaintext == f.Size && entry.ModTimeNs == f.ModTimeNs {
				mu.Lock()
				current.Files[f.RelPath] = entry
				mu.Unlock()
				continue
			}
		}

		wg.Add(1)
		go func(f ScanResult) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			data, err := os.ReadFile(f.AbsPath)
			if err != nil {
				return
			}
			hash := ContentHash(data)

			mu.Lock()
			current.Files[f.RelPath] = FileEntry{
				ContentHash: hash,
			}
			mu.Unlock()
		}(f)
	}
	wg.Wait()

	diff := current.Diff(manifest)
	return &diff, nil
}

// UnsealStatus returns what Unseal would do without actually writing anything.
// "Added" = files in manifest but missing on disk (would be restored).
// "Modified" = files on disk with different content than manifest (would be overwritten).
// "Deleted" = managed files on disk but not in manifest (would be deleted).
func UnsealStatus(cfg *config.Config, opts ...UnsealOption) (*DiffResult, error) {
	opt := unsealConfig{remap: RemapAuto}
	for _, fn := range opts {
		fn(&opt)
	}

	manifest, err := LoadManifest(cfg.Seal.SealDir)
	if err != nil {
		return nil, fmt.Errorf("loading manifest: %w", err)
	}
	if manifest == nil {
		return nil, noManifestErr(cfg.Seal.SealDir)
	}

	// Preview against the same effective local manifest unseal would build (in
	// the requested mode), so --dry-run reports exactly what the real command
	// would restore.
	manifest, _ = remapManifest(manifest, cfg, opt.remap)

	// Scan existing files. If claudeDir doesn't exist yet (first-time
	// restore), treat as empty — all manifest files would be restored.
	var files []ScanResult
	if _, statErr := os.Stat(cfg.Seal.ClaudeDir); statErr == nil {
		files, err = ScanFiles(cfg.Seal.ClaudeDir, cfg.Include.Patterns, cfg.Exclude.Patterns)
		if err != nil {
			return nil, fmt.Errorf("scanning files: %w", err)
		}
	}

	// Build current state from disk
	onDisk := make(map[string]string) // relPath -> hash

	var mu sync.Mutex
	var wg sync.WaitGroup
	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers > 16 {
		numWorkers = 16
	}
	if numWorkers < 1 {
		numWorkers = 1
	}
	sem := make(chan struct{}, numWorkers)

	for _, f := range files {
		// Fast path: see Status above for the rationale and the
		// ModTimeNs==0 guard against legacy manifests.
		if entry, ok := manifest.Files[f.RelPath]; ok && entry.ModTimeNs != 0 {
			if entry.SizePlaintext == f.Size && entry.ModTimeNs == f.ModTimeNs {
				mu.Lock()
				onDisk[f.RelPath] = entry.ContentHash
				mu.Unlock()
				continue
			}
		}

		wg.Add(1)
		go func(f ScanResult) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			data, err := os.ReadFile(f.AbsPath)
			if err != nil {
				return
			}
			hash := ContentHash(data)

			mu.Lock()
			onDisk[f.RelPath] = hash
			mu.Unlock()
		}(f)
	}
	wg.Wait()

	var result DiffResult

	// Files in manifest: would be restored or overwritten
	for relPath, entry := range manifest.Files {
		diskHash, exists := onDisk[relPath]
		if !exists {
			result.Added = append(result.Added, relPath) // missing on disk, would restore
		} else if diskHash != entry.ContentHash {
			result.Modified = append(result.Modified, relPath) // different, would overwrite
		}
	}

	// Managed files on disk but not in manifest: would be deleted
	for relPath := range onDisk {
		if _, inManifest := manifest.Files[relPath]; !inManifest {
			result.Deleted = append(result.Deleted, relPath)
		}
	}

	return &result, nil
}

// PurgeScope controls which sealed plaintext files are removed from claudeDir.
type PurgeScope int

const (
	// PurgeCompletedSessions removes only completed top-level session JSONLs.
	PurgeCompletedSessions PurgeScope = iota
	// PurgeAllManaged removes every managed file whose on-disk bytes still
	// match the sealed manifest entry. Callers should gate this carefully.
	PurgeAllManaged
)

// PurgePlaintextStats describes a plaintext purge run.
type PurgePlaintextStats struct {
	Candidates int
	Removed    int
	Missing    int
	Skipped    int
	Errors     int
	Bytes      int64
}

func (s PurgePlaintextStats) String() string {
	return fmt.Sprintf("%d candidates: %d removed, %d missing, %d skipped, %d errors",
		s.Candidates, s.Removed, s.Missing, s.Skipped, s.Errors)
}

func (s PurgePlaintextStats) Multiline(indent string, dryRun bool) string {
	action := "removed"
	if dryRun {
		action = "would remove"
	}
	return fmt.Sprintf("%s%d candidates: %d %s (%s), %d missing, %d skipped, %d errors",
		indent, s.Candidates, s.Removed, action, FormatSize(s.Bytes), s.Missing, s.Skipped, s.Errors)
}

// PurgePlaintext removes plaintext copies only when the on-disk bytes still
// match the sealed manifest hash. Unsynced edits are skipped, not deleted.
func PurgePlaintext(cfg *config.Config, identity age.Identity, scope PurgeScope, shred, dryRun, verbose bool) (PurgePlaintextStats, error) {
	var stats PurgePlaintextStats

	manifest, err := LoadManifest(cfg.Seal.SealDir)
	if err != nil {
		return stats, fmt.Errorf("loading manifest: %w", err)
	}
	if manifest == nil {
		return stats, noManifestErr(cfg.Seal.SealDir)
	}

	activeSessions := activeSessionIDs(cfg.Seal.ClaudeDir)
	objects := NewObjectStore(cfg.Seal.SealDir)
	root, err := os.OpenRoot(cfg.Seal.ClaudeDir)
	if err != nil {
		return stats, fmt.Errorf("opening Claude root: %w", err)
	}
	defer root.Close()

	paths := make([]string, 0, len(manifest.Files))
	for path := range manifest.Files {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, relPath := range paths {
		entry := manifest.Files[relPath]
		if !purgeSelects(relPath, entry, scope, activeSessions) {
			continue
		}
		stats.Candidates++

		if !filepath.IsLocal(relPath) {
			stats.Errors++
			if verbose {
				fmt.Fprintf(os.Stderr, "  warning: skipping invalid path %s\n", relPath)
			}
			continue
		}

		info, err := lstatNoSymlinkComponents(root, relPath)
		if err != nil {
			if errors.Is(err, errPathHasSymlink) {
				stats.Skipped++
				if verbose {
					fmt.Fprintf(os.Stderr, "  warning: skipping symlinked path %s\n", relPath)
				}
				continue
			}
			if os.IsNotExist(err) {
				stats.Missing++
				continue
			}
			stats.Errors++
			if verbose {
				fmt.Fprintf(os.Stderr, "  warning: cannot stat %s: %v\n", relPath, err)
			}
			continue
		}
		if !info.Mode().IsRegular() {
			stats.Skipped++
			continue
		}

		openFlag := os.O_RDONLY
		if shred {
			openFlag = os.O_RDWR
		}
		purgeAfterPathCheck(relPath)
		file, fileInfo, err := openRootedSameFile(root, relPath, info, openFlag)
		if err != nil {
			stats.Errors++
			if verbose {
				fmt.Fprintf(os.Stderr, "  warning: cannot read %s: %v\n", relPath, err)
			}
			continue
		}
		plaintext, err := io.ReadAll(file)
		if err != nil {
			_ = file.Close()
			stats.Errors++
			if verbose {
				fmt.Fprintf(os.Stderr, "  warning: cannot read %s: %v\n", relPath, err)
			}
			continue
		}
		if ContentHash(plaintext) != entry.ContentHash {
			_ = file.Close()
			stats.Skipped++
			if verbose {
				fmt.Fprintf(os.Stderr, "  warning: skipping changed plaintext %s\n", relPath)
			}
			continue
		}
		if err := verifySealedObject(objects, identity, entry); err != nil {
			_ = file.Close()
			stats.Errors++
			if verbose {
				fmt.Fprintf(os.Stderr, "  warning: sealed object for %s is not recoverable: %v\n", relPath, err)
			}
			continue
		}

		if dryRun {
			_ = file.Close()
			stats.Bytes += int64(len(plaintext))
			stats.Removed++
			if verbose {
				fmt.Printf("  [purge] %s (%s)\n", relPath, FormatSize(int64(len(plaintext))))
			}
			continue
		}

		err = purgeOpenRootedFile(root, relPath, file, fileInfo, entry.ContentHash, shred)
		if err != nil {
			stats.Errors++
			if verbose {
				fmt.Fprintf(os.Stderr, "  warning: cannot purge %s: %v\n", relPath, err)
			}
			continue
		}
		stats.Bytes += int64(len(plaintext))
		stats.Removed++
		if verbose {
			fmt.Printf("  [purge] %s (%s)\n", relPath, FormatSize(int64(len(plaintext))))
		}
		dir := filepath.Dir(relPath)
		if dir != "." {
			_ = root.Remove(dir)
		}
	}

	return stats, nil
}

var (
	errPathHasSymlink = errors.New("path contains symlink")
	errPathChanged    = errors.New("path changed after validation")
)

// purgeAfterPathCheck is a test hook for exercising path-swap races.
var purgeAfterPathCheck = func(string) {}

// purgeBeforeQuarantine is a test hook for exercising final remove races.
var purgeBeforeQuarantine = func(string) {}

// purgeAfterQuarantine is a test hook for exercising post-quarantine errors.
var purgeAfterQuarantine = func(string, *os.File) {}

func lstatNoSymlinkComponents(root *os.Root, relPath string) (os.FileInfo, error) {
	clean := filepath.Clean(relPath)
	if clean == "." {
		return nil, fmt.Errorf("empty path")
	}

	// Optimization: avoid strings.Split to eliminate slice allocations.
	var info os.FileInfo
	var err error
	for i := 0; i < len(clean); i++ {
		if os.IsPathSeparator(clean[i]) {
			if i == 0 {
				continue // e.g. leading slash in absolute paths
			}
			info, err = root.Lstat(clean[:i])
			if err != nil {
				return nil, err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil, errPathHasSymlink
			}
		}
	}
	info, err = root.Lstat(clean)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errPathHasSymlink
	}
	return info, nil
}

func openRootedSameFile(root *os.Root, relPath string, expected os.FileInfo, flag int) (*os.File, os.FileInfo, error) {
	f, err := root.OpenFile(relPath, flag, 0)
	if err != nil {
		return nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(expected, info) {
		_ = f.Close()
		return nil, nil, errPathChanged
	}
	return f, info, nil
}

func purgeOpenRootedFile(root *os.Root, relPath string, f *os.File, info os.FileInfo, expectedHash string, shred bool) error {
	purgeBeforeQuarantine(relPath)
	tombstone, tombDir, err := quarantineRootedSameFile(root, relPath, info)
	if err != nil {
		_ = f.Close()
		return err
	}
	purgeAfterQuarantine(relPath, f)

	if shred {
		err = shredOpenFile(f, relPath, info.Size())
	} else {
		err = f.Close()
	}
	if err != nil {
		if cleanupErr := cleanupQuarantinedRootedFile(root, relPath, tombstone, tombDir, info, expectedHash); cleanupErr != nil {
			return errors.Join(err, cleanupErr)
		}
		return err
	}
	if err := root.Remove(tombstone); err != nil {
		return fmt.Errorf("removing %s: %w", relPath, err)
	}
	_ = root.Remove(tombDir)
	return nil
}

func quarantineRootedSameFile(root *os.Root, relPath string, expected os.FileInfo) (string, string, error) {
	parent := filepath.Dir(relPath)
	base := filepath.Base(relPath)
	for range 16 {
		suffix, err := RandomHex(8)
		if err != nil {
			return "", "", err
		}
		tombDir := filepath.Join(parent, ".enclaude-purge-"+suffix)
		if err := root.Mkdir(tombDir, 0700); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", "", err
		}
		tombstone := filepath.Join(tombDir, base)
		if err := root.Rename(relPath, tombstone); err != nil {
			_ = root.Remove(tombDir)
			return "", "", err
		}
		tombInfo, err := root.Lstat(tombstone)
		if err != nil {
			_ = root.Rename(tombstone, relPath)
			_ = root.Remove(tombDir)
			return "", "", err
		}
		if !tombInfo.Mode().IsRegular() || !os.SameFile(expected, tombInfo) {
			_ = root.Rename(tombstone, relPath)
			_ = root.Remove(tombDir)
			return "", "", errPathChanged
		}
		return tombstone, tombDir, nil
	}
	return "", "", fmt.Errorf("creating unique purge tombstone")
}

func cleanupQuarantinedRootedFile(root *os.Root, relPath, tombstone, tombDir string, expected os.FileInfo, expectedHash string) error {
	tombInfo, err := root.Lstat(tombstone)
	if err != nil {
		if os.IsNotExist(err) {
			_ = root.Remove(tombDir)
			return nil
		}
		return fmt.Errorf("checking quarantined %s: %w", relPath, err)
	}
	if !tombInfo.Mode().IsRegular() || !os.SameFile(expected, tombInfo) {
		return errPathChanged
	}

	restorable, hashErr := rootedFileMatchesHash(root, tombstone, expected, expectedHash)
	if hashErr != nil {
		if err := root.Remove(tombstone); err != nil {
			return errors.Join(hashErr, fmt.Errorf("cleaning quarantined %s: %w", relPath, err))
		}
		_ = root.Remove(tombDir)
		return hashErr
	}
	if restorable {
		err := root.Link(tombstone, relPath)
		if err == nil {
			if err := root.Remove(tombstone); err != nil {
				return fmt.Errorf("removing quarantined %s after restore: %w", relPath, err)
			}
			_ = root.Remove(tombDir)
			return nil
		}
	}

	if err := root.Remove(tombstone); err != nil {
		return fmt.Errorf("cleaning quarantined %s: %w", relPath, err)
	}
	_ = root.Remove(tombDir)
	return nil
}

func rootedFileMatchesHash(root *os.Root, relPath string, expected os.FileInfo, expectedHash string) (bool, error) {
	if expectedHash == "" {
		return false, nil
	}
	f, _, err := openRootedSameFile(root, relPath, expected, os.O_RDONLY)
	if err != nil {
		return false, err
	}
	data, err := io.ReadAll(f)
	closeErr := f.Close()
	if err != nil {
		return false, err
	}
	if closeErr != nil {
		return false, closeErr
	}
	return ContentHash(data) == expectedHash, nil
}

// RandomHex returns n cryptographically random bytes encoded as a hex string.
func RandomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := cryptorand.Read(buf); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	return fmt.Sprintf("%x", buf), nil
}

func shredOpenFile(f *os.File, relPath string, size int64) error {
	if _, err := f.Seek(0, 0); err != nil {
		f.Close()
		return fmt.Errorf("seeking %s: %w", relPath, err)
	}
	buf := make([]byte, min(size, 64*1024))
	for written := int64(0); written < size; {
		chunk := int(min(int64(len(buf)), size-written))
		if _, err := cryptorand.Read(buf[:chunk]); err != nil {
			_ = f.Close()
			return fmt.Errorf("generating overwrite bytes for %s: %w", relPath, err)
		}
		if _, err := f.Write(buf[:chunk]); err != nil {
			_ = f.Close()
			return fmt.Errorf("overwriting %s: %w", relPath, err)
		}
		written += int64(chunk)
	}

	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("syncing %s: %w", relPath, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", relPath, err)
	}
	return nil
}

func verifySealedObject(store *ObjectStore, identity age.Identity, entry FileEntry) error {
	encrypted, err := store.Read(entry.ContentHash)
	if err != nil {
		return fmt.Errorf("read object: %w", err)
	}
	plaintext, err := crypto.Decrypt(encrypted, identity)
	if err != nil {
		return fmt.Errorf("decrypt object: %w", err)
	}
	if ContentHash(plaintext) != entry.ContentHash {
		return fmt.Errorf("hash mismatch")
	}
	return nil
}

func purgeSelects(relPath string, entry FileEntry, scope PurgeScope, active map[string]bool) bool {
	switch scope {
	case PurgeCompletedSessions:
		return isTopLevelSessionPath(relPath) && entry.SessionComplete
	case PurgeAllManaged:
		if isSessionPath(relPath) && !isSessionCompleteFor(relPath, active) {
			return false
		}
		return true
	default:
		return false
	}
}

// ResolveMergeStrategy finds the merge strategy for a file based on glob patterns.
func ResolveMergeStrategy(relPath string, strategies map[string]string) string {
	strategy, _ := ResolveMergeStrategyWithPattern(relPath, strategies)
	return strategy
}

// ResolveMergeStrategyWithPattern returns both the strategy and the pattern that matched.
// An empty pattern means a built-in default was used.
// When multiple glob patterns match, the most specific wins (most segments,
// fewest wildcards). This ensures deterministic resolution regardless of
// Go map iteration order.
func ResolveMergeStrategyWithPattern(relPath string, strategies map[string]string) (strategy string, pattern string) {
	// Try exact match first (highest priority)
	if s, ok := strategies[relPath]; ok {
		return s, relPath
	}

	// Collect all matching glob patterns, pick the most specific
	bestPattern := ""
	bestStrategy := ""
	bestTotal := -1
	bestSegments := -1

	for p, s := range strategies {
		if MatchGlob(relPath, p) {
			total, segments := patternSpecificity(p)
			isBetter := false
			if total > bestTotal {
				isBetter = true
			} else if total == bestTotal {
				if segments > bestSegments {
					isBetter = true
				} else if segments == bestSegments {
					if p > bestPattern {
						isBetter = true
					}
				}
			}
			if isBetter {
				bestTotal = total
				bestSegments = segments
				bestPattern = p
				bestStrategy = s
			}
		}
	}

	if bestPattern != "" {
		return bestStrategy, bestPattern
	}

	// Default
	if strings.HasSuffix(relPath, ".md") {
		return "text_merge", ""
	}
	return "last_write_wins", ""
}

// patternSpecificity returns a comparable score for a glob pattern.
// Per-segment scoring: literal=3, constrained glob (contains [)=2, *=1, **=0.
// Ties broken by segment count (more = more specific) then pattern string.
func patternSpecificity(pattern string) (total, segments int) {
	// Optimization: avoid strings.Split to eliminate slice allocations.
	total = 0
	segments = 0
	rem := pattern
	for {
		idx := strings.IndexByte(rem, '/')
		var seg string
		if idx == -1 {
			seg = rem
		} else {
			seg = rem[:idx]
		}

		segments++
		// Sum per-segment scores: literal=3, glob=2, *=1, **=0.
		// Higher total = more specific.
		switch {
		case seg == "**":
			total += 0
		case seg == "*":
			total += 1
		case strings.ContainsAny(seg, "*?["):
			total += 2
		default:
			total += 3
		}

		if idx == -1 {
			break
		}
		rem = rem[idx+1:]
	}
	return total, segments
}

// isSessionCompleteFor determines whether a session JSONL file looks
// complete. A session is "complete" iff it lives under projects/, ends in
// .jsonl, and its UUID is not in the active set. An empty active set means
// caller couldn't enumerate live sessions, in which case we fall back to
// the historical path-based heuristic ("any matching path is complete").
func isSessionCompleteFor(relPath string, active map[string]bool) bool {
	if !isSessionPath(relPath) {
		return false
	}
	if len(active) == 0 {
		return true
	}
	base := filepath.Base(relPath)
	uuid := strings.TrimSuffix(base, ".jsonl")
	return !active[uuid]
}

// RepairResult describes the outcome of a seal store integrity check.
type RepairResult struct {
	TotalManifest  int
	TotalOnDisk    int
	MissingObjects []string // manifest entries with no .age file
	CorruptObjects []string // objects that fail decrypt or hash mismatch
	OrphanObjects  []string // .age files not in manifest
	Fixed          int
}

// Verify checks seal store integrity without modifying anything.
func Verify(cfg *config.Config, identity age.Identity, verbose bool) (*RepairResult, error) {
	sealDir := cfg.Seal.SealDir
	store := NewObjectStore(sealDir)
	result := &RepairResult{}

	manifest, err := LoadManifest(sealDir)
	if err != nil {
		return nil, fmt.Errorf("loading manifest: %w", err)
	}
	if manifest == nil {
		return nil, noManifestErr(sealDir)
	}

	result.TotalManifest = len(manifest.Files)

	// Check all manifest entries have objects
	for path, entry := range manifest.Files {
		if !store.Exists(entry.ContentHash) {
			result.MissingObjects = append(result.MissingObjects, path)
			if verbose {
				fmt.Fprintf(os.Stderr, "  [missing] %s (hash: %s)\n", path, shortHash(entry.ContentHash))
			}
			continue
		}

		// Verify decrypt + hash
		encrypted, err := store.Read(entry.ContentHash)
		if err != nil {
			result.CorruptObjects = append(result.CorruptObjects, path)
			continue
		}
		plaintext, err := crypto.Decrypt(encrypted, identity)
		if err != nil {
			result.CorruptObjects = append(result.CorruptObjects, path)
			if verbose {
				fmt.Fprintf(os.Stderr, "  [corrupt] %s: decrypt failed\n", path)
			}
			continue
		}
		if ContentHash(plaintext) != entry.ContentHash {
			result.CorruptObjects = append(result.CorruptObjects, path)
			if verbose {
				fmt.Fprintf(os.Stderr, "  [corrupt] %s: hash mismatch\n", path)
			}
		}
	}

	// Find orphan objects
	allOnDisk, err := store.ListAll()
	if err != nil {
		return result, fmt.Errorf("listing objects: %w", err)
	}
	result.TotalOnDisk = len(allOnDisk)

	referenced := make(map[string]bool)
	for _, entry := range manifest.Files {
		referenced[entry.ContentHash] = true
	}
	for _, hash := range allOnDisk {
		if !referenced[hash] {
			result.OrphanObjects = append(result.OrphanObjects, hash)
			if verbose {
				fmt.Fprintf(os.Stderr, "  [orphan] %s\n", shortHash(hash))
			}
		}
	}

	return result, nil
}

// Repair fixes seal store integrity issues.
func Repair(cfg *config.Config, identity age.Identity, deleteOrphans bool, verbose bool) (*RepairResult, error) {
	result, err := Verify(cfg, identity, verbose)
	if err != nil {
		return nil, err
	}

	manifest, err := LoadManifest(cfg.Seal.SealDir)
	if err != nil {
		return nil, fmt.Errorf("loading manifest: %w", err)
	}

	store := NewObjectStore(cfg.Seal.SealDir)

	// Mirror Seal's PID-aware completion check so a Repair doesn't
	// silently flip SessionComplete=true on currently-active session
	// transcripts.
	activeSessions := activeSessionIDs(cfg.Seal.ClaudeDir)

	// Track old hashes that get superseded during repair so we can
	// include them in orphan deletion (they become orphaned only after
	// the manifest is updated with new hashes).
	var superseded []string

	// Try to fix missing/corrupt by re-sealing from plaintext
	for _, path := range append(result.MissingObjects, result.CorruptObjects...) {
		absPath := filepath.Join(cfg.Seal.ClaudeDir, path)
		if !filepath.IsLocal(path) {
			continue
		}
		plaintext, err := os.ReadFile(absPath)
		if err != nil {
			continue // plaintext not available
		}

		hash := ContentHash(plaintext)
		encrypted, err := crypto.Encrypt(plaintext, identity.(*age.X25519Identity).Recipient())
		if err != nil {
			continue
		}
		if err := store.Write(hash, encrypted); err != nil {
			continue
		}

		// Track superseded hash before updating manifest
		oldHash := manifest.Files[path].ContentHash
		if oldHash != hash && isValidHash(oldHash) {
			superseded = append(superseded, oldHash)
		}

		// Rebuild full manifest entry from repaired state
		entry := manifest.Files[path]
		entry.ContentHash = hash
		entry.SizePlaintext = int64(len(plaintext))
		entry.SizeEncrypted = int64(len(encrypted))
		entry.MergeStrategy = ResolveMergeStrategy(path, cfg.Merge)
		// Update Mtime from current file stat (important for last_write_wins)
		if info, err := os.Stat(absPath); err == nil {
			entry.Mtime = info.ModTime().UTC().Format(time.RFC3339)
			entry.ModTimeNs = info.ModTime().UnixNano()
		}
		// Recompute JSONL line count
		if strings.HasSuffix(path, ".jsonl") {
			entry.JSONLLineCount = bytes.Count(plaintext, []byte("\n"))
			if len(plaintext) > 0 && plaintext[len(plaintext)-1] != '\n' {
				entry.JSONLLineCount++
			}
		}
		entry.SessionComplete = isSessionCompleteFor(path, activeSessions)
		manifest.Files[path] = entry

		result.Fixed++
		if verbose {
			fmt.Printf("  [fixed] %s (re-sealed from plaintext)\n", path)
		}
	}

	// Save updated manifest before deleting any objects — if save fails,
	// the old manifest still has valid references to existing objects.
	if result.Fixed > 0 {
		if err := manifest.Save(cfg.Seal.SealDir); err != nil {
			return result, fmt.Errorf("saving manifest: %w", err)
		}
	}

	// Delete orphans only after manifest is safely persisted
	if deleteOrphans {
		// Build set of hashes still referenced by the updated manifest
		// so we don't delete objects that another entry still needs
		// (e.g., two files with identical content share one object).
		referenced := make(map[string]bool)
		for _, entry := range manifest.Files {
			referenced[entry.ContentHash] = true
		}

		// Include superseded hashes (old objects replaced during repair)
		allOrphans := append(result.OrphanObjects, superseded...)
		for _, hash := range allOrphans {
			if referenced[hash] {
				continue // still needed by another manifest entry
			}
			store.Delete(hash)
			if verbose {
				fmt.Printf("  [deleted] orphan %s\n", shortHash(hash))
			}
		}
	}

	return result, nil
}

// Rotate re-encrypts all sealed objects with a new key.
func Rotate(cfg *config.Config, oldIdentity age.Identity, newRecipient age.Recipient, verbose bool, progress ProgressFunc) (int, error) {
	sealDir := cfg.Seal.SealDir
	store := NewObjectStore(sealDir)

	manifest, err := LoadManifest(sealDir)
	if err != nil {
		return 0, rotationStoreUnchangedError(fmt.Errorf("loading manifest: %w", err))
	}
	if manifest == nil {
		return 0, rotationStoreUnchangedError(noManifestErr(sealDir))
	}

	hashes := uniqueManifestHashes(manifest)
	total := len(hashes)
	tmpDir, err := os.MkdirTemp(sealDir, ".rotate-*")
	if err != nil {
		return 0, rotationStoreUnchangedError(fmt.Errorf("creating rotation staging dir: %w", err))
	}
	defer os.RemoveAll(tmpDir)
	newRoot := filepath.Join(tmpDir, "new")
	oldRoot := filepath.Join(tmpDir, "old")

	for i, hash := range hashes {
		i++
		if progress != nil {
			progress(i, total, hash)
		}

		encrypted, err := store.Read(hash)
		if err != nil {
			return 0, rotationStoreUnchangedError(fmt.Errorf("reading object %s: %w", shortHash(hash), err))
		}

		plaintext, err := crypto.Decrypt(encrypted, oldIdentity)
		if err != nil {
			return 0, rotationStoreUnchangedError(fmt.Errorf("decrypting object %s: %w", shortHash(hash), err))
		}

		newEncrypted, err := crypto.Encrypt(plaintext, newRecipient)
		if err != nil {
			return 0, rotationStoreUnchangedError(fmt.Errorf("re-encrypting object %s: %w", shortHash(hash), err))
		}

		if err := writeStagedObject(newRoot, hash, newEncrypted); err != nil {
			return 0, rotationStoreUnchangedError(fmt.Errorf("staging rotated object %s: %w", shortHash(hash), err))
		}
		if err := writeStagedObject(oldRoot, hash, encrypted); err != nil {
			return 0, rotationStoreUnchangedError(fmt.Errorf("staging rollback object %s: %w", shortHash(hash), err))
		}
	}

	applied := make([]string, 0, len(hashes))
	for _, hash := range hashes {
		data, err := os.ReadFile(stagedObjectPath(newRoot, hash))
		if err != nil {
			return len(applied), recoverRotationApplyFailure(
				store, oldRoot, newRoot, hashes, applied,
				fmt.Errorf("reading staged object %s: %w", shortHash(hash), err),
			)
		}
		if err := rotateObjectWrite(store, hash, data); err != nil {
			rollbackHashes := append(append([]string{}, applied...), hash)
			return len(applied), recoverRotationApplyFailure(
				store, oldRoot, newRoot, hashes, rollbackHashes,
				fmt.Errorf("applying rotated object %s: %w", shortHash(hash), err),
			)
		}
		applied = append(applied, hash)
		if verbose {
			fmt.Printf("  [rotate] %s\n", shortHash(hash))
		}
	}

	// Save manifest (updates SealedAt timestamp)
	if err := manifest.Save(sealDir); err != nil {
		return len(applied), fmt.Errorf("saving manifest: %w", err)
	}

	return len(applied), nil
}

// ErrRotationStoreUnchanged marks a rotation failure whose object writes either
// never started or were fully rolled back. When callers already persisted a new
// key before Rotate, this error means the old key should be restored.
var ErrRotationStoreUnchanged = errors.New("rotation left object store encrypted with old key")

// ErrRotationStoreAmbiguous marks a catastrophic rotation failure where neither
// rollback nor roll-forward could prove a single-key object-store state.
var ErrRotationStoreAmbiguous = errors.New("rotation left object store in ambiguous key state")

// IsRotationStoreUnchanged reports whether a rotation error left the object
// store decryptable with the old key.
func IsRotationStoreUnchanged(err error) bool {
	return errors.Is(err, ErrRotationStoreUnchanged)
}

// IsRotationStoreAmbiguous reports whether a rotation error could not recover
// the object store to a proven old-key or new-key state.
func IsRotationStoreAmbiguous(err error) bool {
	return errors.Is(err, ErrRotationStoreAmbiguous)
}

func rotationStoreUnchangedError(err error) error {
	return fmt.Errorf("%w: %w", ErrRotationStoreUnchanged, err)
}

func rotationStoreAmbiguousError(err error) error {
	return fmt.Errorf("%w: %w", ErrRotationStoreAmbiguous, err)
}

// rotateObjectWrite is a test hook for exercising apply/rollback failures.
var rotateObjectWrite = func(store *ObjectStore, hash string, data []byte) error {
	return store.Write(hash, data)
}

func recoverRotationApplyFailure(store *ObjectStore, oldRoot, newRoot string, allHashes, rollbackHashes []string, cause error) error {
	rollbackErr := rollbackRotatedObjects(store, oldRoot, rollbackHashes)
	if rollbackErr == nil {
		return rotationStoreUnchangedError(cause)
	}

	if forwardErr := rollForwardRotatedObjects(store, newRoot, allHashes); forwardErr != nil {
		return rotationStoreAmbiguousError(errors.Join(
			cause,
			fmt.Errorf("rollback failed: %w", rollbackErr),
			fmt.Errorf("new-key recovery failed: %w", forwardErr),
		))
	}

	return errors.Join(
		cause,
		fmt.Errorf("rollback failed; recovered all rotated objects with new key: %w", rollbackErr),
	)
}

func uniqueManifestHashes(manifest *Manifest) []string {
	seen := make(map[string]bool, len(manifest.Files))
	for _, entry := range manifest.Files {
		if entry.ContentHash != "" {
			seen[entry.ContentHash] = true
		}
	}
	hashes := make([]string, 0, len(seen))
	for hash := range seen {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)
	return hashes
}

func stagedObjectPath(root, hash string) string {
	return filepath.Join(root, hash[:2], hash[2:]+".age")
}

func writeStagedObject(root, hash string, data []byte) error {
	path := stagedObjectPath(root, hash)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func rollbackRotatedObjects(store *ObjectStore, oldRoot string, hashes []string) error {
	var errs []error
	for _, hash := range hashes {
		data, err := os.ReadFile(stagedObjectPath(oldRoot, hash))
		if err != nil {
			errs = append(errs, fmt.Errorf("read backup %s: %w", shortHash(hash), err))
			continue
		}
		if err := rotateObjectWrite(store, hash, data); err != nil {
			errs = append(errs, fmt.Errorf("restore %s: %w", shortHash(hash), err))
		}
	}
	return errors.Join(errs...)
}

func rollForwardRotatedObjects(store *ObjectStore, newRoot string, hashes []string) error {
	var errs []error
	for _, hash := range hashes {
		data, err := os.ReadFile(stagedObjectPath(newRoot, hash))
		if err != nil {
			errs = append(errs, fmt.Errorf("read rotated %s: %w", shortHash(hash), err))
			continue
		}
		if err := rotateObjectWrite(store, hash, data); err != nil {
			errs = append(errs, fmt.Errorf("recover %s: %w", shortHash(hash), err))
		}
	}
	return errors.Join(errs...)
}

func shortHash(hash string) string {
	if len(hash) < 16 {
		return hash
	}
	return hash[:16]
}

// FormatSize formats a byte count as a human-readable string.
func FormatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
