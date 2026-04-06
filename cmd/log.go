package cmd

import (
	"fmt"
	"strings"

	"github.com/coredipper/claude-vault/internal/gitops"
	"github.com/coredipper/claude-vault/internal/ui"
	"github.com/spf13/cobra"
)

var logCount int

var logCmd = &cobra.Command{
	Use:   "log",
	Short: "Show vault history",
	RunE: func(cmd *cobra.Command, args []string) error {
		git := gitops.New(getVaultDir())
		out, err := git.LogFull(logCount)
		if err != nil {
			return fmt.Errorf("git log: %w", err)
		}
		// Color each line: hash in cyan, time in faint
		for _, line := range strings.Split(out, "\n") {
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, " ", 2)
			if len(parts) == 2 {
				fmt.Printf("%s %s\n", ui.Cyan(parts[0]), parts[1])
			} else {
				fmt.Println(line)
			}
		}
		return nil
	},
}

func init() {
	logCmd.Flags().IntVarP(&logCount, "count", "n", 20, "number of entries to show")
	rootCmd.AddCommand(logCmd)
}
