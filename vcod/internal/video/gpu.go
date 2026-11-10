package video

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GPU is which graphics chip the video client renders on.
//
// On a hybrid laptop (NVIDIA Optimus) the integrated chip drives the screen and
// every program renders on it unless it asks for the NVIDIA card through the driver's
// render offload environment. The client's SDL picks its GL library through glvnd: GLX
// under X11 (__GLX_VENDOR_LIBRARY_NAME) and EGL under Wayland
// (__EGL_VENDOR_LIBRARY_FILENAMES), so both are set.
type GPU string

const (
	// GPUAuto renders on the NVIDIA card when the machine is hybrid, and leaves the
	// system's choice alone otherwise.
	GPUAuto GPU = "auto"
	// GPUNvidia asks for the NVIDIA card through PRIME render offload.
	GPUNvidia GPU = "nvidia"
	// GPUIntegrated keeps the client on the chip that drives the screen.
	GPUIntegrated GPU = "integrated"
)

// GPUs lists the choices, for completion and help.
var GPUs = []GPU{GPUAuto, GPUNvidia, GPUIntegrated}

// ParseGPU reads a GPU choice or one of its short forms.
func ParseGPU(s string) (GPU, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "auto", "default", "":
		return GPUAuto, nil
	case "nvidia", "nv", "dgpu", "dedicated", "discrete", "prime":
		return GPUNvidia, nil
	case "integrated", "igpu", "intel", "internal":
		return GPUIntegrated, nil
	}
	return "", fmt.Errorf("unknown gpu %q: auto, nvidia or integrated", s)
}

// IsGPUWord reports whether s names nvidia or integrated on its own. "auto" is left
// out: among other display words it should be written "gpu auto".
func IsGPUWord(s string) bool {
	g, err := ParseGPU(s)
	return err == nil && g != GPUAuto
}

// Probe is what the machine has: the NVIDIA card the driver sees, whether another
// chip drives a display, and the glvnd EGL vendor files.
type Probe struct {
	NvidiaModel string // "" when the NVIDIA driver sees no card
	Hybrid      bool   // a non-NVIDIA DRM card is present too
	EGLNvidia   string // path of the NVIDIA EGL vendor file, "" when missing
	EGLMesa     string // path of the Mesa EGL vendor file, "" when missing
}

// Roots for Detect; tests point them at a fake tree.
var (
	procNvidia  = "/proc/driver/nvidia/gpus"
	sysDRM      = "/sys/class/drm"
	eglVendorDs = []string{"/etc/glvnd/egl_vendor.d", "/usr/share/glvnd/egl_vendor.d"}
)

// Detect looks at the machine. It only reads /proc and /sys, so it is cheap.
func Detect() Probe {
	var p Probe
	infos, _ := filepath.Glob(filepath.Join(procNvidia, "*", "information"))
	for _, f := range infos {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			if v, ok := strings.CutPrefix(line, "Model:"); ok {
				p.NvidiaModel = strings.TrimSpace(v)
				break
			}
		}
		if p.NvidiaModel != "" {
			break
		}
	}
	cards, _ := filepath.Glob(filepath.Join(sysDRM, "card[0-9]*"))
	for _, c := range cards {
		if strings.Contains(filepath.Base(c), "-") {
			continue // a connector, not a card
		}
		b, err := os.ReadFile(filepath.Join(c, "device", "vendor"))
		if err != nil {
			continue
		}
		if v := strings.TrimSpace(string(b)); v != "" && v != "0x10de" {
			p.Hybrid = true
		}
	}
	for _, d := range eglVendorDs {
		if p.EGLNvidia == "" {
			if m, _ := filepath.Glob(filepath.Join(d, "*nvidia*.json")); len(m) > 0 {
				p.EGLNvidia = m[0]
			}
		}
		if p.EGLMesa == "" {
			if m, _ := filepath.Glob(filepath.Join(d, "*mesa*.json")); len(m) > 0 {
				p.EGLMesa = m[0]
			}
		}
	}
	return p
}

// Resolve turns a choice into what is asked of the system: nvidia, integrated, or
// auto when nothing should be set (no NVIDIA card, or one that drives the screen
// itself).
func (g GPU) Resolve(p Probe) GPU {
	if g != GPUAuto {
		return g
	}
	if p.NvidiaModel != "" && p.Hybrid {
		return GPUNvidia
	}
	return GPUAuto
}

// Env is the environment that asks for a resolved choice.
func (g GPU) Env(p Probe) []string {
	switch g {
	case GPUNvidia:
		env := []string{
			"__NV_PRIME_RENDER_OFFLOAD=1",
			"__GLX_VENDOR_LIBRARY_NAME=nvidia",
			"__VK_LAYER_NV_optimus=NVIDIA_only",
		}
		if p.EGLNvidia != "" {
			env = append(env, "__EGL_VENDOR_LIBRARY_FILENAMES="+p.EGLNvidia)
		}
		return env
	case GPUIntegrated:
		env := []string{
			"__NV_PRIME_RENDER_OFFLOAD=0",
			"__GLX_VENDOR_LIBRARY_NAME=mesa",
			"DRI_PRIME=0",
		}
		if p.EGLMesa != "" {
			env = append(env, "__EGL_VENDOR_LIBRARY_FILENAMES="+p.EGLMesa)
		}
		return env
	}
	return nil
}

// Describe is a choice and what it comes to on this machine, for display:
// "auto → nvidia (NVIDIA RTX 500 …)".
func (g GPU) Describe(p Probe) string {
	r := g.Resolve(p)
	what := string(r)
	switch r {
	case GPUNvidia:
		if p.NvidiaModel != "" {
			what = "nvidia (" + p.NvidiaModel + ")"
		} else {
			what = "nvidia (the driver sees no NVIDIA card)"
		}
	case GPUAuto:
		what = "the system's choice"
	}
	if g == GPUAuto {
		return "auto → " + what
	}
	return what
}

// RendererIsNvidia reports whether a GL_RENDERER string is the NVIDIA driver's.
func RendererIsNvidia(r string) bool {
	return strings.Contains(strings.ToUpper(r), "NVIDIA") && !strings.Contains(strings.ToLower(r), "nouveau")
}
