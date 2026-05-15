package cmd

import (
	"fmt"

	"github.com/coredipper/enclaude/internal/gitops"
	"github.com/spf13/cobra"
)

var remoteCmd = &cobra.Command{
	Use:   "remote",
	Short: "Manage git remotes for sync",
}

var remoteAddCmd = &cobra.Command{
	Use:   "add <name> <url>",
	Short: "Add a git remote",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		sealDir := getSealDir()
		git := gitops.New(sealDir)

		name, url := args[0], args[1]
		if err := git.RemoteAdd(name, url); err != nil {
			return fmt.Errorf("adding remote: %w", err)
		}

		// Register merge driver
		driverArgs := []string{"merge-driver", "manifest", "%O", "%A", "%B"}
		if err := git.ConfigMergeDriver("enclaude-manifest", driverArgs); err != nil {
			fmt.Printf("Warning: could not register merge driver: %v\n", err)
		}

		fmt.Printf("Remote '%s' added: %s\n", name, url)
		fmt.Println("Run 'enclaude push' to sync.")
		return nil
	},
}

var remoteListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured remotes",
	RunE: func(cmd *cobra.Command, args []string) error {
		git := gitops.New(getSealDir())
		out, err := git.RemoteList()
		if err != nil {
			return err
		}
		if out == "" {
			fmt.Println("No remotes configured.")
			fmt.Println("Run 'enclaude remote add origin <url>' to set up sync.")
		} else {
			fmt.Println(out)
		}
		return nil
	},
}

var remoteRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a git remote",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		git := gitops.New(getSealDir())
		name := args[0]
		if err := git.RemoteRemove(name); err != nil {
			return fmt.Errorf("removing remote: %w", err)
		}
		fmt.Printf("Remote '%s' removed.\n", name)
		return nil
	},
}

var remoteEditCmd = &cobra.Command{
	Use:   "edit <name> <url>",
	Short: "Update the URL of an existing git remote",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		git := gitops.New(getSealDir())
		name, url := args[0], args[1]
		if err := git.RemoteSetURL(name, url); err != nil {
			return fmt.Errorf("editing remote: %w", err)
		}
		fmt.Printf("Remote '%s' updated: %s\n", name, url)
		return nil
	},
}

func init() {
	remoteCmd.AddCommand(remoteAddCmd)
	remoteCmd.AddCommand(remoteListCmd)
	remoteCmd.AddCommand(remoteRemoveCmd)
	remoteCmd.AddCommand(remoteEditCmd)
	rootCmd.AddCommand(remoteCmd)
}
