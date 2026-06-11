package cmd

import (
	"fmt"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/gitops"
	"github.com/coredipper/enclaude/internal/store"
	"github.com/spf13/cobra"
)

var pushCmd = &cobra.Command{
	Use:   "push [remote]",
	Short: "Seal and push to remote",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runPush,
}

func init() {
	rootCmd.AddCommand(pushCmd)
}

func runPush(cmd *cobra.Command, args []string) error {
	sealDir := getSealDir()
	remote := "origin"
	if len(args) > 0 {
		remote = args[0]
	}

	cfg, err := config.Load(sealDir)
	if err != nil {
		return err
	}
	cfg.Seal.ClaudeDir = getClaudeDir()

	git := gitops.New(sealDir)

	// Check remote exists
	if !git.HasRemote(remote) {
		return fmt.Errorf("remote '%s' not configured. Run: enclaude remote add %s <url>", remote, remote)
	}

	// Seal first
	recipient, _, err := crypto.LoadPublicKey()
	if err != nil {
		return err
	}

	fmt.Println("Sealing...")
	stats, err := store.Seal(cfg, recipient, flagVerbose, nil)
	if err != nil {
		return fmt.Errorf("seal: %w", err)
	}
	if stats.Errors > 0 {
		return fmt.Errorf("seal had %d errors — resolve before pushing", stats.Errors)
	}
	fmt.Println(stats.Multiline("  "))

	msg := fmt.Sprintf("seal: seal from %s (%s)",
		cfg.Seal.DeviceID, stats)
	if _, err := commitSealStore(sealDir, msg); err != nil {
		return err
	}

	// Push
	branch, _ := git.CurrentBranch()
	fmt.Printf("Pushing to %s/%s...\n", remote, branch)

	var pushStats gitops.PushStats
	var pushOut string
	if git.HasUpstream() {
		pushStats, pushOut, err = git.Push(remote, branch)
	} else {
		pushStats, pushOut, err = git.PushWithUpstream(remote, branch)
	}
	if err != nil {
		return fmt.Errorf("push failed: %w\n%s", err, pushOut)
	}

	fmt.Println(formatPushLine("  ", pushStats))
	return nil
}
