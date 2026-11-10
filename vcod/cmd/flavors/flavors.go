// Package flavorscmd is `vcod flavors`: the four side-by-side installs.
package flavorscmd

import (
	"os"
	"path/filepath"

	"vcod/internal/app"
	"vcod/internal/config"
	"vcod/internal/game"
	"vcod/internal/registry"
	"vcod/internal/run"
	"vcod/internal/ui"
)

const help = `Usage:
  vcod flavors                     the four installs and what is in them
  vcod flavors make                create the missing ones (cod_rea/server/make_flavors.sh)
  vcod --write flavors rebuild server-1.6x
                                     delete one and build it again from ../cod

A flavor is a reflink copy of cod/ with its own configs, logs and demos:
client-1.5 and client-1.6x (Windows installs, used here as the native client's
fs_basepath), server-1.5 and server-1.6x (cod_lnxded and the paks, run in podman).
See flavors/README.md.

rebuild deletes the folder first, so it needs write mode, and it refuses while that
flavor's server is up.`

func init() {
	registry.Register(registry.Command{
		Name:        "flavors",
		Summary:     "List, create or rebuild the four flavor installs",
		Args:        "[make|rebuild <name>]",
		Help:        help,
		Group:       registry.GroupSetup,
		Order:       20,
		NeedsConfig: true,
		Complete: func([]string) []string {
			return []string{"make", "rebuild", "client-1.5", "client-1.6x", "server-1.5", "server-1.6x"}
		},
		Run: runFlavors,
	})
}

var names = []string{"client-1.5", "client-1.6x", "server-1.5", "server-1.6x"}

func runFlavors(ctx *app.Context, args []string) int {
	w, err := ctx.Workspace()
	if err != nil {
		return ctx.Fail(err)
	}
	if len(args) == 0 {
		ctx.Out.Section("flavors", w.Flavors())
		var rows [][]string
		for _, n := range names {
			dir := filepath.Join(w.Flavors(), n)
			state := ui.StyleGreen.Render("✔ present")
			if _, err := os.Stat(dir); err != nil {
				state = ui.StyleYellow.Render("▲ missing") + ui.StyleDim.Render("  vcod flavors make")
			}
			proto := "protocol 6"
			if filepath.Ext(n) == ".6x" {
				proto = "protocol 10"
			}
			extra := ""
			if _, err := os.Stat(filepath.Join(dir, "cod1plus.so")); err == nil {
				extra = "cod1plus.so"
			}
			if _, err := os.Stat(filepath.Join(dir, "mss32_original.dll")); err == nil {
				extra = "COD1.6X client"
			}
			rows = append(rows, []string{ui.StyleWhite.Render(n), state, ui.StyleDim.Render(proto), ui.StyleDim.Render(extra)})
		}
		ctx.Out.Listing(rows)
		ctx.Out.Plain("")
		return 0
	}
	script := []string{"sh", w.MakeFlavors()}
	switch args[0] {
	case "make", "create":
		if len(args) > 1 {
			return ctx.Usage("flavors make takes no arguments")
		}
	case "rebuild":
		if len(args) != 2 {
			return ctx.Usage("usage: vcod --write flavors rebuild <%s>", "client-1.5|client-1.6x|server-1.5|server-1.6x")
		}
		if !ctx.Write {
			ctx.Errorf("%s", app.ErrReadOnly.Error())
			ctx.Hint("vcod --write flavors rebuild %s", args[1])
			return 2
		}
		name := args[1]
		ok := false
		for _, n := range names {
			ok = ok || n == name
		}
		if !ok {
			return ctx.Usage("unknown flavor %q", name)
		}
		if f, err := config.ParseFlavor(name); err == nil && filepath.Base(name)[:6] == "server" {
			for _, s := range game.Servers(ctx.Ctx, ctx.Cfg) {
				if s.Flavor == f && s.Running() {
					ctx.Errorf("server %s is up — vcod server stop %s first", f, f)
					return 1
				}
			}
		}
		script = append(script, "--force", name)
	default:
		return ctx.Usage("unknown flavors subcommand %q — make or rebuild", args[0])
	}
	code, err := run.Run(ctx.Ctx, run.Spec{Args: script, Dir: w.Root, Stream: ctx.Stream})
	if err != nil {
		return ctx.Fail(err)
	}
	if code != 0 {
		ctx.Out.Bad("make_flavors.sh failed (exit %d)", code)
		return code
	}
	ctx.Out.OK("flavors ready in %s", w.Flavors())
	return 0
}
