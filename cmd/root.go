package cmd

import (
	"fmt"
	"os"

	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/gitops"
	"github.com/coredipper/enclaude/internal/store"
	"github.com/coredipper/enclaude/internal/ui"
	"github.com/spf13/cobra"
)

var Version = "0.2.0"

var (
	flagVerbose   bool
	flagDryRun    bool
	flagClaudeDir string
	flagSealDir   string
	flagYes       bool
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
	store.DefaultRemapResolver = remapResolver

	rootCmd.PersistentFlags().BoolVarP(&flagVerbose, "verbose", "v", false, "verbose output")
	rootCmd.PersistentFlags().BoolVar(&flagDryRun, "dry-run", false, "show what would happen without doing it")
	rootCmd.PersistentFlags().StringVar(&flagClaudeDir, "claude-dir", "", "override ~/.claude/ location")
	rootCmd.PersistentFlags().StringVar(&flagSealDir, "seal-dir", "", "override ~/.enclaude/ location")
	rootCmd.PersistentFlags().BoolVarP(&flagYes, "yes", "y", false, "accept prompts non-interactively (e.g. auto-accept project remaps)")
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

// commitSealStore stages the seal dir and commits when anything is staged,
// returning whether a commit was made. The commit is gated on staged changes
// rather than the seal's own stats: AddAll force-stages store metadata that a
// gitignore may have kept out of history, and that rescue must not wait for
// the next content change.
func commitSealStore(sealDir, msg string) (bool, error) {
	git := gitops.New(sealDir)
	if err := git.AddAll(); err != nil {
		return false, fmt.Errorf("git add: %w", err)
	}
	if !git.HasCachedChanges() {
		return false, nil
	}
	if err := git.Commit(msg); err != nil {
		return false, fmt.Errorf("git commit: %w", err)
	}
	return true, nil
}
