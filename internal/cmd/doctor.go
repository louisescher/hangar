package cmd

import (
	"fmt"
	"os"

	"github.com/louisescher/hangar/internal/doctor"
	"github.com/louisescher/hangar/internal/engine"
	"github.com/louisescher/hangar/internal/present"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	var (
		global bool
		fix    bool
		asJSON bool
	)
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose (and optionally repair) the install state",
		Long: "Check for drift between the lockfile and disk: orphaned skill dirs,\n" +
			"broken or orphaned per-agent symlinks, and a references block out of\n" +
			"sync with the lockfile. With --fix, repairable problems are corrected.\n" +
			"Exits non-zero if problems remain.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			baseDir := "."
			if global {
				home, err := os.UserHomeDir()
				if err != nil {
					return err
				}
				baseDir = home
			}

			w := c.OutOrStdout()

			// With --fix, first restore any lockfile entries whose files went
			// missing: a reinstall re-fetches them and recreates their per-agent
			// links, so this must run before the symlink checks below.
			if fix {
				missing, err := doctor.MissingEntries(baseDir)
				if err != nil {
					return err
				}
				if len(missing) > 0 {
					p := newProgress(c.ErrOrStderr(), false)
					p.start()
					rep, err := engine.New().Reinstall(c.Context(), missing, engine.InstallOptions{Global: global, BaseDir: baseDir, OnProgress: p.event})
					p.finish()
					if err != nil {
						return fmt.Errorf("restoring missing skills: %w", err)
					}
					if !asJSON {
						if n := len(rep.Skills); n > 0 {
							fmt.Fprintf(w, "restored %d missing item(s)\n", n)
						}
						for _, name := range rep.Gone {
							fmt.Fprintf(w, "! %s no longer exists upstream — kept; remove with `hangar remove %s`\n", name, name)
						}
					}
				}
			}

			rep, err := doctor.Diagnose(baseDir, global)
			if err != nil {
				return err
			}
			if fix && rep.FixableCount() > 0 {
				fixed, errs := rep.Fix()
				if !asJSON {
					fmt.Fprintf(w, "fixed %d problem(s)\n", fixed)
					for _, e := range errs {
						fmt.Fprintf(w, "  ! %v\n", e)
					}
				}
			}

			if asJSON {
				if err := present.WriteJSON(w, present.Doctor(rep)); err != nil {
					return err
				}
				if rep.HasIssues() {
					return errSilent
				}
				return nil
			}

			fmt.Fprintf(w, "detected agents: %s\n", agentNamesLine(rep))
			printProblems(w, rep)

			if rep.HasIssues() {
				return fmt.Errorf("doctor found problems%s", fixHint(fix, rep))
			}
			fmt.Fprintln(w, "\neverything looks healthy ✓")
			return nil
		},
	}
	cmd.Flags().BoolVarP(&global, "global", "g", false, "check the global (~/) install instead of this project")
	cmd.Flags().BoolVar(&fix, "fix", false, "repair fixable problems")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}

func agentNamesLine(rep doctor.Report) string {
	if len(rep.Detected) == 0 {
		return "(none)"
	}
	out := ""
	for i, a := range rep.Detected {
		if i > 0 {
			out += ", "
		}
		out += a.Def.Name
	}
	return out
}

func printProblems(w interface{ Write([]byte) (int, error) }, rep doctor.Report) {
	if len(rep.Problems) == 0 {
		return
	}
	fmt.Fprintln(w)
	for _, p := range rep.Problems {
		mark := map[doctor.Severity]string{
			doctor.SevInfo:  "•",
			doctor.SevWarn:  "!",
			doctor.SevError: "✗",
		}[p.Severity]
		fmt.Fprintf(w, "  %s %s\n", mark, p.Message)
	}
}

func fixHint(fix bool, rep doctor.Report) string {
	if !fix && rep.FixableCount() > 0 {
		return fmt.Sprintf(" (%d fixable — rerun with --fix)", rep.FixableCount())
	}
	return ""
}
