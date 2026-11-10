// Package testcmd is `vcod test`: list and run cod_rea's tests.
package testcmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"vcod/internal/app"
	"vcod/internal/config"
	"vcod/internal/game"
	"vcod/internal/jobs"
	"vcod/internal/registry"
	"vcod/internal/report"
	"vcod/internal/run"
	"vcod/internal/ui"
	"vcod/internal/ws"
)

const help = `Usage:
  vcod test                     list the tests (ctest plus cod_rea/tools/*test*.sh)
  vcod test ctest [-R regex]    the unit and oracle tests in build/
  vcod test <name> [args...]    a live test script, e.g. vcod test viewtest server-1.6x
  vcod test --json              the list as JSON

A live test runs from cod_rea/ as 'sh tools/<name>.sh args...', exactly as its header
says; its output streams here (into a tab of its own in the shell). Its exit code is
the command's. Afterwards the screenshots and contact sheets it wrote under
cod_rea/work/screens are listed (vcod screens open shows them).

Tests that start a flavor server themselves (through run.sh) replace a server vcod
started for that flavor: run.sh removes a container of the same name first. The list
marks them with ⚑, and running one warns when such a server is up.

The list is read from the scripts, so a new tools/xyztest.sh shows up without any
change here; its summary is the first sentence of its header comment.`

func init() {
	registry.Register(registry.Command{
		Name:        "test",
		Summary:     "List or run cod_rea's tests (ctest and the live scripts)",
		Args:        "[name] [args...]",
		Help:        help,
		Group:       registry.GroupTesting,
		Order:       20,
		NeedsConfig: true,
		Complete:    complete,
		Run:         runTest,
	})
}

// complete offers the test names; it needs a workspace, which the completion script is
// generated without, so it falls back to the current directory's.
func complete([]string) []string {
	cfg, err := config.Load("", false)
	if err != nil || !config.IsWorkspace(cfg.Workspace) {
		return []string{"ctest", "--json"}
	}
	var out []string
	for _, t := range ws.New(cfg.Workspace).Tests() {
		out = append(out, t.Name)
	}
	return append(out, "--json")
}

func runTest(ctx *app.Context, args []string) int {
	w, err := ctx.Workspace()
	if err != nil {
		return ctx.Fail(err)
	}
	if len(args) == 0 || args[0] == "--json" || args[0] == "list" {
		if len(args) > 0 && args[0] == "--json" {
			ctx.JSON = true
		}
		return list(ctx, w)
	}
	t := w.LookupTest(args[0])
	if t == nil {
		ctx.Errorf("no test %q", args[0])
		ctx.Hint("vcod test lists them")
		return 1
	}
	return Run(ctx, w, t, args[1:])
}

func list(ctx *app.Context, w *ws.Workspace) int {
	tests := w.Tests()
	if ctx.JSON {
		b, _ := json.MarshalIndent(tests, "", "  ")
		if ctx.Out != ui.Std {
			ctx.Out.Plain("%s", b)
		} else {
			os.Stdout.Write(append(b, '\n'))
		}
		return 0
	}
	ctx.Out.Section("tests", w.Tools())
	var rows [][]string
	for _, t := range tests {
		mark := " "
		if t.StartsServers {
			mark = ui.StyleYellow.Render("⚑")
		}
		usage := t.Usage
		if usage == "" {
			usage = t.Name + ".sh"
		}
		rows = append(rows, []string{mark, ui.StyleAccent.Render(t.Name), ui.StyleDim.Render(t.Summary)})
	}
	ctx.Out.Listing(rows)
	ctx.Out.Plain("")
	ctx.Out.Note("⚑ starts flavor servers itself (replaces one vcod started) · vcod help test")
	ctx.Out.Note("run one: vcod test <name> [args]   ·   usage of each: vcod test <name> --usage")
	return 0
}

// Run runs one test and reports the screenshots it wrote.
func Run(ctx *app.Context, w *ws.Workspace, t *ws.Test, args []string) int {
	if len(args) == 1 && args[0] == "--usage" {
		ctx.Out.Plain("  %s", ui.StyleWhite.Render(orName(t)))
		ctx.Out.Note("%s", t.Summary)
		if t.Script != "" {
			ctx.Out.Note("%s", t.Script)
		}
		return 0
	}
	var argv []string
	switch t.Name {
	case "ctest":
		b := ws.LookupBuild("headless")
		if _, err := os.Stat(filepath.Join(w.BuildDir(b), "CTestTestfile.cmake")); err != nil {
			ctx.Errorf("build/ has no tests yet — vcod build headless")
			return 1
		}
		bin, ok := run.LookPath("ctest")
		if !ok {
			ctx.Errorf("ctest not found (looked on PATH and in ~/.local/bin)")
			return 1
		}
		argv = append([]string{bin, "--test-dir", b.Dir, "--output-on-failure"}, args...)
	default:
		argv = append([]string{"sh", filepath.Join("tools", filepath.Base(t.Script))}, args...)
		if t.StartsServers {
			for _, f := range config.Flavors {
				if j := jobs.Get(game.ServerJob(f)); j.Running() {
					ctx.Out.Warn("server %s is up (vcod started it); %s may replace it with its own", f, t.Name)
				}
			}
		}
	}
	ctx.Out.Section("test "+t.Name, strings.Join(argv[1:], " "))
	start := time.Now()
	code, err := run.Run(ctx.Ctx, run.Spec{Args: argv, Dir: w.CodRea(), Stream: ctx.Stream})
	if err != nil {
		return ctx.Fail(err)
	}
	took := report.Age(time.Since(start))
	ctx.Out.Plain("")
	switch {
	case ctx.Ctx.Err() != nil:
		ctx.Out.Warn("%s interrupted after %s", t.Name, took)
		return 130
	case code == 0:
		ctx.Out.OK("%s passed in %s", t.Name, took)
	default:
		ctx.Out.Bad("%s failed (exit %d) after %s", t.Name, code, took)
	}
	if shots := NewFiles(w.Screens(), start); len(shots) > 0 {
		ctx.Out.Info("new in work/screens:")
		for i, s := range shots {
			if i == 8 {
				ctx.Out.Detail("%s", ui.StyleDim.Render("… and "+itoa(len(shots)-8)+" more"))
				break
			}
			ctx.Out.Detail("%s", s)
		}
		ctx.Out.Note("open the newest: vcod screens open")
	}
	return code
}

// NewFiles lists the files under dir changed since t, newest first, relative to dir.
func NewFiles(dir string, t time.Time) []string {
	type f struct {
		rel string
		mod time.Time
	}
	var found []f
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && info.ModTime().After(t) {
			rel, _ := filepath.Rel(dir, path)
			found = append(found, f{rel, info.ModTime()})
		}
		return nil
	})
	sort.Slice(found, func(i, j int) bool { return found[i].mod.After(found[j].mod) })
	out := make([]string, len(found))
	for i, x := range found {
		out[i] = x.rel
	}
	return out
}

func orName(t *ws.Test) string {
	if t.Usage != "" {
		return t.Usage
	}
	if t.Name == "ctest" {
		return "ctest [-R regex]"
	}
	return t.Name + ".sh"
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}
