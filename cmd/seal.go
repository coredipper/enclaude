package cmd

import (
	"fmt"
	"os/exec"

	"github.com/coredipper/claude-vault/internal/config"
	"github.com/coredipper/claude-vault/internal/crypto"
	"github.com/coredipper/claude-vault/internal/vault"
	"github.com/spf13/cobra"
)

var sealCmd = &cobra.Command{
	Use:   "seal",
	Short: "Encrypt changed files into the vault",
	Long:  "Scan ~/.claude/ for changes, encrypt new/modified files, and commit to vault.",
	RunE:  runSeal,
}

func init() {
	rootCmd.AddCommand(sealCmd)
}

func runSeal(cmd *cobra.Command, args []string) error {
	vaultDir := getVaultDir()

	cfg, err := config.Load(vaultDir)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Override dirs from flags
	if flagClaudeDir != "" {
		cfg.Vault.ClaudeDir = flagClaudeDir
	}

	recipient, source, err := crypto.LoadPublicKey()
	if err != nil {
		return fmt.Errorf("loading key: %w", err)
	}
	if flagVerbose {
		fmt.Printf("Using key from %s\n", source)
	}

	if flagDryRun {
		fmt.Println("(dry run — no changes will be made)")
	}

	fmt.Println("Sealing...")
	stats, err := vault.Seal(cfg, recipient, flagVerbose, nil)
	if err != nil {
		return fmt.Errorf("seal: %w", err)
	}
	fmt.Printf("  %s\n", stats)

	if flagDryRun {
		return nil
	}

	// Commit if there are changes
	if stats.Added > 0 || stats.Modified > 0 {
		gitAdd := exec.Command("git", "-C", vaultDir, "add", ".")
		if err := gitAdd.Run(); err != nil {
			return fmt.Errorf("git add: %w", err)
		}

		msg := fmt.Sprintf("vault: seal from %s (%d new, %d modified)",
			cfg.Vault.DeviceID, stats.Added, stats.Modified)
		gitCommit := exec.Command("git", "-C", vaultDir, "commit", "-m", msg)
		if flagVerbose {
			gitCommit.Stdout = cmd.OutOrStdout()
			gitCommit.Stderr = cmd.ErrOrStderr()
		}
		if err := gitCommit.Run(); err != nil {
			return fmt.Errorf("git commit: %w", err)
		}
		fmt.Println("  Committed to vault.")
	} else {
		fmt.Println("  No changes to commit.")
	}

	if stats.Errors > 0 {
		fmt.Printf("  Warning: %d errors encountered.\n", stats.Errors)
	}

	return nil
}
