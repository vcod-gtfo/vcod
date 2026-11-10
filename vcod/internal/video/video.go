// Package video turns vcod's client display settings (a resolution, a display mode and
// an fps cap) into the cvars cod_client starts with.
//
// What the port's renderer does with them (cod_rea/src/renderer/tr_init.c,
// sdl/sdl_glimp.c):
//
//	r_mode -2                    the desktop's resolution
//	r_mode -1                    r_customwidth × r_customheight
//	r_fullscreen 0 / 1 / 2       window / exclusive fullscreen / borderless at the
//	                             desktop's size (2 ignores r_mode: it never switches
//	                             the monitor's mode)
//	com_maxfps N                 frame cap; 0 = unlocked, which the engine runs at
//	                             1000 (its time is in whole milliseconds)
//	r_swapInterval 0             vsync off (the default), so the monitor does not cap it
//
// r_mode and the custom size are latched, so they only apply at start (or on
// vid_restart); vcod passes them on the command line, which wins over the client's own
// config_mp.cfg.
package video

import (
	"fmt"
	"strconv"
	"strings"
)

// Preset is a named resolution. W and H are 0 for "desktop".
type Preset struct {
	Name string
	W, H int
	Note string
}

// Desktop is the preset that follows the monitor.
const Desktop = "desktop"

// Presets are the resolutions offered by name, in the order they are listed.
var Presets = []Preset{
	{Desktop, 0, 0, "the monitor's own resolution"},
	{"640x480", 640, 480, "4:3, the 2003 default"},
	{"800x600", 800, 600, "4:3"},
	{"1024x768", 1024, 768, "4:3"},
	{"1280x960", 1280, 960, "4:3"},
	{"720p", 1280, 720, "16:9"},
	{"900p", 1600, 900, "16:9"},
	{"1080p", 1920, 1080, "16:9"},
	{"1440p", 2560, 1440, "16:9"},
	{"4k", 3840, 2160, "16:9"},
	{"1680x1050", 1680, 1050, "16:10"},
	{"1920x1200", 1920, 1200, "16:10"},
	{"uw1080", 2560, 1080, "21:9"},
	{"uw1440", 3440, 1440, "21:9"},
}

// PresetNames lists the preset names, for completion.
func PresetNames() []string {
	out := make([]string, len(Presets))
	for i, p := range Presets {
		out[i] = p.Name
	}
	return out
}

// Res is a resolved resolution: Desktop, or W×H.
type Res struct {
	// Name is what was asked for, normalised: a preset name or "WxH".
	Name string
	W, H int
}

// IsDesktop reports whether the resolution follows the monitor.
func (r Res) IsDesktop() bool { return r.W == 0 }

// String is the name plus the size for named presets: "1080p (1920x1080)".
func (r Res) String() string {
	if r.IsDesktop() {
		return Desktop
	}
	size := strconv.Itoa(r.W) + "x" + strconv.Itoa(r.H)
	if r.Name == size {
		return size
	}
	return r.Name + " (" + size + ")"
}

// ParseRes reads a preset name (any case) or WIDTHxHEIGHT.
func ParseRes(s string) (Res, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, p := range Presets {
		if p.Name == s {
			return Res{p.Name, p.W, p.H}, nil
		}
	}
	ws, hs, ok := strings.Cut(strings.ReplaceAll(s, "×", "x"), "x")
	if ok {
		w, errW := strconv.Atoi(ws)
		h, errH := strconv.Atoi(hs)
		if errW == nil && errH == nil {
			if w < 320 || h < 200 || w > 7680 || h > 4320 {
				return Res{}, fmt.Errorf("resolution %dx%d is out of range (320x200 … 7680x4320)", w, h)
			}
			name := strconv.Itoa(w) + "x" + strconv.Itoa(h)
			for _, p := range Presets {
				if p.W == w && p.H == h {
					name = p.Name
				}
			}
			return Res{name, w, h}, nil
		}
	}
	return Res{}, fmt.Errorf("unknown resolution %q: a preset (%s) or WIDTHxHEIGHT", s, strings.Join(PresetNames(), " "))
}

// Display is how the client's window is shown.
type Display string

const (
	Window     Display = "window"
	Fullscreen Display = "fullscreen"
	Borderless Display = "borderless"
)

// Displays lists the display modes, for completion and help.
var Displays = []Display{Window, Fullscreen, Borderless}

// ParseDisplay reads a display mode, its short forms, or the r_fullscreen number.
func ParseDisplay(s string) (Display, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "window", "windowed", "win", "w", "0":
		return Window, nil
	case "fullscreen", "full", "fs", "f", "exclusive", "1":
		return Fullscreen, nil
	case "borderless", "bl", "b", "noborder", "2":
		return Borderless, nil
	}
	return "", fmt.Errorf("unknown display mode %q: window, fullscreen or borderless", s)
}

// IsDisplay reports whether s names a display mode (not counting the bare numbers,
// which would be ambiguous among other arguments).
func IsDisplay(s string) bool {
	if _, err := strconv.Atoi(s); err == nil {
		return false
	}
	_, err := ParseDisplay(s)
	return err == nil
}

// fullscreenCvar is the r_fullscreen value for a display mode.
func (d Display) fullscreenCvar() string {
	switch d {
	case Fullscreen:
		return "1"
	case Borderless:
		return "2"
	}
	return "0"
}

// MaxFPSLimit is the highest cap accepted. The engine counts time in whole
// milliseconds, so 1000 is as fast as a frame can come; an unlocked client (0) runs
// there too.
const MaxFPSLimit = 1000

// FPSPreset is a well-known cap.
type FPSPreset struct {
	FPS  int
	Note string
}

// FPSPresets are the caps offered by name. Every cap from 1 to MaxFPSLimit is accepted;
// these are the ones CoD players reach for, because pmove's jump height depends on the
// frame time.
var FPSPresets = []FPSPreset{
	{60, "the 1.6x rules' floor"},
	{85, "the engine's default"},
	{125, "the classic competitive cap: higher jumps (8 ms frames)"},
	{142, ""},
	{166, ""},
	{250, "the 1.6x rules' ceiling (4 ms frames)"},
	{333, "the highest jumps (3 ms frames); outside the 1.6x rules"},
	{500, ""},
	{1000, "the engine's limit"},
	{0, "unlocked: no cap, which the engine runs at 1000"},
}

// Unlocked is the word for an fps cap of 0.
const Unlocked = "unlocked"

// FPSWords are the values offered for completion.
func FPSWords() []string {
	out := make([]string, 0, len(FPSPresets))
	for _, p := range FPSPresets {
		if p.FPS == 0 {
			out = append(out, Unlocked)
		} else {
			out = append(out, strconv.Itoa(p.FPS))
		}
	}
	return out
}

// ParseFPS reads an fps cap: a number from 0 to MaxFPSLimit, with an optional "fps"
// suffix, or unlocked (also: unlimited, uncapped, off, max) for 0.
func ParseFPS(s string) (int, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case Unlocked, "unlock", "unlimited", "uncapped", "off", "none", "max":
		return 0, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(s, "fps")))
	if err != nil || n < 0 || n > MaxFPSLimit {
		return 0, fmt.Errorf("fps cap %q: a number from 1 to %d, or unlocked", s, MaxFPSLimit)
	}
	return n, nil
}

// FPSString is an fps cap for display: "125 fps" or "unlocked".
func FPSString(n int) string {
	if n == 0 {
		return Unlocked
	}
	return strconv.Itoa(n) + " fps"
}

// Settings is everything the video client is started with. Mode, when set, is a raw
// r_mode that overrides Res.
type Settings struct {
	Res     Res
	Display Display
	MaxFPS  int
	Mode    *int
}

// Cvars are the +set arguments for a client. Window settings apply only to a client with
// a window; the fps cap applies to both builds.
func (s Settings) Cvars(windowed bool) []string {
	var args []string
	if windowed {
		args = append(args, "+set", "r_fullscreen", s.Display.fullscreenCvar())
		switch {
		case s.Mode != nil:
			args = append(args, "+set", "r_mode", strconv.Itoa(*s.Mode))
		case s.Res.IsDesktop():
			args = append(args, "+set", "r_mode", "-2")
		default:
			args = append(args, "+set", "r_mode", "-1",
				"+set", "r_customwidth", strconv.Itoa(s.Res.W),
				"+set", "r_customheight", strconv.Itoa(s.Res.H))
		}
	}
	return append(args, "+set", "com_maxfps", strconv.Itoa(s.MaxFPS))
}

// Summary is one line: "1080p (1920x1080) · window · 125 fps".
func (s Settings) Summary() string {
	res := s.Res.String()
	if s.Mode != nil {
		res = "r_mode " + strconv.Itoa(*s.Mode)
	} else if s.Display == Borderless {
		res = Desktop
	}
	return res + " · " + string(s.Display) + " · " + FPSString(s.MaxFPS)
}

// Notes are the caveats a combination deserves.
func (s Settings) Notes() []string {
	var out []string
	if s.Display == Borderless && s.Mode == nil && !s.Res.IsDesktop() {
		out = append(out, "borderless always runs at the desktop's size, so "+s.Res.String()+" is ignored until you pick window or fullscreen")
	}
	if s.MaxFPS < 60 || s.MaxFPS > 250 {
		out = append(out, FPSString(s.MaxFPS)+" is outside 60–250, the com_maxfps range 1.6x servers check: they log the violation, or kick when set to")
	}
	return out
}

// legacyModes is the port's r_vidModes table, to read an old CLIENT_MODE number.
var legacyModes = [][2]int{
	{320, 240}, {400, 300}, {512, 384}, {640, 480}, {800, 600}, {960, 720},
	{1024, 768}, {1152, 864}, {1280, 1024}, {1600, 1200}, {2048, 1536}, {856, 480},
}

// ResFromMode turns an r_mode number into a resolution name: -2 is the desktop, a table
// mode its size. -1 (the custom size) and unknown numbers give "".
func ResFromMode(mode int) string {
	if mode == -2 {
		return Desktop
	}
	if mode >= 0 && mode < len(legacyModes) {
		m := legacyModes[mode]
		r, err := ParseRes(strconv.Itoa(m[0]) + "x" + strconv.Itoa(m[1]))
		if err == nil {
			return r.Name
		}
	}
	return ""
}
