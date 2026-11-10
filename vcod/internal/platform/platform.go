// Package platform is the small set of things that genuinely differ per machine: how
// to name the OS, how wide the terminal is, whether stdout is a terminal.
package platform

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"golang.org/x/term"
)

// IsMacOS reports whether this build is running on macOS.
func IsMacOS() bool { return runtime.GOOS == "darwin" }

// OS returns a human readable operating system name for banners and doctor.
func OS() string {
	switch runtime.GOOS {
	case "linux":
		if name := osReleasePretty(); name != "" {
			return name
		}
		return "Linux"
	case "darwin":
		if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
			if v := strings.TrimSpace(string(out)); v != "" {
				return "macOS " + v
			}
		}
		return "macOS"
	default:
		return runtime.GOOS
	}
}

func osReleasePretty() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return ""
	}
	defer f.Close()

	var name string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"'`)
		switch key {
		case "PRETTY_NAME":
			return value
		case "NAME":
			name = value
		}
	}
	return name
}

// Arch reports the CPU architecture in uname's vocabulary (x86_64, arm64).
func Arch() string {
	if runtime.GOARCH == "amd64" {
		return "x86_64"
	}
	return runtime.GOARCH
}

// ReleaseTarget is the "<goos>-<goarch>" suffix of this machine's release artifact,
// spelled the way the Makefile spells it. Deliberately not OS()/Arch(): those are for
// humans, and "Ubuntu 24.04-x86_64" is a filename that never exists.
func ReleaseTarget() string {
	return runtime.GOOS + "-" + runtime.GOARCH
}

// TermCols is the terminal width, defaulting to 80 when stdout is not a terminal.
func TermCols() int {
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
			return w
		}
	}
	if v := os.Getenv("COLUMNS"); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return 80
}

// TermRows is the terminal height, 0 when it cannot be measured.
func TermRows() int {
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		if _, h, err := term.GetSize(int(f.Fd())); err == nil && h > 0 {
			return h
		}
	}
	return 0
}

// IsTTY reports whether stdout is an interactive terminal.
func IsTTY() bool { return term.IsTerminal(int(os.Stdout.Fd())) }

// IsTerminal reports whether f is an interactive terminal.
func IsTerminal(f *os.File) bool { return term.IsTerminal(int(f.Fd())) }

// IsStdinTTY reports whether stdin is interactive.
func IsStdinTTY() bool { return term.IsTerminal(int(os.Stdin.Fd())) }

// Editor honours $VISUAL then $EDITOR, then the first of a short list that exists.
func Editor() ([]string, error) {
	candidates := []string{os.Getenv("VISUAL"), os.Getenv("EDITOR"), "nano", "vim", "vi"}
	for _, c := range candidates {
		if strings.TrimSpace(c) == "" {
			continue
		}
		parts := strings.Fields(c)
		if _, err := exec.LookPath(parts[0]); err == nil {
			return parts, nil
		}
	}
	return nil, fmt.Errorf("no editor found: set $EDITOR")
}

// LookPath is exec.LookPath with the error swallowed.
func LookPath(bin string) (string, bool) {
	p, err := exec.LookPath(bin)
	return p, err == nil
}
