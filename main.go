// Command hangar is a TUI package manager for AI agent skills.
//
// It discovers SKILL.md files in GitHub repositories and npm packages, lets the
// user pick which to install via an interactive tree picker, and installs them
// into the directories of the AI coding agents present on the machine.
package main

import (
	"os"

	"github.com/louisescher/hangar/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		// Cobra already prints the error; just set the exit code.
		os.Exit(1)
	}
}
