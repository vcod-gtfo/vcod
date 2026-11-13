// Package democmd is `vcod demo`: the demos recorded in a flavor's client home, and
// playing one in the client.
package democmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	clientcmd "vcod/cmd/client"
	"vcod/internal/app"
	"vcod/internal/game"
	"vcod/internal/registry"
	"vcod/internal/ui"
)

const help = `Usage:
  vcod demo                     list the demos recorded by this flavor's client
  vcod demo <name>              start the client and play one (tab completes the names)
  vcod demo <name> --headless   any client option works: --res, --window, --maxfps, -- +set …
  vcod demo --names             just the names, one per line (for scripts and completion)
  vcod demo --json              name, file, size, modtime as JSON
  vcod -F 1.6x demo             the 1.6x client's demos

Demos live in the client home: CLIENT_HOME/home-<flavor>/main/demos (default
~/.local/share/vcod/home-<flavor>/main/demos). Record one in game with
'record [name]' and 'stoprecord' in the console, or set 'seta cg_autodemo 1' for the
servers that start and stop recordings at each round. The file format is retail's
(.dm_2), so demos from a Windows install play too: copy them into that folder.

Playing a demo starts the client without joining a server; any key or Escape leaves
it for the main menu. In the console: 'timedemo 1' before 'demo <name>' plays it as
fast as the machine can and prints the fps; 'cl_freezeDemo 1' pauses it.`

func init() {
	registry.Register(registry.Command{
		Name:        "demo",
		Summary:     "List recorded demos, or play one in the client",
		Args:        "[name] [client options]",
		Help:        help,
		Group:       registry.GroupPlay,
		Order:       34,
		NeedsConfig: true,
		Complete: func(prev []string) []string {
			if v := clientcmd.FlagValues(prev); v != nil {
				return v
			}
			return []string{"--names", "--json", "--headless", "--video", "--res", "--window", "--fullscreen",
				"--borderless", "--maxfps", "--gpu", "--build"}
		},
		Dynamic: "demo --names",
		Run:     run,
	})
}

// Names lists the playable demo names of a client home, for the shell's completion.
func Names(home string) []string {
	demos, _ := game.ListDemos(home)
	var out []string
	for _, d := range demos {
		if d.Playable() {
			out = append(out, d.Name)
		}
	}
	return out
}

func run(ctx *app.Context, args []string) int {
	var name string
	names := false
	var client []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			client = append(client, args[i:]...)
			i = len(args)
		case a == "--names" || a == "-1":
			names = true
		case a == "--json":
			ctx.JSON = true
		case strings.HasPrefix(a, "-"):
			client = append(client, a)
			if clientcmd.FlagValues([]string{a}) != nil || a == "--mode" {
				if i+1 < len(args) {
					client = append(client, args[i+1])
					i++
				}
			}
		case name == "":
			name = a
		default:
			return ctx.Usage("demo takes one name, not %q too  (try: vcod help demo)", a)
		}
	}

	f := ctx.Flavor()
	home := ctx.Cfg.ClientHome(f)
	demos, err := game.ListDemos(home)
	if err != nil {
		return ctx.Fail(err)
	}

	if name != "" {
		return play(ctx, demos, name, client)
	}

	switch {
	case names:
		for _, d := range demos {
			if d.Playable() {
				ctx.Out.Plain("%s", d.Name)
			}
		}
		return 0
	case ctx.JSON:
		if demos == nil {
			demos = []game.DemoInfo{}
		}
		b, _ := json.MarshalIndent(demos, "", "  ")
		if ctx.Out != ui.Std {
			ctx.Out.Plain("%s", b)
		} else {
			fmt.Fprintln(os.Stdout, string(b))
		}
		return 0
	}

	ctx.Out.Section("demos", fmt.Sprintf("%d in client-%s", len(demos), f))
	if len(demos) == 0 {
		ctx.Out.Note("none in %s", game.DemosDir(home))
		ctx.Out.Note("record in game: record [name] … stoprecord   ·   or copy .dm_2 files there")
		return 0
	}
	var rows [][]string
	for _, d := range demos {
		n := ui.StyleWhite.Render(d.Name)
		if !d.Playable() {
			n = ui.StyleDim.Render(d.Name + " (not .dm_2)")
		}
		rows = append(rows, []string{n, ui.StyleDim.Render(sizeString(d.Size)),
			ui.StyleDim.Render(d.ModTime.Format("2006-01-02 15:04"))})
	}
	ctx.Out.Listing(rows)
	ctx.Out.Plain("")
	ctx.Out.Note("play one: vcod demo <name>   ·   folder %s", game.DemosDir(home))
	return 0
}

// play starts the client on a demo. The client gets '+demo <name>' and no server to
// join, so it plays the demo from the main menu's state.
func play(ctx *app.Context, demos []game.DemoInfo, name string, client []string) int {
	var found *game.DemoInfo
	for i := range demos {
		if demos[i].Name == name || strings.EqualFold(demos[i].Name+".dm_2", name) {
			found = &demos[i]
			break
		}
	}
	if found == nil {
		ctx.Errorf("no demo %q in %s", name, game.DemosDir(ctx.Cfg.ClientHome(ctx.Flavor())))
		ctx.Hint("list them: vcod demo")
		return 1
	}
	if !found.Playable() {
		ctx.Errorf("%s is not a .dm_2 demo: the client (like retail) only plays .dm_2", found.File)
		return 1
	}
	ctx.Out.Info("playing %s (%s)", found.Name, sizeString(found.Size))

	// options first, then the demo command after --, with whatever -- the user gave
	var opts, extra []string
	for i, a := range client {
		if a == "--" {
			extra = client[i+1:]
			break
		}
		opts = append(opts, a)
	}
	args := append([]string{"--menu"}, opts...)
	args = append(args, "--")
	args = append(args, extra...)
	args = append(args, "+demo", found.Name)
	return clientcmd.Start(ctx, args)
}

func sizeString(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}
