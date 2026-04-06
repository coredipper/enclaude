package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const Version = "0.1.0"

var (
	flagVerbose  bool
	flagDryRun   bool
	flagClaudeDir string
	flagVaultDir  string
)

var rootCmd = &cobra.Command{
	Use:   "claude-vault",
	Short: "Encrypted git-like sync for ~/.claude/",
	Long: `claude-vault provides age-encrypted, git-backed, JSONL-aware sync
for your Claude Code session data. It encrypts your conversation history,
settings, and memory at rest and syncs them across devices with version history.`,
	Version: Version,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVar(&flagDryRun, "dry-run", false, "show what would happen without doing it")
	rootCmd.PersistentFlags().StringVar(&flagClaudeDir, "claude-dir", "", "override ~/.claude/ location")
	rootCmd.PersistentFlags().StringVar(&flagVaultDir, "vault-dir", "", "override ~/.claude-vault/ location")
}

func getClaudeDir() string {
	if flagClaudeDir != "" {
		return flagClaudeDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: cannot determine home directory:", err)
		os.Exit(1)
	}
	return home + "/.claude"
}

func getVaultDir() string {
	if flagVaultDir != "" {
		return flagVaultDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: cannot determine home directory:", err)
		os.Exit(1)
	}
	return home + "/.claude-vault"
}
