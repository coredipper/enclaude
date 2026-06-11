package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/gitops"
	"github.com/coredipper/enclaude/internal/session"
	"github.com/coredipper/enclaude/internal/store"
	"github.com/spf13/cobra"
)

const lockTimeout = 5 * time.Second

var hookHandlerCmd = &cobra.Command{
	Use:    "hook-handler <event>",
	Short:  "Handle Claude Code lifecycle hooks (internal)",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE:   runHookHandler,
}

func init() {
	rootCmd.AddCommand(hookHandlerCmd)
}

func runHookHandler(cmd *cobra.Command, args []string) error {
	event := args[0]

	switch event {
	case "session-start":
		return handleSessionStart()
	case "session-end":
		return handleSessionEnd()
	default:
		return fmt.Errorf("unknown hook event: %s", event)
	}
}

// loadHookConfig loads the seal-store config for a hook invocation. ok=false
// means the hook should exit silently — the store isn't initialized or the
// config is unreadable, and hooks must never block Claude Code.
func loadHookConfig() (cfg *config.Config, sealDir string, ok bool) {
	sealDir = getSealDir()

	// Check seal store exists
	if _, err := os.Stat(sealDir + "/seal.toml"); os.IsNotExist(err) {
		return nil, sealDir, false // seal store not initialized, skip silently
	}

	cfg, err := config.Load(sealDir)
	if err != nil {
		logHook("error loading config: %v", err)
		return nil, sealDir, false // don't block Claude Code
	}

	// Like every other command, resolve the Claude dir for THIS machine rather
	// than trusting the synced seal.toml. Hooks are the one path where getting
	// this wrong is destructive: a session-end seal against another machine's
	// (nonexistent) claude_dir scans zero files and commits a manifest with
	// every entry deleted.
	cfg.Seal.ClaudeDir = getClaudeDir()

	return cfg, sealDir, true
}

func handleSessionStart() error {
	cfg, sealDir, ok := loadHookConfig()
	if !ok {
		return nil
	}

	if !cfg.Sync.AutoUnsealOnSessionStart {
		return nil
	}

	// Acquire lock with short timeout — don't block Claude startup
	lock := session.NewSealLock(sealDir)
	acquired, err := lock.Acquire(lockTimeout)
	if err != nil || !acquired {
		logHook("could not acquire lock, skipping session-start hook")
		return nil
	}
	defer lock.Release()

	// Pull if auto-pull enabled and remote configured
	if cfg.Sync.AutoPull {
		git := gitops.New(sealDir)
		if git.HasRemote("origin") {
			branch, _ := git.CurrentBranch()
			if _, out, err := git.Pull("origin", branch); err != nil {
				logHook("pull warning: %v (%s)", err, out)
				// Don't fail — proceed with local state
			}
		}
	}

	// Unseal
	identity, _, err := crypto.LoadKey()
	if err != nil {
		logHook("key error: %v", err)
		return nil
	}

	// Session start can't block on a prompt, so remap runs in auto mode:
	// deterministic home-prefix swaps are applied; anything ambiguous is left
	// under its original key for `enclaude unseal --remap=interactive` to resolve.
	_, err = store.Unseal(cfg, identity, false, nil, store.WithRemap(store.RemapAuto))
	if err != nil {
		logHook("unseal error: %v", err)
	}

	return nil
}

func handleSessionEnd() error {
	cfg, sealDir, ok := loadHookConfig()
	if !ok {
		return nil
	}

	if !cfg.Sync.AutoSealOnSessionEnd {
		return nil
	}

	// Acquire lock
	lock := session.NewSealLock(sealDir)
	acquired, err := lock.Acquire(lockTimeout)
	if err != nil || !acquired {
		logHook("could not acquire lock, skipping session-end hook")
		return nil
	}
	defer lock.Release()

	// Seal
	recipient, _, err := crypto.LoadPublicKey()
	if err != nil {
		logHook("key error: %v", err)
		return nil
	}

	stats, err := store.Seal(cfg, recipient, false, nil)
	if err != nil {
		logHook("seal error: %v", err)
		return nil
	}
	if stats.Errors > 0 {
		logHook("seal had %d errors, skipping commit", stats.Errors)
		return nil
	}

	msg := fmt.Sprintf("seal: auto-seal from %s (%s)",
		cfg.Seal.DeviceID, stats)
	committed, err := commitSealStore(sealDir, msg)
	if err != nil {
		logHook("git warning: %v", err)
		return nil
	}

	// Push if auto-push enabled
	if committed && cfg.Sync.AutoPush {
		git := gitops.New(sealDir)
		if git.HasRemote("origin") {
			branch, _ := git.CurrentBranch()
			if _, out, err := git.Push("origin", branch); err != nil {
				logHook("push warning: %v (%s)", err, out)
			}
		}
	}

	return nil
}

// logHook writes to stderr — Claude Code captures hook stderr for verbose mode.
func logHook(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "[enclaude] "+format+"\n", args...)
}
