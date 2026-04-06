package cmd

import (
	"fmt"

	"github.com/coredipper/claude-seal/internal/config"
	"github.com/coredipper/claude-seal/internal/crypto"
	"github.com/coredipper/claude-seal/internal/gitops"
	"github.com/coredipper/claude-seal/internal/ui"
	"github.com/coredipper/claude-seal/internal/store"
	"github.com/spf13/cobra"
)

var rollbackForce bool

var rollbackCmd = &cobra.Command{
	Use:   "rollback <ref>",
	Short: "Restore seal to a previous state",
	Long:  "Restores ~/.claude/ to the state captured at a specific seal commit. Creates a safety commit first.",
	Args:  cobra.ExactArgs(1),
	RunE:  runRollback,
}

func init() {
	rollbackCmd.Flags().BoolVar(&rollbackForce, "force", false, "skip confirmation")
	rootCmd.AddCommand(rollbackCmd)
}

func runRollback(cmd *cobra.Command, args []string) error {
	ref := args[0]
	sealDir := getSealDir()

	cfg, err := config.Load(sealDir)
	if err != nil {
		return err
	}
	if flagClaudeDir != "" {
		cfg.Seal.ClaudeDir = flagClaudeDir
	}

	git := gitops.New(sealDir)
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
	stats, err := store.Seal(cfg, recipient, false, nil)
	if err != nil {
		return fmt.Errorf("safety seal: %w", err)
	}
	if stats.Added > 0 || stats.Modified > 0 {
		git.AddAll()
		git.Commit(fmt.Sprintf("seal: pre-rollback safety seal from %s", cfg.Seal.DeviceID))
		fmt.Printf("    %s\n", stats)
	} else {
		fmt.Println("    No changes to commit.")
	}

	// Step 2: Checkout manifest + objects from ref
	fmt.Printf("2/4 Restoring seal to %s...\n", ref)
	if out, err := git.Checkout(ref, "manifest.json", "objects/"); err != nil {
		return fmt.Errorf("git checkout: %w\n%s", err, out)
	}

	// Step 3: Commit the rollback
	fmt.Println("3/4 Committing rollback...")
	git.AddAll()
	git.Commit(fmt.Sprintf("seal: rollback to %s from %s", ref, cfg.Seal.DeviceID))

	// Step 4: Unseal
	fmt.Println("4/4 Unsealing restored state...")
	unsealStats, err := store.Unseal(cfg, identity, flagVerbose, nil)
	if err != nil {
		return fmt.Errorf("unseal: %w", err)
	}
	fmt.Printf("    %s\n", unsealStats)

	fmt.Printf("\n%s Rolled back to %s.\n", ui.Green("Done."), ref)
	fmt.Println("The pre-rollback state is preserved in git history.")
	return nil
}
