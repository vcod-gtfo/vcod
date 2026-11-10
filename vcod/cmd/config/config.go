// Package configcmd is `vcod config`: show, edit or set the configuration.
package configcmd

import (
	"os"
	"os/exec"
	"strconv"
	"strings"

	"vcod/internal/app"
	"vcod/internal/config"
	"vcod/internal/platform"
	"vcod/internal/registry"
	"vcod/internal/ui"
	"vcod/internal/video"
)

var help = `Usage:
  vcod config                show every setting, its value and where it came from
  vcod config set KEY VALUE  set one key
  vcod config unset KEY      remove one key (back to its default)
  vcod config edit           open the file in $EDITOR
  vcod config path           print the file's path

The file is ~/.config/vcod/config (KEY=VALUE lines, mode 600). Every key can be
overridden for one run with VCOD_<KEY> in the environment.

Keys:
` + keyList

var keyList = func() string {
	var b strings.Builder
	for _, k := range config.Keys {
		def := k.Default
		if def == "" {
			def = "—"
		}
		b.WriteString("  " + pad(k.Key, 18) + pad(def, 10) + k.Help + "\n")
	}
	return strings.TrimRight(b.String(), "\n")
}()

func pad(s string, n int) string {
	for len(s) < n {
		s += " "
	}
	return s
}

func init() {
	registry.Register(registry.Command{
		Name:        "config",
		Summary:     "Show or change the settings",
		Args:        "[set KEY VALUE|unset KEY|edit|path]",
		Help:        help,
		Group:       registry.GroupSetup,
		Order:       30,
		NeedsConfig: true,
		Complete: func(prev []string) []string {
			if len(prev) > 0 && (prev[len(prev)-1] == "set" || prev[len(prev)-1] == "unset") {
				var ks []string
				for _, k := range config.Keys {
					ks = append(ks, k.Key)
				}
				return ks
			}
			return []string{"set", "unset", "edit", "path"}
		},
		Run: run,
	})
}

func run(ctx *app.Context, args []string) int {
	if len(args) == 0 {
		return Show(ctx)
	}
	switch args[0] {
	case "path":
		ctx.Out.Plain("%s", ctx.Cfg.Path)
		return 0
	case "edit":
		return edit(ctx)
	case "set":
		if len(args) < 3 {
			return ctx.Usage("usage: vcod config set KEY VALUE")
		}
		return set(ctx, args[1], strings.Join(args[2:], " "))
	case "unset":
		if len(args) != 2 {
			return ctx.Usage("usage: vcod config unset KEY")
		}
		key := strings.ToUpper(args[1])
		if !config.KnownKey(key) {
			return ctx.Usage("unknown key %s (vcod help config lists them)", key)
		}
		if err := config.Save(ctx.Cfg.Path, map[string]string{key: ""}); err != nil {
			return ctx.Fail(err)
		}
		ctx.Out.OK("%s removed from %s", key, ctx.Cfg.Path)
		return 0
	}
	return ctx.Usage("unknown argument: %s  (try: vcod help config)", args[0])
}

// Show prints the resolved settings.
func Show(ctx *app.Context) int {
	cfg := ctx.Cfg
	ctx.Out.Section("configuration", cfg.Path)
	var rows [][]string
	for _, k := range config.Keys {
		v := cfg.Get(k.Key)
		src := cfg.Source(k.Key)
		switch k.Key {
		case config.KeyWorkspace:
			v, src = cfg.Workspace, cfg.WorkspaceSource
		case config.KeyFlavor:
			v = string(cfg.Flavor)
			if cfg.FlavorFromFlag {
				src = "flag"
			}
		case config.KeyRcon:
			if v == "" {
				v = ui.StyleDim.Render("(generated at the first server start)")
			} else {
				v = config.Mask(v)
			}
		}
		if v == "" {
			v = ui.StyleDim.Render("—")
		}
		rows = append(rows, []string{strings.ToLower(k.Key), v, ui.StyleDim.Render(src)})
	}
	ctx.Out.Listing(rows)
	if !cfg.HasFile {
		ctx.Out.Note("no file yet; these are the defaults (vcod setup, or vcod config set)")
	}
	ctx.Out.Plain("")
	return 0
}

func set(ctx *app.Context, key, value string) int {
	key = strings.ToUpper(key)
	if !config.KnownKey(key) {
		return ctx.Usage("unknown key %s (vcod help config lists them)", key)
	}
	switch key {
	case config.KeyFlavor:
		f, err := config.ParseFlavor(value)
		if err != nil {
			return ctx.Fail(err)
		}
		value = string(f)
	case config.KeyWorkspace:
		value = expand(value)
		if !config.IsWorkspace(value) {
			return ctx.Usage("%s has no cod_rea/ and flavors/", value)
		}
	case config.KeyPort15, config.KeyPort16x, config.KeyReadyTimeoutSec:
		if _, err := strconv.Atoi(value); err != nil {
			return ctx.Usage("%s takes a number", key)
		}
	case config.KeyCheats, config.KeyAllowUnversion:
		if value != "0" && value != "1" {
			return ctx.Usage("%s is 0 or 1", key)
		}
	case config.KeyClientBuild:
		if value != "video" && value != "headless" {
			return ctx.Usage("%s is video or headless", key)
		}
	case config.KeyClientHome:
		value = expand(value)
	case config.KeyClientRes:
		r, err := video.ParseRes(value)
		if err != nil {
			return ctx.Usage("%v", err)
		}
		value = r.Name
	case config.KeyClientDisplay:
		d, err := video.ParseDisplay(value)
		if err != nil {
			return ctx.Usage("%v", err)
		}
		value = string(d)
	case config.KeyClientMaxFPS:
		n, err := video.ParseFPS(value)
		if err != nil {
			return ctx.Usage("%v", err)
		}
		value = strconv.Itoa(n)
		if n == 0 {
			value = video.Unlocked
		}
	case config.KeyClientGPU:
		g, err := video.ParseGPU(value)
		if err != nil {
			return ctx.Usage("%v", err)
		}
		value = string(g)
	}
	if err := config.Save(ctx.Cfg.Path, map[string]string{key: value}); err != nil {
		return ctx.Fail(err)
	}
	shown := value
	if key == config.KeyRcon {
		shown = config.Mask(value)
	}
	ctx.Out.OK("%s=%s written to %s", key, shown, ctx.Cfg.Path)
	if key == config.KeyRcon {
		ctx.Out.Note("servers already running keep their old password until restarted")
	}
	return 0
}

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + p[1:]
		}
	}
	return p
}

func edit(ctx *app.Context) int {
	if ctx.Out != ui.Std {
		ctx.Errorf("config edit needs the terminal; run it outside the shell")
		return 1
	}
	editor, err := platform.Editor()
	if err != nil {
		return ctx.Fail(err)
	}
	if !ctx.Cfg.HasFile {
		if err := config.Save(ctx.Cfg.Path, map[string]string{}); err != nil {
			return ctx.Fail(err)
		}
	}
	cmd := exec.Command(editor[0], append(editor[1:], ctx.Cfg.Path)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return ctx.Fail(err)
	}
	_ = os.Chmod(ctx.Cfg.Path, 0o600)
	return 0
}
