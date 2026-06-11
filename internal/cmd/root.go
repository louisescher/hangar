// Package cmd wires up the Hangar command-line interface (cobra) and decides,
// for each invocation, whether to run interactively (TUI) or headless.
package cmd

import (
	"context"
	"os"
	"os/signal"

	"github.com/spf13/cobra"
)

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
		SilenceErrors: false,
		// With no subcommand we'll eventually launch the browse TUI. For now,
		// show help so the skeleton is usable.
		RunE: func(c *cobra.Command, args []string) error {
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

	return root
}

// Execute runs the root command with a context cancelled on SIGINT/SIGTERM, so
// in-flight network and install operations can stop cleanly. Returns a non-nil
// error on failure so main can set the process exit code.
func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return newRootCmd().ExecuteContext(ctx)
}
