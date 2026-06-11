package cmd

import (
	"fmt"

	"github.com/louisescher/hangar/internal/engine"
	"github.com/spf13/cobra"
)

func newRemoveCmd() *cobra.Command {
	var (
		agentNames []string
		global     bool
	)

	cmd := &cobra.Command{
		Use:     "remove <skill>",
		Aliases: []string{"rm", "uninstall"},
		Short:   "Remove an installed skill",
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			eng := engine.New()
			if err := eng.Remove(args[0], engine.InstallOptions{Agents: agentNames, Global: global}); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "removed %s\n", args[0])
			return nil
		},
	}

	cmd.Flags().StringArrayVarP(&agentNames, "agent", "a", nil, "target agent (repeatable); default: auto-detect")
	cmd.Flags().BoolVarP(&global, "global", "g", false, "remove from the global (~/) install")
	return cmd
}
