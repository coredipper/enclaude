package cmd

import (
	"fmt"
	"os"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/gitops"
	"github.com/coredipper/enclaude/internal/ui"
	"github.com/spf13/cobra"
)

var Version = "0.2.0"

var (
	flagVerbose  bool
	flagDryRun   bool
	flagClaudeDir string
	flagSealDir  string
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

// setupSyncCmd encapsulates common initialization for pull, push, and sync commands.
func setupSyncCmd(args []string) (*config.Config, *gitops.Git, string, error) {
	sealDir := getSealDir()
	remote := "origin"
	if len(args) > 0 {
		remote = args[0]
	}

	cfg, err := config.Load(sealDir)
	if err != nil {
		return nil, nil, "", err
	}
	if flagClaudeDir != "" {
		cfg.Seal.ClaudeDir = flagClaudeDir
	}

	git := gitops.New(sealDir)

	if !git.HasRemote(remote) {
		return nil, nil, "", fmt.Errorf("remote '%s' not configured. Run: enclaude remote add %s <url>", remote, remote)
	}

	return cfg, git, remote, nil
}
