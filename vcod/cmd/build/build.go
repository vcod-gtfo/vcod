// Package buildcmd is `vcod build`: configure and build cod_rea's CMake trees.
package buildcmd

import (
	"os"
	"path/filepath"
	"time"

	"vcod/internal/app"
	"vcod/internal/registry"
	"vcod/internal/report"
	"vcod/internal/run"
	"vcod/internal/ui"
	"vcod/internal/ws"
)

const help = `Usage:
  vcod build                  both trees: build/ (headless) and build-video/ (window)
  vcod build video            only build-video/: the client you play with
  vcod build headless         only build/: headless client, cod_server, cod_probe, ctests
  vcod build --status         which trees are older than their sources, without building
  vcod --write build --clean video   delete the tree first, then configure and build

A tree with no build.ninja is configured first (cmake -G Ninja, plus
-DCOD_CLIENT_VIDEO=ON for video), then built with ninja. cmake and ninja come from
~/.local/bin when they are not on PATH. --clean deletes the build folder, so it needs
write mode.`

func init() {
	registry.Register(registry.Command{
		Name:        "build",
		Summary:     "Build cod_rea's client and server (cmake + ninja)",
		Args:        "[headless|video] [--clean]",
		Help:        help,
		Group:       registry.GroupTesting,
		Order:       10,
		NeedsConfig: true,
		Complete:    func([]string) []string { return []string{"headless", "video", "--clean", "--status"} },
		Run:         runBuild,
	})
}

func runBuild(ctx *app.Context, args []string) int {
	w, err := ctx.Workspace()
	if err != nil {
		return ctx.Fail(err)
	}
	clean, status := false, false
	var builds []*ws.Build
	for _, a := range args {
		switch a {
		case "--clean":
			clean = true
		case "--status", "-s":
			status = true
		case "all", "both":
			builds = nil
		default:
			b := ws.LookupBuild(a)
			if b == nil {
				return ctx.Usage("unknown build %q — headless or video", a)
			}
			builds = append(builds, b)
		}
	}
	if status {
		ctx.Out.Section("builds", w.CodRea())
		ctx.Out.Listing(report.BuildRows(w))
		ctx.Out.Plain("")
		return 0
	}
	if len(builds) == 0 {
		for i := range ws.Builds {
			builds = append(builds, &ws.Builds[i])
		}
	}
	if clean && !ctx.Write {
		ctx.Errorf("%s", app.ErrReadOnly.Error())
		ctx.Hint("vcod --write build --clean ...")
		return 2
	}
	return Build(ctx, w, builds, clean)
}

// Build configures (when needed) and builds each tree, stopping at the first failure.
func Build(ctx *app.Context, w *ws.Workspace, builds []*ws.Build, clean bool) int {
	cmake, ok := run.LookPath("cmake")
	if !ok {
		ctx.Errorf("cmake not found (looked on PATH and in ~/.local/bin)")
		return 1
	}
	ninja, ok := run.LookPath("ninja")
	if !ok {
		ctx.Errorf("ninja not found (looked on PATH and in ~/.local/bin)")
		return 1
	}
	for _, b := range builds {
		dir := w.BuildDir(b)
		start := time.Now()
		ctx.Out.Section("build "+b.Name, b.Dir)
		if clean {
			if err := os.RemoveAll(dir); err != nil {
				return ctx.Fail(err)
			}
			ctx.Out.OK("deleted %s", dir)
		}
		if _, err := os.Stat(filepath.Join(dir, "build.ninja")); err != nil {
			args := append([]string{cmake, "-S", ".", "-B", b.Dir, "-G", "Ninja"}, b.Flags...)
			ctx.Out.Info("configuring: %s", ui.StyleDim.Render(join(args[1:])))
			if code := child(ctx, w.CodRea(), args); code != 0 {
				ctx.Out.Bad("cmake failed (exit %d)", code)
				return code
			}
		}
		if code := child(ctx, w.CodRea(), []string{ninja, "-C", b.Dir}); code != 0 {
			if ctx.Ctx.Err() != nil {
				return 130
			}
			ctx.Out.Bad("ninja failed in %s (exit %d)", b.Dir, code)
			return code
		}
		ctx.Out.OK("%s built in %s", b.Name, report.Age(time.Since(start)))
	}
	return 0
}

func child(ctx *app.Context, dir string, args []string) int {
	code, err := run.Run(ctx.Ctx, run.Spec{Args: args, Dir: dir, Stream: ctx.Stream})
	if err != nil {
		ctx.Errorf("%v", err)
		return 1
	}
	return code
}

func join(a []string) string {
	s := ""
	for i, x := range a {
		if i > 0 {
			s += " "
		}
		s += x
	}
	return s
}
