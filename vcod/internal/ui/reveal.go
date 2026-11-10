package ui

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"

	"vcod/internal/platform"
)

// EnvNoAnim keeps the banner still while leaving colour alone.
const EnvNoAnim = "VCOD_NO_ANIM"

// The accent from its darkest shade to white-hot. The stop at rampRest is ColorAccent
// exactly, so the settled wordmark does not change colour when an effect ends.
var (
	accentRamp = append(
		blend(lipgloss.AdaptiveColor{Light: "#2e1065", Dark: "#2e1065"}, ColorAccent, 5),
		blend(ColorAccent, lipgloss.AdaptiveColor{Light: "#c4b5fd", Dark: "#f5f3ff"}, 7)[1:]...)
	rampStyles []lipgloss.Style
)

const (
	rampRest = 4
	// shimmerBand is the half-width of the sweep's highlight in cells.
	shimmerBand = 7
)

func initRamp() {
	rampStyles = make([]lipgloss.Style, len(accentRamp))
	for i, c := range accentRamp {
		rampStyles[i] = lipgloss.NewStyle().Foreground(c).Bold(true)
	}
}

// AnimEnabled reports whether the banner may animate: colour on, an interactive terminal
// on both ends, not CI and not switched off.
func AnimEnabled() bool {
	return colorEnabled && platform.IsTTY() && platform.IsStdinTTY() &&
		os.Getenv("CI") == "" && os.Getenv(EnvNoAnim) == ""
}

func (p *Printer) reveal(rows []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	Reveal(p.w, rows)
}

// Reveal draws the wordmark animated and leaves it in its settled colour: the letters
// condense column by column out of shade blocks, white-hot and cooling, then one highlight
// sweeps across. The last frame is byte for byte the still banner. Frames are redrawn in
// place, so nothing above the banner is cleared; a window too short to hold it, or anything
// that is not the terminal's stdout, gets the still frame.
func Reveal(w io.Writer, rows []string) {
	settled := make([]string, len(rows))
	for i, r := range rows {
		settled[i] = StyleBrand.Render(r)
	}
	still := strings.Join(settled, "\n") + "\n"
	if !AnimEnabled() || w != io.Writer(os.Stdout) || platform.TermRows() < len(rows)+2 {
		_, _ = fmt.Fprint(w, still)
		return
	}
	if len(rampStyles) == 0 {
		initRamp()
	}

	const (
		frame       = 16 * time.Millisecond
		dropFrames  = 14
		sweepFrames = 20
	)
	width := WordmarkWidth(rows)

	var interrupted atomic.Bool
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		if _, ok := <-sig; ok {
			interrupted.Store(true)
		}
	}()
	_, _ = fmt.Fprint(w, "\x1b[?25l")
	defer func() {
		signal.Stop(sig)
		close(sig)
		_, _ = fmt.Fprint(w, "\x1b[?25h")
		if interrupted.Load() {
			_ = syscall.Kill(os.Getpid(), syscall.SIGINT)
		}
	}()

	draw := func(render func(i int, row string) string) {
		var b strings.Builder
		for i, r := range rows {
			b.WriteString(render(i, r) + "\x1b[K\n")
		}
		_, _ = fmt.Fprint(w, b.String())
	}
	rewind := func() { _, _ = fmt.Fprint(w, "\x1b["+strconv.Itoa(len(rows))+"A\r") }

	for f := 0; f <= dropFrames && !interrupted.Load(); f++ {
		if f > 0 {
			rewind()
		}
		p := float64(f) / dropFrames
		draw(func(i int, row string) string { return materialiseAt(row, i, p) })
		time.Sleep(frame)
	}
	for f := 0; f < sweepFrames && !interrupted.Load(); f++ {
		head := -shimmerBand + (width+2*shimmerBand)*f/sweepFrames
		rewind()
		draw(func(_ int, row string) string { return shimmerAt(row, head) })
		time.Sleep(frame)
	}
	rewind()
	_, _ = fmt.Fprint(w, still)
}

// materialiseAt is row r at progress p of the drop-in. Each pair of columns arrives at its
// own hashed moment, lower rows a beat later, as ░ ▒ ▓ before the real glyph, cooling from
// the top of the ramp to the accent. Cells not yet arrived are blank, so widths never change.
func materialiseAt(row string, r int, p float64) string {
	top := len(rampStyles) - 1
	var b strings.Builder
	runIdx := -1
	var run strings.Builder
	flush := func() {
		if run.Len() == 0 {
			return
		}
		if runIdx < 0 {
			b.WriteString(run.String())
		} else {
			b.WriteString(rampStyles[runIdx].Render(run.String()))
		}
		run.Reset()
	}
	for x, ch := range []rune(row) {
		idx := -1
		glyph := ' '
		if ch != ' ' {
			if age := p - unit(uint64(x/2), 31)*0.45 - float64(r)*0.03; age >= 0 {
				glyph = ch
				switch {
				case age < 0.05:
					glyph = '░'
				case age < 0.1:
					glyph = '▒'
				case age < 0.15:
					glyph = '▓'
				}
				idx = top - int(clamp01((age-0.1)/0.3)*float64(top-rampRest))
			}
		}
		if idx != runIdx {
			flush()
			runIdx = idx
		}
		run.WriteRune(glyph)
	}
	flush()
	return b.String()
}

// shimmerAt is s lit by a highlight centred on cell head, graded from the accent at the
// band's edge to the top of the ramp, emitted in runs of one shade.
func shimmerAt(s string, head int) string {
	top := len(rampStyles) - 1
	var b strings.Builder
	var run strings.Builder
	runIdx := rampRest
	flush := func() {
		if run.Len() > 0 {
			b.WriteString(rampStyles[runIdx].Render(run.String()))
			run.Reset()
		}
	}
	for col, r := range []rune(s) {
		idx := runIdx
		if r != ' ' {
			idx = rampRest
			if d := abs(col - head); d < shimmerBand {
				idx = rampRest + (top-rampRest)*(shimmerBand-d)/shimmerBand
			}
		}
		if idx != runIdx {
			flush()
			runIdx = idx
		}
		run.WriteRune(r)
	}
	flush()
	return b.String()
}

// blend is n colours from a to b inclusive, mixed per theme.
func blend(a, b lipgloss.AdaptiveColor, n int) []lipgloss.AdaptiveColor {
	out := make([]lipgloss.AdaptiveColor, n)
	for i := range out {
		t := float64(i) / float64(n-1)
		out[i] = lipgloss.AdaptiveColor{Light: mixHex(a.Light, b.Light, t), Dark: mixHex(a.Dark, b.Dark, t)}
	}
	return out
}

func mixHex(a, b string, t float64) string {
	ca, cb := parseHex(a), parseHex(b)
	var out [3]int
	for i := range out {
		out[i] = int(float64(ca[i]) + (float64(cb[i])-float64(ca[i]))*t + 0.5)
	}
	return fmt.Sprintf("#%02x%02x%02x", out[0], out[1], out[2])
}

func parseHex(h string) [3]int {
	v, _ := strconv.ParseUint(strings.TrimPrefix(h, "#"), 16, 32)
	return [3]int{int(v >> 16 & 0xff), int(v >> 8 & 0xff), int(v & 0xff)}
}

// unit is a reproducible pseudo-random fraction in [0, 1) for the given seeds, so every
// frame agrees on when a column arrives.
func unit(seed ...uint64) float64 {
	h := uint64(0x2545f4914f6cdd1d)
	for _, s := range seed {
		h = hash64(h ^ s)
	}
	return float64(h>>11) / float64(1<<53)
}

// hash64 is splitmix64's finalising mix.
func hash64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

func clamp01(t float64) float64 { return max(0, min(1, t)) }

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
