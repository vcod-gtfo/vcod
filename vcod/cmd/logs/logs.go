// Package logscmd is `vcod logs`: the consoles and logs of the servers and clients.
package logscmd

import (
	"os"
	"strconv"

	"vcod/internal/app"
	"vcod/internal/game"
	"vcod/internal/jobs"
	"vcod/internal/registry"
)

const help = `Usage:
  vcod logs [which] [-n 40] [-f]

Which:
  server      the server's console output as vcod captured it (default)
  games       main/games_mp.log in the server flavor: J; K; D; Weapon; lines
  serverlog   main/console_mp_server.log in the server flavor (written by the server)
  client      the client's console output

  -n N        the last N lines (default 40)
  -f          keep following new lines until ctrl+c (in the shell: a tab, until close)

The flavor is the global one: vcod --16x logs games. The job logs live in
~/.local/state/vcod/jobs/ and are truncated each time the job starts again.`

func init() {
	registry.Register(registry.Command{
		Name:        "logs",
		Summary:     "Show or follow a server's or client's console",
		Args:        "[server|games|serverlog|client] [-f]",
		Help:        help,
		Group:       registry.GroupPlay,
		Order:       60,
		NeedsConfig: true,
		Complete:    func([]string) []string { return []string{"server", "games", "serverlog", "client", "-f", "-n"} },
		Run:         run,
	})
}

func run(ctx *app.Context, args []string) int {
	w, err := ctx.Workspace()
	if err != nil {
		return ctx.Fail(err)
	}
	which, n, follow := "server", 40, false
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "-f", "--follow":
			follow = true
		case "-n":
			if i+1 >= len(args) {
				return ctx.Usage("-n needs a number")
			}
			i++
			v, err := strconv.Atoi(args[i])
			if err != nil || v < 1 {
				return ctx.Usage("bad line count %q", args[i])
			}
			n = v
		case "server", "console", "games", "serverlog", "client":
			which = a
		default:
			return ctx.Usage("unknown argument %q  (try: vcod help logs)", a)
		}
	}
	f := ctx.Flavor()
	var path string
	switch which {
	case "client":
		path = jobs.LogPath(game.ClientJob(f))
	case "games":
		path = game.ServerLogs(w, f)["games"]
	case "serverlog":
		path = game.ServerLogs(w, f)["server"]
	default:
		path = game.ServerLogs(w, f)["console"]
	}
	if _, err := os.Stat(path); err != nil {
		ctx.Errorf("no %s log for %s yet: %s", which, f, path)
		return 1
	}
	ctx.Out.Note("%s", path)
	lines, err := jobs.Tail(path, n)
	if err != nil {
		return ctx.Fail(err)
	}
	for _, l := range lines {
		ctx.Out.Plain("%s", l)
	}
	if !follow {
		return 0
	}
	st, err := os.Stat(path)
	if err != nil {
		return ctx.Fail(err)
	}
	jobs.Follow(path, st.Size(), ctx.Ctx.Done(), func(ls []string) {
		for _, l := range ls {
			ctx.Out.Plain("%s", l)
		}
	})
	return 0
}
