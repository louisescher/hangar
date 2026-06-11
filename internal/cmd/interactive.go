package cmd

import (
	"os"
	"strings"

	"github.com/louisescher/hangar/internal/config"
	"github.com/louisescher/hangar/internal/engine"
	"github.com/louisescher/hangar/internal/security/sanitize"
	"github.com/louisescher/hangar/internal/spec"
	"github.com/louisescher/hangar/internal/tui"
	"github.com/spf13/cobra"
)

// interactive reports whether the TUI should be used: both standard streams are
// terminals, no CI marker is set, TERM is usable, and --no-tty was not given.
func interactive(noTTY bool) bool {
	if noTTY || os.Getenv("CI") != "" || strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	return stdinIsTTY() && stdoutIsTTY()
}

func stdinIsTTY() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// runTUI launches the interactive picker. A nil startSpec opens the catalog; a
// non-nil discovered pre-seeds the picker (skipping the crawl). secOverride,
// when non-nil, replaces the prefs-derived sanitize options (CLI flags win).
func runTUI(c *cobra.Command, startSpec *spec.SourceSpec, discovered *engine.Discovered, agentNames []string, global bool, secOverride *sanitize.Opts) error {
	prefs, _ := config.LoadPrefs()
	sec := sanitize.Opts{StripComments: prefs.StripComments, StripInvisible: prefs.StripInvisible}
	if secOverride != nil {
		sec = *secOverride
	}
	return tui.Run(tui.Options{
		Engine:     engine.New(),
		Prefs:      prefs,
		Global:     global || prefs.Scope == config.ScopeGlobal,
		Security:   sec,
		Agents:     agentNames,
		StartSpec:  startSpec,
		Discovered: discovered,
		Ctx:        c.Context(),
	})
}

// runManageTUI launches the manage-installed screen (`hangar list`).
func runManageTUI(c *cobra.Command, global bool) error {
	prefs, _ := config.LoadPrefs()
	return tui.Run(tui.Options{
		Engine: engine.New(),
		Prefs:  prefs,
		Global: global || prefs.Scope == config.ScopeGlobal,
		Manage: true,
		Ctx:    c.Context(),
	})
}

// runProfilesTUI launches the interactive saved-profile browser.
func runProfilesTUI(c *cobra.Command) error {
	prefs, _ := config.LoadPrefs()
	return tui.Run(tui.Options{
		Engine:   engine.New(),
		Prefs:    prefs,
		Profiles: true,
		Ctx:      c.Context(),
	})
}

// runInfoTUI launches the info browser for a discovered source. The Discovered
// is closed by tui.Run on exit.
func runInfoTUI(c *cobra.Command, d *engine.Discovered, global bool) error {
	prefs, _ := config.LoadPrefs()
	return tui.Run(tui.Options{
		Engine:     engine.New(),
		Prefs:      prefs,
		Global:     global,
		Info:       true,
		Discovered: d,
		Ctx:        c.Context(),
	})
}
