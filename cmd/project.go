package cmd

import (
	"fmt"
	"sort"

	"github.com/coredipper/enclaude/internal/config"
	"github.com/coredipper/enclaude/internal/store"
	"github.com/spf13/cobra"
)

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Inspect and remap synced Claude Code project directories",
	Long: `Claude Code keys per-project state by the project's absolute path, so a
store synced to a machine with a different home restores those dirs under keys
the local Claude Code never looks at. These subcommands show that mapping and let
you pin device-local overrides that unseal will apply.`,
}

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List synced project directories and whether they are local or foreign",
	Args:  cobra.NoArgs,
	RunE:  runProjectList,
}

var projectMapCmd = &cobra.Command{
	Use:   "map <source> <target>",
	Short: "Pin a device-local mapping from a synced project key to a local one",
	Long:  "Source and target may be encoded keys or absolute project paths (paths are encoded for you).",
	Args:  cobra.ExactArgs(2),
	RunE:  runProjectMap,
}

var projectUnmapCmd = &cobra.Command{
	Use:   "unmap <source>",
	Short: "Remove a device-local project mapping",
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectUnmap,
}

func init() {
	projectCmd.AddCommand(projectListCmd, projectMapCmd, projectUnmapCmd)
	rootCmd.AddCommand(projectCmd)
}

func runProjectList(cmd *cobra.Command, args []string) error {
	sealDir := getSealDir()
	cfg, err := config.Load(sealDir)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	cfg.Seal.ClaudeDir = getClaudeDir()

	m, err := store.LoadManifest(sealDir)
	if err != nil {
		return fmt.Errorf("loading manifest: %w", err)
	}
	if m == nil {
		fmt.Println("No manifest — nothing has been sealed yet.")
		return nil
	}

	infos := store.ProjectDirs(m, cfg)
	if len(infos) == 0 {
		fmt.Println("No project directories in the seal store.")
		return nil
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Key < infos[j].Key })

	for _, p := range infos {
		status := "local"
		if p.Foreign {
			status = "foreign"
		}
		line := fmt.Sprintf("  %-7s  %s  (%d files)", status, p.Key, p.Files)
		if p.Override != "" {
			line += "  → " + p.Override
		}
		fmt.Println(line)
	}
	return nil
}

func runProjectMap(cmd *cobra.Command, args []string) error {
	sealDir := getSealDir()
	src := store.NormalizeProjectKey(args[0])
	dst := store.NormalizeProjectKey(args[1])

	ov := store.LoadOverrides(sealDir)
	ov[src] = dst
	if err := store.SaveOverrides(sealDir, ov); err != nil {
		return fmt.Errorf("saving project map: %w", err)
	}
	fmt.Printf("Mapped projects/%s → projects/%s\n", src, dst)
	return nil
}

func runProjectUnmap(cmd *cobra.Command, args []string) error {
	sealDir := getSealDir()
	src := store.NormalizeProjectKey(args[0])

	ov := store.LoadOverrides(sealDir)
	if _, ok := ov[src]; !ok {
		fmt.Printf("No mapping for projects/%s\n", src)
		return nil
	}
	delete(ov, src)
	if err := store.SaveOverrides(sealDir, ov); err != nil {
		return fmt.Errorf("saving project map: %w", err)
	}
	fmt.Printf("Removed mapping for projects/%s\n", src)
	return nil
}
