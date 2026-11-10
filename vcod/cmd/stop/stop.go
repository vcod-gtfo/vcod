// Package stopcmd is `vcod stop`: end every server and client, of both flavors.
package stopcmd

import (
	"vcod/internal/app"
	"vcod/internal/config"
	"vcod/internal/game"
	"vcod/internal/registry"
	"vcod/internal/ui"
)

const help = `Usage:
  vcod stop          stop both servers and both clients
  vcod stop server   only the servers   ·   vcod stop client   only the clients

Servers are stopped whether vcod started them or not (the podman containers by
name), so this also ends a server a test left behind.`

func init() {
	registry.Register(registry.Command{
		Name:        "stop",
		Summary:     "Stop every server and client",
		Args:        "[server|client]",
		Help:        help,
		Group:       registry.GroupPlay,
		Order:       35,
		NeedsConfig: true,
		Complete:    func([]string) []string { return []string{"server", "client"} },
		Run:         run,
	})
}

func run(ctx *app.Context, args []string) int {
	servers, clients := true, true
	for _, a := range args {
		switch a {
		case "server", "servers":
			clients = false
		case "client", "clients":
			servers = false
		default:
			return ctx.Usage("stop takes server or client, not %q", a)
		}
	}
	failed, did := 0, 0
	for _, f := range config.Flavors {
		if clients {
			stopped, err := game.StopClient(f)
			if err != nil {
				failed++
				ctx.Out.Bad("client %s: %v", f, err)
			} else if stopped {
				did++
				ctx.Out.OK("client %s closed", ui.FlavorBadge(string(f)))
			}
		}
		if servers {
			busy := ctx.Out.Busy("stopping server " + string(f))
			stopped, err := game.StopServer(f)
			busy.Stop()
			if err != nil {
				failed++
				ctx.Out.Bad("server %s: %v", f, err)
			} else if stopped {
				did++
				ctx.Out.OK("server %s stopped", ui.FlavorBadge(string(f)))
			}
		}
	}
	if did == 0 && failed == 0 {
		ctx.Out.Note("nothing was running")
	}
	if failed > 0 {
		return 1
	}
	return 0
}
