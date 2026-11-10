// Package install knows where vcod lives on disk: the install tree, the PATH symlink,
// the record of which source folder it was built from, the state and data folders, and
// the shell rc lines for completion.
//
// The install records the vcod source folder (SourceFile). That folder sits in a git
// checkout of Remote, so `vcod update` pulls Branch there and rebuilds when the sources
// changed; an install without one clones Remote into CloneDir first.
package install

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"vcod/internal/version"
)

// Binary is the command name.
const Binary = "vcod"

// moduleLine is the first line of this program's go.mod, which is what makes a
// directory a vcod source tree.
const moduleLine = "module " + Binary + "\n"

// EnvSource overrides the recorded source folder.
const EnvSource = "VCOD_SOURCE"

// Paths are the install locations, all overridable so a test never touches the real ones.
type Paths struct {
	// Prefix is the install directory, ~/.vcod by default.
	Prefix string
	// BinDir is where the symlink goes, ~/.local/bin by default.
	BinDir string
	// ConfigDir holds the config file and the shell history.
	ConfigDir string
	// StateDir holds the job records and their logs (servers and clients started here).
	StateDir string
	// DataDir holds the clients' fs_homepath folders (configs, screenshots, demos).
	DataDir string
}

// Resolve returns the install paths.
func Resolve() (*Paths, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	xdg := func(env, def string) string {
		if v := os.Getenv(env); v != "" {
			return v
		}
		return filepath.Join(home, def)
	}
	return &Paths{
		Prefix:    envOr("VCOD_PREFIX", filepath.Join(home, "."+Binary)),
		BinDir:    envOr("VCOD_BIN_DIR", filepath.Join(home, ".local", "bin")),
		ConfigDir: envOr("VCOD_CONFIG_DIR", filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), Binary)),
		StateDir:  envOr("VCOD_STATE_DIR", filepath.Join(xdg("XDG_STATE_HOME", ".local/state"), Binary)),
		DataDir:   envOr("VCOD_DATA_DIR", filepath.Join(xdg("XDG_DATA_HOME", ".local/share"), Binary)),
	}, nil
}

// BinaryPath is the installed binary's location.
func (p *Paths) BinaryPath() string { return filepath.Join(p.Prefix, Binary) }

// SymlinkPath is the PATH entry pointing at the installed binary.
func (p *Paths) SymlinkPath() string { return filepath.Join(p.BinDir, Binary) }

// SourceFile records the source folder `update` rebuilds from.
func (p *Paths) SourceFile() string { return filepath.Join(p.Prefix, ".source") }

// VersionFile records which source hash the install came from.
func (p *Paths) VersionFile() string { return filepath.Join(p.Prefix, version.RecordFile) }

// CompletionFile is the generated completion script.
func (p *Paths) CompletionFile() string { return filepath.Join(p.Prefix, Binary+"-completion.bash") }

// ConfigFile is the settings file.
func (p *Paths) ConfigFile() string { return filepath.Join(p.ConfigDir, "config") }

// JobsDir holds one record and one log per job.
func (p *Paths) JobsDir() string { return filepath.Join(p.StateDir, "jobs") }

// Source is the vcod source folder: the VCOD_SOURCE override, the folder recorded at
// install time, or the folder the running binary sits in when that is a source tree.
func (p *Paths) Source() string {
	if v := os.Getenv(EnvSource); v != "" {
		return v
	}
	if b, err := os.ReadFile(p.SourceFile()); err == nil {
		if dir := strings.TrimSpace(string(b)); IsSourceTree(dir) {
			return dir
		}
	}
	if root := Root(); IsSourceTree(root) {
		return root
	}
	return ""
}

// Root is the directory holding the running binary, with any symlink resolved.
func Root() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

// IsInstalled reports whether the running binary is the installed copy rather than one
// built in the source folder, so purge never deletes the source.
func IsInstalled() bool {
	paths, err := Resolve()
	if err != nil {
		return false
	}
	root := Root()
	if root == "" {
		return false
	}
	rootResolved, _ := filepath.EvalSymlinks(root)
	prefixResolved, _ := filepath.EvalSymlinks(paths.Prefix)
	if rootResolved != "" && prefixResolved != "" {
		return rootResolved == prefixResolved
	}
	return root == paths.Prefix
}

// IsSourceTree reports whether dir is a vcod source folder: its go.mod declares
// module vcod.
func IsSourceTree(dir string) bool {
	if dir == "" {
		return false
	}
	mod, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	return bytes.HasPrefix(mod, []byte(moduleLine))
}

// InPath reports whether a directory is on PATH.
func InPath(dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == dir {
			return true
		}
		if abs, err := filepath.Abs(p); err == nil && abs == dir {
			return true
		}
	}
	return false
}

// RCFiles are the shell startup files worth wiring completion into: only those that
// exist, and only for shells that are installed.
func RCFiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var out []string
	add := func(name string) {
		p := filepath.Join(home, name)
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}
	if _, err := exec.LookPath("bash"); err == nil {
		add(".bashrc")
		add(".bash_profile")
	}
	if _, err := exec.LookPath("zsh"); err == nil {
		add(".zshrc")
	}
	return out
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
