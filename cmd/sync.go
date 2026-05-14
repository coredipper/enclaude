package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/gitops"
	"github.com/coredipper/enclaude/internal/merge"
	"github.com/coredipper/enclaude/internal/store"
	"github.com/spf13/cobra"
)

var syncCmd = &cobra.Command{
	Use:   "sync [remote]",
	Short: "Seal, pull, push — the daily driver",
	Long:  "Encrypts local changes, pulls remote changes (with merge), then pushes.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runSync,
}

func init() {
	rootCmd.AddCommand(syncCmd)
}

func runSync(cmd *cobra.Command, args []string) error {
	start := time.Now()

	sealDir := getSealDir()
	remote := "origin"
	if len(args) > 0 {
		remote = args[0]
	}

	cfg, err := config.Load(sealDir)
	if err != nil {
		return err
	}
	if flagClaudeDir != "" {
		cfg.Seal.ClaudeDir = flagClaudeDir
	}

	git := gitops.New(sealDir)

	if !git.HasRemote(remote) {
		return fmt.Errorf("remote '%s' not configured. Run: enclaude remote add %s <url>", remote, remote)
	}

	// 1. Seal
	recipient, _, err := crypto.LoadPublicKey()
	if err != nil {
		return err
	}

	fmt.Println("1/3 Sealing local changes...")
	sealStats, err := store.Seal(cfg, recipient, flagVerbose, nil)
	if err != nil {
		return fmt.Errorf("seal: %w", err)
	}
	if sealStats.Errors > 0 {
		return fmt.Errorf("seal had %d errors — resolve before syncing to avoid data loss", sealStats.Errors)
	}
	fmt.Println(sealStats.Multiline("    "))

	if sealStats.HasChanges() {
		if err := git.AddAll(); err != nil {
			return fmt.Errorf("git add: %w", err)
		}
		msg := fmt.Sprintf("seal: seal from %s (%s)",
			cfg.Seal.DeviceID, sealStats)
		if err := git.Commit(msg); err != nil {
			return fmt.Errorf("git commit: %w", err)
		}
	}

	// 2. Pull
	branch, _ := git.CurrentBranch()
	fmt.Printf("2/3 Pulling from %s/%s...\n", remote, branch)

	pullStats, pullOut, err := git.Pull(remote, branch)
	if err != nil {
		if strings.Contains(pullOut, "CONFLICT") {
			fmt.Println("    Merge conflicts detected — resolve manually or run 'enclaude repair'.")
		} else {
			return fmt.Errorf("pull failed: %w\n%s", err, pullOut)
		}
	}

	mergeAgg := merge.ParseDriverLines(pullOut)
	if pullStats.UpToDate {
		fmt.Println("    Already up to date.")
	} else {
		// Unseal merged state
		identity, _, err := crypto.LoadKey()
		if err != nil {
			return err
		}
		unsealStats, err := store.Unseal(cfg, identity, flagVerbose, nil)
		if err != nil {
			return fmt.Errorf("unseal: %w", err)
		}
		unsealStats.Merges = mergeAgg
		fmt.Println(formatPullLine("    ", pullStats, mergeAgg))
		fmt.Println(unsealStats.Multiline("    "))
	}

	// 3. Push
	fmt.Printf("3/3 Pushing to %s/%s...\n", remote, branch)
	var pushStats gitops.PushStats
	var pushOut string
	if git.HasUpstream() {
		pushStats, pushOut, err = git.Push(remote, branch)
	} else {
		pushStats, pushOut, err = git.PushWithUpstream(remote, branch)
	}
	if err != nil {
		return fmt.Errorf("push failed: %w\n%s", err, pushOut)
	}
	fmt.Println(formatPushLine("    ", pushStats))

	fmt.Printf("\nSync complete in %s.\n", humanElapsed(time.Since(start)))
	return nil
}

// formatPullLine renders the pull summary for sync display.
func formatPullLine(indent string, p gitops.PullStats, m merge.Aggregate) string {
	if p.UpToDate {
		return indent + "Already up to date."
	}
	if m.FilesMerged > 0 {
		return fmt.Sprintf("%sPulled %d commits, %d files changed; merged %d files (%d dup lines removed)",
			indent, p.Commits, p.FilesChanged, m.FilesMerged, m.LinesDeduped)
	}
	return fmt.Sprintf("%sPulled %d commits, %d files changed.",
		indent, p.Commits, p.FilesChanged)
}

// formatPushLine renders the push summary for sync display.
func formatPushLine(indent string, s gitops.PushStats) string {
	if s.NoOp {
		return indent + "Nothing to push."
	}
	if s.Bytes > 0 {
		return fmt.Sprintf("%sPushed %d commits, %d objects (%s).",
			indent, s.Commits, s.Objects, store.FormatSize(s.Bytes))
	}
	if s.Objects > 0 {
		return fmt.Sprintf("%sPushed %d commits, %d objects.",
			indent, s.Commits, s.Objects)
	}
	return fmt.Sprintf("%sPushed %d commits.", indent, s.Commits)
}

// humanElapsed formats a wall-clock duration for end-of-sync display.
func humanElapsed(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return d.Round(time.Second).String()
}
