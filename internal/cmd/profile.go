package cmd

import (
	"fmt"

	"github.com/louisescher/hangar/internal/config"
	"github.com/louisescher/hangar/internal/engine"
	"github.com/spf13/cobra"
)

func newProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Save and re-apply named sets of installed skills",
		Long: "Profiles capture the skills installed in a project so you can re-apply them\n" +
			"elsewhere. They are stored under your config dir (~/.config/hangar/profiles).",
	}
	cmd.AddCommand(newProfileSaveCmd(), newProfileApplyCmd(), newProfileListCmd(), newProfileRemoveCmd())
	return cmd
}

func newProfileSaveCmd() *cobra.Command {
	var global bool
	cmd := &cobra.Command{
		Use:   "save <name>",
		Short: "Save the current install as a named profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			eng := engine.New()
			entries, err := eng.Installed(global)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				return fmt.Errorf("nothing installed to save")
			}
			if err := config.SaveProfile(config.Profile{Name: args[0], Skills: entries}); err != nil {
				return err
			}
			fmt.Fprintf(c.OutOrStdout(), "saved profile %q (%d entries)\n", args[0], len(entries))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&global, "global", "g", false, "save from the global (~/) install")
	return cmd
}

func newProfileApplyCmd() *cobra.Command {
	var (
		agentNames []string
		global     bool
		asJSON     bool
		verbose    bool
		sec        securityFlags
	)
	cmd := &cobra.Command{
		Use:   "apply <name>",
		Short: "Install all skills from a saved profile",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			p, err := config.LoadProfile(args[0])
			if err != nil {
				return err
			}
			eng := engine.New()
			prog := newProgress(c.ErrOrStderr(), verbose)
			prog.start()
			rep, err := eng.ApplyProfile(c.Context(), p.Skills, engine.InstallOptions{
				Agents:     agentNames,
				Global:     global,
				Security:   sec.sanitizeOpts(),
				OnProgress: prog.event,
			})
			prog.finish()
			if err != nil {
				return err
			}
			return outputReport(c, rep, &sec, asJSON)
		},
	}
	cmd.Flags().StringArrayVarP(&agentNames, "agent", "a", nil, "target agent (repeatable); default: auto-detect")
	cmd.Flags().BoolVarP(&global, "global", "g", false, "install into the global (~/) location")
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "print progress")
	sec.register(cmd)
	return cmd
}

func newProfileListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "Browse saved profiles (interactive) or list them",
		Long: "On a terminal this opens an interactive browser: pick a profile, inspect\n" +
			"its skills, and remove individual entries. Piped or with --no-tty it prints.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, args []string) error {
			if interactive(false) {
				return runProfilesTUI(c)
			}
			names, err := config.ListProfiles()
			if err != nil {
				return err
			}
			w := c.OutOrStdout()
			if len(names) == 0 {
				fmt.Fprintln(w, "no saved profiles")
				return nil
			}
			for _, n := range names {
				if p, err := config.LoadProfile(n); err == nil {
					fmt.Fprintf(w, "  %-24s %d entries\n", n, len(p.Skills))
				} else {
					fmt.Fprintf(w, "  %s\n", n)
				}
			}
			return nil
		},
	}
}

func newProfileRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm"},
		Short:   "Delete a saved profile",
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			removed, err := config.RemoveProfile(args[0])
			if err != nil {
				return err
			}
			if !removed {
				return fmt.Errorf("no profile named %q", args[0])
			}
			fmt.Fprintf(c.OutOrStdout(), "removed profile %q\n", args[0])
			return nil
		},
	}
	return cmd
}
