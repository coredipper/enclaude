package cmd

import (
	"fmt"
	"os/exec"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/store"
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

	if flagDryRun {
		fmt.Println("(dry run — showing what would be sealed)")
		diff, err := store.Status(cfg)
		if err != nil {
			return fmt.Errorf("status: %w", err)
		}
		if len(diff.Added) == 0 && len(diff.Modified) == 0 && len(diff.Deleted) == 0 {
			fmt.Println("  No changes to seal.")
		} else {
			for _, p := range diff.Added {
				fmt.Printf("  [new] %s\n", p)
			}
			for _, p := range diff.Modified {
				fmt.Printf("  [mod] %s\n", p)
			}
			for _, p := range diff.Deleted {
				fmt.Printf("  [del] %s\n", p)
			}
		}
		return nil
	}

	recipient, source, err := crypto.LoadPublicKey()
	if err != nil {
		return fmt.Errorf("loading key: %w", err)
	}
	if flagVerbose {
		fmt.Printf("Using key from %s\n", source)
	}

	fmt.Println("Sealing...")
	stats, err := store.Seal(cfg, recipient, flagVerbose, nil)
	if err != nil {
		return fmt.Errorf("seal: %w", err)
	}
	fmt.Printf("  %s\n", stats)

	if stats.Errors > 0 {
		fmt.Printf("  Warning: %d errors encountered. Skipping commit.\n", stats.Errors)
		return fmt.Errorf("seal had %d errors", stats.Errors)
	}

	// Commit if there are changes
	if stats.HasChanges() {
		gitAdd := exec.Command("git", "-C", sealDir, "add", ".")
		if err := gitAdd.Run(); err != nil {
			return fmt.Errorf("git add: %w", err)
		}

		msg := fmt.Sprintf("seal: seal from %s (%s)",
			cfg.Seal.DeviceID, stats)
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

	return nil
}
