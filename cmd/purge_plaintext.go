package cmd

import (
	"fmt"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/session"
	"github.com/coredipper/enclaude/internal/store"
	"github.com/spf13/cobra"
)

var (
	purgeAllManaged bool
	purgeShred      bool
	purgeForce      bool
)

var purgePlaintextCmd = &cobra.Command{
	Use:   "purge-plaintext",
	Short: "Remove sealed plaintext files from ~/.claude/",
	Long: `Remove plaintext files from the Claude directory after they have been
sealed into the encrypted store. By default this removes only completed
top-level session JSONL files. Use --all-managed to remove every managed file
whose on-disk bytes still match a recoverable encrypted object.`,
	RunE: runPurgePlaintext,
}

func init() {
	purgePlaintextCmd.Flags().BoolVar(&purgeAllManaged, "all-managed", false, "purge every managed file, not just completed session JSONLs")
	purgePlaintextCmd.Flags().BoolVar(&purgeShred, "shred", false, "overwrite plaintext before removing it")
	purgePlaintextCmd.Flags().BoolVar(&purgeForce, "force", false, "purge even if an active Claude session is detected")
	rootCmd.AddCommand(purgePlaintextCmd)
}

func runPurgePlaintext(cmd *cobra.Command, args []string) error {
	sealDir := getSealDir()

	cfg, err := config.Load(sealDir)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	cfg.Seal.ClaudeDir = getClaudeDir()

	if purgeAllManaged && !purgeForce && !flagYes && !flagDryRun {
		return fmt.Errorf("refusing --all-managed without --yes or --force")
	}
	if session.HasActiveSessions(cfg.Seal.ClaudeDir) && !purgeForce && !flagDryRun {
		return fmt.Errorf("active Claude Code session detected; refusing to purge plaintext while Claude may be writing (use --force to override)")
	}

	scope := store.PurgeCompletedSessions
	if purgeAllManaged {
		scope = store.PurgeAllManaged
	}

	identity, source, err := crypto.LoadKey()
	if err != nil {
		return fmt.Errorf("loading key: %w", err)
	}
	if flagVerbose {
		fmt.Printf("Using key from %s\n", source)
	}

	if flagDryRun {
		fmt.Println("(dry run — showing plaintext files that would be purged)")
	}
	stats, err := store.PurgePlaintext(cfg, identity, scope, purgeShred, flagDryRun, flagVerbose)
	if err != nil {
		return fmt.Errorf("purging plaintext: %w", err)
	}
	fmt.Println(stats.Multiline("  ", flagDryRun))
	if stats.Errors > 0 {
		return fmt.Errorf("purge had %d errors", stats.Errors)
	}
	return nil
}
