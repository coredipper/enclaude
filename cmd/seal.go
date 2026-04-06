package cmd

import (
	"fmt"
	"os/exec"

	"github.com/coredipper/claude-seal/internal/config"
	"github.com/coredipper/claude-seal/internal/crypto"
	"github.com/coredipper/claude-seal/internal/store"
	"github.com/spf13/cobra"
)

var sealCmd = &cobra.Command{
	Use:   "seal",
	Short: "Encrypt changed files into the seal store",
	Long:  "Scan ~/.claude/ for changes, encrypt new/modified files, and commit to seal store.",
	RunE:  runSeal,
}

func init() {
	rootCmd.AddCommand(sealCmd)
}

func runSeal(cmd *cobra.Command, args []string) error {
	sealDir := getSealDir()

	cfg, err := config.Load(sealDir)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// Override dirs from flags
	if flagClaudeDir != "" {
		cfg.Seal.ClaudeDir = flagClaudeDir
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
	stats, err := store.Seal(cfg, recipient, flagVerbose, nil)
	if err != nil {
		return fmt.Errorf("seal: %w", err)
	}
	fmt.Printf("  %s\n", stats)

	if flagDryRun {
		return nil
	}

	// Commit if there are changes
	if stats.Added > 0 || stats.Modified > 0 {
		gitAdd := exec.Command("git", "-C", sealDir, "add", ".")
		if err := gitAdd.Run(); err != nil {
			return fmt.Errorf("git add: %w", err)
		}

		msg := fmt.Sprintf("seal: seal from %s (%d new, %d modified)",
			cfg.Seal.DeviceID, stats.Added, stats.Modified)
		gitCommit := exec.Command("git", "-C", sealDir, "commit", "-m", msg)
		if flagVerbose {
			gitCommit.Stdout = cmd.OutOrStdout()
			gitCommit.Stderr = cmd.ErrOrStderr()
		}
		if err := gitCommit.Run(); err != nil {
			return fmt.Errorf("git commit: %w", err)
		}
		fmt.Println("  Committed to seal store.")
	} else {
		fmt.Println("  No changes to commit.")
	}

	if stats.Errors > 0 {
		fmt.Printf("  Warning: %d errors encountered.\n", stats.Errors)
	}

	return nil
}
