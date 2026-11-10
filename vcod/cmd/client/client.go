// Package clientcmd is `vcod client`: start and stop the port's native client.
package clientcmd

import (
	"strconv"
	"strings"
	"time"

	buildcmd "vcod/cmd/build"
	"vcod/internal/app"
	"vcod/internal/game"
	"vcod/internal/jobs"
	"vcod/internal/q3"
	"vcod/internal/registry"
	"vcod/internal/ui"
	"vcod/internal/ws"
)

const help = `Usage:
  vcod client [start] [host:port]    start cod_client (joins this flavor's local server)
  vcod client stop                   close it
  vcod --16x client                  the 1.6x client (net_ecosystem 1.6x)

Options:
  --menu              do not join anything: stay at the main menu
  --headless          the headless build (build/), no window
  --video             the SDL build (build-video/), the default
  --fullscreen        fullscreen (default from CLIENT_FULLSCREEN, 0)
  --window            windowed
  --mode N            r_mode (default from CLIENT_MODE, -1 = desktop size)
  --build             build the client first when its sources are newer
  -- +set k v        anything after -- goes to the client's command line

The client is cod_rea's cod_client pointed at flavors/client-<flavor> as fs_basepath,
with its own fs_homepath (CLIENT_HOME/home-<flavor>, default
~/.local/share/vcod/home-<flavor>), so its config, screenshots and demos stay out of
the flavor folders. With no address it joins this flavor's local server when one is
answering, and otherwise opens at the main menu.

It runs detached; 'vcod logs client' shows its console. A client older than its
sources gets a warning (or a rebuild with --build).`

func init() {
	registry.Register(registry.Command{
		Name:        "client",
		Summary:     "Start or stop the native Linux client",
		Args:        "[start|stop] [host:port]",
		Help:        help,
		Group:       registry.GroupPlay,
		Order:       30,
		NeedsConfig: true,
		Complete: func([]string) []string {
			return []string{"start", "stop", "--menu", "--headless", "--video", "--fullscreen", "--window", "--mode", "--build"}
		},
		Run: run,
	})
}

func run(ctx *app.Context, args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "start":
			args = args[1:]
		case "stop", "close", "kill":
			return Stop(ctx)
		case "restart":
			if code := Stop(ctx); code != 0 {
				return code
			}
			args = args[1:]
		}
	}
	return Start(ctx, args)
}

// Start starts this flavor's client.
func Start(ctx *app.Context, args []string) int {
	w, err := ctx.Workspace()
	if err != nil {
		return ctx.Fail(err)
	}
	menu, build := false, false
	var rest []string
	for i, a := range args {
		if a == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		switch a {
		case "--menu":
			menu = true
		case "--build":
			build = true
		default:
			rest = append(rest, a)
		}
	}
	f := ctx.Flavor()
	o, err := game.ParseClientArgs(game.DefaultClient(ctx.Cfg, f), rest)
	if err != nil {
		return ctx.Usage("%v  (try: vcod help client)", err)
	}
	if code := ensureBuilt(ctx, w, o.Build, build); code != 0 {
		return code
	}
	if o.Connect == "" && !menu {
		addr := "127.0.0.1:" + strconv.Itoa(ctx.Cfg.Port(f))
		if j := jobs.Get(game.ServerJob(f)); j.Running() && j.Port > 0 {
			addr = "127.0.0.1:" + strconv.Itoa(j.Port)
		}
		if st, err := q3.GetStatus(ctx.Ctx, addr, 500*time.Millisecond); err == nil {
			o.Connect = addr
			ctx.Out.Info("joining the local %s server: %s %s on %s", f, st.Get("mapname"), st.Get("g_gametype"), addr)
		} else {
			ctx.Out.Note("no %s server answers on %s — opening at the main menu (vcod play starts both)", f, addr)
		}
	}
	j, err := game.StartClient(ctx.Cfg, w, o)
	if err != nil {
		return ctx.Fail(err)
	}
	to := "the main menu"
	if o.Connect != "" {
		to = o.Connect
	}
	ctx.Out.OK("client %s started (%s build) → %s  %s", ui.FlavorBadge(string(f)), o.Build.Name, to,
		ui.StyleDim.Render("(pid "+strconv.Itoa(j.PID)+")"))
	ctx.Out.Note("home %s  ·  console: vcod logs client  ·  close: vcod client stop", ctx.Cfg.ClientHome(f))
	// A client that cannot open its window dies within a second; say so now rather than
	// leaving a "started" that never appears.
	time.Sleep(700 * time.Millisecond)
	if !j.Running() {
		ctx.Out.Bad("the client exited right away")
		if lines, _ := jobs.Tail(j.Log, 10); len(lines) > 0 {
			for _, l := range lines {
				ctx.Out.Detail("%s", ui.StyleDim.Render(l))
			}
		}
		return 1
	}
	return 0
}

// ensureBuilt warns about a missing or stale client, or builds it when asked.
func ensureBuilt(ctx *app.Context, w *ws.Workspace, b *ws.Build, build bool) int {
	st := w.State(b)
	if st.Exists && !st.Stale {
		return 0
	}
	if build {
		return buildcmd.Build(ctx, w, []*ws.Build{b}, false)
	}
	if !st.Exists {
		ctx.Errorf("no %s client at %s", b.Name, st.Binary)
		ctx.Hint("build it: vcod build %s   (or add --build)", b.Name)
		return 1
	}
	ctx.Out.Warn("the %s client is older than its sources (%s changed %s after the build) — add --build, or: vcod build %s",
		b.Name, st.NewFile, strings.TrimSuffix(st.Newest.Sub(st.Built).Round(time.Second).String(), "0s"), b.Name)
	return 0
}

// Stop closes this flavor's client.
func Stop(ctx *app.Context) int {
	f := ctx.Flavor()
	stopped, err := game.StopClient(f)
	switch {
	case err != nil:
		return ctx.Fail(err)
	case stopped:
		ctx.Out.OK("client %s closed", ui.FlavorBadge(string(f)))
	default:
		ctx.Out.Note("client %s was not running", f)
	}
	return 0
}
