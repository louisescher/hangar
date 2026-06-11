package cmd

import (
	"fmt"

	"github.com/louisescher/hangar/internal/engine"
	"github.com/louisescher/hangar/internal/spec"
	"github.com/spf13/cobra"
)

func newInfoCmd() *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:   "info <skill|source>",
		Short: "Inspect a skill: metadata + rendered SKILL.md",
		Long: "Browse a skill's metadata and rendered SKILL.md. The argument is first\n" +
			"matched against an installed skill by name; otherwise it is treated as a\n" +
			"source (owner/repo, npm:pkg, or a local path). A source with several skills\n" +
			"opens a selectable list. On a terminal this is an interactive viewer; piped\n" +
			"or with --no-tty it prints instead.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			eng := engine.New()

			d, err := eng.InstalledInfo(args[0], global)
			if err != nil {
				return err
			}
			if d == nil {
				s, err := spec.Parse(args[0])
				if err != nil {
					return err
				}
				if d, err = eng.Discover(c.Context(), s); err != nil {
					return err
				}
			}
			if len(d.Skills) == 0 {
				src := d.Source
				_ = d.Close()
				return fmt.Errorf("no skills found in %s", src)
			}

			if interactive(false) {
				return runInfoTUI(c, d, global) // closes d on exit
			}
			defer d.Close()
			return printInfoHeadless(c, eng, d)
		},
	}
	cmd.Flags().BoolVarP(&global, "global", "g", false, "look in the global (~/) install")
	return cmd
}

// printInfoHeadless prints a skill's metadata and SKILL.md (single skill), or
// the list of skills a source contains (many).
func printInfoHeadless(c *cobra.Command, eng *engine.Engine, d *engine.Discovered) error {
	w := c.OutOrStdout()
	if len(d.Skills) == 1 {
		sk := d.Skills[0]
		fmt.Fprintln(w, sk.Name)
		src := d.Source
		if d.Ref != "" {
			src += "@" + d.Ref
		}
		fmt.Fprintln(w, src)
		if sk.RelPath != "" {
			fmt.Fprintln(w, sk.RelPath)
		}
		if sk.Description != "" {
			fmt.Fprintf(w, "\n%s\n", sk.Description)
		}
		if doc, err := eng.ReadSkillDoc(sk); err == nil {
			fmt.Fprintf(w, "\n%s\n", doc.Body)
		}
		return nil
	}

	fmt.Fprintf(w, "%s — %d skills:\n", d.Source, len(d.Skills))
	for _, sk := range d.Skills {
		fmt.Fprintf(w, "  %-26s %s\n", sk.Name, sk.Description)
	}
	fmt.Fprintln(w, "\npick one with owner/repo#<name> (or pass a single skill)")
	return nil
}
