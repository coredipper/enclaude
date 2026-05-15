package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/gitops"
	"github.com/coredipper/enclaude/internal/ui"
	sealstore "github.com/coredipper/enclaude/internal/store"
	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff [ref]",
	Short: "Show plaintext diff between seal states",
	Long:  "Decrypts and diffs seal contents between the current state and a previous commit.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runDiff,
}

func init() {
	rootCmd.AddCommand(diffCmd)
}

func runDiff(cmd *cobra.Command, args []string) error {
	sealDir := getSealDir()
	ref := "HEAD~1"
	if len(args) > 0 {
		ref = args[0]
	}

	cfg, err := config.Load(sealDir)
	if err != nil {
		return err
	}

	identity, _, err := crypto.LoadKey()
	if err != nil {
		return err
	}

	git := gitops.New(sealDir)
	store := sealstore.NewObjectStore(sealDir)

	// Load current manifest
	currentManifest, err := sealstore.LoadManifest(sealDir)
	if err != nil {
		return fmt.Errorf("loading current manifest: %w", err)
	}
	if currentManifest == nil {
		return fmt.Errorf("no manifest found")
	}

	// Load old manifest from git ref
	oldManifestJSON, err := git.ShowFileAtRef(ref, "manifest.json")
	if err != nil {
		return fmt.Errorf("cannot read manifest at %s: %w", ref, err)
	}

	var oldManifest sealstore.Manifest
	if err := json.Unmarshal([]byte(oldManifestJSON), &oldManifest); err != nil {
		return fmt.Errorf("parsing manifest at %s: %w", ref, err)
	}
	if oldManifest.Files == nil {
		oldManifest.Files = make(map[string]sealstore.FileEntry)
	}

	// Diff manifests
	diff := currentManifest.Diff(&oldManifest)

	if len(diff.Added) == 0 && len(diff.Modified) == 0 && len(diff.Deleted) == 0 {
		fmt.Printf("No changes between %s and current seal store.\n", ref)
		return nil
	}

	_ = cfg // used for context if needed

	// Show added files
	for _, path := range diff.Added {
		entry := currentManifest.Files[path]
		fmt.Printf("%s %s %s\n", ui.Green("+++ new"), path, ui.Faint(fmt.Sprintf("(%s)", sealstore.FormatSize(entry.SizePlaintext))))
	}

	// Show deleted files
	for _, path := range diff.Deleted {
		fmt.Printf("%s %s\n", ui.Red("--- deleted"), path)
	}

	// Show modified files with content diff
	for _, path := range diff.Modified {
		currentEntry := currentManifest.Files[path]
		oldEntry := oldManifest.Files[path]

		fmt.Printf("\n%s %s %s\n", ui.Yellow("~~~ modified"), path, ui.Faint("("+currentEntry.MergeStrategy+")"))

		// Decrypt both versions
		currentEnc, err := store.Read(currentEntry.ContentHash)
		if err != nil {
			fmt.Printf("  %s\n", ui.Red("cannot read current version"))
			continue
		}
		currentPlain, err := crypto.Decrypt(currentEnc, identity)
		if err != nil {
			fmt.Printf("  %s\n", ui.Red("cannot decrypt current version"))
			continue
		}

		oldEnc, err := store.Read(oldEntry.ContentHash)
		if err != nil {
			fmt.Printf("  %s\n", ui.Faint("(old object not available locally)"))
			continue
		}
		oldPlain, err := crypto.Decrypt(oldEnc, identity)
		if err != nil {
			fmt.Printf("  %s\n", ui.Red("cannot decrypt old version"))
			continue
		}

		// For JSONL files, show added/removed lines
		if strings.HasSuffix(path, ".jsonl") {
			showJSONLDiff(string(oldPlain), string(currentPlain))
		} else {
			showTextDiff(string(oldPlain), string(currentPlain))
		}
	}

	fmt.Printf("\n%s %d added, %d modified, %d deleted (vs %s)\n",
		ui.Bold("Summary:"),
		len(diff.Added), len(diff.Modified), len(diff.Deleted), ref)

	return nil
}

func showJSONLDiff(old, current string) {
	oldLines := toSet(nonEmpty(strings.Split(old, "\n")))
	curLines := toSet(nonEmpty(strings.Split(current, "\n")))

	added := 0
	removed := 0

	for line := range curLines {
		if _, exists := oldLines[line]; !exists {
			added++
			if added <= 10 { // limit output
				fmt.Printf("  %s %s\n", ui.Green("+"), truncate(line, 120))
			}
		}
	}
	for line := range oldLines {
		if _, exists := curLines[line]; !exists {
			removed++
			if removed <= 10 {
				fmt.Printf("  %s %s\n", ui.Red("-"), truncate(line, 120))
			}
		}
	}

	if added > 10 {
		fmt.Printf("  %s\n", ui.Faint(fmt.Sprintf("... and %d more added lines", added-10)))
	}
	if removed > 10 {
		fmt.Printf("  %s\n", ui.Faint(fmt.Sprintf("... and %d more removed lines", removed-10)))
	}
	if added == 0 && removed == 0 {
		fmt.Printf("  %s\n", ui.Faint("(content identical after normalization)"))
	}
}

func showTextDiff(old, current string) {
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(old, current, true)
	diffs = dmp.DiffCleanupSemantic(diffs)

	for _, d := range diffs {
		lines := strings.Split(d.Text, "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			switch d.Type {
			case diffmatchpatch.DiffInsert:
				fmt.Printf("  %s %s\n", ui.Green("+"), line)
			case diffmatchpatch.DiffDelete:
				fmt.Printf("  %s %s\n", ui.Red("-"), line)
			}
		}
	}
}

func toSet(lines []string) map[string]struct{} {
	s := make(map[string]struct{})
	for _, l := range lines {
		s[l] = struct{}{}
	}
	return s
}

func nonEmpty(lines []string) []string {
	var result []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			result = append(result, l)
		}
	}
	return result
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
