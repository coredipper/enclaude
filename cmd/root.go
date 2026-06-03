package cmd

import (
	"fmt"
	"os"

	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/ui"
	"github.com/spf13/cobra"
)

var Version = "0.2.0"

var (
	flagVerbose   bool
	flagDryRun    bool
	flagClaudeDir string
	flagSealDir   string
)

var rootCmd = &cobra.Command{
	Use:   "enclaude",
	Short: "Encrypted git-like sync for ~/.claude/",
	Long: `enclaude provides age-encrypted, git-backed, JSONL-aware sync
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
	crypto.DefaultPassphraseFunc = ui.ReadPassphrase

	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVar(&flagDryRun, "dry-run", false, "show what would happen without doing it")
	rootCmd.PersistentFlags().StringVar(&flagClaudeDir, "claude-dir", "", "override ~/.claude/ location")
	rootCmd.PersistentFlags().StringVar(&flagSealDir, "seal-dir", "", "override ~/.enclaude/ location")
}

// getClaudeDir resolves the Claude directory for this machine. Commands call
// it instead of trusting the claude_dir in seal.toml: the store is synced
// across devices, so that value belongs to whichever machine ran `init` and is
// wrong everywhere else. --claude-dir still wins when set.
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

func getSealDir() string {
	if flagSealDir != "" {
		return flagSealDir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: cannot determine home directory:", err)
		os.Exit(1)
	}
	return home + "/.enclaude"
}
