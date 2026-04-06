package cmd

import (
	"fmt"

	"github.com/coredipper/claude-seal/internal/gitops"
	"github.com/spf13/cobra"
)

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Manage Claude Code hook integration",
}

var hooksInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install auto-sync hooks into Claude Code settings",
	Long:  "Adds SessionStart and SessionEnd hooks to ~/.claude/settings.json. Existing hooks are preserved.",
	RunE: func(cmd *cobra.Command, args []string) error {
		claudeDir := getClaudeDir()

		if gitops.HooksInstalled(claudeDir) {
			fmt.Println("Seal hooks are already installed.")
			return nil
		}

		fmt.Println("Installing seal hooks into settings.json...")
		if err := gitops.InstallHooks(claudeDir); err != nil {
			return fmt.Errorf("installing hooks: %w", err)
		}

		fmt.Println("  SessionStart hook: pull + unseal on session start")
		fmt.Println("  SessionEnd hook:   seal + push on session end (async)")
		fmt.Println("\nHooks installed. Existing hooks were preserved.")
		return nil
	},
}

var hooksRemoveCmd = &cobra.Command{
	Use:   "remove",
	Short: "Remove seal hooks from Claude Code settings",
	RunE: func(cmd *cobra.Command, args []string) error {
		claudeDir := getClaudeDir()

		if !gitops.HooksInstalled(claudeDir) {
			fmt.Println("No seal hooks found.")
			return nil
		}

		fmt.Println("Removing seal hooks...")
		if err := gitops.RemoveHooks(claudeDir); err != nil {
			return fmt.Errorf("removing hooks: %w", err)
		}

		fmt.Println("Seal hooks removed. Other hooks were preserved.")
		return nil
	},
}

var hooksStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check if seal hooks are installed",
	RunE: func(cmd *cobra.Command, args []string) error {
		claudeDir := getClaudeDir()

		if gitops.HooksInstalled(claudeDir) {
			fmt.Println("Seal hooks: installed")
		} else {
			fmt.Println("Seal hooks: not installed")
			fmt.Println("Run 'claude-seal hooks install' to enable auto-sync.")
		}
		return nil
	},
}

func init() {
	hooksCmd.AddCommand(hooksInstallCmd)
	hooksCmd.AddCommand(hooksRemoveCmd)
	hooksCmd.AddCommand(hooksStatusCmd)
	rootCmd.AddCommand(hooksCmd)
}
