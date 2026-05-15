package cmd

import (
	"fmt"
	"os"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/ui"
	"github.com/coredipper/enclaude/internal/store"
	"github.com/spf13/cobra"
)

var (
	repairCheck        bool
	repairDeleteOrphans bool
)

var repairCmd = &cobra.Command{
	Use:   "repair",
	Short: "Verify seal store integrity and fix issues",
	Long: `Checks seal store integrity: missing objects, corrupt objects, and orphan files.
Use --check for verify-only mode (exit code 1 if issues found).
Use --delete-orphans to remove unreferenced object files.`,
	RunE: runRepair,
}

func init() {
	repairCmd.Flags().BoolVar(&repairCheck, "check", false, "verify only, don't fix (exit code 1 if issues)")
	repairCmd.Flags().BoolVar(&repairDeleteOrphans, "delete-orphans", false, "delete unreferenced object files")
	rootCmd.AddCommand(repairCmd)
}

func runRepair(cmd *cobra.Command, args []string) error {
	sealDir := getSealDir()

	cfg, err := config.Load(sealDir)
	if err != nil {
		return err
	}
	if flagClaudeDir != "" {
		cfg.Seal.ClaudeDir = flagClaudeDir
	}

	identity, _, err := crypto.LoadKey()
	if err != nil {
		return err
	}

	if repairCheck {
		fmt.Println("Verifying seal store integrity...")
		result, err := store.Verify(cfg, identity, flagVerbose)
		if err != nil {
			return err
		}
		printRepairResult(result)
		if len(result.MissingObjects) > 0 || len(result.CorruptObjects) > 0 {
			os.Exit(1)
		}
		return nil
	}

	fmt.Println("Repairing seal store...")
	result, err := store.Repair(cfg, identity, repairDeleteOrphans, flagVerbose)
	if err != nil {
		return err
	}
	printRepairResult(result)

	if result.Fixed > 0 {
		fmt.Printf("\n%s %d objects re-sealed from plaintext.\n", ui.Green("Fixed:"), result.Fixed)
	}

	return nil
}

func printRepairResult(r *store.RepairResult) {
	fmt.Printf("\nManifest entries: %d\n", r.TotalManifest)
	fmt.Printf("Objects on disk:  %d\n", r.TotalOnDisk)

	if len(r.MissingObjects) > 0 {
		fmt.Printf("\n%s Missing objects (%d):\n", ui.Red("!"), len(r.MissingObjects))
		for _, path := range r.MissingObjects {
			fmt.Printf("  %s %s\n", ui.Red("missing"), path)
		}
	}

	if len(r.CorruptObjects) > 0 {
		fmt.Printf("\n%s Corrupt objects (%d):\n", ui.Red("!"), len(r.CorruptObjects))
		for _, path := range r.CorruptObjects {
			fmt.Printf("  %s %s\n", ui.Red("corrupt"), path)
		}
	}

	if len(r.OrphanObjects) > 0 {
		fmt.Printf("\n%s Orphan objects (%d):\n", ui.Yellow("?"), len(r.OrphanObjects))
		for _, hash := range r.OrphanObjects {
			fmt.Printf("  %s %s\n", ui.Yellow("orphan"), hash[:16]+"...")
		}
	}

	if len(r.MissingObjects) == 0 && len(r.CorruptObjects) == 0 && len(r.OrphanObjects) == 0 {
		fmt.Printf("\n%s Seal integrity verified. No issues found.\n", ui.Green("OK"))
	}
}
