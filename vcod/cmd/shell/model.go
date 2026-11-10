package shell

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"vcod/internal/app"
	"vcod/internal/config"
	"vcod/internal/game"
	"vcod/internal/ui"
)

// mode is what the prompt line is asking for.
type mode int

const (
	modeCommand mode = iota
	modeConfirm
)

// ask is a y/n question the prompt is holding.
type ask struct {
	question string
	yes      func(m *Model) tea.Cmd
	cancel   string
}

// Messages from the background. Every one is stamped with the tab's id rather than its
// position, so output in flight lands in the right tab after a close or a switch.
type (
	// linesMsg is output for one tab.
	linesMsg struct {
		tabID int
		lines []string
	}
	// doneMsg ends a tab's task.
	doneMsg struct {
		tabID int
		code  int
	}
	// followMsg asks for a log-follow tab on a job that has just started.
	followMsg struct {
		job  string
		path string
	}
	// serversMsg is the background refresh of the header's server line.
	serversMsg struct{ servers []game.Server }
	tickMsg    struct{}
	pollMsg    struct{}
)

const (
	spinEvery = 100 * time.Millisecond
	pollEvery = 3 * time.Second
	// maxTabs is nine, because alt+1 through alt+9 select them.
	maxTabs = 9
	// maxLines caps a tab's transcript; a server console followed for hours would
	// otherwise grow without end.
	maxLines = 5000
	// maxHistory is how many lines the history file keeps.
	maxHistory = 500
)

func tick() tea.Cmd { return tea.Tick(spinEvery, func(time.Time) tea.Msg { return tickMsg{} }) }
func poll() tea.Cmd { return tea.Tick(pollEvery, func(time.Time) tea.Msg { return pollMsg{} }) }

// task is a command running in a tab.
type task struct {
	line    string
	started time.Time
	cancel  context.CancelFunc
	stage   *atomic.Value
	// done and code are set when it ends.
	done bool
	code int
}

// tab is one transcript.
type tab struct {
	id     int
	title  string
	lines  []string
	scroll int
	follow bool
	// task is the command this tab runs or ran; rerun starts its line again.
	task *task
	// job is set on a tab following a job's log; stop ends the follower.
	job  string
	stop chan struct{}
	// unread marks output that arrived while the tab was in the back.
	unread bool
}

func (t *tab) running() bool { return t.task != nil && !t.task.done }

// Model is the whole session.
type Model struct {
	app     *app.Context
	cfg     *config.Config
	version string
	flavor  config.Flavor
	write   bool

	width  int
	height int

	tabs      []*tab
	active    int
	nextTabID *int

	input textinput.Model
	mode  mode
	ask   *ask

	history     []string
	histIdx     int
	draft       string
	historyPath string
	completions []string

	// events carries every background message; listen re-arms after each one.
	events chan tea.Msg
	// servers is the last header refresh; polling marks one in flight.
	servers []game.Server
	polling bool
	ticking bool

	quitting bool

	sel    selection
	copied string
	copy   func(text string) tea.Cmd
}

// freshConfig re-reads the settings file, so a command sees what an earlier one saved.
// It keeps the loaded configuration when the file cannot be read.
func (m Model) freshConfig() *config.Config {
	c, err := config.LoadFrom(m.cfg.Path, string(m.cfg.Flavor), m.cfg.FlavorFromFlag)
	if err != nil {
		return m.cfg
	}
	return c
}

// NewModel builds the session over an app context.
func NewModel(ctx *app.Context) Model {
	in := textinput.New()
	in.CharLimit = 400
	in.Focus()
	next := 0
	m := Model{
		app:         ctx,
		cfg:         ctx.Cfg,
		version:     ctx.Version,
		flavor:      ctx.Flavor(),
		write:       ctx.Write,
		width:       100,
		height:      30,
		input:       in,
		nextTabID:   &next,
		historyPath: filepath.Join(filepath.Dir(ctx.Cfg.Path), "history"),
		copy:        copyToClipboard,
		events:      make(chan tea.Msg, 256),
	}
	m.tabs = []*tab{m.newTab("session")}
	m.setPrompt()
	m.history = loadHistory(m.historyPath)
	m.histIdx = len(m.history)
	m.say(m.welcome())
	return m
}

func (m Model) welcome() string {
	if _, err := m.app.Workspace(); err != nil {
		return strings.Join([]string{
			"",
			"  " + ui.StyleYellow.Render("▲ "+err.Error()),
			"  " + ui.StyleDim.Render("config set WORKSPACE <folder>, or start vcod from inside the workspace"),
			"",
		}, "\n")
	}
	return strings.Join([]string{
		"",
		"  " + ui.StyleAccent.Render("play") + ui.StyleDim.Render("            server + client, each log in a tab") +
			"     " + ui.StyleAccent.Render("status") + ui.StyleDim.Render("   what is up"),
		"  " + ui.StyleAccent.Render("flavor 1.6x") + ui.StyleDim.Render("     switch flavor") +
			"                       " + ui.StyleAccent.Render("test") + ui.StyleDim.Render("     the live tests"),
		"  " + ui.StyleAccent.Render("stop") + ui.StyleDim.Render("            stop servers and clients") +
			"                " + ui.StyleAccent.Render("help") + ui.StyleDim.Render("     everything else"),
		"",
	}, "\n")
}

// Init starts the cursor blink, the event listener and the header's first refresh.
func (m Model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.listen(), m.refresh())
}

// listen waits for the next background message, then drains whatever else is queued
// for the same tab, so a fast build costs one frame per batch rather than per line.
func (m Model) listen() tea.Cmd {
	ch := m.events
	return func() tea.Msg {
		msg := <-ch
		lm, ok := msg.(linesMsg)
		if !ok {
			return msg
		}
		for len(lm.lines) < 400 {
			select {
			case next := <-ch:
				if nl, ok := next.(linesMsg); ok && nl.tabID == lm.tabID {
					lm.lines = append(lm.lines, nl.lines...)
					continue
				}
				// Something else: put it back in front of the queue by re-sending.
				go func() { ch <- next }()
				return lm
			default:
				return lm
			}
		}
		return lm
	}
}

// refresh queries both servers in the background for the header.
func (m Model) refresh() tea.Cmd {
	if m.app.WS == nil {
		return nil
	}
	cfg, ctx := m.cfg, m.app.Ctx
	return func() tea.Msg {
		c, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return serversMsg{game.Servers(c, cfg)}
	}
}

// Update is the Bubble Tea state transition.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.setPrompt()
		for _, t := range m.tabs {
			m.clampTab(t)
		}
		return m, nil
	case tickMsg:
		if !m.anyRunning() {
			m.ticking = false
			return m, nil
		}
		return m, tick()
	case pollMsg:
		return m, m.refresh()
	case serversMsg:
		m.servers = msg.servers
		return m, poll()
	case linesMsg:
		if t := m.tabByID(msg.tabID); t != nil {
			m.appendLines(t, msg.lines)
		}
		return m, m.listen()
	case doneMsg:
		// The command may have changed the settings (display, config set, setup).
		m.cfg = m.freshConfig()
		cmds := []tea.Cmd{m.listen(), m.refresh()}
		if t := m.tabByID(msg.tabID); t != nil && t.task != nil {
			t.task.done, t.task.code = true, msg.code
			if t.task.cancel != nil {
				t.task.cancel()
			}
			if m.cur() != t {
				t.unread = true
			}
		}
		return m, tea.Batch(cmds...)
	case followMsg:
		m.openFollow(msg.job, msg.path)
		return m, m.listen()
	case tea.MouseMsg:
		cmd := m.mouse(msg)
		return m, cmd
	case tea.KeyMsg:
		m.clearSelection()
		return m.key(msg)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// View renders the frame with the mouse selection painted over it.
func (m Model) View() string { return m.paintSelection(m.render()) }

// Transcript is the transcript of the tab in front, for tests.
func (m Model) Transcript() string { return strings.Join(m.cur().lines, "\n") }

// Tabs lists the tab titles in order, for tests.
func (m Model) Tabs() []string {
	out := make([]string, 0, len(m.tabs))
	for _, t := range m.tabs {
		out = append(out, t.title)
	}
	return out
}

func (m Model) anyRunning() bool {
	for _, t := range m.tabs {
		if t.running() {
			return true
		}
	}
	return false
}

// shutdown cancels every task and follower; the jobs themselves keep running.
func (m Model) shutdown() {
	for _, t := range m.tabs {
		m.endTab(t)
	}
}

func (m Model) endTab(t *tab) {
	if t.task != nil && t.task.cancel != nil {
		t.task.cancel()
	}
	if t.stop != nil {
		close(t.stop)
		t.stop = nil
	}
}

// key routes a keystroke.
func (m Model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeCommand && m.tabKey(msg) {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyCtrlD:
		return m.quit()
	case tea.KeyCtrlC:
		if m.mode != modeCommand {
			m.cancelAsk()
			return m, nil
		}
		if m.input.Value() != "" {
			m.input.SetValue("")
			m.completions = nil
			return m, nil
		}
		if t := m.cur(); t.running() {
			t.task.cancel()
			m.say(ui.StyleYellow.Render("  ^C cancelling " + t.task.line))
			return m, nil
		}
		return m.quit()
	case tea.KeyCtrlX:
		m.toggleWrite()
		return m, nil
	case tea.KeyCtrlL:
		m.cur().lines = nil
		m.cur().scroll = 0
		return m, nil
	case tea.KeyPgUp:
		m.scrollBy(-m.paneHeight())
		return m, nil
	case tea.KeyPgDown:
		m.scrollBy(m.paneHeight())
		return m, nil
	case tea.KeyEnd:
		if m.input.Value() == "" {
			t := m.cur()
			t.follow = true
			m.clampTab(t)
			return m, nil
		}
	}
	if m.mode != modeCommand {
		return m.keyAsk(msg)
	}
	switch msg.Type {
	case tea.KeyEnter:
		return m.submit()
	case tea.KeyUp:
		m.historyMove(-1)
		return m, nil
	case tea.KeyDown:
		m.historyMove(1)
		return m, nil
	case tea.KeyTab:
		m.complete()
		return m, nil
	case tea.KeyEsc:
		m.completions = nil
		m.input.SetValue("")
		return m, nil
	}
	m.completions = nil
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// quit leaves, asking first when commands are still running. Jobs survive either way.
func (m Model) quit() (tea.Model, tea.Cmd) {
	n := 0
	for _, t := range m.tabs {
		if t.running() {
			n++
		}
	}
	if n == 0 || m.mode == modeConfirm {
		m.quitting = true
		return m, tea.Quit
	}
	m.ask = &ask{
		question: strconv.Itoa(n) + " command(s) still running; cancel them and leave?",
		yes: func(m *Model) tea.Cmd {
			m.quitting = true
			return tea.Quit
		},
		cancel: "staying",
	}
	m.mode = modeConfirm
	m.input.SetValue("")
	m.setPrompt()
	return m, nil
}

func (m Model) keyAsk(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.cancelAsk()
		return m, nil
	case tea.KeyEnter:
		answer := strings.TrimSpace(m.input.Value())
		a := m.ask
		m.ask = nil
		m.mode = modeCommand
		m.input.SetValue("")
		m.setPrompt()
		if strings.EqualFold(answer, "y") || strings.EqualFold(answer, "yes") {
			cmd := a.yes(&m)
			return m, cmd
		}
		m.say(ui.StyleDim.Render("  " + a.cancel))
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) cancelAsk() {
	if m.ask != nil {
		m.say(ui.StyleDim.Render("  " + m.ask.cancel))
	}
	m.ask = nil
	m.mode = modeCommand
	m.input.SetValue("")
	m.setPrompt()
}

// submit runs the line at the prompt.
func (m Model) submit() (tea.Model, tea.Cmd) {
	line := strings.TrimSpace(m.input.Value())
	m.input.SetValue("")
	m.completions = nil
	if line == "" {
		return m, nil
	}
	m.remember(line)
	return m.dispatch(line)
}

// say appends text to the tab in front.
func (m *Model) say(text string) { m.sayTo(m.cur(), text) }

func (m *Model) sayTo(t *tab, text string) {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		m.appendLines(t, []string{""})
		return
	}
	m.appendLines(t, strings.Split(text, "\n"))
}

func (m *Model) appendLines(t *tab, lines []string) {
	for _, l := range lines {
		// Tabs and stray carriage returns would break the frame's column arithmetic.
		l = strings.ReplaceAll(l, "\t", "    ")
		l = strings.ReplaceAll(l, "\r", "")
		t.lines = append(t.lines, l)
	}
	if over := len(t.lines) - maxLines; over > 0 {
		t.lines = append([]string(nil), t.lines[over:]...)
		t.scroll -= over
	}
	if m.cur() != t && len(lines) > 0 {
		t.unread = true
	}
	m.clampTab(t)
}

// ── tabs ────────────────────────────────────────────────────────────────────

func (m *Model) newTab(title string) *tab {
	*m.nextTabID++
	return &tab{id: *m.nextTabID, title: title, follow: true}
}

func (m Model) cur() *tab { return m.tabs[m.active] }

func (m Model) tabByID(id int) *tab {
	for _, t := range m.tabs {
		if t.id == id {
			return t
		}
	}
	return nil
}

// openTab opens a tab and brings it to the front, or explains the limit and returns nil.
func (m *Model) openTab(title string) *tab {
	if len(m.tabs) >= maxTabs {
		m.say(ui.StyleYellow.Render("  " + strconv.Itoa(maxTabs) + " tabs is the limit — close one with alt+w or the close command"))
		return nil
	}
	t := m.newTab(title)
	m.tabs = append(m.tabs, t)
	m.active = len(m.tabs) - 1
	return t
}

// closeTab closes the tab in front, cancelling what it runs. The last one stays.
func (m *Model) closeTab() {
	if len(m.tabs) == 1 {
		m.say(ui.StyleDim.Render("  this is the last tab — quit or ctrl+d leaves the session"))
		return
	}
	m.endTab(m.cur())
	m.tabs = append(m.tabs[:m.active], m.tabs[m.active+1:]...)
	if m.active >= len(m.tabs) {
		m.active = len(m.tabs) - 1
	}
}

func (m *Model) selectTab(n int) {
	if n >= 0 && n < len(m.tabs) {
		m.active = n
		m.cur().unread = false
	}
}

func (m *Model) cycleTab(delta int) {
	n := len(m.tabs)
	m.selectTab(((m.active+delta)%n + n) % n)
}

// tabKey handles the tab bindings, reporting whether it consumed the key.
func (m *Model) tabKey(msg tea.KeyMsg) bool {
	if msg.Type == tea.KeyShiftTab && len(m.tabs) > 1 {
		m.cycleTab(-1)
		return true
	}
	if !msg.Alt && msg.Type == tea.KeyTab && m.input.Value() == "" && len(m.tabs) > 1 {
		m.cycleTab(1)
		return true
	}
	if !msg.Alt {
		return false
	}
	switch msg.Type {
	case tea.KeyLeft:
		m.cycleTab(-1)
		return true
	case tea.KeyRight:
		m.cycleTab(1)
		return true
	case tea.KeyRunes:
	default:
		return false
	}
	if len(msg.Runes) != 1 {
		return false
	}
	switch r := msg.Runes[0]; {
	case r >= '1' && r <= '9':
		m.selectTab(int(r - '1'))
		return true
	case r == 't':
		m.openTab("scratch")
		return true
	case r == 'w':
		m.closeTab()
		return true
	}
	return false
}

func (m *Model) toggleWrite() {
	m.write = !m.write
	if m.write {
		m.say(ui.StyleRed.Render("  ✎ write mode on — flavors rebuild and build --clean are live; ctrl+x locks again"))
		return
	}
	m.say(ui.StyleGreen.Render("  🔒 safe mode again"))
}

// setPrompt sets the prompt text for the current mode, and the input width so a long
// line scrolls inside the row instead of wrapping the frame.
func (m *Model) setPrompt() {
	m.input.Placeholder = ""
	switch m.mode {
	case modeCommand:
		m.input.Prompt = ui.StyleDim.Render(string(m.flavor)+" ") + ui.StyleAccent.Render("❯ ")
	case modeConfirm:
		m.input.Prompt = ui.StyleWhite.Render(m.ask.question) + ui.StyleDim.Render(" [y/N] ")
	}
	w := m.width - 2 - ui.VisibleWidth(m.input.Prompt) - 1
	if w < 10 {
		w = 10
	}
	m.input.Width = w
}

// ── scrolling ───────────────────────────────────────────────────────────────

func (m Model) maxScroll() int { return m.maxScrollOf(m.cur()) }

func (m Model) maxScrollOf(t *tab) int {
	n := len(t.lines) - m.paneHeight()
	if n < 0 {
		return 0
	}
	return n
}

func (m *Model) scrollBy(delta int) {
	t := m.cur()
	t.follow = false
	t.scroll += delta
	m.clampTab(t)
	t.follow = t.scroll >= m.maxScroll()
}

func (m *Model) clampScroll() { m.clampTab(m.cur()) }

func (m *Model) clampTab(t *tab) {
	if t.follow {
		t.scroll = m.maxScrollOf(t)
	}
	if t.scroll > m.maxScrollOf(t) {
		t.scroll = m.maxScrollOf(t)
	}
	if t.scroll < 0 {
		t.scroll = 0
	}
}

// ── history ─────────────────────────────────────────────────────────────────

func (m *Model) remember(line string) {
	if n := len(m.history); n > 0 && m.history[n-1] == line {
		m.histIdx = len(m.history)
		return
	}
	m.history = append(m.history, line)
	if len(m.history) > maxHistory {
		m.history = m.history[len(m.history)-maxHistory:]
	}
	m.histIdx = len(m.history)
	m.draft = ""
	saveHistory(m.historyPath, m.history)
}

func (m *Model) historyMove(delta int) {
	if len(m.history) == 0 {
		return
	}
	if m.histIdx == len(m.history) {
		m.draft = m.input.Value()
	}
	next := m.histIdx + delta
	if next < 0 {
		next = 0
	}
	if next > len(m.history) {
		next = len(m.history)
	}
	m.histIdx = next
	if next == len(m.history) {
		m.input.SetValue(m.draft)
	} else {
		m.input.SetValue(m.history[next])
	}
	m.input.CursorEnd()
}

func loadHistory(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			out = append(out, line)
		}
	}
	if len(out) > maxHistory {
		out = out[len(out)-maxHistory:]
	}
	return out
}

func saveHistory(path string, lines []string) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
}
