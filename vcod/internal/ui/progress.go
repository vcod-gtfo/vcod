package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"vcod/internal/platform"
)

// SpinnerFrames is a braille spinner: one cell wide in every terminal.
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner picks a frame from an elapsed duration rather than a counter.
func Spinner(elapsed time.Duration) string {
	return SpinnerFrames[int(elapsed/(100*time.Millisecond))%len(SpinnerFrames)]
}

// Elapsed renders a duration the way a person reads a wait.
func Elapsed(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
}

// Progress is a live status line for a long operation. It occupies exactly one line, and
// only when there is a terminal to draw on; piped, it degrades to one plain line so a
// transcript does not fill with carriage returns.
type Progress struct {
	label string
	start time.Time
	live  bool
	w     io.Writer
	// onStop, when set, is told once when the progress ends.
	onStop func()

	mu      sync.Mutex
	stopped bool
	done    chan struct{}
}

// StartProgress begins a progress line. Always arrange for Stop to be called.
func StartProgress(label string) *Progress {
	p := newProgress(os.Stdout, label, platform.IsTTY() && ColorEnabled())
	if !p.live {
		fmt.Printf("  %s %s...\n", StyleInfo.Render("·"), label)
	}
	return p
}

// Busy spins while a query runs, on a terminal only. Piped, or captured into a session
// transcript, it draws nothing at all: the answer follows in a moment and a "querying"
// line would only pad the output. Always arrange for Stop to be called before the next
// line is printed.
//
// A Printer with OnBusy set is told the label instead, and "" when the query is over —
// the shell shows it in its footer, since its transcript only receives the output once
// the command returns.
func (p *Printer) Busy(label string) *Progress {
	f, isFile := p.w.(*os.File)
	prog := newProgress(p.w, label, isFile && platform.IsTerminal(f) && ColorEnabled())
	if p.OnBusy != nil {
		p.OnBusy(label)
		prog.onStop = func() { p.OnBusy("") }
	}
	return prog
}

func newProgress(w io.Writer, label string, live bool) *Progress {
	p := &Progress{
		label: label,
		start: time.Now(),
		live:  live,
		w:     w,
		done:  make(chan struct{}),
	}
	if p.live {
		go p.animate()
	}
	return p
}

func (p *Progress) animate() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			p.draw()
		}
	}
}

func (p *Progress) draw() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopped {
		return
	}
	elapsed := time.Since(p.start)
	line := fmt.Sprintf("  %s %s %s",
		StyleInfo.Render(Spinner(elapsed)), p.label, StyleDim.Render("("+Elapsed(elapsed)+")"))
	fmt.Fprintf(p.w, "\r%s\x1b[K", truncateVisible(line, platform.TermCols()))
}

// Stop ends the progress line, clearing it. Safe to call more than once.
func (p *Progress) Stop() {
	p.mu.Lock()
	if p.stopped {
		p.mu.Unlock()
		return
	}
	p.stopped = true
	p.mu.Unlock()
	close(p.done)
	if p.live {
		fmt.Fprint(p.w, "\r\x1b[K")
	}
	if p.onStop != nil {
		p.onStop()
	}
}

// truncateVisible shortens a styled line to width cells, counting only visible text.
func truncateVisible(s string, width int) string {
	if width <= 1 {
		return s
	}
	visible := 0
	inEscape := false
	var b strings.Builder
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			b.WriteRune(r)
			continue
		}
		if inEscape {
			b.WriteRune(r)
			if r >= '@' && r <= '~' && r != '[' {
				inEscape = false
			}
			continue
		}
		if visible >= width-1 {
			b.WriteString("\x1b[0m")
			return b.String()
		}
		b.WriteRune(r)
		visible++
	}
	return b.String()
}
