package cmd

import (
	"fmt"
	"strings"

	"github.com/coredipper/claude-seal/internal/config"
	"github.com/coredipper/claude-seal/internal/crypto"
	"github.com/coredipper/claude-seal/internal/gitops"
	"github.com/coredipper/claude-seal/internal/store"
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
		return fmt.Errorf("remote '%s' not configured. Run: claude-seal remote add %s <url>", remote, remote)
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
	fmt.Printf("    %s\n", sealStats)

	if sealStats.Added > 0 || sealStats.Modified > 0 {
		git.AddAll()
		msg := fmt.Sprintf("seal: seal from %s (%d new, %d modified)",
			cfg.Seal.DeviceID, sealStats.Added, sealStats.Modified)
		git.Commit(msg)
	}

	// 2. Pull
	branch, _ := git.CurrentBranch()
	fmt.Printf("2/3 Pulling from %s/%s...\n", remote, branch)

	out, err := git.Pull(remote, branch)
	if err != nil {
		if strings.Contains(out, "CONFLICT") {
			fmt.Println("    Merge conflicts detected — resolve manually or run 'claude-seal repair'.")
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
