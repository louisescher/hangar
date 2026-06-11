// Package cmd wires up the Hangar command-line interface (cobra) and decides,
// for each invocation, whether to run interactively (TUI) or headless.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
)

// errSilent signals a non-zero exit with no extra output (the command already
// wrote everything it needed to, e.g. JSON). Execute suppresses its message.
var errSilent = errors.New("")

// Build-time metadata, overridable via -ldflags.
var (
	version = "0.1.0-dev"
	commit  = "none"
	date    = "unknown"
)

// newRootCmd constructs the top-level `hangar` command and attaches subcommands.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "hangar",
		Short: "A TUI package manager for AI agent skills",
		Long: "Hangar discovers Agent Skills (SKILL.md) in GitHub repositories and npm\n" +
			"packages and installs them into the AI coding agents on your machine.\n\n" +
			"Run `hangar` with no arguments to browse interactively.",
		SilenceUsage:  true,
		SilenceErrors: true, // Execute prints errors itself, skipping errSilent
		// With no subcommand, launch the browse TUI on a terminal; otherwise
		// show help (e.g. piped or in CI).
		RunE: func(c *cobra.Command, args []string) error {
			if interactive(false) {
				return runTUI(c, nil, nil, nil, false, nil)
			}
			return c.Help()
		},
	}

	var cwd string
	root.PersistentFlags().StringVar(&cwd, "cwd", "", "run as if hangar were started in this directory")
	root.PersistentPreRunE = func(c *cobra.Command, args []string) error {
		if cwd != "" {
			return os.Chdir(cwd)
		}
		return nil
	}

	root.AddCommand(newVersionCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newInstallCmd())
	root.AddCommand(newUpdateCmd())
	root.AddCommand(newRemoveCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newInfoCmd())
	root.AddCommand(newPinCmd())
	root.AddCommand(newUnpinCmd())
	root.AddCommand(newProfileCmd())
	root.AddCommand(newNukeCmd())

	return root
}

// Execute runs the root command with a context cancelled on SIGINT/SIGTERM, so
// in-flight network and install operations can stop cleanly. Returns a non-nil
// error on failure so main can set the process exit code.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := newRootCmd().ExecuteContext(ctx)
	if err != nil && !errors.Is(err, errSilent) {
		fmt.Fprintln(os.Stderr, "Error:", err)
	}
	return err
}
