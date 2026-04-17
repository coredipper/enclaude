package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/spf13/cobra"
)

var readmeRegenCmd = &cobra.Command{
	Use:   "readme-regen",
	Short: "Regenerate README.md in the seal store",
	Long:  "Regenerate and commit README.md in an existing seal store.",
	RunE:  runReadmeRegen,
}

func init() {
	rootCmd.AddCommand(readmeRegenCmd)
}

func runReadmeRegen(cmd *cobra.Command, args []string) error {
	sealDir := getSealDir()

	if _, err := os.Stat(filepath.Join(sealDir, "seal.toml")); os.IsNotExist(err) {
		return fmt.Errorf("no seal store found at %s — run enclaude init first", sealDir)
	}

	cfg, err := config.Load(sealDir)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	recipient, source, err := crypto.LoadPublicKey()
	if err != nil {
		return fmt.Errorf("loading key: %w", err)
	}
	if flagVerbose {
		fmt.Printf("Using key from %s\n", source)
	}

	readme := buildReadme(recipient.String(), cfg.Seal.DeviceID)
	readmePath := filepath.Join(sealDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0644); err != nil {
		return fmt.Errorf("writing README: %w", err)
	}
	fmt.Println("Wrote README.md")

	committed, err := stageAndCommitReadme(cmd, sealDir)
	if err != nil {
		return err
	}
	if !committed {
		fmt.Println("README.md unchanged, nothing to commit.")
		return nil
	}
	fmt.Println("Committed README.md")

	return nil
}

func stageAndCommitReadme(cmd *cobra.Command, sealDir string) (bool, error) {
	gitAdd := exec.Command("git", "-C", sealDir, "add", "README.md")
	if err := gitAdd.Run(); err != nil {
		return false, fmt.Errorf("git add: %w", err)
	}

	// Skip commit if nothing changed.
	if err := exec.Command("git", "-C", sealDir, "diff", "--quiet", "--cached", "README.md").Run(); err == nil {
		return false, nil
	}

	gitCommit := exec.Command("git", "-C", sealDir, "commit", "--only", "-m", "seal: regenerate README.md", "README.md")
	gitCommit.Stdout = cmd.OutOrStdout()
	gitCommit.Stderr = cmd.ErrOrStderr()
	if err := gitCommit.Run(); err != nil {
		return false, fmt.Errorf("git commit: %w", err)
	}

	return true, nil
}
