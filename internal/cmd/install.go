package cmd

import (
	"fmt"
	"sort"

	"github.com/louisescher/hangar/internal/engine"
	"github.com/louisescher/hangar/internal/install"
	"github.com/louisescher/hangar/internal/present"
	"github.com/louisescher/hangar/internal/security/sanitize"
	"github.com/louisescher/hangar/internal/spec"
	"github.com/spf13/cobra"
)

func newInstallCmd() *cobra.Command {
	var (
		agentNames []string
		global     bool
		all        bool
		yes        bool
		noTTY      bool
		asJSON     bool
		verbose    bool
		sec        securityFlags
	)

	cmd := &cobra.Command{
		Use:   "install [source]",
		Short: "Install skills from a source (or reinstall from the lockfile)",
		Long: "Install Agent Skills from a GitHub repo (owner/repo[/subpath][@ref][#skill]),\n" +
			"an npm package (npm:pkg), or a local path. On a terminal the interactive\n" +
			"picker opens when a source has multiple skills; pass --all or -y to install\n" +
			"headless.\n\n" +
			"With no source, reinstall everything recorded in .agents/hangar.lock — handy\n" +
			"after cloning a repo that commits its lockfile.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			eng := engine.New()
			secOpt := sec.sanitizeOpts()

			// No source: reinstall everything recorded in the lockfile.
			if len(args) == 0 {
				p := newProgress(c.ErrOrStderr(), verbose)
				p.start()
				rep, err := eng.Sync(c.Context(), engine.InstallOptions{
					Agents:     agentNames,
					Global:     global,
					Security:   secOpt,
					OnProgress: p.event,
				})
				p.finish()
				if err != nil {
					return err
				}
				return outputReport(c, rep, &sec, asJSON)
			}

			s, err := spec.Parse(args[0])
			if err != nil {
				return err
			}

			// Interactive path: open the picker unless the user asked for a
			// headless install (--all/-y/--json) or there is no terminal.
			if interactive(noTTY) && !all && !yes && !asJSON {
				d, err := eng.Discover(c.Context(), s)
				if err != nil {
					return err
				}
				// A single (or zero) skill is unambiguous — install directly.
				if len(d.Skills) <= 1 {
					defer d.Close()
					rep, err := installSelectedHeadless(eng, d, s, agentNames, global, secOpt)
					if err != nil {
						return err
					}
					return outputReport(c, rep, &sec, false)
				}
				// Otherwise hand the pre-fetched source to the picker (it Closes d).
				return runTUI(c, &s, d, agentNames, global, &secOpt)
			}

			p := newProgress(c.ErrOrStderr(), verbose)
			p.start()
			rep, err := eng.Install(c.Context(), s, engine.InstallOptions{
				Agents:     agentNames,
				Global:     global,
				All:        all,
				Yes:        yes,
				Security:   secOpt,
				OnProgress: p.event,
			})
			p.finish()
			if err != nil {
				return err
			}
			return outputReport(c, rep, &sec, asJSON)
		},
	}

	cmd.Flags().StringArrayVarP(&agentNames, "agent", "a", nil, "target agent (repeatable); default: auto-detect")
	cmd.Flags().BoolVarP(&global, "global", "g", false, "install into ~/ (global) instead of the current project")
	cmd.Flags().BoolVarP(&all, "all", "A", false, "install every skill the source contains")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "accept without the interactive picker")
	cmd.Flags().BoolVar(&noTTY, "no-tty", false, "never open the interactive picker")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON (implies headless)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print per-item progress")
	sec.register(cmd)
	return cmd
}

// installSelectedHeadless installs an already-discovered, unambiguous source
// without a picker, resolving target agents (explicit -a or auto-detected).
func installSelectedHeadless(eng *engine.Engine, d *engine.Discovered, s spec.SourceSpec, agentNames []string, global bool, secOpt sanitize.Opts) (install.Report, error) {
	var agts []engine.Agent
	var err error
	if len(agentNames) > 0 {
		agts, err = eng.ResolveAgents(agentNames, global)
	} else {
		agts, err = eng.DetectAgents(global)
	}
	if err != nil {
		return install.Report{}, err
	}
	return eng.InstallSelected(d, s, d.Skills, d.References, agts, engine.InstallOptions{Global: global, Security: secOpt})
}

// outputReport renders an install/update report as JSON or plain text, emitting
// the audit envelope only in plain mode (JSON carries findings inline).
func outputReport(c *cobra.Command, rep install.Report, sec *securityFlags, asJSON bool) error {
	if asJSON {
		return present.WriteJSON(c.OutOrStdout(), present.InstallReport(rep))
	}
	printReport(c, rep)
	sec.emitAudit(c, rep.Audit)
	return nil
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
	for _, name := range rep.Gone {
		fmt.Fprintf(w, "! %s no longer exists upstream — kept; remove with `hangar remove %s`\n", name, name)
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
