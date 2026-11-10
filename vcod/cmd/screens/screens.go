// Package screenscmd is `vcod screens`: the screenshots and contact sheets the tests
// and the clients wrote.
package screenscmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"vcod/internal/app"
	"vcod/internal/config"
	"vcod/internal/registry"
	"vcod/internal/report"
	"vcod/internal/ui"
)

const help = `Usage:
  vcod screens               the 15 newest images: cod_rea/work/screens and the clients' homes
  vcod screens -n 40         more
  vcod screens open          open the newest in the desktop's image viewer (xdg-open)
  vcod screens open 3        the third newest
  vcod screens open fx       the newest whose name contains fx

Images are .jpg, .jpeg, .png and .tga. The clients' screenshots land in their homes
(CLIENT_HOME/home-<flavor>/main/screenshots), the tests' under cod_rea/work/screens.`

func init() {
	registry.Register(registry.Command{
		Name:        "screens",
		Summary:     "List or open the newest screenshots",
		Args:        "[open [N|text]] [-n N]",
		Help:        help,
		Group:       registry.GroupTesting,
		Order:       30,
		NeedsConfig: true,
		Complete:    func([]string) []string { return []string{"open", "-n"} },
		Run:         run,
	})
}

type shot struct {
	path string
	mod  time.Time
}

func run(ctx *app.Context, args []string) int {
	w, err := ctx.Workspace()
	if err != nil {
		return ctx.Fail(err)
	}
	n, open, pick := 15, false, ""
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "open":
			open = true
		case a == "-n" && i+1 < len(args):
			i++
			v, err := strconv.Atoi(args[i])
			if err != nil || v < 1 {
				return ctx.Usage("bad count %q", args[i])
			}
			n = v
		default:
			pick = a
		}
	}
	roots := []string{w.Screens()}
	for _, f := range config.Flavors {
		roots = append(roots, filepath.Join(ctx.Cfg.ClientHome(f), "main", "screenshots"))
	}
	shots := collect(roots)
	if len(shots) == 0 {
		ctx.Out.Note("no screenshots yet under %s", strings.Join(roots, ", "))
		return 0
	}
	if open {
		s := choose(shots, pick)
		if s == nil {
			ctx.Errorf("no screenshot matches %q", pick)
			return 1
		}
		cmd := exec.Command("xdg-open", s.path)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err != nil {
			return ctx.Fail(err)
		}
		go func() { _ = cmd.Wait() }()
		ctx.Out.OK("opened %s", s.path)
		return 0
	}
	ctx.Out.Section("screenshots", "newest first")
	var rows [][]string
	for i, s := range shots {
		if i == n {
			break
		}
		rows = append(rows, []string{ui.StyleDim.Render(strconv.Itoa(i + 1)), short(w.Root, s.path), ui.StyleDim.Render(report.Since(s.mod))})
	}
	ctx.Out.Listing(rows)
	ctx.Out.Plain("")
	ctx.Out.Note("vcod screens open [N|text] opens one")
	return 0
}

func collect(roots []string) []shot {
	var out []shot
	for _, root := range roots {
		_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".jpg", ".jpeg", ".png", ".tga":
				out = append(out, shot{path, info.ModTime()})
			}
			return nil
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].mod.After(out[j].mod) })
	return out
}

func choose(shots []shot, pick string) *shot {
	if pick == "" {
		return &shots[0]
	}
	if n, err := strconv.Atoi(pick); err == nil {
		if n >= 1 && n <= len(shots) {
			return &shots[n-1]
		}
		return nil
	}
	for i := range shots {
		if strings.Contains(strings.ToLower(filepath.Base(shots[i].path)), strings.ToLower(pick)) {
			return &shots[i]
		}
	}
	return nil
}

func short(root, path string) string {
	if rel, err := filepath.Rel(root, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}
