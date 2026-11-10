// Package ui is the presentation layer: one place for colour and one vocabulary for
// status lines, so output from every command reads as one tool.
//
//	Step("...")   a section heading
//	OK("...")     something that worked
//	Info("...")   a fact worth printing that is not a verdict
//	Warn("...")   something the user should probably act on
//	Errorf("...") an error line on stderr
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"vcod/internal/platform"
)

// The palette: lipgloss.AdaptiveColor pairs (Tailwind 600/400) so a warning is legible
// on a light theme as well as a dark one. lipgloss degrades them to the nearest ANSI
// colour on a 16-colour terminal.
var (
	ColorGreen  = lipgloss.AdaptiveColor{Light: "#16a34a", Dark: "#4ade80"}
	ColorRed    = lipgloss.AdaptiveColor{Light: "#dc2626", Dark: "#f87171"}
	ColorYellow = lipgloss.AdaptiveColor{Light: "#ca8a04", Dark: "#facc15"}
	ColorCyan   = lipgloss.AdaptiveColor{Light: "#0891b2", Dark: "#22d3ee"}
	ColorBlue   = lipgloss.AdaptiveColor{Light: "#2563eb", Dark: "#60a5fa"}
	ColorGray   = lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#9ca3af"}
	ColorText   = lipgloss.AdaptiveColor{Light: "#1f2937", Dark: "#f9fafb"}

	// ColorAccent is the brand colour: the wordmark, the selected row, the prompt. Violet,
	// deliberately not one of the status colours, so chrome never reads as a verdict.
	ColorAccent   = lipgloss.AdaptiveColor{Light: "#7c3aed", Dark: "#a78bfa"}
	ColorOnAccent = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#1e1b4b"}
	ColorSurface  = lipgloss.AdaptiveColor{Light: "#e5e7eb", Dark: "#1f2937"}
	// ColorAmber marks the 1.6x flavor, the modded one, so the two flavors never look
	// alike in a header.
	ColorAmber   = lipgloss.AdaptiveColor{Light: "#d97706", Dark: "#fbbf24"}
	ColorOnAmber = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#1f2937"}
	ColorOnBlue  = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#172554"}
	ColorOnGreen = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#052e16"}
	ColorOnRed   = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#450a0a"}
	ColorOnGray  = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#111827"}
)

// Styles are resolved once at startup and exported so commands compose their own lines
// without inventing new colours.
var (
	StylePass   lipgloss.Style
	StyleFail   lipgloss.Style
	StyleInfo   lipgloss.Style
	StyleWarn   lipgloss.Style
	StyleStep   lipgloss.Style
	StyleBold   lipgloss.Style
	StyleCyan   lipgloss.Style
	StyleGreen  lipgloss.Style
	StyleRed    lipgloss.Style
	StyleYellow lipgloss.Style
	StyleWhite  lipgloss.Style
	StyleDim    lipgloss.Style

	StyleAccent      lipgloss.Style
	StyleBrand       lipgloss.Style
	StyleBadge       lipgloss.Style
	StyleSurface     lipgloss.Style
	StyleSurfaceDim  lipgloss.Style
	StyleSurfaceBold lipgloss.Style
	StyleSelected    lipgloss.Style
	StyleLink        lipgloss.Style
)

var colorEnabled bool

func init() { InitColor(false) }

// InitColor decides whether ANSI escapes are emitted and builds the styles.
//
// Suppressed when stdout is not a terminal, when NO_COLOR is set, when TERM is dumb, or
// when --no-color was passed. VCOD_FORCE_COLOR=1 overrides every one of those.
func InitColor(noColorFlag bool) {
	enabled := true
	switch {
	case os.Getenv("NO_COLOR") != "":
		enabled = false
	case os.Getenv("TERM") == "dumb":
		enabled = false
	case noColorFlag:
		enabled = false
	case !platform.IsTTY():
		enabled = false
	}
	if os.Getenv("VCOD_FORCE_COLOR") == "1" {
		enabled = true
	}
	colorEnabled = enabled

	if enabled {
		profile := termenv.ColorProfile()
		if profile == termenv.Ascii {
			profile = termenv.ANSI
		}
		lipgloss.SetColorProfile(profile)
	} else {
		lipgloss.SetColorProfile(termenv.Ascii)
	}

	StylePass = lipgloss.NewStyle().Foreground(ColorGreen).Bold(true)
	StyleFail = lipgloss.NewStyle().Foreground(ColorRed).Bold(true)
	StyleInfo = lipgloss.NewStyle().Foreground(ColorCyan)
	StyleWarn = lipgloss.NewStyle().Foreground(ColorYellow).Bold(true)
	StyleStep = lipgloss.NewStyle().Foreground(ColorBlue).Bold(true)
	StyleBold = lipgloss.NewStyle().Bold(true)
	StyleCyan = lipgloss.NewStyle().Foreground(ColorCyan)
	StyleGreen = lipgloss.NewStyle().Foreground(ColorGreen)
	StyleRed = lipgloss.NewStyle().Foreground(ColorRed)
	StyleYellow = lipgloss.NewStyle().Foreground(ColorYellow)
	StyleWhite = lipgloss.NewStyle().Foreground(ColorText).Bold(true)
	StyleDim = lipgloss.NewStyle().Foreground(ColorGray)

	StyleAccent = lipgloss.NewStyle().Foreground(ColorAccent)
	StyleBrand = lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	StyleBadge = lipgloss.NewStyle().Foreground(ColorOnAccent).Background(ColorAccent).Bold(true)
	StyleSurface = lipgloss.NewStyle().Foreground(ColorText).Background(ColorSurface)
	StyleSurfaceDim = lipgloss.NewStyle().Foreground(ColorGray).Background(ColorSurface)
	StyleSurfaceBold = lipgloss.NewStyle().Foreground(ColorText).Background(ColorSurface).Bold(true)
	StyleSelected = lipgloss.NewStyle().Reverse(true)
	StyleLink = lipgloss.NewStyle().Foreground(ColorAccent).Underline(true)

	if enabled {
		// Settle the dark-background query while the terminal is still ours, before any
		// Bubble Tea program takes it over and eats the answer.
		_ = lipgloss.HasDarkBackground()
	}
}

// ColorEnabled reports whether escapes are being emitted.
func ColorEnabled() bool { return colorEnabled }

// FlavorBadge renders the flavor in use as a badge: filled amber for 1.6x (protocol 10,
// the cod1plus server), hollow blue for retail 1.5 (protocol 6). The name is always in
// the text, so the badge reads without colour too.
func FlavorBadge(flavor string) string {
	return flavorStyle(flavor).Render(" " + FlavorGlyph(flavor) + " " + flavor + " ")
}

// FlavorGlyph is the marker for a flavor: filled for 1.6x, hollow for 1.5.
func FlavorGlyph(flavor string) string {
	if flavor == "1.6x" {
		return "●"
	}
	return "○"
}

// FlavorColor is the hue a flavor's badge is drawn in.
func FlavorColor(flavor string) lipgloss.AdaptiveColor {
	switch flavor {
	case "1.6x":
		return ColorAmber
	case "1.5":
		return ColorBlue
	}
	return ColorGray
}

func flavorStyle(flavor string) lipgloss.Style {
	var on lipgloss.AdaptiveColor
	switch flavor {
	case "1.6x":
		on = ColorOnAmber
	case "1.5":
		on = ColorOnBlue
	default:
		on = ColorOnGray
	}
	return lipgloss.NewStyle().Foreground(on).Background(FlavorColor(flavor)).Bold(true)
}

// ModeBadge renders the safe / write state. Safe is a quiet green; write mode is red
// and filled, because it is the state in which a command deletes a flavor or a build.
func ModeBadge(write bool) string {
	if write {
		return lipgloss.NewStyle().Foreground(ColorOnRed).Background(ColorRed).Bold(true).Render(" ✎ WRITE ")
	}
	return lipgloss.NewStyle().Foreground(ColorOnGreen).Background(ColorGreen).Bold(true).Render(" 🔒 SAFE ")
}

// Printer writes status lines.
type Printer struct {
	mu sync.Mutex
	w  io.Writer
	// quiet drops the banner: a transcript inside the shell already has the wordmark
	// in its header.
	quiet bool
	// OnBusy, when set, receives the label of the query in flight from Busy, and ""
	// once it is over, for a host that draws its own progress.
	OnBusy func(label string)
}

// New returns a Printer writing to w.
func New(w io.Writer) *Printer { return &Printer{w: w} }

// NewQuiet returns a Printer that skips banners, for output captured into the shell.
func NewQuiet(w io.Writer) *Printer { return &Printer{w: w, quiet: true} }

// Std is the default Printer, writing to stdout.
var Std = New(os.Stdout)

// Err is where Errorf and Hint go: stderr, or wherever the shell points it while a
// command runs inside the alt screen.
var Err = New(os.Stderr)

func (p *Printer) printf(format string, a ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(p.w, format, a...)
}

// Info prints a fact that is not a verdict.
func (p *Printer) Info(format string, a ...any) {
	p.printf("  %s %s\n", StyleInfo.Render("INFO"), fmt.Sprintf(format, a...))
}

// Note prints dimmed secondary detail.
func (p *Printer) Note(format string, a ...any) {
	p.printf("  %s\n", StyleDim.Render(fmt.Sprintf(format, a...)))
}

const detailIndent = "      "

// Detail prints an indented supporting line under a finding.
func (p *Printer) Detail(format string, a ...any) {
	p.printf("%s%s\n", detailIndent, fmt.Sprintf(format, a...))
}

// Fields prints aligned label/value detail. An empty value prints as a dash rather than
// nothing, because a row that stops after its label reads as a rendering fault.
func (p *Printer) Fields(rows [][2]string) {
	if len(rows) == 0 {
		return
	}
	tbl := make([][]string, 0, len(rows))
	for _, r := range rows {
		value := r[1]
		if value == "" {
			value = StyleDim.Render("—")
		}
		tbl = append(tbl, []string{StyleDim.Render(r[0]), value})
	}
	p.Raw(Table(tbl, detailIndent))
}

// Listing prints an aligned multi-column table at the detail indent.
func (p *Printer) Listing(rows [][]string) {
	p.Raw(Table(rows, detailIndent))
}

// Warn prints something the user should probably act on.
func (p *Printer) Warn(format string, a ...any) {
	p.printf("  %s %s\n", StyleWarn.Render("▲"), fmt.Sprintf(format, a...))
}

// OK prints a tick.
func (p *Printer) OK(format string, a ...any) {
	p.printf("  %s %s\n", StylePass.Render("✔"), fmt.Sprintf(format, a...))
}

// Bad prints a cross.
func (p *Printer) Bad(format string, a ...any) {
	p.printf("  %s %s\n", StyleFail.Render("✘"), fmt.Sprintf(format, a...))
}

// Plain writes a line with no decoration.
func (p *Printer) Plain(format string, a ...any) { p.printf(format+"\n", a...) }

// Raw writes exactly what it is given.
func (p *Printer) Raw(s string) { p.printf("%s", s) }

// Section layout: a blank line, a marker, the title, an optional dim caption, then a
// rule of fixed width so sections from different commands share a right edge.
const (
	gutter      = "  "
	sectionMark = "▸ "
	captionSep  = "  ·  "
	ruleChar    = "─"
	bannerChar  = "━"
	ruleWidth   = 56
)

func rule(char string, width int) string {
	if cols := platform.TermCols() - len(gutter); cols < width {
		width = cols
	}
	if width < 1 {
		width = 1
	}
	return strings.Repeat(char, width)
}

func visibleWidth(s string) int { return ansi.StringWidth(s) }

// VisibleWidth is the width of s in terminal cells, ignoring escape sequences.
func VisibleWidth(s string) int { return visibleWidth(s) }

// Section prints a section heading with a dim caption after the title.
func (p *Printer) Section(title, caption string) {
	head := StyleStep.Render(sectionMark) + StyleWhite.Render(title)
	if caption != "" {
		head += StyleDim.Render(captionSep + caption)
	}
	p.printf("\n%s%s\n%s%s\n", gutter, head, gutter, StyleDim.Render(rule(ruleChar, ruleWidth)))
}

// Step prints a section heading with no caption.
func (p *Printer) Step(format string, a ...any) {
	p.Section(fmt.Sprintf(format, a...), "")
}

// HR draws a horizontal rule at the shared edges.
func (p *Printer) HR() {
	p.printf("%s%s\n", gutter, StyleDim.Render(rule(ruleChar, ruleWidth)))
}

// Heading prints the banner a long-running command opens with: heavy rules above and
// below, spanning the title.
func (p *Printer) Heading(format string, a ...any) {
	text := fmt.Sprintf(format, a...)
	width := visibleWidth(text)
	if width < ruleWidth {
		width = ruleWidth
	}
	bar := StyleStep.Render(rule(bannerChar, width))
	p.printf("\n%s%s\n%s%s\n%s%s\n", gutter, bar, gutter, StyleWhite.Render(text), gutter, bar)
}

// Package-level shorthands over Std.

// Info prints a fact that is not a verdict.
func Info(format string, a ...any) { Std.Info(format, a...) }

// Note prints dimmed secondary detail.
func Note(format string, a ...any) { Std.Note(format, a...) }

// Detail prints an indented supporting line.
func Detail(format string, a ...any) { Std.Detail(format, a...) }

// Fields prints aligned label/value detail.
func Fields(rows [][2]string) { Std.Fields(rows) }

// Listing prints an aligned table at the detail indent.
func Listing(rows [][]string) { Std.Listing(rows) }

// Warn prints something the user should act on.
func Warn(format string, a ...any) { Std.Warn(format, a...) }

// OK prints a tick.
func OK(format string, a ...any) { Std.OK(format, a...) }

// Bad prints a cross.
func Bad(format string, a ...any) { Std.Bad(format, a...) }

// Step prints a section heading.
func Step(format string, a ...any) { Std.Step(format, a...) }

// Section prints a section heading with a dim caption.
func Section(title, caption string) { Std.Section(title, caption) }

// Plain writes an undecorated line.
func Plain(format string, a ...any) { Std.Plain(format, a...) }

// Raw writes exactly what it is given.
func Raw(s string) { Std.Raw(s) }

// HR draws a horizontal rule.
func HR() { Std.HR() }

// Heading prints the opening banner of a long-running command.
func Heading(format string, a ...any) { Std.Heading(format, a...) }

// Errorf prints an error line to stderr without exiting.
func Errorf(format string, a ...any) {
	Err.printf("%s %s\n", StyleFail.Render("ERROR"), fmt.Sprintf(format, a...))
}

// Hint prints an indented suggestion to stderr, alongside an error.
func Hint(format string, a ...any) {
	Err.printf("  %s\n", StyleDim.Render(fmt.Sprintf(format, a...)))
}

// Errorf prints an error line through this printer: inside the shell it lands in the
// tab the command runs in, in a terminal Std's goes to stderr through the package Errorf.
func (p *Printer) Errorf(format string, a ...any) {
	if p == Std {
		Errorf(format, a...)
		return
	}
	p.printf("  %s %s\n", StyleFail.Render("✘"), fmt.Sprintf(format, a...))
}

// Hint prints a suggestion under an error, through this printer.
func (p *Printer) Hint(format string, a ...any) {
	if p == Std {
		Hint(format, a...)
		return
	}
	p.printf("    %s\n", StyleDim.Render(fmt.Sprintf(format, a...)))
}

// LineWriter is an io.Writer that hands complete lines to a callback, for a printer
// whose output streams into the shell.
type LineWriter struct {
	mu      sync.Mutex
	pending string
	Emit    func(line string)
}

// Write splits on newlines and emits each complete line.
func (w *LineWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	text := w.pending + string(b)
	parts := strings.Split(text, "\n")
	w.pending = parts[len(parts)-1]
	for _, line := range parts[:len(parts)-1] {
		w.Emit(line)
	}
	return len(b), nil
}

// Flush emits a final partial line.
func (w *LineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending != "" {
		w.Emit(w.pending)
		w.pending = ""
	}
}
