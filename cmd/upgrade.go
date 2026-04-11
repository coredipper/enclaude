package cmd

import (
	"fmt"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/store"
	"github.com/coredipper/enclaude/internal/ui"
	"github.com/spf13/cobra"
)

var upgradeCmd = &cobra.Command{
	Use:   "upgrade",
	Short: "Upgrade seal.toml to the latest config version",
	Long:  "Applies one-time migrations to seal.toml and bumps the config version.",
	RunE:  runUpgrade,
}

func init() {
	rootCmd.AddCommand(upgradeCmd)
}

func runUpgrade(cmd *cobra.Command, args []string) error {
	sealDir := getSealDir()

	cfg, err := config.Load(sealDir)
	if err != nil {
		return err
	}

	changed := false

	// v0/v1 -> v2: fix sessions-index.json merge strategy
	if cfg.Version < 2 {
		if cfg.Merge["projects/*/sessions-index.json"] == "jsonl_dedup" {
			cfg.Merge["projects/*/sessions-index.json"] = "sessions_index"
			fmt.Println("  Fixed: projects/*/sessions-index.json strategy: jsonl_dedup -> sessions_index")
			changed = true
		}
	}

	// Validate: check if sessions-index.json still effectively resolves to
	// jsonl_dedup after migration. Uses the same precedence logic as the
	// merge driver. Best-effort probe set — the merge driver catches any
	// edge cases that slip through at runtime.
	probes := []string{
		"projects/a/sessions-index.json",
		"projects/A/sessions-index.json",
		"projects/ABC/sessions-index.json",
		"projects/abc/sessions-index.json",
		"projects/0/sessions-index.json",
		"projects/00/sessions-index.json",
		"projects/123/sessions-index.json",
		"projects/my-project/sessions-index.json",
		"projects/proj-abc123/sessions-index.json",
		"projects/customer-42/sessions-index.json",
		"projects/ABCD/sessions-index.json",
		"projects/test/sessions-index.json",
	}
	for _, testPath := range probes {
		resolvedStrategy, winningPattern := store.ResolveMergeStrategyWithPattern(testPath, cfg.Merge)
		if resolvedStrategy == "jsonl_dedup" {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"\n%s %s resolves to jsonl_dedup (via rule %q).\n"+
					"  Update it to 'sessions_index' in seal.toml.\n", ui.Red("Warning:"), testPath, winningPattern)
			return fmt.Errorf("manual fix required: update rule %q to 'sessions_index' in seal.toml", winningPattern)
		}
	}

	if cfg.Version >= config.ConfigVersion && !changed {
		fmt.Printf("Config is already at version %d. No issues found.\n", cfg.Version)
		return nil
	}

	cfg.Version = config.ConfigVersion
	if err := cfg.Save(sealDir); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	if changed {
		fmt.Printf("\n%s Config upgraded to version %d.\n", ui.Green("Done."), config.ConfigVersion)
	} else {
		fmt.Printf("\n%s Config version bumped to %d (no strategy changes needed).\n", ui.Green("Done."), config.ConfigVersion)
	}
	return nil
}


