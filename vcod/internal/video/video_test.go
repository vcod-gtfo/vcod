package video

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRes(t *testing.T) {
	cases := map[string]Res{
		"1080p":     {"1080p", 1920, 1080},
		"1080P":     {"1080p", 1920, 1080},
		"1920x1080": {"1080p", 1920, 1080},
		"1366×768":  {"1366x768", 1366, 768},
		"desktop":   {"desktop", 0, 0},
		"uw1440":    {"uw1440", 3440, 1440},
	}
	for in, want := range cases {
		got, err := ParseRes(in)
		if err != nil || got != want {
			t.Fatalf("%q: got %+v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "big", "100x100", "1920x", "x1080", "99999x2"} {
		if _, err := ParseRes(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestParseDisplayAndFPS(t *testing.T) {
	for in, want := range map[string]Display{"window": Window, "fs": Fullscreen, "2": Borderless, "Borderless": Borderless} {
		if got, err := ParseDisplay(in); err != nil || got != want {
			t.Fatalf("%q: %v %v", in, got, err)
		}
	}
	if IsDisplay("1") || !IsDisplay("fullscreen") {
		t.Fatal("IsDisplay")
	}
	for in, want := range map[string]int{"125": 125, "333fps": 333, "333": 333, "777": 777, "off": 0, "0": 0, "unlocked": 0, "Unlimited": 0} {
		if got, err := ParseFPS(in); err != nil || got != want {
			t.Fatalf("%q: %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"-1", "1001", "fast"} {
		if _, err := ParseFPS(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestCvars(t *testing.T) {
	r, _ := ParseRes("1080p")
	s := Settings{Res: r, Display: Window, MaxFPS: 125}
	got := strings.Join(s.Cvars(true), " ")
	if got != "+set r_fullscreen 0 +set r_mode -1 +set r_customwidth 1920 +set r_customheight 1080 +set com_maxfps 125" {
		t.Fatal(got)
	}
	if got := strings.Join(s.Cvars(false), " "); got != "+set com_maxfps 125" {
		t.Fatal(got)
	}
	d, _ := ParseRes("desktop")
	s = Settings{Res: d, Display: Borderless, MaxFPS: 0}
	if got := strings.Join(s.Cvars(true), " "); got != "+set r_fullscreen 2 +set r_mode -2 +set com_maxfps 0" {
		t.Fatal(got)
	}
	mode := 3
	s.Mode = &mode
	if got := strings.Join(s.Cvars(true), " "); !strings.Contains(got, "r_mode 3") || strings.Contains(got, "custom") {
		t.Fatal(got)
	}
}

func TestNotes(t *testing.T) {
	r, _ := ParseRes("720p")
	if n := (Settings{Res: r, Display: Borderless, MaxFPS: 125}).Notes(); len(n) != 1 || !strings.Contains(n[0], "borderless") {
		t.Fatal(n)
	}
	if n := (Settings{Res: r, Display: Window, MaxFPS: 333}).Notes(); len(n) != 1 || !strings.Contains(n[0], "60–250") {
		t.Fatal(n)
	}
	if n := (Settings{Res: r, Display: Window, MaxFPS: 125}).Notes(); len(n) != 0 {
		t.Fatal(n)
	}
}

func TestResFromMode(t *testing.T) {
	if ResFromMode(-2) != "desktop" || ResFromMode(6) != "1024x768" || ResFromMode(-1) != "" || ResFromMode(99) != "" {
		t.Fatal(ResFromMode(6))
	}
}

func TestGPU(t *testing.T) {
	hybrid := Probe{NvidiaModel: "NVIDIA RTX 500", Hybrid: true, EGLNvidia: "/x/10_nvidia.json", EGLMesa: "/x/50_mesa.json"}
	only := Probe{NvidiaModel: "NVIDIA RTX 4090"}
	none := Probe{Hybrid: true}
	if g := GPUAuto.Resolve(hybrid); g != GPUNvidia {
		t.Fatalf("auto on hybrid = %s", g)
	}
	if g := GPUAuto.Resolve(only); g != GPUAuto {
		t.Fatalf("auto on nvidia-only = %s", g)
	}
	if g := GPUAuto.Resolve(none); g != GPUAuto || g.Env(none) != nil {
		t.Fatalf("auto without nvidia = %s %v", g, g.Env(none))
	}
	env := strings.Join(GPUNvidia.Env(hybrid), " ")
	for _, want := range []string{"__NV_PRIME_RENDER_OFFLOAD=1", "__GLX_VENDOR_LIBRARY_NAME=nvidia", "__EGL_VENDOR_LIBRARY_FILENAMES=/x/10_nvidia.json"} {
		if !strings.Contains(env, want) {
			t.Fatalf("nvidia env %q lacks %s", env, want)
		}
	}
	if env := strings.Join(GPUIntegrated.Env(hybrid), " "); !strings.Contains(env, "/x/50_mesa.json") || !strings.Contains(env, "__NV_PRIME_RENDER_OFFLOAD=0") {
		t.Fatalf("integrated env %q", env)
	}
	for in, want := range map[string]GPU{"dgpu": GPUNvidia, "NVIDIA": GPUNvidia, "igpu": GPUIntegrated, "": GPUAuto} {
		if g, err := ParseGPU(in); err != nil || g != want {
			t.Fatalf("ParseGPU(%q) = %s, %v", in, g, err)
		}
	}
	if IsGPUWord("auto") || !IsGPUWord("nvidia") || IsGPUWord("1080p") {
		t.Fatal("IsGPUWord")
	}
	if !RendererIsNvidia("NVIDIA RTX 500 Ada Generation Laptop GPU/PCIe/SSE2") || RendererIsNvidia("Mesa Intel(R) Arc(tm) Graphics (MTL)") {
		t.Fatal("RendererIsNvidia")
	}
}

func TestDetectFake(t *testing.T) {
	root := t.TempDir()
	mk := func(p, body string) {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("proc/0000:01:00.0/information", "Model: \t\t NVIDIA RTX 500 Ada Generation Laptop GPU\nIRQ: 216\n")
	mk("drm/card0/device/vendor", "0x10de\n")
	mk("drm/card1/device/vendor", "0x8086\n")
	mk("drm/card1-eDP-1/status", "connected\n")
	mk("egl/10_nvidia.json", "{}")
	mk("egl/50_mesa.json", "{}")
	oldP, oldD, oldE := procNvidia, sysDRM, eglVendorDs
	defer func() { procNvidia, sysDRM, eglVendorDs = oldP, oldD, oldE }()
	procNvidia, sysDRM, eglVendorDs = filepath.Join(root, "proc"), filepath.Join(root, "drm"), []string{filepath.Join(root, "egl")}
	p := Detect()
	if p.NvidiaModel != "NVIDIA RTX 500 Ada Generation Laptop GPU" || !p.Hybrid || filepath.Base(p.EGLNvidia) != "10_nvidia.json" || filepath.Base(p.EGLMesa) != "50_mesa.json" {
		t.Fatalf("Detect = %+v", p)
	}
}
