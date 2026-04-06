package cmd

import (
	"fmt"

	"github.com/coredipper/claude-seal/internal/config"
	"github.com/coredipper/claude-seal/internal/crypto"
	"github.com/coredipper/claude-seal/internal/store"
	"github.com/spf13/cobra"
)

var unsealCmd = &cobra.Command{
	Use:   "unseal",
	Short: "Decrypt seal contents to ~/.claude/",
	Long:  "Decrypt all sealed objects and restore them to the Claude directory.",
	RunE:  runUnseal,
}

func init() {
	rootCmd.AddCommand(unsealCmd)
}

func runUnseal(cmd *cobra.Command, args []string) error {
	sealDir := getSealDir()

	cfg, err := config.Load(sealDir)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if flagClaudeDir != "" {
		cfg.Seal.ClaudeDir = flagClaudeDir
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
	stats, err := store.Unseal(cfg, identity, flagVerbose, nil)
	if err != nil {
		return fmt.Errorf("unseal: %w", err)
	}
	fmt.Printf("  %s\n", stats)

	if stats.Errors > 0 {
		fmt.Printf("  Warning: %d errors encountered.\n", stats.Errors)
	}

	return nil
}
