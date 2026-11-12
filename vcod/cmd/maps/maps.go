// Package mapscmd is `vcod maps`: the maps a flavor's install can load, with the
// gametypes each one supports.
package mapscmd

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vcod/internal/app"
	"vcod/internal/game"
	"vcod/internal/registry"
	"vcod/internal/ui"
)

const help = `Usage:
  vcod maps                    the maps this flavor's server can load
  vcod maps sd                 only those whose .arena lists a gametype (dm tdm sd re bel hq)
  vcod maps --client           what the client install has instead of the server's
  vcod maps --names            just the names, one per line (for scripts)
  vcod maps --json             name, longname, gametypes, source pk3, stock, as JSON
  vcod -F 1.6x maps            the 1.6x flavor (default: the configured flavor)

A map is any maps/mp/<name>.bsp in the install's main/ folder, in a pk3 or loose. The
gametypes come from its mp/*.arena entry, the file the game's own map menu reads; a map
without one is listed with none, and the server still starts it, but a gametype the
map has no spawnpoints for fails at load. pk3s are read in the engine's order
(alphabetical, loose files last), so "from" names the pk3 whose copy the game uses.

Stock maps are listed first, then custom ones. Any listed name works with
'vcod server start <map> [gt]', 'vcod play <map> [gt]' and 'vcod rcon map <map>'.
To add a map, drop its pk3 into flavors/server-<flavor>/main (and the client's, or let
the client download it with sv_allowDownload).`

func init() {
	registry.Register(registry.Command{
		Name:        "maps",
		Summary:     "List the maps a flavor can load, with gametypes",
		Args:        "[gametype] [--client] [--names|--json]",
		Help:        help,
		Group:       registry.GroupPlay,
		Order:       36,
		NeedsConfig: true,
		Complete: func([]string) []string {
			return append([]string{"--client", "--names", "--json"}, game.Gametypes...)
		},
		Run: run,
	})
}

func run(ctx *app.Context, args []string) int {
	var gt string
	client, names := false, false
	for _, a := range args {
		switch {
		case a == "--client":
			client = true
		case a == "--server":
			client = false
		case a == "--names" || a == "-1":
			names = true
		case a == "--json":
			ctx.JSON = true
		case strings.HasPrefix(a, "-"):
			return ctx.Usage("unknown flag %s  (try: vcod help maps)", a)
		case gt == "":
			gt = strings.ToLower(a)
		default:
			return ctx.Usage("maps takes one gametype, not %q too  (try: vcod help maps)", a)
		}
	}
	w, err := ctx.Workspace()
	if err != nil {
		return ctx.Fail(err)
	}
	f := ctx.Flavor()
	side, dir := "server", w.ServerDir(string(f))
	if client {
		side, dir = "client", w.ClientDir(string(f))
	}
	main := filepath.Join(dir, "main")
	maps, err := game.ListMaps(main)
	if err != nil {
		ctx.Errorf("no %s install for %s: %v", side, f, err)
		ctx.Hint("fetch it: vcod get %s %s", side, f)
		return 1
	}
	if gt != "" {
		var keep []game.MapInfo
		for _, m := range maps {
			if m.Supports(gt) {
				keep = append(keep, m)
			}
		}
		maps = keep
	}

	switch {
	case ctx.JSON:
		if maps == nil {
			maps = []game.MapInfo{}
		}
		b, _ := json.MarshalIndent(maps, "", "  ")
		if ctx.Out != ui.Std {
			ctx.Out.Plain("%s", b)
		} else {
			fmt.Fprintln(os.Stdout, string(b))
		}
		return 0
	case names:
		for _, m := range maps {
			ctx.Out.Plain("%s", m.Name)
		}
		return 0
	}

	caption := fmt.Sprintf("%d in %s-%s", len(maps), side, f)
	if gt != "" {
		caption += ", gametype " + gt
	}
	ctx.Out.Section("maps", caption)
	if len(maps) == 0 {
		ctx.Out.Note("none found in %s", main)
		return 0
	}
	var rows [][]string
	for _, m := range maps {
		kind := ui.StyleDim.Render("stock")
		if !m.Stock {
			kind = ui.StyleCyan.Render("custom")
		}
		gts := strings.Join(m.Gametypes, " ")
		if gts == "" {
			gts = ui.StyleDim.Render("(no .arena)")
		}
		long := ""
		if m.LongName != "" && !strings.EqualFold(m.LongName, m.Name) {
			long = ui.StyleDim.Render(m.LongName)
		}
		rows = append(rows, []string{ui.StyleWhite.Render(m.Name), kind, gts, ui.StyleDim.Render(m.Source), long})
	}
	ctx.Out.Listing(rows)
	ctx.Out.Plain("")
	ctx.Out.Note("start one: vcod server start <map> [gt]   ·   vcod play <map> [gt]   ·   vcod help maps")
	return 0
}
