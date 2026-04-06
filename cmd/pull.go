package cmd

import (
	"fmt"
	"strings"

	"github.com/coredipper/claude-vault/internal/config"
	"github.com/coredipper/claude-vault/internal/crypto"
	"github.com/coredipper/claude-vault/internal/gitops"
	"github.com/coredipper/claude-vault/internal/vault"
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
	vaultDir := getVaultDir()
	remote := "origin"
	if len(args) > 0 {
		remote = args[0]
	}

	cfg, err := config.Load(vaultDir)
	if err != nil {
		return err
	}
	if flagClaudeDir != "" {
		cfg.Vault.ClaudeDir = flagClaudeDir
	}

	git := gitops.New(vaultDir)

	if !git.HasRemote(remote) {
		return fmt.Errorf("remote '%s' not configured", remote)
	}

	// Seal local changes first (commit before merge)
	recipient, _, err := crypto.LoadPublicKey()
	if err != nil {
		return err
	}

	fmt.Println("Sealing local changes...")
	sealStats, err := vault.Seal(cfg, recipient, flagVerbose, nil)
	if err != nil {
		return fmt.Errorf("seal: %w", err)
	}
	if sealStats.Added > 0 || sealStats.Modified > 0 {
		git.AddAll()
		msg := fmt.Sprintf("vault: pre-pull seal from %s (%d new, %d modified)",
			cfg.Vault.DeviceID, sealStats.Added, sealStats.Modified)
		git.Commit(msg)
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
			fmt.Println("  If issues remain, run 'claude-vault repair'.")
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
	unsealStats, err := vault.Unseal(cfg, identity, flagVerbose, nil)
	if err != nil {
		return fmt.Errorf("unseal: %w", err)
	}
	fmt.Printf("  %s\n", unsealStats)

	return nil
}
