package cmd

import (
	"fmt"
	"os"

	"github.com/coredipper/enclaude/internal/config"
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
	flagSource    string
	flagSourceDir string
	flagSealDir   string
	flagYes       bool
)

var rootCmd = &cobra.Command{
	Use:   "enclaude",
	Short: "Encrypted git-like sync for AI coding agent data",
	Long: `enclaude provides age-encrypted, git-backed, JSONL-aware sync
for your Claude Code and pi.dev data. It stores encrypted copies of your
conversation history, settings, and memory in a git-backed seal store and syncs
them across devices with version history.`,
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
	rootCmd.PersistentFlags().StringVar(&flagSource, "source", "", "source profile: claude (default) or pi (env: ENCLAUDE_SOURCE)")
	rootCmd.PersistentFlags().StringVar(&flagSourceDir, "source-dir", "", "override the source directory (e.g. ~/.claude or ~/.pi/agent)")
	rootCmd.PersistentFlags().StringVar(&flagSourceDir, "claude-dir", "", "alias for --source-dir")
	rootCmd.PersistentFlags().StringVar(&flagSealDir, "seal-dir", "", "override ~/.enclaude/ location")
	rootCmd.PersistentFlags().BoolVarP(&flagYes, "yes", "y", false, "accept prompts non-interactively (e.g. auto-accept project remaps)")
}

// getSource resolves the active source profile from flags, the ENCLAUDE_SOURCE
// environment variable, or the Claude default.
func getSource() string {
	if flagSource != "" {
		return config.NormalizeSource(flagSource)
	}
	if env := os.Getenv("ENCLAUDE_SOURCE"); env != "" {
		return config.NormalizeSource(env)
	}
	return config.SourceClaude
}

// defaultSourceDir returns the conventional directory for a source profile.
// For pi, PI_CODING_AGENT_DIR is honoured, matching pi's own resolution.
func defaultSourceDir(source string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: cannot determine home directory:", err)
		os.Exit(1)
	}
	if source == config.SourcePi {
		if dir := os.Getenv("PI_CODING_AGENT_DIR"); dir != "" {
			return dir
		}
		return home + "/.pi/agent"
	}
	return home + "/.claude"
}

// defaultSealDir returns the conventional seal store for a source profile. Pi
// uses a distinct default so both agents can be sealed side by side.
func defaultSealDir(source string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: cannot determine home directory:", err)
		os.Exit(1)
	}
	if source == config.SourcePi {
		return home + "/.enclaude-pi"
	}
	return home + "/.enclaude"
}

// getSourceDir resolves the source directory for this machine. Commands call it
// instead of trusting the source_dir in seal.toml: the store is synced across
// devices, so that value belongs to whichever machine ran `init` and is wrong
// everywhere else. --source-dir/--claude-dir still wins when set.
func getSourceDir() string {
	if flagSourceDir != "" {
		return flagSourceDir
	}
	return defaultSourceDir(getSource())
}

func getSealDir() string {
	if flagSealDir != "" {
		return flagSealDir
	}
	return defaultSealDir(getSource())
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
