package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/louisescher/hangar/internal/install"
)

// progressReporter surfaces install/update progress on stderr for headless
// commands. With --verbose it prints one line per event. Otherwise, on a
// terminal, it animates a single spinner line (cleared when finished) so a long
// fetch never looks like a hang. When stderr is not a terminal and not verbose
// it stays silent, keeping piped/CI output clean.
type progressReporter struct {
	w       io.Writer
	verbose bool
	tty     bool

	mu    sync.Mutex
	label string
	idx   int
	total int

	stop    chan struct{}
	done    chan struct{}
	started bool
}

func newProgress(w io.Writer, verbose bool) *progressReporter {
	return &progressReporter{w: w, verbose: verbose, tty: stderrIsTTY(), label: "working…"}
}

// start launches the spinner animation (a no-op when verbose or off a TTY).
func (p *progressReporter) start() {
	if p.verbose || !p.tty {
		return
	}
	p.started = true
	p.stop = make(chan struct{})
	p.done = make(chan struct{})
	go p.spin()
}

func (p *progressReporter) spin() {
	defer close(p.done)
	frames := []rune("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏")
	t := time.NewTicker(90 * time.Millisecond)
	defer t.Stop()
	for i := 0; ; i++ {
		select {
		case <-p.stop:
			return
		case <-t.C:
			p.mu.Lock()
			label, idx, total := p.label, p.idx, p.total
			p.mu.Unlock()
			line := string(frames[i%len(frames)]) + " " + label
			if total > 0 {
				line += fmt.Sprintf(" (%d/%d)", idx, total)
			}
			fmt.Fprintf(p.w, "\r\033[K%s", line)
		}
	}
}

// event is the install.OnProgress callback.
func (p *progressReporter) event(ev install.Event) {
	label := strings.TrimSpace(ev.Phase + " " + ev.Name)
	if label == "" {
		label = "working…"
	}
	if p.verbose {
		if ev.Total > 0 {
			fmt.Fprintf(p.w, "[%d/%d] %s\n", ev.Index, ev.Total, label)
		} else {
			fmt.Fprintf(p.w, "%s…\n", label)
		}
		return
	}
	p.mu.Lock()
	p.label, p.idx, p.total = label, ev.Index, ev.Total
	p.mu.Unlock()
}

// finish stops the spinner and clears its line. Safe to call more than once.
func (p *progressReporter) finish() {
	if !p.started {
		return
	}
	p.started = false
	close(p.stop)
	<-p.done
	fmt.Fprint(p.w, "\r\033[K")
}

func stderrIsTTY() bool {
	fi, err := os.Stderr.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
