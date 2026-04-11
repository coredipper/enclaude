package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/merge"
	sealstore "github.com/coredipper/enclaude/internal/store"
	"github.com/spf13/cobra"
)

var mergeDriverCmd = &cobra.Command{
	Use:    "merge-driver <type> <ancestor> <ours> <theirs>",
	Short:  "Git custom merge driver (invoked by git)",
	Hidden: true,
	Args:   cobra.ExactArgs(4),
	RunE:   runMergeDriver,
}

func init() {
	rootCmd.AddCommand(mergeDriverCmd)
}

func runMergeDriver(cmd *cobra.Command, args []string) error {
	mergeType := args[0]
	ancestorFile := args[1]
	oursFile := args[2]
	theirsFile := args[3]

	if mergeType != "manifest" {
		return fmt.Errorf("unknown merge type: %s", mergeType)
	}

	return mergeManifests(ancestorFile, oursFile, theirsFile)
}

// mergeManifests resolves a manifest.json conflict by merging per-file.
func mergeManifests(ancestorFile, oursFile, theirsFile string) error {
	sealDir := getSealDir()

	cfg, err := config.Load(sealDir)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	identity, _, err := crypto.LoadKey()
	if err != nil {
		return fmt.Errorf("loading key: %w", err)
	}

	// Read manifests
	ancestorData, _ := os.ReadFile(ancestorFile) // may not exist
	oursData, err := os.ReadFile(oursFile)
	if err != nil {
		return fmt.Errorf("reading ours: %w", err)
	}
	theirsData, err := os.ReadFile(theirsFile)
	if err != nil {
		return fmt.Errorf("reading theirs: %w", err)
	}

	var ancestor, ours, theirs sealstore.Manifest
	if len(ancestorData) > 0 {
		json.Unmarshal(ancestorData, &ancestor)
	}
	if err := json.Unmarshal(oursData, &ours); err != nil {
		return fmt.Errorf("parsing ours manifest: %w", err)
	}
	if err := json.Unmarshal(theirsData, &theirs); err != nil {
		return fmt.Errorf("parsing theirs manifest: %w", err)
	}

	objStore := sealstore.NewObjectStore(sealDir)
	merged := sealstore.Manifest{
		Version:  ours.Version,
		DeviceID: ours.DeviceID,
		SealedAt: time.Now().UTC().Format(time.RFC3339),
		Files:    make(map[string]sealstore.FileEntry),
	}

	// Collect all file paths from both sides
	allPaths := make(map[string]bool)
	for p := range ours.Files {
		allPaths[p] = true
	}
	for p := range theirs.Files {
		allPaths[p] = true
	}

	for path := range allPaths {
		oursEntry, inOurs := ours.Files[path]
		theirsEntry, inTheirs := theirs.Files[path]

		// Only in ours
		if inOurs && !inTheirs {
			merged.Files[path] = oursEntry
			continue
		}

		// Only in theirs — need to keep their object
		if !inOurs && inTheirs {
			merged.Files[path] = theirsEntry
			continue
		}

		// Same content — no conflict
		if oursEntry.ContentHash == theirsEntry.ContentHash {
			merged.Files[path] = oursEntry
			continue
		}

		// Different content — resolve merge strategy from config.
		resolvedStrategy, winningPattern := sealstore.ResolveMergeStrategyWithPattern(path, cfg.Merge)
		strategy := merge.Strategy(resolvedStrategy)
		if strategy == "" {
			strategy = merge.LastWriteWins
		}
		// Fail-safe: sessions-index.json is a JSON object, not JSONL.
		// jsonl_dedup is structurally incompatible and would produce corrupt
		// data regardless of which config rule resolved it.
		if strategy == merge.JSONLDedup && filepath.Base(path) == "sessions-index.json" {
			return fmt.Errorf("refusing to merge %s with jsonl_dedup (sessions-index.json is JSON, not JSONL; rule %q matched). Fix: change the strategy to 'sessions_index' in seal.toml, or run 'enclaude upgrade'", path, winningPattern)
		}

		// For immutable files, both sides should have the same hash.
		// If not, prefer ours (shouldn't happen for completed sessions).
		if strategy == merge.Immutable {
			merged.Files[path] = oursEntry
			continue
		}

		// For strategies that need content, decrypt both versions
		oursEncrypted, err := objStore.Read(oursEntry.ContentHash)
		if err != nil {
			merged.Files[path] = oursEntry
			continue
		}
		theirsEncrypted, err := objStore.Read(theirsEntry.ContentHash)
		if err != nil {
			merged.Files[path] = oursEntry
			continue
		}

		oursPlain, err := crypto.Decrypt(oursEncrypted, identity)
		if err != nil {
			merged.Files[path] = oursEntry
			continue
		}
		theirsPlain, err := crypto.Decrypt(theirsEncrypted, identity)
		if err != nil {
			merged.Files[path] = oursEntry
			continue
		}

		// Get ancestor content if available
		var ancestorPlain []byte
		if ancestorEntry, ok := ancestor.Files[path]; ok {
			if ancestorEnc, err := objStore.Read(ancestorEntry.ContentHash); err == nil {
				ancestorPlain, _ = crypto.Decrypt(ancestorEnc, identity)
			}
		}

		oursMtime, _ := time.Parse(time.RFC3339, oursEntry.Mtime)
		theirsMtime, _ := time.Parse(time.RFC3339, theirsEntry.Mtime)

		mergedContent, err := merge.Merge(
			strategy,
			ancestorPlain, oursPlain, theirsPlain,
			merge.FileMeta{Mtime: oursMtime},
			merge.FileMeta{Mtime: theirsMtime},
		)
		if err != nil {
			// On error, prefer ours
			merged.Files[path] = oursEntry
			continue
		}

		// Encrypt merged content and store
		mergedHash := sealstore.ContentHash(mergedContent)
		mergedEncrypted, err := crypto.Encrypt(mergedContent, identity.Recipient())
		if err != nil {
			merged.Files[path] = oursEntry
			continue
		}

		if err := objStore.Write(mergedHash, mergedEncrypted); err != nil {
			merged.Files[path] = oursEntry
			continue
		}

		merged.Files[path] = sealstore.FileEntry{
			ContentHash:    mergedHash,
			SizePlaintext:  int64(len(mergedContent)),
			SizeEncrypted:  int64(len(mergedEncrypted)),
			Mtime:          time.Now().UTC().Format(time.RFC3339),
			MergeStrategy:  string(strategy),
			JSONLLineCount: oursEntry.JSONLLineCount + theirsEntry.JSONLLineCount, // approximate
		}

		if flagVerbose || true { // always show merge activity
			fmt.Fprintf(os.Stderr, "  [merge:%s] %s\n", strategy, path)
		}
	}

	// Write merged manifest back to "ours" file (git convention)
	mergedData, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling merged manifest: %w", err)
	}

	// Git expects the result written to the "ours" file
	if err := os.WriteFile(oursFile, mergedData, 0600); err != nil {
		return fmt.Errorf("writing merged manifest: %w", err)
	}

	// Also update the actual manifest.json in the seal store
	if err := os.WriteFile(cfg.Seal.SealDir+"/manifest.json", mergedData, 0600); err != nil {
		return fmt.Errorf("writing seal manifest: %w", err)
	}

	return nil
}
