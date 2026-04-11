package cmd

import (
	"fmt"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/store"
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

	if flagDryRun {
		fmt.Println("(dry run — showing what would change)")
		diff, err := store.UnsealStatus(cfg)
		if err != nil {
			return fmt.Errorf("unseal status: %w", err)
		}
		if len(diff.Added) == 0 && len(diff.Modified) == 0 && len(diff.Deleted) == 0 {
			fmt.Println("  No changes needed.")
		} else {
			for _, p := range diff.Added {
				fmt.Printf("  [restore] %s\n", p)
			}
			for _, p := range diff.Modified {
				fmt.Printf("  [update] %s\n", p)
			}
			for _, p := range diff.Deleted {
				fmt.Printf("  [delete] %s\n", p)
			}
		}
		return nil
	}

	identity, source, err := crypto.LoadKey()
	if err != nil {
		return fmt.Errorf("loading key: %w", err)
	}
	if flagVerbose {
		fmt.Printf("Using key from %s\n", source)
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
