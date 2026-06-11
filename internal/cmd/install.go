package cmd

import (
	"fmt"
	"sort"

	"github.com/louisescher/hangar/internal/engine"
	"github.com/louisescher/hangar/internal/install"
	"github.com/louisescher/hangar/internal/spec"
	"github.com/spf13/cobra"
)

func newInstallCmd() *cobra.Command {
	var (
		agentNames []string
		global     bool
		all        bool
		yes        bool
		sec        securityFlags
	)

	cmd := &cobra.Command{
		Use:   "install [source]",
		Short: "Install skills from a source",
		Long: "Install Agent Skills from a GitHub repo (owner/repo[/subpath][@ref][#skill])\n" +
			"or a local path. With multiple skills, pass --all to take them all (the\n" +
			"interactive picker is coming soon).",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return fmt.Errorf("reinstalling from the lockfile is not yet implemented; pass a source")
			}
			s, err := spec.Parse(args[0])
			if err != nil {
				return err
			}

			eng := engine.New()
			rep, err := eng.Install(c.Context(), s, engine.InstallOptions{
				Agents:   agentNames,
				Global:   global,
				All:      all,
				Yes:      yes,
				Security: sec.sanitizeOpts(),
			})
			if err != nil {
				return err
			}
			printReport(c, rep)
			sec.emitAudit(c, rep.Audit)
			return nil
		},
	}

	cmd.Flags().StringArrayVarP(&agentNames, "agent", "a", nil, "target agent (repeatable); default: auto-detect")
	cmd.Flags().BoolVarP(&global, "global", "g", false, "install into ~/ (global) instead of the current project")
	cmd.Flags().BoolVarP(&all, "all", "A", false, "install every skill the source contains")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip confirmation prompts")
	sec.register(cmd)
	return cmd
}

func printReport(c *cobra.Command, rep install.Report) {
	w := c.OutOrStdout()
	for _, sr := range rep.Skills {
		if sr.Kind == "ref" {
			fmt.Fprintf(w, "added reference %s\n", sr.Name)
			continue
		}
		fmt.Fprintf(w, "installed %s\n", sr.Name)
		for agent, reason := range sr.FailedAgents {
			fmt.Fprintf(w, "  ! %s: %s\n", agent, reason)
		}
	}
	agentsList := append([]string(nil), rep.InstalledAgents...)
	sort.Strings(agentsList)
	if len(agentsList) > 0 {
		fmt.Fprintf(w, "\nagents: %v\n", agentsList)
	} else {
		fmt.Fprintln(w, "\nno agents detected — skills are in .agents/skills/")
	}
	if rep.InstalledInstruction != "" {
		fmt.Fprintf(w, "updated %s\n", rep.InstalledInstruction)
	}
}
