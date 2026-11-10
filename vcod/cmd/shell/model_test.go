package shell

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"vcod/internal/app"
	"vcod/internal/config"
	"vcod/internal/jobs"

	// The registry fills from init, as main's blank imports do.
	_ "vcod/cmd/build"
	_ "vcod/cmd/config"
	_ "vcod/cmd/flavors"
	_ "vcod/cmd/logs"
	_ "vcod/cmd/play"
	_ "vcod/cmd/rcon"
	_ "vcod/cmd/server"
	_ "vcod/cmd/status"
	_ "vcod/cmd/test"
)

func newTestModel(t *testing.T) Model {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("VCOD_WORKSPACE", "")
	cfg, err := config.LoadFrom(filepath.Join(dir, "config"), "", false)
	if err != nil {
		t.Fatal(err)
	}
	jobs.Dir = filepath.Join(dir, "jobs")
	ctx := app.New(cfg, "test", dir)
	ctx.Ctx = context.Background()
	m := NewModel(ctx)
	m.width, m.height = 120, 40
	m.historyPath = filepath.Join(dir, "history")
	return m
}

func typeLine(t *testing.T, m Model, line string) (Model, tea.Cmd) {
	t.Helper()
	m.input.SetValue(line)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	return next.(Model), cmd
}

func TestUnknownCommand(t *testing.T) {
	m, _ := typeLine(t, newTestModel(t), "frobnicate")
	if !strings.Contains(m.Transcript(), "unknown command: frobnicate") {
		t.Fatal(m.Transcript())
	}
}

func TestFlavorSwitch(t *testing.T) {
	m, _ := typeLine(t, newTestModel(t), "flavor 1.6x")
	if m.flavor != config.F16x {
		t.Fatalf("flavor = %s", m.flavor)
	}
	if !strings.Contains(m.input.Prompt, "1.6x") {
		t.Fatalf("prompt %q does not show the flavor", m.input.Prompt)
	}
	m, _ = typeLine(t, m, "flavor 1.1")
	if m.flavor != config.F16x || !strings.Contains(m.Transcript(), "unknown flavor") {
		t.Fatal("accepted 1.1")
	}
}

func TestQuickCommandRunsInPlaceAndStreams(t *testing.T) {
	m := newTestModel(t)
	m, cmd := typeLine(t, m, "config path")
	if len(m.Tabs()) != 1 {
		t.Fatalf("a quick command opened a tab: %v", m.Tabs())
	}
	if cmd == nil {
		t.Fatal("no command returned")
	}
	// Run the task the way Bubble Tea would, then deliver what it sent.
	done := runBatch(cmd)
	for {
		select {
		case msg := <-m.events:
			next, _ := m.Update(msg)
			m = next.(Model)
			continue
		default:
		}
		break
	}
	next, _ := m.Update(done)
	m = next.(Model)
	if !strings.Contains(m.Transcript(), "config") {
		t.Fatalf("transcript: %s", m.Transcript())
	}
	if m.cur().running() || m.cur().task.code != 0 {
		t.Fatalf("task state %+v", m.cur().task)
	}
}

// runBatch runs a command and returns the doneMsg it produced, unwrapping a batch.
func runBatch(cmd tea.Cmd) tea.Msg {
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		for _, c := range b {
			if c == nil {
				continue
			}
			if r := c(); r != nil {
				if _, ok := r.(doneMsg); ok {
					return r
				}
			}
		}
	}
	return msg
}

func TestLongRunning(t *testing.T) {
	for line, want := range map[string]bool{
		"play":               true,
		"test":               false,
		"test viewtest":      true,
		"test --json":        false,
		"server":             false,
		"server start sd":    true,
		"srv restart":        true,
		"logs":               false,
		"logs -f":            true,
		"build":              true,
		"status":             false,
		"flavors":            false,
		"flavors make":       true,
		"rcon map mp_harbor": false,
	} {
		if got := longRunning(strings.Fields(line)); got != want {
			t.Errorf("longRunning(%q) = %v", line, got)
		}
	}
}

func TestCompletion(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("fla")
	m.complete()
	if len(m.completions) != 2 { // flavor, flavors
		t.Fatalf("completions %v, value %q", m.completions, m.input.Value())
	}
	m.input.SetValue("play mp_car")
	m.complete()
	if m.input.Value() != "play mp_carentan " {
		t.Fatalf("value %q", m.input.Value())
	}
	m.input.SetValue("flavor 1.6")
	m.complete()
	if m.input.Value() != "flavor 1.6x " {
		t.Fatalf("value %q", m.input.Value())
	}
}

func TestTabsAndClose(t *testing.T) {
	m := newTestModel(t)
	m.openTab("one")
	m.openTab("two")
	if m.active != 2 || len(m.Tabs()) != 3 {
		t.Fatalf("tabs %v active %d", m.Tabs(), m.active)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}, Alt: true})
	m = next.(Model)
	if len(m.Tabs()) != 2 || m.active != 1 {
		t.Fatalf("after close: %v active %d", m.Tabs(), m.active)
	}
	for i := 0; i < 20; i++ {
		m.openTab("x")
	}
	if len(m.Tabs()) != maxTabs {
		t.Fatalf("%d tabs", len(m.Tabs()))
	}
}

func TestRenderFitsTheTerminal(t *testing.T) {
	m := newTestModel(t)
	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 14}} {
		m.width, m.height = size[0], size[1]
		m.setPrompt()
		rows := strings.Split(m.render(), "\n")
		if len(rows) != m.height {
			t.Errorf("%dx%d: %d rows", size[0], size[1], len(rows))
		}
	}
}

func TestWriteGate(t *testing.T) {
	m := newTestModel(t)
	if m.write {
		t.Fatal("starts in write mode")
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlX})
	m = next.(Model)
	if !m.write {
		t.Fatal("ctrl+x did not unlock")
	}
}
