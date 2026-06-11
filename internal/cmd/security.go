package cmd

import (
	"os"

	"github.com/louisescher/hangar/internal/security/audit"
	"github.com/louisescher/hangar/internal/security/sanitize"
	"github.com/spf13/cobra"
)

// securityFlags holds the sanitize/audit flags shared by install and update.
type securityFlags struct {
	noStrip          bool
	noStripComments  bool
	noStripInvisible bool
	forceAudit       bool
	noAudit          bool
}

func (f *securityFlags) register(cmd *cobra.Command) {
	fl := cmd.Flags()
	fl.BoolVar(&f.noStrip, "no-strip", false, "disable all content sanitization")
	fl.BoolVar(&f.noStripComments, "no-strip-comments", false, "keep markdown comments in references")
	fl.BoolVar(&f.noStripInvisible, "no-strip-invisible", false, "keep invisible/zero-width characters")
	fl.BoolVar(&f.forceAudit, "audit", false, "always print the audit log")
	fl.BoolVar(&f.noAudit, "no-audit", false, "never print the audit log")
}

// sanitizeOpts resolves the sanitize options: everything on by default, with
// --no-strip* turning passes off.
func (f *securityFlags) sanitizeOpts() sanitize.Opts {
	o := sanitize.All
	if f.noStrip {
		return sanitize.None
	}
	if f.noStripComments {
		o.StripComments = false
	}
	if f.noStripInvisible {
		o.StripInvisible = false
	}
	return o
}

// emitAudit prints the audit envelope to stdout when appropriate: forced by
// --audit, or by default when stdout is not a terminal (an agent is likely
// reading it). --no-audit suppresses it. Interactive humans don't see it.
func (f *securityFlags) emitAudit(c *cobra.Command, log *audit.Log) {
	if log == nil || log.Empty() {
		return
	}
	emit := f.forceAudit || (!f.noAudit && !stdoutIsTTY())
	if emit {
		_, _ = c.OutOrStdout().Write([]byte(log.StdoutEnvelope()))
	}
}

func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
