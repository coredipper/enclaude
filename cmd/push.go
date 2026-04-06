package cmd

import (
	"fmt"

	"github.com/coredipper/claude-vault/internal/config"
	"github.com/coredipper/claude-vault/internal/crypto"
	"github.com/coredipper/claude-vault/internal/gitops"
	"github.com/coredipper/claude-vault/internal/vault"
	"github.com/spf13/cobra"
)

var pushCmd = &cobra.Command{
	Use:   "push [remote]",
	Short: "Seal and push to remote",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runPush,
}

func init() {
	rootCmd.AddCommand(pushCmd)
}

func runPush(cmd *cobra.Command, args []string) error {
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

	// Check remote exists
	if !git.HasRemote(remote) {
		return fmt.Errorf("remote '%s' not configured. Run: claude-vault remote add %s <url>", remote, remote)
	}

	// Seal first
	recipient, _, err := crypto.LoadPublicKey()
	if err != nil {
		return err
	}

	fmt.Println("Sealing...")
	stats, err := vault.Seal(cfg, recipient, flagVerbose, nil)
	if err != nil {
		return fmt.Errorf("seal: %w", err)
	}
	fmt.Printf("  %s\n", stats)

	if stats.Added > 0 || stats.Modified > 0 {
		git.AddAll()
		msg := fmt.Sprintf("vault: seal from %s (%d new, %d modified)",
			cfg.Vault.DeviceID, stats.Added, stats.Modified)
		git.Commit(msg)
	}

	// Push
	branch, _ := git.CurrentBranch()
	fmt.Printf("Pushing to %s/%s...\n", remote, branch)

	var out string
	if git.HasUpstream() {
		out, err = git.Push(remote, branch)
	} else {
		out, err = git.PushWithUpstream(remote, branch)
	}
	if err != nil {
		return fmt.Errorf("push failed: %w\n%s", err, out)
	}

	fmt.Println("  Pushed successfully.")
	return nil
}
