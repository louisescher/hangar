package cmd

import (
	"fmt"

	"github.com/louisescher/hangar/internal/engine"
	"github.com/spf13/cobra"
)

func newPinCmd() *cobra.Command {
	var (
		agentNames []string
		global     bool
	)
	cmd := &cobra.Command{
		Use:   "pin <skill> [ref]",
		Short: "Pin an installed skill (optionally reinstalling at a ref/version)",
		Long: "Mark an installed skill as pinned so `hangar update` leaves it alone. With\n" +
			"a ref/version, the skill is reinstalled at that exact ref and pinned.",
		Args: cobra.RangeArgs(1, 2),
		RunE: func(c *cobra.Command, args []string) error {
			ref := ""
			if len(args) == 2 {
				ref = args[1]
			}
			eng := engine.New()
			rep, err := eng.Pin(c.Context(), args[0], ref, engine.InstallOptions{Agents: agentNames, Global: global})
			if err != nil {
				return err
			}
			w := c.OutOrStdout()
			if ref == "" {
				fmt.Fprintf(w, "pinned %s\n", args[0])
			} else {
				fmt.Fprintf(w, "pinned %s @ %s\n", args[0], ref)
				_ = rep
			}
			return nil
		},
	}
	cmd.Flags().StringArrayVarP(&agentNames, "agent", "a", nil, "target agent (repeatable); default: auto-detect")
	cmd.Flags().BoolVarP(&global, "global", "g", false, "operate on the global (~/) install")
	return cmd
}

func newUnpinCmd() *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:   "unpin <skill>",
		Short: "Clear the pinned flag on an installed skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			eng := engine.New()
			if err := eng.Unpin(args[0], engine.InstallOptions{Global: global}); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "unpinned %s\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVarP(&global, "global", "g", false, "operate on the global (~/) install")
	return cmd
}
