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

func handleSessionStart() error {
	sealDir := getSealDir()

	// Check seal store exists
	if _, err := os.Stat(sealDir + "/seal.toml"); os.IsNotExist(err) {
		return nil // seal store not initialized, skip silently
	}

	cfg, err := config.Load(sealDir)
	if err != nil {
		logHook("error loading config: %v", err)
		return nil // don't block Claude Code
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
			if out, err := git.Pull("origin", branch); err != nil {
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

	_, err = store.Unseal(cfg, identity, false, nil)
	if err != nil {
		logHook("unseal error: %v", err)
	}

	return nil
}

func handleSessionEnd() error {
	sealDir := getSealDir()

	if _, err := os.Stat(sealDir + "/seal.toml"); os.IsNotExist(err) {
		return nil
	}

	cfg, err := config.Load(sealDir)
	if err != nil {
		logHook("error loading config: %v", err)
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

	// Commit if changes
	if stats.HasChanges() {
		git := gitops.New(sealDir)
		if err := git.AddAll(); err != nil {
			logHook("git add warning: %v", err)
			return nil
		}
		msg := fmt.Sprintf("seal: auto-seal from %s (%s)",
			cfg.Seal.DeviceID, stats)
		if err := git.Commit(msg); err != nil {
			logHook("git commit warning: %v", err)
			return nil
		}

		// Push if auto-push enabled
		if cfg.Sync.AutoPush && git.HasRemote("origin") {
			branch, _ := git.CurrentBranch()
			if out, err := git.Push("origin", branch); err != nil {
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
