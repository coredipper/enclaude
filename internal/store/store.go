package store

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
)

// SealStats tracks what happened during a seal operation.
type SealStats struct {
	Scanned   int
	Added     int
	Modified  int
	Deleted   int
	Unchanged int
	Errors    int
}

// HasChanges returns true if the seal produced any modifications worth committing.
func (s SealStats) HasChanges() bool {
	return s.Added > 0 || s.Modified > 0 || s.Deleted > 0
}

func (s SealStats) String() string {
	if s.Deleted > 0 {
		return fmt.Sprintf("scanned %d files: %d new, %d modified, %d deleted, %d unchanged",
			s.Scanned, s.Added, s.Modified, s.Deleted, s.Unchanged)
	}
	return fmt.Sprintf("scanned %d files: %d new, %d modified, %d unchanged",
		s.Scanned, s.Added, s.Modified, s.Unchanged)
}

// UnsealStats tracks what happened during an unseal operation.
type UnsealStats struct {
	Total     int
	Restored  int
	Unchanged int
	Deleted   int
	Errors    int
}

func (s UnsealStats) String() string {
	if s.Deleted > 0 {
		return fmt.Sprintf("%d files: %d restored, %d unchanged, %d deleted",
			s.Total, s.Restored, s.Unchanged, s.Deleted)
	}
	return fmt.Sprintf("%d files: %d restored, %d unchanged",
		s.Total, s.Restored, s.Unchanged)
}

// ProgressFunc is called during long operations to report progress.
type ProgressFunc func(current, total int, path string)

// Seal encrypts changed files from claudeDir into the seal store.
func Seal(cfg *config.Config, recipient age.Recipient, verbose bool, progress ProgressFunc) (SealStats, error) {
	var stats SealStats
	sealDir := cfg.Seal.SealDir

	store := NewObjectStore(sealDir)
	if err := store.Init(); err != nil {
		return stats, fmt.Errorf("initializing object store: %w", err)
	}

	// Load existing manifest (may be nil on first seal)
	manifest, err := LoadManifest(sealDir)
	if err != nil {
		return stats, fmt.Errorf("loading manifest: %w", err)
	}
	if manifest == nil {
		manifest = NewManifest(cfg.Seal.DeviceID)
	}
	manifest.DeviceID = cfg.Seal.DeviceID

	// Scan files
	files, err := ScanFiles(cfg.Seal.ClaudeDir, cfg.Include.Patterns, cfg.Exclude.Patterns)
	if err != nil {
		return stats, fmt.Errorf("scanning files: %w", err)
	}
	stats.Scanned = len(files)

	// Track which files still exist (for deletion detection)
	seen := make(map[string]bool)

	for i, f := range files {
		if progress != nil {
			progress(i+1, len(files), f.RelPath)
		}
		seen[f.RelPath] = true

		plaintext, err := os.ReadFile(f.AbsPath)
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "  warning: cannot read %s: %v\n", f.RelPath, err)
			}
			stats.Errors++
			continue
		}

		hash := ContentHash(plaintext)

		// Check if unchanged
		if existing, ok := manifest.Files[f.RelPath]; ok && existing.ContentHash == hash {
			stats.Unchanged++
			continue
		}

		// Encrypt and store
		encrypted, err := crypto.Encrypt(plaintext, recipient)
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "  warning: cannot encrypt %s: %v\n", f.RelPath, err)
			}
			stats.Errors++
			continue
		}

		if err := store.Write(hash, encrypted); err != nil {
			stats.Errors++
			continue
		}

		// Determine if this is new or modified
		if _, existed := manifest.Files[f.RelPath]; existed {
			stats.Modified++
		} else {
			stats.Added++
		}

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
			ContentHash:    hash,
			SizePlaintext:  f.Size,
			SizeEncrypted:  int64(len(encrypted)),
			Mtime:          time.UnixMilli(f.ModTimeMs).UTC().Format(time.RFC3339),
			MergeStrategy:  ResolveMergeStrategy(f.RelPath, cfg.Merge),
			JSONLLineCount: lineCount,
			SessionComplete: isSessionComplete(f.RelPath),
		}
	}

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

	// Save manifest
	if err := manifest.Save(sealDir); err != nil {
		return stats, fmt.Errorf("saving manifest: %w", err)
	}

	return stats, nil
}

// Unseal decrypts seal contents back to claudeDir and removes managed
// files not in the manifest. The manifest is the source of truth.
func Unseal(cfg *config.Config, identity age.Identity, verbose bool, progress ProgressFunc) (UnsealStats, error) {
	var stats UnsealStats
	sealDir := cfg.Seal.SealDir

	store := NewObjectStore(sealDir)

	manifest, err := LoadManifest(sealDir)
	if err != nil {
		return stats, fmt.Errorf("loading manifest: %w", err)
	}
	if manifest == nil {
		return stats, fmt.Errorf("no manifest found — is the seal store initialized?")
	}

	stats.Total = len(manifest.Files)

	i := 0
	for relPath, entry := range manifest.Files {
		i++
		if progress != nil {
			progress(i, stats.Total, relPath)
		}
		absPath := filepath.Join(cfg.Seal.ClaudeDir, relPath)

		// Check if file already exists and matches
		if existing, err := os.ReadFile(absPath); err == nil {
			if ContentHash(existing) == entry.ContentHash {
				stats.Unchanged++
				continue
			}
		}

		// Read encrypted object
		encrypted, err := store.Read(entry.ContentHash)
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "  warning: missing object for %s: %v\n", relPath, err)
			}
			stats.Errors++
			continue
		}

		// Decrypt
		plaintext, err := crypto.Decrypt(encrypted, identity)
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "  warning: cannot decrypt %s: %v\n", relPath, err)
			}
			stats.Errors++
			continue
		}

		// Write to claude directory
		dir := filepath.Dir(absPath)
		if err := os.MkdirAll(dir, 0700); err != nil {
			stats.Errors++
			continue
		}
		if err := os.WriteFile(absPath, plaintext, 0600); err != nil {
			stats.Errors++
			continue
		}

		if verbose {
			fmt.Printf("  [restore] %s (%s)\n", relPath, FormatSize(entry.SizePlaintext))
		}
		stats.Restored++
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

	files, err := ScanFiles(cfg.Seal.ClaudeDir, cfg.Include.Patterns, cfg.Exclude.Patterns)
	if err != nil {
		return nil, fmt.Errorf("scanning files: %w", err)
	}

	// Build a "current" manifest from disk
	current := NewManifest(cfg.Seal.DeviceID)
	for _, f := range files {
		data, err := os.ReadFile(f.AbsPath)
		if err != nil {
			continue
		}
		current.Files[f.RelPath] = FileEntry{
			ContentHash: ContentHash(data),
		}
	}

	diff := current.Diff(manifest)
	return &diff, nil
}

// UnsealStatus returns what Unseal would do without actually writing anything.
// "Added" = files in manifest but missing on disk (would be restored).
// "Modified" = files on disk with different content than manifest (would be overwritten).
// "Deleted" = managed files on disk but not in manifest (would be deleted).
func UnsealStatus(cfg *config.Config) (*DiffResult, error) {
	manifest, err := LoadManifest(cfg.Seal.SealDir)
	if err != nil {
		return nil, fmt.Errorf("loading manifest: %w", err)
	}
	if manifest == nil {
		return nil, fmt.Errorf("no manifest found — is the seal store initialized?")
	}

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
	for _, f := range files {
		data, err := os.ReadFile(f.AbsPath)
		if err != nil {
			continue
		}
		onDisk[f.RelPath] = ContentHash(data)
	}

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
	bestScore := ""

	for p, s := range strategies {
		if p == relPath {
			continue // already checked exact match
		}
		if MatchGlob(relPath, p) {
			score := patternSpecificity(p)
			if score > bestScore {
				bestScore = score
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

// patternSpecificity returns a comparable score slice for a glob pattern.
// Compared lexicographically, more specific patterns sort higher.
// Per-segment scoring: literal=3, constrained glob (contains [)=2, *=1, **=0.
// Ties broken by segment count (more = more specific) then pattern string.
func patternSpecificity(pattern string) string {
	segs := strings.Split(pattern, "/")
	// Sum per-segment scores: literal=3, glob=2, *=1, **=0.
	// Higher total = more specific.
	total := 0
	for _, seg := range segs {
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
	}
	// Fixed-width: total score (2 digits), segment count (2 digits), pattern.
	// Higher total wins; ties broken by more segments, then lexical.
	return fmt.Sprintf("%02d:%02d:%s", total, len(segs), pattern)
}

// isSessionComplete determines if a session JSONL file is likely complete.
// Session files under projects/ with UUID-like names are complete if they exist
// (active sessions are still being written to, but we check PIDs elsewhere).
func isSessionComplete(relPath string) bool {
	return strings.HasPrefix(relPath, "projects/") && strings.HasSuffix(relPath, ".jsonl")
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
		return nil, fmt.Errorf("no manifest found")
	}

	result.TotalManifest = len(manifest.Files)

	// Check all manifest entries have objects
	for path, entry := range manifest.Files {
		if !store.Exists(entry.ContentHash) {
			result.MissingObjects = append(result.MissingObjects, path)
			if verbose {
				fmt.Fprintf(os.Stderr, "  [missing] %s (hash: %s)\n", path, entry.ContentHash[:16])
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
				fmt.Fprintf(os.Stderr, "  [orphan] %s\n", hash[:16])
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

	// Track old hashes that get superseded during repair so we can
	// include them in orphan deletion (they become orphaned only after
	// the manifest is updated with new hashes).
	var superseded []string

	// Try to fix missing/corrupt by re-sealing from plaintext
	for _, path := range append(result.MissingObjects, result.CorruptObjects...) {
		absPath := filepath.Join(cfg.Seal.ClaudeDir, path)
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
		if oldHash != hash && oldHash != "" {
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
		}
		// Recompute JSONL line count
		if strings.HasSuffix(path, ".jsonl") {
			entry.JSONLLineCount = bytes.Count(plaintext, []byte("\n"))
			if len(plaintext) > 0 && plaintext[len(plaintext)-1] != '\n' {
				entry.JSONLLineCount++
			}
		}
		entry.SessionComplete = isSessionComplete(path)
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
				fmt.Printf("  [deleted] orphan %s\n", hash[:16])
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
		return 0, fmt.Errorf("loading manifest: %w", err)
	}
	if manifest == nil {
		return 0, fmt.Errorf("no manifest found")
	}

	total := len(manifest.Files)
	rotated := 0
	i := 0

	for path, entry := range manifest.Files {
		i++
		if progress != nil {
			progress(i, total, path)
		}

		encrypted, err := store.Read(entry.ContentHash)
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "  warning: cannot read %s: %v\n", path, err)
			}
			continue
		}

		plaintext, err := crypto.Decrypt(encrypted, oldIdentity)
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "  warning: cannot decrypt %s: %v\n", path, err)
			}
			continue
		}

		newEncrypted, err := crypto.Encrypt(plaintext, newRecipient)
		if err != nil {
			if verbose {
				fmt.Fprintf(os.Stderr, "  warning: cannot re-encrypt %s: %v\n", path, err)
			}
			continue
		}

		// Overwrite in place — content hash stays the same
		if err := store.Write(entry.ContentHash, newEncrypted); err != nil {
			continue
		}

		rotated++
	}

	// Save manifest (updates SealedAt timestamp)
	if err := manifest.Save(sealDir); err != nil {
		return rotated, fmt.Errorf("saving manifest: %w", err)
	}

	return rotated, nil
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
