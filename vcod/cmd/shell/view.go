package shell

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"vcod/internal/game"
	"vcod/internal/q3"
	"vcod/internal/ui"
)

// The frame: the wordmark with the flavor, the mode and the servers beside it, the tab
// strip, a rule, the transcript, a rule, the prompt, a rule, the hint line. Assembled
// as an exact number of rows summing to the terminal height.
const (
	minWidth  = 48
	minHeight = 12
	// logoGap is the room kept beside the wordmark for the facts column.
	logoGap = 44

	scrollThumb = "┃"
	scrollTrack = "│"
)

func (m Model) render() string {
	if m.quitting {
		return ""
	}
	if m.width < minWidth || m.height < minHeight {
		return m.tooSmall()
	}
	var b strings.Builder
	b.WriteString(m.header())
	if strip := m.tabStrip(); strip != "" {
		b.WriteString(strip + "\n")
	}
	b.WriteString(m.rule())
	b.WriteString(m.pane())
	b.WriteString(m.candidateRow())
	b.WriteString(m.rule())
	b.WriteString(m.clip(" "+m.input.View()) + "\n")
	b.WriteString(m.rule())
	b.WriteString(m.footer())
	return b.String()
}

func (m Model) tooSmall() string {
	msg := []string{
		ui.StyleWarn.Render("terminal too small"),
		ui.StyleDim.Render("need " + strconv.Itoa(minWidth) + "×" + strconv.Itoa(minHeight) +
			", have " + strconv.Itoa(m.width) + "×" + strconv.Itoa(m.height)),
	}
	rows := make([]string, m.height)
	top := (m.height - len(msg)) / 2
	for i, line := range msg {
		if top+i >= 0 && top+i < m.height {
			rows[top+i] = m.clip(lipgloss.PlaceHorizontal(m.width, lipgloss.Center, line))
		}
	}
	return strings.Join(rows, "\n")
}

// logo is the wordmark the window can afford, and the single source of truth for the
// header height.
func (m Model) logo() []string {
	large := ui.WordmarkLarge()
	if m.width >= ui.WordmarkWidth(large)+logoGap && m.height >= 26 {
		return large
	}
	return ui.WordmarkSmall()
}

func (m Model) header() string {
	logo := m.logo()
	col := ui.WordmarkWidth(logo) + 2
	facts := m.facts(len(logo))
	var b strings.Builder
	for i, row := range logo {
		line := ui.StyleBrand.Render(row)
		if i < len(facts) && facts[i] != "" {
			line = padTo(line, col) + facts[i]
		}
		b.WriteString(m.clip(line) + "\n")
	}
	return b.String()
}

// facts is the column beside the wordmark: the flavor and the mode first, then one line
// per server.
func (m Model) facts(rows int) []string {
	badges := ui.FlavorBadge(string(m.flavor)) + "  " + ui.ModeBadge(m.write)
	srv := m.serverLines()
	if rows >= 6 {
		out := []string{"", badges}
		out = append(out, srv...)
		if len(out) < 5 {
			out = append(out, m.clientLine())
		}
		for len(out) < 5 {
			out = append(out, "")
		}
		return append(out, ui.StyleDim.Render("version "+m.version+" · "+ui.Slogan))
	}
	return append([]string{badges}, srv...)
}

// clientLine is what the client starts with: resolution, display mode, fps cap.
func (m Model) clientLine() string {
	if m.cfg == nil {
		return ""
	}
	v, err := m.cfg.Video()
	if err != nil {
		return ui.StyleDim.Render("client  ") + ui.StyleYellow.Render("bad display setting — vcod display")
	}
	return ui.StyleDim.Render("client  " + v.Summary())
}

// serverLines is one line per flavor: up or down, and what an answering server plays.
func (m Model) serverLines() []string {
	if m.servers == nil {
		if m.app.WS == nil {
			return []string{ui.StyleYellow.Render("no workspace"), ""}
		}
		return []string{ui.StyleDim.Render("servers …"), ""}
	}
	var out []string
	for _, s := range m.servers {
		out = append(out, serverLine(s))
	}
	return out
}

func serverLine(s game.Server) string {
	name := ui.StyleDim.Render("server " + string(s.Flavor) + " ")
	switch {
	case s.Status != nil:
		st := s.Status
		players := strconv.Itoa(len(st.Players)) + "p"
		return name + ui.StyleGreen.Render("● up ") + ui.StyleWhite.Render(st.Get("mapname")) + " " +
			ui.StyleDim.Render(st.Get("g_gametype")+" · "+players+" · "+strings.TrimPrefix(s.Addr, "127.0.0.1")+" · "+q3.StripColors(st.Get("sv_hostname")))
	case s.Running():
		return name + ui.StyleYellow.Render("◐ starting") + ui.StyleDim.Render(" (not answering yet)")
	}
	return name + ui.StyleDim.Render("○ down")
}

// tabStrip is the row of tabs, or nothing while there is only one.
func (m Model) tabStrip() string {
	if len(m.tabs) < 2 {
		return ""
	}
	parts := make([]string, 0, len(m.tabs))
	for i, t := range m.tabs {
		label := strconv.Itoa(i+1) + " " + t.title
		mark := m.tabMark(t)
		if i == m.active {
			parts = append(parts, ui.StyleBadge.Render(" "+label+" ")+mark)
			continue
		}
		l := " " + ui.StyleWhite.Render(strconv.Itoa(i+1)) + " " + ui.StyleDim.Render(t.title)
		if t.unread {
			l = " " + ui.StyleWhite.Render(strconv.Itoa(i+1)) + " " + ui.StyleWhite.Render(t.title)
		}
		parts = append(parts, l+mark+" ")
	}
	return m.clip(" " + lipgloss.JoinHorizontal(lipgloss.Top, parts...))
}

// tabMark is a tab's state: a spinner while its command runs, ✔ or ✘ after, ⇣ while it
// follows a log.
func (m Model) tabMark(t *tab) string {
	switch {
	case t.running():
		return ui.StyleYellow.Render(ui.Spinner(time.Since(t.task.started)))
	case t.task != nil && t.task.code == 0:
		return ui.StyleGreen.Render("✔")
	case t.task != nil:
		return ui.StyleRed.Render("✘")
	}
	return ""
}

func (m Model) tabStripHeight() int {
	if len(m.tabs) < 2 {
		return 0
	}
	return 1
}

func (m Model) rule() string {
	return ui.StyleDim.Render(strings.Repeat("─", m.width)) + "\n"
}

// pane is the transcript window, exactly paneHeight rows, with a scrollbar when the
// transcript is longer than the window.
func (m Model) pane() string {
	height := m.paneHeight()
	width := m.width - 3
	t := m.cur()
	start := t.scroll
	if start > len(t.lines) {
		start = len(t.lines)
	}
	end := start + height
	if end > len(t.lines) {
		end = len(t.lines)
	}
	rows := t.lines[start:end]
	if len(t.lines) == 0 {
		rows = []string{"", ui.StyleDim.Render("  nothing here yet — type a command, or ") + ui.StyleAccent.Render("help") + ui.StyleDim.Render(" lists them")}
		if len(m.tabs) > 1 {
			rows = append(rows, ui.StyleDim.Render("  alt+w closes this tab"))
		}
	}
	bar := scrollbar(height, start, len(t.lines))
	var b strings.Builder
	for i := 0; i < height; i++ {
		row := ""
		if i < len(rows) {
			row = rows[i]
		}
		if bar != nil {
			row = ansi.Truncate(row, width, "")
			row = " " + row + strings.Repeat(" ", max(0, width-ansi.StringWidth(row))) + " " + bar[i]
		} else {
			row = " " + row
		}
		b.WriteString(m.clip(row) + "\n")
	}
	return b.String()
}

func scrollbar(height, start, total int) []string {
	if total <= height || height < 1 {
		return nil
	}
	thumb := height * height / total
	if thumb < 1 {
		thumb = 1
	}
	pos := 0
	if room := total - height; room > 0 {
		pos = start * (height - thumb) / room
	}
	bar := make([]string, height)
	for i := range bar {
		if i >= pos && i < pos+thumb {
			bar[i] = ui.StyleAccent.Render(scrollThumb)
		} else {
			bar[i] = ui.StyleDim.Render(scrollTrack)
		}
	}
	return bar
}

// candidateRow is the completion row under the pane, or nothing.
func (m Model) candidateRow() string {
	if len(m.completions) == 0 {
		return ""
	}
	fields := strings.Fields(m.input.Value())
	typed := ""
	if len(fields) > 0 && !strings.HasSuffix(m.input.Value(), " ") {
		typed = fields[len(fields)-1]
	}
	var shown []string
	width := 0
	for _, c := range m.completions {
		w := ansi.StringWidth(c)
		if width+w+2 > m.width-12 {
			break
		}
		if typed != "" && strings.HasPrefix(c, typed) {
			shown = append(shown, ui.StyleBrand.Render(typed)+ui.StyleDim.Render(c[len(typed):]))
		} else {
			shown = append(shown, ui.StyleDim.Render(c))
		}
		width += w + 2
	}
	line := " " + strings.Join(shown, "  ")
	if rest := len(m.completions) - len(shown); rest > 0 {
		line += ui.StyleDim.Render("  +" + strconv.Itoa(rest) + " more")
	}
	return m.clip(line) + "\n"
}

// footer is the hint line under the prompt.
func (m Model) footer() string {
	var line string
	t := m.cur()
	switch {
	case m.copied != "":
		line = ui.StyleGreen.Render("✔ copied") + ui.StyleDim.Render("  ·  "+m.copied+"  ·  shift+drag for the terminal's own selection")
	case m.mode == modeConfirm:
		line = ui.StyleDim.Render("y confirms  ·  anything else cancels")
	case t.running():
		elapsed := time.Since(t.task.started)
		line = ui.StyleYellow.Render(ui.Spinner(elapsed) + " " + t.task.line)
		if stage, _ := t.task.stage.Load().(string); stage != "" {
			line += ui.StyleDim.Render(" · " + stage)
		}
		line += ui.StyleDim.Render(" (" + ui.Elapsed(elapsed) + ")  ·  ctrl+c cancels  ·  other tabs keep working")
	case t.stop != nil:
		line = m.fit(
			ui.StyleDim.Render("following "+t.job+"'s log  ·  pgup scrolls back, end follows again  ·  cancel stops following  ·  alt+w closes"),
			ui.StyleDim.Render("following "+t.job+"  ·  end follows  ·  alt+w closes"),
		)
	case len(m.tabs) > 1:
		line = m.fit(
			ui.StyleDim.Render("tab/shift+tab switch  ·  alt+1…9 select  ·  alt+w close  ·  rerun  ·  ↑↓ history  ·  ctrl+x write  ·  help  ·  ctrl+d exit"),
			ui.StyleDim.Render("tab switch  ·  alt+w close  ·  help  ·  ctrl+d exit"),
			ui.StyleDim.Render("tab switch  ·  ctrl+d exit"),
		)
	default:
		line = m.fit(
			ui.StyleDim.Render("tab complete  ·  ↑↓ history  ·  pgup/pgdn scroll  ·  drag to copy  ·  ctrl+x write mode  ·  help  ·  ctrl+d exit"),
			ui.StyleDim.Render("tab complete  ·  ↑↓ history  ·  help  ·  ctrl+d exit"),
			ui.StyleDim.Render("help  ·  ctrl+d exit"),
		)
	}
	return m.clip(" " + line)
}

func (m Model) fit(options ...string) string {
	for _, o := range options {
		if ansi.StringWidth(o) <= m.width-1 {
			return o
		}
	}
	return options[len(options)-1]
}

// paneHeight is the only slack in the frame.
func (m Model) paneHeight() int {
	h := m.height - len(m.logo()) - m.tabStripHeight() - 5
	if len(m.completions) > 0 {
		h--
	}
	if h < 3 {
		h = 3
	}
	return h
}

func padTo(s string, width int) string {
	if n := ansi.StringWidth(s); n < width {
		return s + strings.Repeat(" ", width-n)
	}
	return s
}

func (m Model) clip(s string) string {
	if ansi.StringWidth(s) <= m.width {
		return s
	}
	return ansi.Truncate(s, m.width, "")
}
