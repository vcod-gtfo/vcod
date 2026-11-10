// Package rconcmd is `vcod rcon`: run a console command on a local server.
package rconcmd

import (
	"strings"

	"vcod/internal/app"
	"vcod/internal/game"
	"vcod/internal/registry"
)

const help = `Usage:
  vcod rcon status               the server's player list
  vcod rcon map mp_carentan      change the map
  vcod rcon g_gametype sd; vcod rcon map_restart
  vcod --16x rcon serverinfo

Sends the command to this flavor's server on 127.0.0.1 with the rcon password every
vcod server is started with (RCON_PASSWORD in the config). A server started some
other way (a test, run.sh by hand) has another password or none, and refuses.`

func init() {
	registry.Register(registry.Command{
		Name:        "rcon",
		Summary:     "Run a console command on this flavor's server",
		Args:        "<command...>",
		Help:        help,
		Group:       registry.GroupPlay,
		Order:       50,
		NeedsConfig: true,
		Complete: func([]string) []string {
			return []string{"status", "serverinfo", "map", "devmap", "map_restart", "fast_restart", "map_rotate", "g_gametype", "kick", "clientkick", "say", "dumpuser"}
		},
		Run: run,
	})
}

func run(ctx *app.Context, args []string) int {
	if len(args) == 0 {
		return ctx.Usage("usage: vcod rcon <command...>  (try: vcod rcon status)")
	}
	out, err := game.Rcon(ctx.Ctx, ctx.Cfg, ctx.Flavor(), strings.Join(args, " "))
	if err != nil {
		ctx.Errorf("%v", err)
		ctx.Hint("is the %s server up? vcod status", ctx.Flavor())
		return 1
	}
	out = strings.TrimRight(out, "\n")
	if out == "" {
		ctx.Out.Note("(no output)")
		return 0
	}
	for _, line := range strings.Split(out, "\n") {
		ctx.Out.Plain("  %s", line)
	}
	return 0
}
