package cmd

import (
	"fmt"

	"github.com/coredipper/claude-vault/internal/config"
	"github.com/coredipper/claude-vault/internal/ui"
	"github.com/coredipper/claude-vault/internal/vault"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show what changed since last seal",
	Long:  "Compare ~/.claude/ against the vault manifest to show pending changes.",
	RunE:  runStatus,
}

func init() {
	rootCmd.AddCommand(statusCmd)
}

func runStatus(cmd *cobra.Command, args []string) error {
	vaultDir := getVaultDir()

	cfg, err := config.Load(vaultDir)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if flagClaudeDir != "" {
		cfg.Vault.ClaudeDir = flagClaudeDir
	}

	diff, err := vault.Status(cfg)
	if err != nil {
		return fmt.Errorf("computing status: %w", err)
	}

	if len(diff.Added) == 0 && len(diff.Modified) == 0 && len(diff.Deleted) == 0 {
		fmt.Println("Vault is up to date. No changes since last seal.")
		return nil
	}

	if len(diff.Added) > 0 {
		fmt.Printf("\n%s (%d):\n", ui.Green("New files"), len(diff.Added))
		for _, f := range diff.Added {
			fmt.Printf("  %s %s\n", ui.Green("+"), f)
		}
	}

	if len(diff.Modified) > 0 {
		fmt.Printf("\n%s (%d):\n", ui.Yellow("Modified files"), len(diff.Modified))
		for _, f := range diff.Modified {
			fmt.Printf("  %s %s\n", ui.Yellow("~"), f)
		}
	}

	if len(diff.Deleted) > 0 {
		fmt.Printf("\n%s (%d):\n", ui.Red("Deleted files"), len(diff.Deleted))
		for _, f := range diff.Deleted {
			fmt.Printf("  %s %s\n", ui.Red("-"), f)
		}
	}

	fmt.Printf("\nTotal: %s new, %s modified, %s deleted\n",
		ui.Green(fmt.Sprintf("%d", len(diff.Added))),
		ui.Yellow(fmt.Sprintf("%d", len(diff.Modified))),
		ui.Red(fmt.Sprintf("%d", len(diff.Deleted))))
	fmt.Println("Run 'claude-vault seal' to encrypt and commit these changes.")

	return nil
}
