package cmd

import (
	"fmt"
	"strings"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/gitops"
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
	fmt.Printf("    %s\n", sealStats)

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

	out, err := git.Pull(remote, branch)
	if err != nil {
		if strings.Contains(out, "CONFLICT") {
			fmt.Println("    Merge conflicts detected — resolve manually or run 'enclaude repair'.")
		} else {
			return fmt.Errorf("pull failed: %w\n%s", err, out)
		}
	} else if strings.Contains(out, "Already up to date") {
		fmt.Println("    Already up to date.")
	} else {
		// Unseal after pull
		identity, _, err := crypto.LoadKey()
		if err != nil {
			return err
		}
		unsealStats, err := store.Unseal(cfg, identity, flagVerbose, nil)
		if err != nil {
			return fmt.Errorf("unseal: %w", err)
		}
		fmt.Printf("    Merged and unsealed: %s\n", unsealStats)
	}

	// 3. Push
	fmt.Printf("3/3 Pushing to %s/%s...\n", remote, branch)
	if git.HasUpstream() {
		out, err = git.Push(remote, branch)
	} else {
		out, err = git.PushWithUpstream(remote, branch)
	}
	if err != nil {
		return fmt.Errorf("push failed: %w\n%s", err, out)
	}
	fmt.Println("    Pushed successfully.")

	fmt.Println("\nSync complete.")
	return nil
}
