package cmd

import (
	"fmt"

	"github.com/louisescher/hangar/internal/engine"
	"github.com/louisescher/hangar/internal/present"
	"github.com/louisescher/hangar/internal/spec"
	"github.com/spf13/cobra"
)

func newListCmd() *cobra.Command {
	var (
		asJSON bool
		global bool
	)
	cmd := &cobra.Command{
		Use:   "list [source]",
		Short: "List installed skills, or the skills available in a source",
		Long: "With no argument, manage the skills installed here: on a terminal this\n" +
			"opens an interactive list (update or remove with a keypress); otherwise it\n" +
			"prints them. With a source (owner/repo, npm:pkg, a subpath, or a local\n" +
			"path), list the skills that source contains.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			w := c.OutOrStdout()
			if len(args) == 0 {
				return listInstalled(c, global, asJSON)
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

			// On a terminal, browse the source's skills interactively (same
			// viewer as `hangar info`); piped/JSON prints instead.
			if interactive(false) && !asJSON {
				return runInfoTUI(c, d, global) // closes d on exit
			}
			defer d.Close()

			if asJSON {
				return present.WriteJSON(w, present.List(d))
			}

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
			for _, r := range d.References {
				fmt.Fprintf(w, "  %-28s %s\n", "[ref]", r.RelPath)
			}
			fmt.Fprintf(w, "\n%d skill(s)", len(d.Skills))
			if len(d.References) > 0 {
				fmt.Fprintf(w, ", %d reference(s)", len(d.References))
			}
			fmt.Fprintln(w)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	cmd.Flags().BoolVarP(&global, "global", "g", false, "list the global (~/) install")
	return cmd
}

// listInstalled handles `hangar list` with no source: the interactive manage
// screen on a terminal, or a printed/JSON listing when headless.
func listInstalled(c *cobra.Command, global, asJSON bool) error {
	if interactive(false) && !asJSON {
		return runManageTUI(c, global)
	}

	eng := engine.New()
	statuses, err := eng.CheckUpdates(c.Context(), global)
	if err != nil {
		return err
	}
	w := c.OutOrStdout()
	if asJSON {
		return present.WriteJSON(w, present.Installed(statuses))
	}

	if len(statuses) == 0 {
		fmt.Fprintln(w, "nothing installed — run `hangar install <source>`")
		return nil
	}

	const format = "%-24s  %-5s  %-16s  %s\n"
	fmt.Fprintf(w, format, "NAME", "KIND", "STATUS", "SOURCE")
	fmt.Fprintf(w, format, "----", "----", "------", "------")
	for _, st := range statuses {
		kind := "skill"
		if st.Entry.Kind == "ref" {
			kind = "ref"
		}
		status := "up to date"
		switch {
		case st.Gone:
			status = "deleted upstream"
		case st.Entry.Pinned:
			status = "pinned"
		case st.Err != "":
			status = "?"
		case st.Outdated:
			status = "⬆ " + st.Latest
		}
		src := st.Entry.Source
		if v := st.Latest; v != "" && st.Entry.Pinned {
			src += "@" + v
		}
		fmt.Fprintf(w, format, truncate24(st.Entry.Name), kind, status, src)
	}
	fmt.Fprintf(w, "\n%d installed\n", len(statuses))
	return nil
}

func truncate24(s string) string {
	if len(s) <= 24 {
		return s
	}
	return s[:23] + "…"
}
