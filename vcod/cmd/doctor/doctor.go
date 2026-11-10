// Package doctorcmd is `vcod doctor`: check that this machine can run the servers,
// the client and the tests.
package doctorcmd

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"

	"vcod/internal/app"
	"vcod/internal/config"
	"vcod/internal/game"
	"vcod/internal/jobs"
	"vcod/internal/platform"
	"vcod/internal/registry"
	"vcod/internal/run"
	"vcod/internal/ui"
	"vcod/internal/ws"
)

const help = `Usage:
  vcod doctor

Checks, in order: the tools (podman and the cod_rea-server image, cmake, ninja, ctest,
xdg-open), the workspace (cod_rea, run.sh, the four flavors and their cod_lnxded), the
two build trees and whether they are older than the sources, the server ports (free, or
held by a server vcod knows), and the configuration file. Exit code 0 means nothing
failed; warnings do not count.`

func init() {
	registry.Register(registry.Command{
		Name:        "doctor",
		Summary:     "Check tools, workspace, builds, ports and config",
		Help:        help,
		Group:       registry.GroupSetup,
		Order:       40,
		NeedsConfig: true,
		Run:         run1,
	})
}

func run1(ctx *app.Context, args []string) int {
	if len(args) > 0 {
		return ctx.Usage("doctor takes no arguments")
	}
	out, cfg := ctx.Out, ctx.Cfg
	failures := 0
	fail := func(format string, a ...any) {
		failures++
		out.Bad(format, a...)
	}

	out.Banner("doctor")
	out.Step("this machine")
	out.Info("%s (%s), vcod %s", platform.OS(), platform.Arch(), ctx.Version)
	if p, ok := run.LookPath("podman"); ok {
		out.OK("podman at %s", p)
		if err := game.PodmanReady(); err != nil {
			fail("%v", err)
		} else if exec.Command("podman", "image", "exists", "cod_rea-server").Run() == nil {
			out.OK("image cod_rea-server present")
		} else {
			out.Warn("image cod_rea-server not built yet — the first server start builds it (takes a minute)")
		}
	} else {
		fail("podman not found — the servers run in it (%s)", platform.InstallHint("podman"))
	}
	for _, t := range []struct{ bin, why string }{
		{"cmake", "vcod build"}, {"ninja", "vcod build"}, {"ctest", "vcod test ctest"},
		{platform.OpenCommand(), "vcod screens open"},
	} {
		if p, ok := run.LookPath(t.bin); ok {
			out.OK("%s at %s", t.bin, p)
		} else {
			out.Warn("%s not found — %s needs it", t.bin, t.why)
		}
	}

	out.Step("workspace")
	w, err := ctx.Workspace()
	if err != nil {
		fail("%v", err)
		out.Note("vcod config set WORKSPACE ~/personal/codworkspace, or run vcod from inside it")
		return finish(ctx, failures)
	}
	out.OK("%s  %s", w.Root, ui.StyleDim.Render("("+cfg.WorkspaceSource+")"))
	for _, p := range []string{w.RunScript(), w.MakeFlavors()} {
		if _, err := os.Stat(p); err != nil {
			fail("missing %s", p)
		}
	}
	for _, f := range config.Flavors {
		sd, cd := w.ServerDir(string(f)), w.ClientDir(string(f))
		if _, err := os.Stat(filepath.Join(sd, "cod_lnxded")); err != nil {
			fail("flavor server-%s has no cod_lnxded — vcod flavors make", f)
		} else {
			out.OK("server-%s", f)
		}
		if _, err := os.Stat(filepath.Join(cd, "main")); err != nil {
			fail("flavor client-%s missing — vcod flavors make", f)
		} else {
			out.OK("client-%s", f)
		}
	}
	if _, err := os.Stat(w.Tools()); err == nil {
		out.OK("%d tests in cod_rea/tools (plus ctest)", len(w.Tests())-1)
	}

	out.Step("builds")
	for i := range ws.Builds {
		b := &ws.Builds[i]
		s := w.State(b)
		switch {
		case !s.Exists:
			out.Warn("%s: no cod_client yet — vcod build %s", b.Name, b.Name)
		case s.Stale:
			out.Warn("%s: older than %s — vcod build %s", b.Name, s.NewFile, b.Name)
		default:
			out.OK("%s: up to date", b.Name)
		}
	}

	out.Step("ports")
	for _, f := range config.Flavors {
		port := cfg.Port(f)
		j := jobs.Get(game.ServerJob(f))
		c, err := net.ListenPacket("udp", fmt.Sprintf(":%d", port))
		switch {
		case err == nil:
			c.Close()
			out.OK("udp %d free (%s)", port, f)
		case j.Running():
			out.OK("udp %d held by vcod's server-%s", port, f)
		case game.ContainerState(game.Container(f)) == "running":
			out.Warn("udp %d held by %s, started outside vcod (a test?)", port, game.Container(f))
		default:
			fail("udp %d (%s) is taken by something else: %v", port, f, err)
		}
	}

	out.Step("configuration")
	if cfg.HasFile {
		out.OK("%s", cfg.Path)
		if st, err := os.Stat(cfg.Path); err == nil && st.Mode().Perm()&0o077 != 0 {
			out.Warn("%s is readable by others (mode %o); it holds the rcon password", cfg.Path, st.Mode().Perm())
		}
	} else {
		out.Note("no file yet; defaults apply (vcod setup writes one)")
	}
	return finish(ctx, failures)
}

func finish(ctx *app.Context, failures int) int {
	ctx.Out.Plain("")
	if failures > 0 {
		ctx.Out.Bad("%d check(s) failed", failures)
		return 1
	}
	ctx.Out.OK("all good")
	return 0
}
