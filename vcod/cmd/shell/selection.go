package shell

import (
	"context"
	"encoding/base64"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Selecting text with the mouse.
//
// Mouse reporting is on for the wheel, and the price of that is the terminal's own
// selection: once a program asks for mouse events, dragging over it selects nothing.
// Copying a username or a once-only password out of the transcript is the whole point
// of having it, so the session gives selection back itself, in the form it had before:
// press, drag, let go, and the text is on the clipboard. The selection stays painted
// until the next keystroke or click, so what was copied is visible.
//
// It works on the rendered frame, so anything on screen can be selected, header and
// prompt included, as a run of cells from where the button went down to where it came
// up, wrapping over full rows in between. Trailing blanks and the scrollbar glyph are
// trimmed. shift+drag reaches the terminal's native selection in every terminal tried.

// selection is where the mouse went down, where it is now, and whether it is held.
type selection struct {
	// active is true from press to release.
	active bool
	// shown is true after a release that selected something, until the next input.
	shown bool
	// ax, ay is the anchor; cx, cy the other end. Either may be first in reading order.
	ax, ay int
	cx, cy int
}

// span is the selection in reading order, both ends inclusive.
func (s selection) span() (sy, sx, ey, ex int) {
	if s.ay < s.cy || (s.ay == s.cy && s.ax <= s.cx) {
		return s.ay, s.ax, s.cy, s.cx
	}
	return s.cy, s.cx, s.ay, s.ax
}

// wheelLines is how far one notch of the wheel scrolls.
const wheelLines = 3

// mouse handles a mouse event: the wheel scrolls, the left button selects.
func (m *Model) mouse(msg tea.MouseMsg) tea.Cmd {
	if tea.MouseEvent(msg).IsWheel() {
		if msg.Action != tea.MouseActionPress {
			return nil
		}
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.scrollBy(-wheelLines)
		case tea.MouseButtonWheelDown:
			m.scrollBy(wheelLines)
		}
		return nil
	}

	switch msg.Action {
	case tea.MouseActionPress:
		// Any press clears what was there: a click is how a selection is dismissed in a
		// terminal too.
		m.sel = selection{}
		m.copied = ""
		if msg.Button == tea.MouseButtonLeft {
			m.sel = selection{active: true, ax: msg.X, ay: msg.Y, cx: msg.X, cy: msg.Y}
		}
		return nil

	case tea.MouseActionMotion:
		if m.sel.active {
			m.sel.cx, m.sel.cy = msg.X, msg.Y
		}
		return nil

	case tea.MouseActionRelease:
		if !m.sel.active {
			return nil
		}
		m.sel.cx, m.sel.cy = msg.X, msg.Y
		if m.sel.ax == m.sel.cx && m.sel.ay == m.sel.cy {
			// A click selects nothing; a single character copied by accident would
			// replace what was on the clipboard.
			m.sel = selection{}
			return nil
		}
		text := m.selectedText()
		m.sel.active = false
		if text == "" {
			m.sel = selection{}
			return nil
		}
		m.sel.shown = true
		m.copied = describeCopy(text)
		if m.copy == nil {
			return nil
		}
		return m.copy(text)
	}
	return nil
}

// clearSelection drops the painted selection; every keystroke calls it.
func (m *Model) clearSelection() {
	m.sel = selection{}
	m.copied = ""
}

// selectedText is the text under the selection, one line per row, read off the same
// frame the user is looking at.
func (m Model) selectedText() string {
	if !m.sel.active && !m.sel.shown {
		return ""
	}
	rows := strings.Split(m.render(), "\n")
	sy, sx, ey, ex := m.sel.span()

	var out []string
	for y := sy; y <= ey && y < len(rows); y++ {
		if y < 0 {
			continue
		}
		left, right := rowSpan(y, sy, sx, ey, ex, m.width)
		text := ansi.Strip(ansi.Cut(rows[y], left, right))
		if right >= m.width {
			text = strings.TrimSuffix(strings.TrimSuffix(text, scrollThumb), scrollTrack)
		}
		out = append(out, strings.TrimRight(text, " "))
	}
	for len(out) > 0 && out[0] == "" {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// rowSpan is the cell range [left, right) a selection covers on row y.
func rowSpan(y, sy, sx, ey, ex, width int) (left, right int) {
	left, right = 0, width
	if y == sy {
		left = sx
	}
	if y == ey {
		right = ex + 1
	}
	if left < 0 {
		left = 0
	}
	if right > width {
		right = width
	}
	return left, right
}

// paintSelection draws the selection over a rendered frame in reverse video. The
// selected run is stripped of its own styling first, as a terminal does, so it reads
// as one block.
func (m Model) paintSelection(frame string) string {
	if !m.sel.active && !m.sel.shown {
		return frame
	}
	rows := strings.Split(frame, "\n")
	sy, sx, ey, ex := m.sel.span()
	for y := sy; y <= ey && y < len(rows); y++ {
		if y < 0 {
			continue
		}
		left, right := rowSpan(y, sy, sx, ey, ex, m.width)
		if right <= left {
			continue
		}
		row := rows[y]
		head := ansi.Truncate(row, left, "")
		head += strings.Repeat(" ", max(0, left-ansi.StringWidth(head)))
		mid := ansi.Strip(ansi.Cut(row, left, right))
		mid += strings.Repeat(" ", max(0, right-left-ansi.StringWidth(mid)))
		tail := ansi.TruncateLeft(row, right, "")
		// The reset before the highlight matters: the head may have left dim open, and
		// reverse video on top of dim is a different colour to the row above.
		rows[y] = head + "\x1b[0m" + reverse(mid) + tail
	}
	return strings.Join(rows, "\n")
}

// reverse is s in reverse video, as the raw attribute rather than a lipgloss style: a
// style renders nothing under NO_COLOR, and a selection that cannot be seen without
// colour does not exist for anyone who turned colour off.
func reverse(s string) string {
	return "\x1b[7m" + s + "\x1b[0m"
}

// describeCopy is the receipt shown in the footer.
func describeCopy(text string) string {
	lines := strings.Count(text, "\n") + 1
	if lines == 1 {
		n := ansi.StringWidth(text)
		if n == 1 {
			return "1 character"
		}
		return strconv.Itoa(n) + " characters"
	}
	return strconv.Itoa(lines) + " lines"
}

// ── clipboard ───────────────────────────────────────────────────────────────

// copyToClipboard puts text on the clipboard two ways at once: OSC 52, which is the
// only way that works over ssh and inside tmux, and a clipboard tool on the machine,
// because a terminal that ignores OSC 52 ignores it silently. Both working puts the
// same text there twice, which is invisible.
func copyToClipboard(text string) tea.Cmd {
	return func() tea.Msg {
		writeOSC52(text)
		runClipboardTool(text)
		return nil
	}
}

// writeOSC52 sends the terminal ESC ] 52 ; c ; base64 BEL, as one complete write so it
// cannot land inside a frame.
func writeOSC52(text string) {
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
	_, _ = os.Stdout.WriteString(seq)
}

// runClipboardTool hands the text to the first clipboard program that exists, bounded
// by a timeout so a tool with no display cannot hold a goroutine forever.
func runClipboardTool(text string) {
	for _, tool := range clipboardTools() {
		path, err := exec.LookPath(tool[0])
		if err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		cmd := exec.CommandContext(ctx, path, tool[1:]...)
		cmd.Stdin = strings.NewReader(text)
		err = cmd.Run()
		cancel()
		if err == nil {
			return
		}
	}
}

// clipboardTools is the candidates for this platform. Wayland first: on a Wayland
// session xclip talks to XWayland's clipboard, not the one the desktop pastes from.
func clipboardTools() [][]string {
	switch runtime.GOOS {
	case "darwin":
		return [][]string{{"pbcopy"}}
	case "windows":
		return [][]string{{"clip.exe"}}
	}
	tools := [][]string{
		{"xclip", "-selection", "clipboard", "-in"},
		{"xsel", "--clipboard", "--input"},
		{"clip.exe"}, // WSL
	}
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		tools = append([][]string{{"wl-copy"}}, tools...)
	}
	return tools
}
