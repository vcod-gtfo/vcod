package ui

import (
	"fmt"
	"os"
	"strings"

	"vcod/internal/platform"
)

// The wordmark: V C O D in the ANSI Shadow block font, 36 columns.
const wordmark = `  ██╗   ██╗ ██████╗ ██████╗ ██████╗
  ██║   ██║██╔════╝██╔═══██╗██╔══██╗
  ██║   ██║██║     ██║   ██║██║  ██║
  ╚██╗ ██╔╝██║     ██║   ██║██║  ██║
   ╚████╔╝ ╚██████╗╚██████╔╝██████╔╝
    ╚═══╝   ╚═════╝ ╚═════╝ ╚═════╝`

// A narrow wordmark for terminals too small for the full one: four glyphs, three cells
// each.
const wordmarkSmall = `  ╦  ╦╔═╗╔═╗╔╦╗
  ╚╗╔╝║  ║ ║ ║║
   ╚╝ ╚═╝╚═╝═╩╝`

// WordmarkRows is the widest wordmark that fits the current terminal, unstyled.
func WordmarkRows() []string {
	if platform.TermCols() >= 40 {
		return WordmarkLarge()
	}
	return WordmarkSmall()
}

// WordmarkSmall returns the narrow wordmark unstyled, as three lines.
func WordmarkSmall() []string { return strings.Split(wordmarkSmall, "\n") }

// WordmarkLarge returns the full wordmark unstyled, as six lines.
func WordmarkLarge() []string { return strings.Split(wordmark, "\n") }

// WordmarkWidth is the width in cells of the widest row of a wordmark.
func WordmarkWidth(rows []string) int {
	w := 0
	for _, r := range rows {
		if n := visibleWidth(r); n > w {
			w = n
		}
	}
	return w
}

// Slogan is the line under the wordmark.
const Slogan = "Call of Duty workspace: servers, clients, tests"

// EnvNoBanner suppresses the banner in a child process.
//
// `vcod update` sets it on the installer it execs out of the fresh clone, so an update
// prints one wordmark rather than one per process in the chain. An environment variable
// rather than a flag, so an older installer that predates this ignores it instead of
// rejecting an unknown argument — the same reason install.EnvInstallVersion is one.
const EnvNoBanner = "VCOD_NO_BANNER"

// BannerSuppressed reports whether a caller has asked for no banner.
func BannerSuppressed() bool { return os.Getenv(EnvNoBanner) == "1" }

// Banner prints the wordmark plus a one line context strip. An empty subtitle omits the
// second line rather than printing a blank one. Quiet printers and a child process under
// EnvNoBanner print nothing.
func (p *Printer) Banner(subtitle string) {
	if p.quiet || BannerSuppressed() {
		return
	}
	p.printf("\n")
	p.reveal(WordmarkRows())
	p.printf("  %s\n", StyleBold.Render(Slogan))
	p.printf("  %s\n", StyleDim.Render("cod_rea port · flavors · live tests"))
	if subtitle != "" {
		p.printf("  %s\n", StyleDim.Render(subtitle))
	}
	p.printf("\n")
}

// Banner prints the default Printer's banner.
func Banner(subtitle string) { Std.Banner(subtitle) }

// ContextLine is the one line "who and where am I" strip under the wordmark.
func ContextLine(version string) string {
	return fmt.Sprintf("  %s  ·  %s  ·  %s",
		StyleAccent.Render(platform.OS()),
		platform.Arch(),
		StyleDim.Render("version "+version))
}

// Table renders aligned columns without a border, so the output stays greppable.
func Table(rows [][]string, indent string) string {
	if len(rows) == 0 {
		return ""
	}
	widths := make([]int, 0, len(rows[0]))
	for _, row := range rows {
		for i, cell := range row {
			w := visibleWidth(cell)
			if i >= len(widths) {
				widths = append(widths, w)
			} else if w > widths[i] {
				widths[i] = w
			}
		}
	}

	var b strings.Builder
	for _, row := range rows {
		var line strings.Builder
		line.WriteString(indent)
		for i, cell := range row {
			line.WriteString(cell)
			if i < len(row)-1 {
				pad := widths[i] - visibleWidth(cell)
				line.WriteString(strings.Repeat(" ", pad+2))
			}
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteString("\n")
	}
	return b.String()
}
