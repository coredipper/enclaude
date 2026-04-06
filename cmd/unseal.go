package cmd

import (
	"fmt"

	"github.com/coredipper/claude-vault/internal/config"
	"github.com/coredipper/claude-vault/internal/crypto"
	"github.com/coredipper/claude-vault/internal/vault"
	"github.com/spf13/cobra"
)

var unsealCmd = &cobra.Command{
	Use:   "unseal",
	Short: "Decrypt vault contents to ~/.claude/",
	Long:  "Decrypt all vault objects and restore them to the Claude directory.",
	RunE:  runUnseal,
}

func init() {
	rootCmd.AddCommand(unsealCmd)
}

func runUnseal(cmd *cobra.Command, args []string) error {
	vaultDir := getVaultDir()

	cfg, err := config.Load(vaultDir)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if flagClaudeDir != "" {
		cfg.Vault.ClaudeDir = flagClaudeDir
	}

	identity, source, err := crypto.LoadKey()
	if err != nil {
		return fmt.Errorf("loading key: %w", err)
	}
	if flagVerbose {
		fmt.Printf("Using key from %s\n", source)
	}

	if flagDryRun {
		fmt.Println("(dry run — showing what would be restored)")
	}

	fmt.Println("Unsealing...")
	stats, err := vault.Unseal(cfg, identity, flagVerbose, nil)
	if err != nil {
		return fmt.Errorf("unseal: %w", err)
	}
	fmt.Printf("  %s\n", stats)

	if stats.Errors > 0 {
		fmt.Printf("  Warning: %d errors encountered.\n", stats.Errors)
	}

	return nil
}
