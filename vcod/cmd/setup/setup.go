// Package setupcmd is `vcod setup`: guided first-time configuration.
package setupcmd

import (
	"strconv"

	"vcod/internal/app"
	"vcod/internal/config"
	"vcod/internal/prompt"
	"vcod/internal/registry"
	"vcod/internal/ui"
)

const help = `Usage:
  vcod setup

Asks for the workspace folder, the default flavor, map and gametype, and how the video
client opens (windowed or fullscreen). Existing values are offered as defaults; Enter
keeps them. Ctrl-C at any point writes nothing. The rcon password is generated at the
first server start (vcod config set RCON_PASSWORD … to choose one).`

func init() {
	registry.Register(registry.Command{
		Name:        "setup",
		Summary:     "Guided first-time setup",
		Help:        help,
		Group:       registry.GroupSetup,
		Order:       10,
		NeedsConfig: true,
		Run:         run,
	})
}

func run(ctx *app.Context, args []string) int {
	if len(args) > 0 {
		return ctx.Usage("setup takes no arguments")
	}
	out, in, cfg := ctx.Out, ctx.In(), ctx.Cfg
	out.Banner("setup")
	updates := map[string]string{}

	out.Step("workspace")
	for {
		v, err := in.Ask("codworkspace folder:", cfg.Workspace)
		if err != nil {
			return cancelled(ctx, err)
		}
		if config.IsWorkspace(v) {
			updates[config.KeyWorkspace] = v
			break
		}
		out.Warn("%s has no cod_rea/ and flavors/", v)
	}

	out.Step("defaults")
	for {
		v, err := in.Ask("Default flavor (1.5 | 1.6x):", string(cfg.Flavor))
		if err != nil {
			return cancelled(ctx, err)
		}
		if f, err := config.ParseFlavor(v); err == nil {
			updates[config.KeyFlavor] = string(f)
			break
		}
		out.Warn("1.5 or 1.6x")
	}
	m, err := in.Ask("Default map:", cfg.Get(config.KeyMap))
	if err != nil {
		return cancelled(ctx, err)
	}
	updates[config.KeyMap] = m
	g, err := in.Ask("Default gametype (dm tdm sd re bel hq):", cfg.Get(config.KeyGametype))
	if err != nil {
		return cancelled(ctx, err)
	}
	updates[config.KeyGametype] = g
	cheats, err := in.YesNo("Start servers with cheats (devmap)?", cfg.Bool(config.KeyCheats))
	if err != nil {
		return cancelled(ctx, err)
	}
	updates[config.KeyCheats] = b01(cheats)

	out.Step("client")
	fs, err := in.YesNo("Open the video client fullscreen?", cfg.Bool(config.KeyClientFull))
	if err != nil {
		return cancelled(ctx, err)
	}
	updates[config.KeyClientFull] = b01(fs)
	if !fs {
		mode, err := in.Ask("r_mode (-1 = desktop size, 3 = 640x480, 4 = 800x600, 6 = 1024x768):", cfg.Get(config.KeyClientMode))
		if err != nil {
			return cancelled(ctx, err)
		}
		if _, err := strconv.Atoi(mode); err == nil {
			updates[config.KeyClientMode] = mode
		}
	}

	if err := config.Save(cfg.Path, updates); err != nil {
		return ctx.Fail(err)
	}
	out.Plain("")
	out.OK("wrote %s (mode 600)", cfg.Path)
	out.Note("check the machine with %s, then try %s", ui.StyleCyan.Render("vcod doctor"), ui.StyleCyan.Render("vcod play"))
	return 0
}

func b01(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func cancelled(ctx *app.Context, err error) int {
	if prompt.Cancelled(err) {
		ctx.Out.Info("setup aborted — nothing written")
		return 130
	}
	return ctx.Fail(err)
}
