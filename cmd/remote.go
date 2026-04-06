package cmd

import (
	"fmt"

	"github.com/coredipper/claude-vault/internal/gitops"
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
		vaultDir := getVaultDir()
		git := gitops.New(vaultDir)

		name, url := args[0], args[1]
		if err := git.RemoteAdd(name, url); err != nil {
			return fmt.Errorf("adding remote: %w", err)
		}

		// Register merge driver
		driverCmd := "claude-vault merge-driver manifest %O %A %B"
		if err := git.ConfigMergeDriver("claude-vault-manifest", driverCmd); err != nil {
			fmt.Printf("Warning: could not register merge driver: %v\n", err)
		}

		fmt.Printf("Remote '%s' added: %s\n", name, url)
		fmt.Println("Run 'claude-vault push' to sync.")
		return nil
	},
}

var remoteListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured remotes",
	RunE: func(cmd *cobra.Command, args []string) error {
		git := gitops.New(getVaultDir())
		out, err := git.RemoteList()
		if err != nil {
			return err
		}
		if out == "" {
			fmt.Println("No remotes configured.")
			fmt.Println("Run 'claude-vault remote add origin <url>' to set up sync.")
		} else {
			fmt.Println(out)
		}
		return nil
	},
}

func init() {
	remoteCmd.AddCommand(remoteAddCmd)
	remoteCmd.AddCommand(remoteListCmd)
	rootCmd.AddCommand(remoteCmd)
}
