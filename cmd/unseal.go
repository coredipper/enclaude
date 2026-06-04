package cmd

import (
	"fmt"
	"os"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/crypto"
	"github.com/coredipper/enclaude/internal/store"
	"github.com/coredipper/enclaude/internal/ui"
	"github.com/spf13/cobra"
)

var flagRemap string

var unsealCmd = &cobra.Command{
	Use:   "unseal",
	Short: "Decrypt seal contents to ~/.claude/",
	Long:  "Decrypt all sealed objects and restore them to the Claude directory.",
	RunE:  runUnseal,
}

func init() {
	unsealCmd.Flags().StringVar(&flagRemap, "remap", "interactive",
		"handle project dirs sealed on another machine: auto|interactive|off")
	rootCmd.AddCommand(unsealCmd)
}

func runUnseal(cmd *cobra.Command, args []string) error {
	sealDir := getSealDir()

	cfg, err := config.Load(sealDir)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	cfg.Seal.ClaudeDir = getClaudeDir()

	if flagDryRun {
		fmt.Println("(dry run — showing what would change)")
		diff, err := store.UnsealStatus(cfg)
		if err != nil {
			return fmt.Errorf("unseal status: %w", err)
		}
		if len(diff.Added) == 0 && len(diff.Modified) == 0 && len(diff.Deleted) == 0 {
			fmt.Println("  No changes needed.")
		} else {
			for _, p := range diff.Added {
				fmt.Printf("  [restore] %s\n", p)
			}
			for _, p := range diff.Modified {
				fmt.Printf("  [update] %s\n", p)
			}
			for _, p := range diff.Deleted {
				fmt.Printf("  [delete] %s\n", p)
			}
		}
		return nil
	}

	mode, err := resolveRemapMode()
	if err != nil {
		return err
	}

	identity, source, err := crypto.LoadKey()
	if err != nil {
		return fmt.Errorf("loading key: %w", err)
	}
	if flagVerbose {
		fmt.Printf("Using key from %s\n", source)
	}

	fmt.Println("Unsealing...")
	stats, err := store.Unseal(cfg, identity, flagVerbose, nil, store.WithRemap(mode))
	if err != nil {
		return fmt.Errorf("unseal: %w", err)
	}
	fmt.Println(stats.Multiline("  "))

	if stats.Errors > 0 {
		fmt.Printf("  Warning: %d errors encountered.\n", stats.Errors)
	}

	return nil
}

// resolveRemapMode maps the --remap flag to a store.RemapMode. "interactive"
// downgrades to auto when stdin isn't a terminal or --yes is set, so hooks and
// CI never block on a prompt.
func resolveRemapMode() (store.RemapMode, error) {
	switch flagRemap {
	case "off":
		return store.RemapOff, nil
	case "auto":
		return store.RemapAuto, nil
	case "interactive":
		if flagYes || !ui.IsInteractive() {
			return store.RemapAuto, nil
		}
		return store.RemapInteractive, nil
	default:
		return store.RemapOff, fmt.Errorf("invalid --remap %q (want auto, interactive, or off)", flagRemap)
	}
}

// remapResolver is wired into store.DefaultRemapResolver (see root.go). For each
// project dir sealed on another machine it shows the proposed local key and lets
// the user accept, edit the target, or skip. Only the directory key is remapped;
// absolute paths embedded inside transcripts are left as a historical record.
func remapResolver(plans []store.RemapPlan) ([]store.RemapPlan, error) {
	fmt.Fprintf(os.Stderr, "\n%d project director(ies) were sealed on another machine.\n", len(plans))
	fmt.Fprintln(os.Stderr, "Remapping places them where this machine's Claude Code looks for them.")
	out := make([]store.RemapPlan, 0, len(plans))
	for _, p := range plans {
		fmt.Fprintf(os.Stderr, "\n  from: projects/%s\n  to:   projects/%s\n", p.SrcKey, p.DstKey)
		switch ui.Choose("  Action:", []string{"Accept", "Edit target", "Skip (keep original key)"}) {
		case 1:
			p.DstKey = ui.EditString("  Target key", p.DstKey)
			p.Accepted = true
		case 2:
			p.Accepted = false
		default:
			p.Accepted = true
		}
		out = append(out, p)
	}
	return out, nil
}
