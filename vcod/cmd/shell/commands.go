package shell

import (
	"context"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	democmd "vcod/cmd/demo"
	"vcod/internal/config"
	"vcod/internal/game"
	"vcod/internal/jobs"
	"vcod/internal/prompt"
	"vcod/internal/registry"
	"vcod/internal/ui"
)

// builtin is a command of the session itself, beside the registry's.
type builtin struct {
	name, args, summary string
}

var builtins = []builtin{
	{"flavor", "[1.5|1.6x]", "Show or switch the flavor this session works on"},
	{"follow", "[server|client] [flavor]", "Open a tab following a job's console log"},
	{"rerun", "", "Run this tab's command again"},
	{"cancel", "", "Cancel this tab's command (also ctrl+c)"},
	{"tabs", "", "List the open tabs (alt+1…9 select, alt+w closes)"},
	{"close", "", "Close this tab, cancelling its command (also alt+w)"},
	{"write", "", "Unlock write mode (also ctrl+x)"},
	{"safe", "", "Lock write mode again"},
	{"clear", "", "Clear this tab (also ctrl+l)"},
	{"help", "[command]", "This list, or one command's help"},
	{"quit", "", "Leave (also ctrl+d); servers and clients keep running"},
}

var shellAliases = map[string]string{
	"exit": "quit", "q": "quit", "readonly": "safe", "ro": "safe", "again": "rerun",
	"f": "flavor", "tail": "follow",
}

// notInShell are the registry commands that need the terminal to themselves.
var notInShell = map[string]string{
	"shell":      "you are in it",
	"setup":      "it asks its questions on the terminal",
	"purge":      "it asks its questions on the terminal",
	"install":    "run it from a terminal",
	"completion": "it prints a script for your shell rc",
}

func lookupBuiltin(name string) *builtin {
	if a, ok := shellAliases[name]; ok {
		name = a
	}
	for i := range builtins {
		if builtins[i].name == name {
			return &builtins[i]
		}
	}
	return nil
}

// dispatch runs one line.
func (m Model) dispatch(line string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(line)
	if b := lookupBuiltin(fields[0]); b != nil {
		m.say(ui.StyleAccent.Render("❯ ") + ui.StyleWhite.Render(line))
		return m.runBuiltin(b.name, fields[1:])
	}
	cmd := registry.Lookup(fields[0])
	if cmd == nil {
		m.say(ui.StyleAccent.Render("❯ ") + ui.StyleWhite.Render(line))
		m.say(ui.StyleRed.Render("  unknown command: "+fields[0]) + ui.StyleDim.Render("  — help lists them"))
		return m, nil
	}
	if why, ok := notInShell[cmd.Name]; ok {
		m.say(ui.StyleAccent.Render("❯ ") + ui.StyleWhite.Render(line))
		m.say(ui.StyleYellow.Render("  " + cmd.Name + " runs outside the session: " + why))
		return m, nil
	}
	if cmd.Name == "config" && len(fields) > 1 && fields[1] == "edit" {
		m.say(ui.StyleYellow.Render("  config edit opens an editor — run it outside the session"))
		return m, nil
	}
	if cmd.Writes && !m.write {
		m.say(ui.StyleYellow.Render("  safe mode — press ctrl+x, or type write, to unlock " + cmd.Name))
		return m, nil
	}
	return m.startTask(line, fields, false)
}

// longRunning reports whether a line gets a tab of its own: anything that streams, waits
// for a server or runs for more than a moment.
func longRunning(fields []string) bool {
	name := fields[0]
	if c := registry.Lookup(name); c != nil {
		name = c.Name
	}
	args := fields[1:]
	has := func(words ...string) bool {
		for _, a := range args {
			for _, w := range words {
				if a == w {
					return true
				}
			}
		}
		return false
	}
	switch name {
	case "play", "build", "update":
		return true
	case "test":
		return len(args) > 0 && args[0] != "--json" && args[0] != "list" && !has("--usage")
	case "server":
		return len(args) > 0 && (args[0] == "start" || args[0] == "restart")
	case "logs":
		return has("-f", "--follow")
	case "flavors":
		return len(args) > 0
	}
	return false
}

// startTask runs a registry command in the background, in the tab in front or in a new
// one, with its output streaming into that tab. inPlace keeps it in the tab in front
// (rerun).
func (m Model) startTask(line string, fields []string, inPlace bool) (tea.Model, tea.Cmd) {
	var t *tab
	if !inPlace && (longRunning(fields) || m.cur().running() || m.cur().job != "") {
		t = m.openTab(title(line))
		if t == nil {
			return m, nil
		}
	} else {
		t = m.cur()
	}
	m.sayTo(t, ui.StyleAccent.Render("❯ ")+ui.StyleWhite.Render(line))

	ctx, cancel := context.WithCancel(m.app.Ctx)
	tk := &task{line: line, started: time.Now(), cancel: cancel, stage: &atomic.Value{}}
	tk.stage.Store("")
	t.task = tk

	events, id := m.events, t.id
	emit := func(s string) { events <- linesMsg{tabID: id, lines: []string{s}} }
	lw := &ui.LineWriter{Emit: emit}
	sub := *m.app
	cfg := *m.freshConfig()
	cfg.Flavor, cfg.FlavorFromFlag = m.flavor, true
	sub.Cfg = &cfg
	sub.Out = ui.NewQuiet(lw)
	sub.Out.OnBusy = func(label string) { tk.stage.Store(label) }
	sub.Ctx = ctx
	sub.Write = m.write
	sub.JSON = false
	sub.Stream = emit
	sub.Prompt = prompt.NewFrom(strings.NewReader(""), lw)
	cmd := registry.Lookup(fields[0])
	args := fields[1:]
	for _, a := range args {
		if a == "--json" {
			sub.JSON = true
		}
	}

	run := func() tea.Msg {
		started := time.Now()
		code := func() (code int) {
			defer func() {
				if r := recover(); r != nil {
					lw.Flush()
					emit(ui.StyleRed.Render("  ✘ vcod bug: " + strings.TrimSpace(strings.Split(sprint(r), "\n")[0])))
					code = 70
				}
			}()
			if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
				for _, l := range strings.Split(cmd.Help, "\n") {
					emit("  " + l)
				}
				return 0
			}
			return cmd.Run(&sub, args)
		}()
		lw.Flush()
		// A job this command started gets a tab following its log.
		for _, j := range jobs.List() {
			if j.Running() && !j.Started.Before(started) {
				events <- followMsg{job: j.Name, path: j.Log}
			}
		}
		return doneMsg{tabID: id, code: code}
	}
	if !m.ticking {
		m.ticking = true
		return m, tea.Batch(run, tick())
	}
	return m, run
}

func sprint(v any) string {
	if e, ok := v.(error); ok {
		return e.Error()
	}
	if s, ok := v.(string); ok {
		return s
	}
	return "panic"
}

// title shortens a command line for the tab strip.
func title(line string) string {
	if r := []rune(line); len(r) > 22 {
		return string(r[:21]) + "…"
	}
	return line
}

// openFollow opens a tab that follows a job's log, unless one already does. The tab
// opens in the back, so the command that started the job stays in front.
func (m *Model) openFollow(job, path string) {
	for _, t := range m.tabs {
		if t.job == job && t.stop != nil {
			return
		}
	}
	if len(m.tabs) >= maxTabs {
		return
	}
	t := m.newTab("⇣ " + job)
	t.job = job
	t.stop = make(chan struct{})
	t.unread = true
	m.tabs = append(m.tabs, t)
	events, id, stop := m.events, t.id, t.stop
	go func() {
		ended := make(chan struct{})
		go func() {
			defer close(ended)
			for {
				select {
				case <-stop:
					return
				case <-time.After(time.Second):
				}
				if j := jobs.Get(job); j == nil || !j.Running() {
					// One more read happens when Follow sees stop; give the log a moment.
					time.Sleep(300 * time.Millisecond)
					return
				}
			}
		}()
		inner := make(chan struct{})
		go func() {
			select {
			case <-stop:
			case <-ended:
			}
			close(inner)
		}()
		jobs.Follow(path, 0, inner, func(lines []string) {
			events <- linesMsg{tabID: id, lines: lines}
		})
		select {
		case <-stop:
		default:
			events <- linesMsg{tabID: id, lines: []string{"", ui.StyleDim.Render("  ── " + job + " ended ──")}}
		}
	}()
}

// runBuiltin runs one of the session's own commands.
func (m Model) runBuiltin(name string, args []string) (tea.Model, tea.Cmd) {
	switch name {
	case "help":
		if len(args) > 0 {
			if c := registry.Lookup(args[0]); c != nil {
				m.say("  " + ui.StyleWhite.Render(c.Name+" "+c.Args))
				for _, l := range strings.Split(c.Help, "\n") {
					m.say("  " + l)
				}
				return m, nil
			}
		}
		m.say(m.helpText())
	case "quit":
		return m.quit()
	case "clear":
		m.cur().lines = nil
		m.cur().scroll = 0
	case "write":
		if !m.write {
			m.toggleWrite()
		}
	case "safe":
		if m.write {
			m.toggleWrite()
		}
	case "tabs":
		var b strings.Builder
		for i, t := range m.tabs {
			marker := "  "
			if i == m.active {
				marker = ui.StyleAccent.Render("▸ ")
			}
			b.WriteString("  " + marker + ui.StyleWhite.Render(strconv.Itoa(i+1)) + "  " + t.title + "  " + ui.StyleDim.Render(tabState(t)) + "\n")
		}
		m.say(b.String())
	case "close":
		m.closeTab()
	case "cancel":
		if t := m.cur(); t.running() {
			t.task.cancel()
			m.say(ui.StyleYellow.Render("  cancelling " + t.task.line))
		} else if t.stop != nil {
			close(t.stop)
			t.stop = nil
		} else {
			m.say(ui.StyleDim.Render("  nothing is running in this tab"))
		}
	case "rerun":
		t := m.cur()
		if t.task == nil {
			m.say(ui.StyleDim.Render("  nothing to rerun in this tab"))
			return m, nil
		}
		if t.running() {
			m.say(ui.StyleDim.Render("  still running — cancel it first (ctrl+c)"))
			return m, nil
		}
		line := t.task.line
		t.lines, t.scroll, t.follow = nil, 0, true
		t.task = nil
		// Rerun in the same tab: startTask uses the tab in front when it is idle.
		return m.startTask(line, strings.Fields(line), true)
	case "flavor":
		if len(args) == 0 {
			m.say("  flavor " + ui.FlavorBadge(string(m.flavor)) + ui.StyleDim.Render("  · flavor 1.5 | flavor 1.6x switches"))
			return m, nil
		}
		f, err := config.ParseFlavor(args[0])
		if err != nil {
			m.say(ui.StyleRed.Render("  " + err.Error()))
			return m, nil
		}
		m.flavor = f
		m.setPrompt()
		m.say("  now on " + ui.FlavorBadge(string(f)) + ui.StyleDim.Render("  · play, server, client, rcon and logs use it"))
	case "follow":
		kind, f := "server", m.flavor
		for _, a := range args {
			if a == "client" || a == "server" {
				kind = a
			} else if pf, err := config.ParseFlavor(a); err == nil {
				f = pf
			}
		}
		job := game.ServerJob(f)
		if kind == "client" {
			job = game.ClientJob(f)
		}
		path := jobs.LogPath(job)
		if _, err := os.Stat(path); err != nil {
			m.say(ui.StyleDim.Render("  " + job + " has no log yet"))
			return m, nil
		}
		for i, t := range m.tabs {
			if t.job == job && t.stop != nil {
				m.selectTab(i)
				return m, nil
			}
		}
		m.openFollow(job, path)
		m.selectTab(len(m.tabs) - 1)
	}
	return m, nil
}

func tabState(t *tab) string {
	switch {
	case t.running():
		return "running " + ui.Elapsed(time.Since(t.task.started))
	case t.stop != nil:
		return "following " + t.job
	case t.task != nil && t.task.code == 0:
		return "done"
	case t.task != nil:
		return "exit " + strconv.Itoa(t.task.code)
	}
	return ""
}

// helpText is the command list.
func (m Model) helpText() string {
	var b strings.Builder
	b.WriteString("\n")
	byGroup := map[registry.Group][]*registry.Command{}
	for _, name := range registry.Names() {
		c := registry.Lookup(name)
		if _, skip := notInShell[c.Name]; skip {
			continue
		}
		byGroup[c.Group] = append(byGroup[c.Group], c)
	}
	row := func(name, args, summary string) {
		left := name
		if args != "" {
			left += " " + args
		}
		if ui.VisibleWidth(left) > 34 {
			b.WriteString("    " + ui.StyleAccent.Render(left) + "\n" + strings.Repeat(" ", 40) + ui.StyleDim.Render(summary) + "\n")
			return
		}
		b.WriteString("    " + ui.StyleAccent.Render(left) + strings.Repeat(" ", 36-ui.VisibleWidth(left)) + ui.StyleDim.Render(summary) + "\n")
	}
	for _, g := range registry.GroupOrder {
		cs := byGroup[g]
		if len(cs) == 0 {
			continue
		}
		sort.SliceStable(cs, func(i, j int) bool { return cs[i].Order < cs[j].Order })
		b.WriteString("  " + ui.StyleWhite.Render(string(g)) + "\n")
		for _, c := range cs {
			row(c.Name, c.Args, c.Summary)
		}
		b.WriteString("\n")
	}
	b.WriteString("  " + ui.StyleWhite.Render("Session") + "\n")
	for _, bi := range builtins {
		row(bi.name, bi.args, bi.summary)
	}
	b.WriteString("\n  " + ui.StyleDim.Render("help <command> shows one command's help · long commands open a tab of their own"))
	return b.String()
}

// ── completion ──────────────────────────────────────────────────────────────

// candidates are the completions for the word after prev.
func (m Model) candidates(prev []string) []string {
	if len(prev) == 0 {
		var out []string
		for _, b := range builtins {
			out = append(out, b.name)
		}
		for _, n := range registry.Names() {
			if _, skip := notInShell[n]; !skip {
				out = append(out, n)
			}
		}
		sort.Strings(out)
		return out
	}
	name := prev[0]
	if b := lookupBuiltin(name); b != nil {
		switch b.name {
		case "flavor":
			return []string{"1.5", "1.6x"}
		case "follow":
			return []string{"server", "client", "1.5", "1.6x"}
		case "help":
			return registry.Names()
		}
		return nil
	}
	c := registry.Lookup(name)
	if c == nil {
		return nil
	}
	var out []string
	if c.Complete != nil {
		out = append(out, c.Complete(prev[1:])...)
	}
	switch c.Name {
	case "demo":
		if m.cfg != nil {
			out = append(out, democmd.Names(m.cfg.ClientHome(m.flavor))...)
		}
	case "play", "server":
		out = append(out, game.Maps...)
		out = append(out, game.Gametypes...)
	case "test":
		if len(prev) > 1 {
			return []string{"server-1.5", "server-1.6x", "--usage"}
		}
	}
	return out
}

// complete fills in the word at the cursor. One candidate is applied; several are
// listed under the pane and their common prefix is applied.
func (m *Model) complete() {
	value := m.input.Value()
	fields := strings.Fields(value)
	endsInSpace := strings.HasSuffix(value, " ")
	typed := ""
	prev := fields
	if len(fields) > 0 && !endsInSpace {
		typed = fields[len(fields)-1]
		prev = fields[:len(fields)-1]
	}
	seen := map[string]bool{}
	var matches []string
	for _, c := range m.candidates(prev) {
		if strings.HasPrefix(c, typed) && !seen[c] {
			seen[c] = true
			matches = append(matches, c)
		}
	}
	if len(matches) == 0 {
		m.completions = nil
		return
	}
	head := value[:len(value)-len(typed)]
	if len(matches) == 1 {
		m.input.SetValue(head + matches[0] + " ")
		m.completions = nil
	} else {
		m.input.SetValue(head + commonPrefix(matches))
		m.completions = matches
	}
	m.input.CursorEnd()
}

func commonPrefix(words []string) string {
	if len(words) == 0 {
		return ""
	}
	prefix := words[0]
	for _, w := range words[1:] {
		for !strings.HasPrefix(w, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}
