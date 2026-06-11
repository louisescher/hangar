package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the Hangar version",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			_, err := fmt.Fprintf(c.OutOrStdout(), "hangar %s (commit %s, built %s)\n", version, commit, date)
			return err
		},
	}
}
