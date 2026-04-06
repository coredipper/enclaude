package cmd

import (
	"fmt"

	"github.com/coredipper/claude-vault/internal/config"
	"github.com/coredipper/claude-vault/internal/crypto"
	"github.com/coredipper/claude-vault/internal/gitops"
	"github.com/coredipper/claude-vault/internal/ui"
	"github.com/coredipper/claude-vault/internal/vault"
	"github.com/spf13/cobra"
)

var rollbackForce bool

var rollbackCmd = &cobra.Command{
	Use:   "rollback <ref>",
	Short: "Restore vault to a previous state",
	Long:  "Restores ~/.claude/ to the state captured at a specific vault commit. Creates a safety commit first.",
	Args:  cobra.ExactArgs(1),
	RunE:  runRollback,
}

func init() {
	rollbackCmd.Flags().BoolVar(&rollbackForce, "force", false, "skip confirmation")
	rootCmd.AddCommand(rollbackCmd)
}

func runRollback(cmd *cobra.Command, args []string) error {
	ref := args[0]
	vaultDir := getVaultDir()

	cfg, err := config.Load(vaultDir)
	if err != nil {
		return err
	}
	if flagClaudeDir != "" {
		cfg.Vault.ClaudeDir = flagClaudeDir
	}

	git := gitops.New(vaultDir)
	identity, _, err := crypto.LoadKey()
	if err != nil {
		return err
	}

	// Verify ref exists
	if _, err := git.ShowFileAtRef(ref, "manifest.json"); err != nil {
		return fmt.Errorf("cannot find manifest at ref '%s': %w", ref, err)
	}

	if !rollbackForce {
		fmt.Printf("This will restore ~/.claude/ to the state at %s.\n", ui.Cyan(ref))
		fmt.Println("A safety commit of the current state will be created first.")
		fmt.Printf("Use %s to skip this confirmation.\n\n", ui.Faint("--force"))
		fmt.Print("Continue? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Step 1: Safety seal
	fmt.Println("1/4 Creating safety seal of current state...")
	recipient := identity.Recipient()
	stats, err := vault.Seal(cfg, recipient, false, nil)
	if err != nil {
		return fmt.Errorf("safety seal: %w", err)
	}
	if stats.Added > 0 || stats.Modified > 0 {
		git.AddAll()
		git.Commit(fmt.Sprintf("vault: pre-rollback safety seal from %s", cfg.Vault.DeviceID))
		fmt.Printf("    %s\n", stats)
	} else {
		fmt.Println("    No changes to commit.")
	}

	// Step 2: Checkout manifest + objects from ref
	fmt.Printf("2/4 Restoring vault to %s...\n", ref)
	if out, err := git.Checkout(ref, "manifest.json", "objects/"); err != nil {
		return fmt.Errorf("git checkout: %w\n%s", err, out)
	}

	// Step 3: Commit the rollback
	fmt.Println("3/4 Committing rollback...")
	git.AddAll()
	git.Commit(fmt.Sprintf("vault: rollback to %s from %s", ref, cfg.Vault.DeviceID))

	// Step 4: Unseal
	fmt.Println("4/4 Unsealing restored state...")
	unsealStats, err := vault.Unseal(cfg, identity, flagVerbose, nil)
	if err != nil {
		return fmt.Errorf("unseal: %w", err)
	}
	fmt.Printf("    %s\n", unsealStats)

	fmt.Printf("\n%s Rolled back to %s.\n", ui.Green("Done."), ref)
	fmt.Println("The pre-rollback state is preserved in git history.")
	return nil
}
