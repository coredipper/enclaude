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

var pullCmd = &cobra.Command{
	Use:   "pull [remote]",
	Short: "Pull from remote, merge, and unseal",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runPull,
}

func init() {
	rootCmd.AddCommand(pullCmd)
}

func runPull(cmd *cobra.Command, args []string) error {
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
		return fmt.Errorf("remote '%s' not configured", remote)
	}

	// Seal local changes first (commit before merge)
	recipient, _, err := crypto.LoadPublicKey()
	if err != nil {
		return err
	}

	fmt.Println("Sealing local changes...")
	sealStats, err := store.Seal(cfg, recipient, flagVerbose, nil)
	if err != nil {
		return fmt.Errorf("seal: %w", err)
	}
	if sealStats.Errors > 0 {
		return fmt.Errorf("seal had %d errors — resolve before pulling to avoid data loss", sealStats.Errors)
	}
	if sealStats.HasChanges() {
		if err := git.AddAll(); err != nil {
			return fmt.Errorf("git add: %w", err)
		}
		msg := fmt.Sprintf("seal: pre-pull seal from %s (%s)",
			cfg.Seal.DeviceID, sealStats)
		if err := git.Commit(msg); err != nil {
			return fmt.Errorf("git commit: %w", err)
		}
		fmt.Printf("  %s\n", sealStats)
	} else {
		fmt.Println("  No local changes.")
	}

	// Pull (fetch + merge)
	branch, _ := git.CurrentBranch()
	fmt.Printf("Pulling from %s/%s...\n", remote, branch)

	out, err := git.Pull(remote, branch)
	if err != nil {
		if strings.Contains(out, "CONFLICT") {
			fmt.Println("  Merge conflicts detected. The merge driver should have resolved manifest conflicts.")
			fmt.Println("  If issues remain, run 'enclaude repair'.")
		} else {
			return fmt.Errorf("pull failed: %w\n%s", err, out)
		}
	}

	if strings.Contains(out, "Already up to date") {
		fmt.Println("  Already up to date.")
	} else {
		fmt.Printf("  %s\n", out)
	}

	// Unseal merged state
	identity, _, err := crypto.LoadKey()
	if err != nil {
		return err
	}

	fmt.Println("Unsealing...")
	unsealStats, err := store.Unseal(cfg, identity, flagVerbose, nil)
	if err != nil {
		return fmt.Errorf("unseal: %w", err)
	}
	fmt.Printf("  %s\n", unsealStats)

	return nil
}
