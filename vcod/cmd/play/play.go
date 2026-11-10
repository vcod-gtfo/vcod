// Package playcmd is `vcod play`: a local server and a client joined to it, in one go.
package playcmd

import (
	"strconv"
	"strings"
	"time"

	clientcmd "vcod/cmd/client"
	servercmd "vcod/cmd/server"
	"vcod/internal/app"
	"vcod/internal/game"
	"vcod/internal/jobs"
	"vcod/internal/q3"
	"vcod/internal/registry"
	"vcod/internal/ui"
)

const help = `Usage:
  vcod play                       this flavor's server on the default map, then a client joined to it
  vcod play mp_carentan sd        another map and gametype
  vcod --16x play --cheats        the 1.6x pair, devmap with sv_cheats 1
  vcod play --headless            join with the headless client instead of the window

The port has no listen server yet (its game module is ROADMAP block 6d), so "try the
game" means a dedicated server in its container plus a client on this machine. play
does both:

  1. the server: started when it is down, and waited for until it answers; when it is
     already up on another map or gametype, switched over rcon (map / devmap)
  2. the client: started and joined to 127.0.0.1:<port>, unless one is already running

Server options: --port N, --cheats, --pure (see vcod help server).
Client options: --headless, --video, --res R, --window, --fullscreen, --borderless,
--maxfps N, --mode N, --build (see vcod help client; vcod display saves the defaults). Both keep running after play returns: vcod stop ends them.`

func init() {
	registry.Register(registry.Command{
		Name:        "play",
		Summary:     "Start a local server and join it with the client",
		Args:        "[map] [gametype] [--cheats]",
		Help:        help,
		Group:       registry.GroupPlay,
		Order:       10,
		NeedsConfig: true,
		Complete: func(prev []string) []string {
			if v := clientcmd.FlagValues(prev); v != nil {
				return v
			}
			return append([]string{"--cheats", "--pure", "--headless", "--res", "--window", "--fullscreen", "--borderless",
				"--maxfps", "--build"}, append(game.Maps, game.Gametypes...)...)
		},
		Run: run,
	})
}

func run(ctx *app.Context, args []string) int {
	w, err := ctx.Workspace()
	if err != nil {
		return ctx.Fail(err)
	}
	var srvArgs, cliArgs []string
	explicit := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case game.SwitchFlags[a] || a == "--build":
			cliArgs = append(cliArgs, a)
		case game.ValueFlags[a]:
			cliArgs = append(cliArgs, a)
			if i+1 < len(args) {
				i++
				cliArgs = append(cliArgs, args[i])
			}
		case strings.HasPrefix(a, "--") && strings.Contains(a, "=") && game.ValueFlags[a[:strings.Index(a, "=")]]:
			cliArgs = append(cliArgs, a)
		default:
			if !strings.HasPrefix(a, "-") {
				explicit = true
			}
			srvArgs = append(srvArgs, a)
		}
	}
	f := ctx.Flavor()
	o, err := game.ParseServerArgs(game.DefaultServer(ctx.Cfg, f), srvArgs)
	if err != nil {
		return ctx.Usage("%v  (try: vcod help play)", err)
	}
	if j := jobs.Get(game.ServerJob(f)); j.Running() && j.Port > 0 {
		o.Port = j.Port
	}

	ctx.Out.Section("play "+string(f), o.Map+" "+o.Gametype+" on "+o.Addr())
	st, _ := q3.GetStatus(ctx.Ctx, o.Addr(), 500*time.Millisecond)
	switch {
	case st == nil:
		j, err := game.StartServer(ctx.Cfg, w, o)
		if err != nil {
			return ctx.Fail(err)
		}
		ctx.Out.OK("server %s started  %s", ui.FlavorBadge(string(f)), ui.StyleDim.Render("(pid "+strconv.Itoa(j.PID)+")"))
		if ok, code := servercmd.Await(ctx, o, j); !ok {
			return code
		}
	case explicit && (st.Get("mapname") != o.Map || st.Get("g_gametype") != o.Gametype):
		cmd := "map"
		if o.Cheats {
			cmd = "devmap"
		}
		ctx.Out.Info("server is up on %s %s — switching to %s %s", st.Get("mapname"), st.Get("g_gametype"), o.Map, o.Gametype)
		if _, err := game.Rcon(ctx.Ctx, ctx.Cfg, f, "g_gametype "+o.Gametype); err != nil {
			return ctx.Fail(err)
		}
		if _, err := game.Rcon(ctx.Ctx, ctx.Cfg, f, cmd+" "+o.Map); err != nil {
			return ctx.Fail(err)
		}
		time.Sleep(1500 * time.Millisecond)
		if _, code := servercmd.Await(ctx, o, jobs.Get(game.ServerJob(f))); code != 0 {
			return code
		}
	default:
		ctx.Out.OK("server %s already up: %s %s, %d player%s", ui.FlavorBadge(string(f)), st.Get("mapname"), st.Get("g_gametype"),
			len(st.Players), plural(len(st.Players)))
	}

	if j := jobs.Get(game.ClientJob(f)); j.Running() {
		ctx.Out.OK("client %s already running (pid %d) — it follows the server's map changes", ui.FlavorBadge(string(f)), j.PID)
		return 0
	}
	return clientcmd.Start(ctx, append(cliArgs, o.Addr()))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
