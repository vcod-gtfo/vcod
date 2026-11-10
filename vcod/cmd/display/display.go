// Package displaycmd is `vcod display`: the client's resolution, display mode and fps
// cap, shown and saved to the configuration.
package displaycmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"vcod/internal/app"
	"vcod/internal/config"
	"vcod/internal/game"
	"vcod/internal/jobs"
	"vcod/internal/registry"
	"vcod/internal/ui"
	"vcod/internal/video"
)

const help = `Usage:
  vcod display                          what the client starts with
  vcod display presets                  the resolutions and fps caps offered by name
  vcod display 1080p                    the resolution: a preset or WIDTHxHEIGHT
  vcod display borderless               window, fullscreen or borderless
  vcod display fps 333                  the fps cap: 1–1000, or unlocked
  vcod display gpu nvidia               the GPU: auto, nvidia or integrated
  vcod display 1440p fullscreen fps 165 any mix, in any order
  vcod display reset                    back to the defaults (720p window, 125 fps, gpu auto)

Saved to the configuration (CLIENT_RES, CLIENT_DISPLAY, CLIENT_MAXFPS, CLIENT_GPU), so every
later 'vcod client' and 'vcod play' uses them, in both flavors. A single start can
override them instead: vcod play --res 1080p --fullscreen --maxfps 333.

Display modes:
  window       a window of the chosen size
  fullscreen   exclusive fullscreen: the monitor switches to the chosen size
  borderless   a borderless window over the whole desktop, at the desktop's size,
               whatever the resolution says; it never switches the monitor's mode

The fps cap is com_maxfps. Any number from 1 to 1000 works, 333fps or unlocked too.
Unlocked (0) has no cap, which the engine runs at 1000 fps, since its clock counts
whole milliseconds. Vsync stays off, so the monitor does not cap it either.
CoD's jump height depends on the frame time, which is why 125, 250 and 333 are the
caps players pick. 1.6x servers check com_maxfps against 60–250: a client outside it
is reported to the server, which logs it or, when set to, kicks.

GPU: on a hybrid laptop (NVIDIA Optimus) the integrated chip drives the screen and
programs render on it unless they ask for the NVIDIA card. auto (the default) asks for
it whenever the NVIDIA driver sees a card next to another chip, through PRIME render
offload (__NV_PRIME_RENDER_OFFLOAD, __GLX_VENDOR_LIBRARY_NAME and, for SDL's Wayland
driver, __EGL_VENDOR_LIBRARY_FILENAMES). nvidia always asks; integrated keeps the
client on the screen's chip. 'vcod client' prints the GL_RENDERER it got.

A running client keeps the settings it started with: vcod client restart.
The resolution and display mode only matter to the video client; the fps cap applies
to the headless one too.`

func init() {
	registry.Register(registry.Command{
		Name:        "display",
		Summary:     "Client resolution, fullscreen/window and fps cap",
		Args:        "[preset|WxH] [window|fullscreen|borderless] [fps N|unlocked]",
		Help:        help,
		Group:       registry.GroupPlay,
		Order:       32,
		NeedsConfig: true,
		Complete:    complete,
		Run:         run,
	})
}

// complete offers fps caps after "fps", everything else otherwise.
func complete(prev []string) []string {
	if n := len(prev); n > 0 && (prev[n-1] == "fps" || prev[n-1] == "maxfps") {
		return video.FPSWords()
	}
	if n := len(prev); n > 0 && prev[n-1] == "gpu" {
		return []string{"auto", "nvidia", "integrated"}
	}
	out := []string{"presets", "reset", "fps", "gpu"}
	for _, d := range video.Displays {
		out = append(out, string(d))
	}
	return append(out, video.PresetNames()...)
}

func run(ctx *app.Context, args []string) int {
	if len(args) == 1 {
		switch args[0] {
		case "presets", "list":
			return presets(ctx)
		case "reset", "defaults":
			return save(ctx, map[string]string{config.KeyClientRes: "", config.KeyClientDisplay: "", config.KeyClientMaxFPS: "", config.KeyClientGPU: ""})
		case "show":
			args = nil
		}
	}
	if len(args) == 0 {
		return show(ctx)
	}
	updates, err := parse(args)
	if err != nil {
		return ctx.Usage("%v  (try: vcod help display)", err)
	}
	return save(ctx, updates)
}

// parse reads the words of a `display` line into config updates.
func parse(args []string) (map[string]string, error) {
	updates := map[string]string{}
	set := func(key, value string) error {
		if _, dup := updates[key]; dup {
			return fmt.Errorf("%s is given twice", strings.ToLower(strings.TrimPrefix(key, "CLIENT_")))
		}
		updates[key] = value
		return nil
	}
	for i := 0; i < len(args); i++ {
		a := strings.ToLower(args[i])
		if v, ok := strings.CutPrefix(a, "fps="); ok {
			n, err := video.ParseFPS(v)
			if err != nil {
				return nil, err
			}
			if err := set(config.KeyClientMaxFPS, fpsValue(n)); err != nil {
				return nil, err
			}
			continue
		}
		if v, ok := strings.CutPrefix(a, "gpu="); ok {
			g, err := video.ParseGPU(v)
			if err != nil {
				return nil, err
			}
			if err := set(config.KeyClientGPU, string(g)); err != nil {
				return nil, err
			}
			continue
		}
		switch {
		case a == "gpu" || a == "--gpu":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("gpu needs auto, nvidia or integrated")
			}
			i++
			g, err := video.ParseGPU(args[i])
			if err != nil {
				return nil, err
			}
			if err := set(config.KeyClientGPU, string(g)); err != nil {
				return nil, err
			}
		case video.IsGPUWord(a):
			g, _ := video.ParseGPU(a)
			if err := set(config.KeyClientGPU, string(g)); err != nil {
				return nil, err
			}
		case a == "fps" || a == "maxfps" || a == "--maxfps" || a == "--fps":
			if i+1 >= len(args) {
				return nil, fmt.Errorf("fps needs a number (1–%d) or unlocked", video.MaxFPSLimit)
			}
			i++
			n, err := video.ParseFPS(args[i])
			if err != nil {
				return nil, err
			}
			if err := set(config.KeyClientMaxFPS, fpsValue(n)); err != nil {
				return nil, err
			}
		case strings.HasSuffix(a, "fps") || a == video.Unlocked || a == "unlimited" || a == "uncapped":
			n, err := video.ParseFPS(a)
			if err != nil {
				return nil, err
			}
			if err := set(config.KeyClientMaxFPS, fpsValue(n)); err != nil {
				return nil, err
			}
		case video.IsDisplay(a):
			d, _ := video.ParseDisplay(a)
			if err := set(config.KeyClientDisplay, string(d)); err != nil {
				return nil, err
			}
		default:
			r, err := video.ParseRes(a)
			if err != nil {
				if _, numErr := strconv.Atoi(a); numErr == nil {
					return nil, fmt.Errorf("%s alone is ambiguous: write fps %s for a cap, or WIDTHxHEIGHT for a resolution", a, a)
				}
				return nil, err
			}
			if err := set(config.KeyClientRes, r.Name); err != nil {
				return nil, err
			}
		}
	}
	return updates, nil
}

// fpsValue is how a cap is written to the configuration.
func fpsValue(n int) string {
	if n == 0 {
		return video.Unlocked
	}
	return strconv.Itoa(n)
}

func save(ctx *app.Context, updates map[string]string) int {
	// The keys older configs used are dropped once the new ones are written.
	for _, k := range config.LegacyKeys {
		if ctx.Cfg.FileValue(k) != "" {
			updates[k] = ""
		}
	}
	if err := config.Save(ctx.Cfg.Path, updates); err != nil {
		return ctx.Fail(err)
	}
	cfg, err := config.LoadFrom(ctx.Cfg.Path, string(ctx.Cfg.Flavor), ctx.Cfg.FlavorFromFlag)
	if err != nil {
		return ctx.Fail(err)
	}
	ctx.Cfg = cfg
	v, _ := cfg.Video()
	ctx.Out.OK("saved: %s", ui.StyleWhite.Render(v.Summary()))
	for k := range updates {
		if src := cfg.Source(k); src == "env" {
			ctx.Out.Warn("VCOD_%s is set in the environment and wins over the file", k)
		}
	}
	notes(ctx, v)
	for _, f := range config.Flavors {
		if j := jobs.Get(game.ClientJob(f)); j.Running() {
			ctx.Out.Note("the running %s client keeps its old settings: vcod %sclient restart", f, flavorFlag(f))
		}
	}
	return 0
}

func flavorFlag(f config.Flavor) string {
	if f == config.F16x {
		return "--16x "
	}
	return ""
}

func notes(ctx *app.Context, v video.Settings) {
	for _, n := range v.Notes() {
		ctx.Out.Note("%s", n)
	}
}

func show(ctx *app.Context) int {
	v, err := ctx.Cfg.Video()
	probe := video.Detect()
	if ctx.JSON {
		return showJSON(ctx, v, probe, err)
	}
	if err != nil {
		ctx.Out.Warn("%v — using the default", err)
	}
	res := v.Res.String()
	if v.Display == video.Borderless {
		res = video.Desktop + ui.StyleDim.Render("  (borderless; "+v.Res.String()+" applies to window and fullscreen)")
	}
	ctx.Out.Fields([][2]string{
		{"resolution", res + source(ctx, config.KeyClientRes)},
		{"display", string(v.Display) + source(ctx, config.KeyClientDisplay)},
		{"fps cap", video.FPSString(v.MaxFPS) + source(ctx, config.KeyClientMaxFPS)},
		{"gpu", v.GPU.Describe(probe) + source(ctx, config.KeyClientGPU)},
		{"cvars", ui.StyleDim.Render(strings.Join(v.Cvars(true), " "))},
	})
	for _, f := range config.Flavors {
		if r := game.ClientRenderer(f); r != "" {
			ctx.Out.Info("the last %s client rendered on %s", f, r)
		}
	}
	notes(ctx, v)
	ctx.Out.Note("change: vcod display <preset|WxH> <window|fullscreen|borderless> fps <N|unlocked> gpu <auto|nvidia|integrated>  ·  list: vcod display presets")
	return 0
}

func source(ctx *app.Context, key string) string {
	switch ctx.Cfg.Source(key) {
	case "env":
		return ui.StyleYellow.Render("  (VCOD_" + key + ")")
	case "default":
		return ui.StyleDim.Render("  (default)")
	}
	return ""
}

func showJSON(ctx *app.Context, v video.Settings, probe video.Probe, verr error) int {
	out := struct {
		Res     string   `json:"resolution"`
		Width   int      `json:"width,omitempty"`
		Height  int      `json:"height,omitempty"`
		Display string   `json:"display"`
		MaxFPS  int      `json:"maxfps"`
		GPU     string   `json:"gpu"`
		GPUUsed string   `json:"gpu_resolved"`
		GPUName string   `json:"nvidia_gpu,omitempty"`
		Env     []string `json:"env,omitempty"`
		Cvars   []string `json:"cvars"`
		Notes   []string `json:"notes,omitempty"`
		Error   string   `json:"error,omitempty"`
	}{Res: v.Res.Name, Width: v.Res.W, Height: v.Res.H, Display: string(v.Display), MaxFPS: v.MaxFPS,
		GPU: string(v.GPU), GPUUsed: string(v.GPU.Resolve(probe)), GPUName: probe.NvidiaModel,
		Env: v.GPU.Resolve(probe).Env(probe), Cvars: v.Cvars(true), Notes: v.Notes()}
	if verr != nil {
		out.Error = verr.Error()
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	if ctx.Out != ui.Std {
		ctx.Out.Plain("%s", b)
		return 0
	}
	fmt.Fprintln(os.Stdout, string(b))
	return 0
}

func presets(ctx *app.Context) int {
	ctx.Out.Heading("resolutions")
	rows := [][]string{}
	for _, p := range video.Presets {
		size := ui.StyleDim.Render("—")
		if p.W > 0 {
			size = strconv.Itoa(p.W) + "x" + strconv.Itoa(p.H)
		}
		rows = append(rows, []string{ui.StyleCyan.Render(p.Name), size, ui.StyleDim.Render(p.Note)})
	}
	ctx.Out.Listing(rows)
	ctx.Out.Note("any WIDTHxHEIGHT works too, e.g. 1366x768")
	ctx.Out.Plain("")
	ctx.Out.Heading("fps caps")
	rows = rows[:0]
	for _, p := range video.FPSPresets {
		name := strconv.Itoa(p.FPS)
		if p.FPS == 0 {
			name = video.Unlocked
		}
		rows = append(rows, []string{ui.StyleCyan.Render(name), ui.StyleDim.Render(p.Note)})
	}
	ctx.Out.Listing(rows)
	ctx.Out.Note("any number from 1 to %d works too", video.MaxFPSLimit)
	ctx.Out.Plain("")
	ctx.Out.Heading("gpu")
	ctx.Out.Listing([][]string{
		{ui.StyleCyan.Render("auto"), ui.StyleDim.Render("the NVIDIA card on a hybrid laptop, the system's choice otherwise")},
		{ui.StyleCyan.Render("nvidia"), ui.StyleDim.Render("always ask for the NVIDIA card (PRIME render offload)")},
		{ui.StyleCyan.Render("integrated"), ui.StyleDim.Render("stay on the chip that drives the screen")},
	})
	ctx.Out.Plain("")
	ctx.Out.Heading("display modes")
	ctx.Out.Listing([][]string{
		{ui.StyleCyan.Render("window"), ui.StyleDim.Render("a window of the chosen size")},
		{ui.StyleCyan.Render("fullscreen"), ui.StyleDim.Render("exclusive: the monitor switches to the chosen size")},
		{ui.StyleCyan.Render("borderless"), ui.StyleDim.Render("covers the desktop at its own size; never switches the monitor's mode")},
	})
	return 0
}
