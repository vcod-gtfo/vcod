// Package servercmd is `vcod server`: start, stop and inspect the flavor servers.
package servercmd

import (
	"strconv"
	"time"

	"vcod/internal/app"
	"vcod/internal/config"
	"vcod/internal/game"
	"vcod/internal/jobs"
	"vcod/internal/registry"
	"vcod/internal/report"
	"vcod/internal/ui"
)

const help = `Usage:
  vcod server                       both servers: up or down, map, players
  vcod server start [map] [gt]      start this flavor's server in the background
  vcod server stop [all]            stop this flavor's server (all: both)
  vcod server restart [map] [gt]    stop, then start
  vcod --16x server start mp_carentan sd --cheats

Start options:
  --port N        UDP port (default PORT_1_5 28960, PORT_1_6X 28961)
  --cheats        devmap with sv_cheats 1 (setviewpos, noclip, give ...)
  --pure          sv_pure 1
  --public        dedicated 2: heartbeat the master (listed publicly)
  --no-wait       return as soon as it is launched
  -- +set k v     anything after -- goes to the server's command line

The server is the flavor folder (flavors/server-<flavor>) run by cod_rea/server/run.sh
in its i386 podman container (cod_rea_server_1_5 or cod_rea_server_1_6x), LAN only
unless --public. It runs detached: it keeps running after this command, or the shell,
exits. start waits until the server answers a status query (the first start builds the
podman image, which takes a while), then prints its map and address.

Every server is started with the rcon password from the config (RCON_PASSWORD,
generated once), so 'vcod rcon' reaches it. Its console output is in
'vcod logs server'; games_mp.log in 'vcod logs games'.

The 1.6x server lets clients without a 1.6X build string in (ALLOW_UNVERSIONED=1, the
port's cod_client among them); set ALLOW_UNVERSIONED=0 in the config to refuse them.`

func init() {
	registry.Register(registry.Command{
		Name:        "server",
		Summary:     "Start, stop or show the flavor servers",
		Args:        "[start|stop|restart] [map] [gt]",
		Help:        help,
		Group:       registry.GroupPlay,
		Order:       20,
		NeedsConfig: true,
		Complete: func([]string) []string {
			return append([]string{"start", "stop", "restart", "--cheats", "--pure", "--port", "--no-wait"}, append(game.Maps, game.Gametypes...)...)
		},
		Run: run,
	})
}

func run(ctx *app.Context, args []string) int {
	if len(args) == 0 {
		return Show(ctx)
	}
	switch args[0] {
	case "start", "up":
		return Start(ctx, args[1:])
	case "stop", "down", "kill":
		return Stop(ctx, args[1:])
	case "restart":
		if code := Stop(ctx, nil); code != 0 {
			return code
		}
		return Start(ctx, args[1:])
	case "status", "show":
		return Show(ctx)
	}
	return ctx.Usage("unknown server subcommand %q — start, stop, restart (try: vcod help server)", args[0])
}

// Show prints both servers, and the details of each one that answers.
func Show(ctx *app.Context) int {
	servers := game.Servers(ctx.Ctx, ctx.Cfg)
	ctx.Out.Section("servers", "")
	ctx.Out.Listing(report.ServerRows(servers))
	for _, s := range servers {
		if s.Status == nil {
			continue
		}
		ctx.Out.Section("server "+string(s.Flavor), s.Addr)
		ctx.Out.Fields(report.ServerFields(s))
		if len(s.Status.Players) > 0 {
			ctx.Out.Plain("")
			ctx.Out.Listing(report.Players(s.Status))
		}
	}
	ctx.Out.Plain("")
	return 0
}

// Start starts this flavor's server and waits for it to answer.
func Start(ctx *app.Context, args []string) int {
	w, err := ctx.Workspace()
	if err != nil {
		return ctx.Fail(err)
	}
	wait := true
	var rest []string
	for i, a := range args {
		if a == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		if a == "--no-wait" {
			wait = false
			continue
		}
		rest = append(rest, a)
	}
	f := ctx.Flavor()
	o, err := game.ParseServerArgs(game.DefaultServer(ctx.Cfg, f), rest)
	if err != nil {
		return ctx.Usage("%v  (try: vcod help server)", err)
	}
	j, err := game.StartServer(ctx.Cfg, w, o)
	if err != nil {
		return ctx.Fail(err)
	}
	ctx.Out.OK("server %s started: %s %s on %s  %s", ui.FlavorBadge(string(f)), ui.StyleWhite.Render(o.Map), o.Gametype,
		o.Addr(), ui.StyleDim.Render("(pid "+strconv.Itoa(j.PID)+")"))
	if !wait {
		ctx.Out.Note("not waiting · vcod status shows when it answers · console: vcod logs server")
		return 0
	}
	if _, code := Await(ctx, o, j); code != 0 {
		return code
	}
	return 0
}

// Await waits for a just-started server and reports it.
func Await(ctx *app.Context, o game.ServerOpts, j *jobs.Job) (bool, int) {
	timeout := time.Duration(ctx.Cfg.Int(config.KeyReadyTimeoutSec, 180)) * time.Second
	busy := ctx.Out.Busy("waiting for " + o.Addr() + " to answer")
	st, err := game.WaitReady(ctx.Ctx, o.Addr(), j, timeout, nil)
	busy.Stop()
	if err != nil {
		if ctx.Ctx.Err() != nil {
			ctx.Out.Warn("stopped waiting — the server keeps starting in the background (vcod server stop ends it)")
			return false, 130
		}
		ctx.Out.Bad("%v", err)
		if lines, _ := jobs.Tail(j.Log, 12); len(lines) > 0 {
			ctx.Out.Note("last lines of %s:", j.Log)
			for _, l := range lines {
				ctx.Out.Detail("%s", ui.StyleDim.Render(l))
			}
		}
		return false, 1
	}
	ctx.Out.OK("answering on %s: %s %s, protocol %s, %s",
		o.Addr(), st.Get("mapname"), st.Get("g_gametype"), st.Get("protocol"), report.Age(st.RTT))
	ctx.Out.Note("join with: vcod client   ·   console: vcod logs server   ·   rcon: vcod rcon status")
	return true, 0
}

// Stop stops this flavor's server, or both with "all".
func Stop(ctx *app.Context, args []string) int {
	flavors := []config.Flavor{ctx.Flavor()}
	for _, a := range args {
		switch a {
		case "all", "both":
			flavors = config.Flavors
		default:
			f, err := config.ParseFlavor(a)
			if err != nil {
				return ctx.Usage("server stop takes all, 1.5 or 1.6x, not %q", a)
			}
			flavors = []config.Flavor{f}
		}
	}
	failed := 0
	for _, f := range flavors {
		busy := ctx.Out.Busy("stopping server " + string(f))
		stopped, err := game.StopServer(f)
		busy.Stop()
		switch {
		case err != nil:
			failed++
			ctx.Out.Bad("server %s: %v", f, err)
		case stopped:
			ctx.Out.OK("server %s stopped", ui.FlavorBadge(string(f)))
		default:
			ctx.Out.Note("server %s was not running", f)
		}
	}
	if failed > 0 {
		return 1
	}
	return 0
}
