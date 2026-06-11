package cmd

import (
	"fmt"

	"github.com/louisescher/hangar/internal/engine"
	"github.com/louisescher/hangar/internal/spec"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list [source]",
		Short: "List skills available in a source",
		Long: "With a source (owner/repo, a subpath, or a local path), list the skills it\n" +
			"contains. With no argument, list installed skills (coming soon).",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			w := c.OutOrStdout()
			if len(args) == 0 {
				fmt.Fprintln(w, "listing installed skills is not yet implemented")
				return nil
			}

			s, err := spec.Parse(args[0])
			if err != nil {
				return err
			}

			eng := engine.New()
			d, err := eng.Discover(c.Context(), s)
			if err != nil {
				return err
			}
			defer d.Close()

			header := d.Source
			if d.Ref != "" {
				header += "@" + d.Ref
			}
			fmt.Fprintln(w, header)
			for _, sk := range d.Skills {
				if sk.Description != "" {
					fmt.Fprintf(w, "  %-28s %s\n", sk.Name, sk.Description)
				} else {
					fmt.Fprintf(w, "  %s\n", sk.Name)
				}
			}
			fmt.Fprintf(w, "\n%d skill(s)\n", len(d.Skills))
			return nil
		},
	}
}
