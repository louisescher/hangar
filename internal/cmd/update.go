package cmd

import (
	"fmt"

	"github.com/louisescher/hangar/internal/engine"
	"github.com/spf13/cobra"
)

func newUpdateCmd() *cobra.Command {
	var (
		agentNames []string
		global     bool
		sec        securityFlags
	)

	cmd := &cobra.Command{
		Use:   "update [skill]",
		Short: "Update installed skills to their latest version",
		Long: "Re-resolve installed skills: auto entries move to the latest tag, pinned\n" +
			"entries are re-fetched at their exact ref. Rewritten tags are flagged.\n" +
			"With a skill name, only that skill is updated.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			}

			eng := engine.New()
			rep, err := eng.Update(c.Context(), name, engine.InstallOptions{
				Agents:   agentNames,
				Global:   global,
				Security: sec.sanitizeOpts(),
			})
			if err != nil {
				return err
			}

			w := c.OutOrStdout()
			if len(rep.Skills) == 0 {
				fmt.Fprintln(w, "everything up to date")
			} else {
				for _, sr := range rep.Skills {
					fmt.Fprintf(w, "updated %s\n", sr.Name)
				}
			}
			if rep.Audit != nil {
				for _, f := range rep.Audit.Findings {
					fmt.Fprintf(w, "  ! %s: %s tag %q moved %s → %s\n", f.Severity, f.Kind, f.Ref, short(f.OldSHA), short(f.NewSHA))
				}
			}
			sec.emitAudit(c, rep.Audit)
			return nil
		},
	}

	cmd.Flags().StringArrayVarP(&agentNames, "agent", "a", nil, "target agent (repeatable); default: auto-detect")
	cmd.Flags().BoolVarP(&global, "global", "g", false, "update the global (~/) install")
	sec.register(cmd)
	return cmd
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
