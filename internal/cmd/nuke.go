package cmd

import (
	"fmt"

	"github.com/louisescher/hangar/internal/engine"
	"github.com/spf13/cobra"
)

func newNukeCmd() *cobra.Command {
	var (
		agentNames []string
		global     bool
		yes        bool
	)
	cmd := &cobra.Command{
		Use:   "nuke",
		Short: "Remove every installed skill and reference",
		Long: "Remove all skills and references recorded in the lockfile, unlinking them\n" +
			"from the target agents. Respects --global and --agent. Destructive, so it\n" +
			"requires -y to actually run; without it, the affected items are listed.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			eng := engine.New()
			entries, err := eng.Installed(global)
			if err != nil {
				return err
			}
			w := c.OutOrStdout()
			if len(entries) == 0 {
				fmt.Fprintln(w, "nothing installed")
				return nil
			}

			if !yes {
				fmt.Fprintf(w, "would remove %d item(s):\n", len(entries))
				for _, e := range entries {
					fmt.Fprintf(w, "  - %s\n", e.Name)
				}
				fmt.Fprintln(w, "\nre-run with -y to confirm")
				return nil
			}

			removed, err := eng.Nuke(engine.InstallOptions{Agents: agentNames, Global: global})
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "removed %d item(s)\n", len(removed))
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&agentNames, "agent", "a", nil, "target agent (repeatable); default: auto-detect")
	cmd.Flags().BoolVarP(&global, "global", "g", false, "nuke the global (~/) install")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "actually remove (required)")
	return cmd
}
