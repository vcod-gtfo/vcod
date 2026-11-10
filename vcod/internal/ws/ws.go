// Package ws is the workspace layout: where cod_rea, its build trees, the flavors, the
// run script and the test scripts are, and whether a build is older than its sources.
package ws

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Workspace is a codworkspace root.
type Workspace struct {
	Root string
}

// New wraps a root folder.
func New(root string) *Workspace { return &Workspace{Root: root} }

// CodRea is the source port.
func (w *Workspace) CodRea() string { return filepath.Join(w.Root, "cod_rea") }

// Flavors is the folder holding the four installs.
func (w *Workspace) Flavors() string { return filepath.Join(w.Root, "flavors") }

// ServerDir is a flavor's server install, e.g. flavors/server-1.5.
func (w *Workspace) ServerDir(flavor string) string {
	return filepath.Join(w.Flavors(), "server-"+flavor)
}

// ClientDir is a flavor's client install, e.g. flavors/client-1.6x.
func (w *Workspace) ClientDir(flavor string) string {
	return filepath.Join(w.Flavors(), "client-"+flavor)
}

// RunScript starts a server flavor in its podman container.
func (w *Workspace) RunScript() string { return filepath.Join(w.CodRea(), "server", "run.sh") }

// MakeFlavors builds the flavor folders.
func (w *Workspace) MakeFlavors() string {
	return filepath.Join(w.CodRea(), "server", "make_flavors.sh")
}

// Tools is cod_rea/tools, where the live test scripts are.
func (w *Workspace) Tools() string { return filepath.Join(w.CodRea(), "tools") }

// Screens is where the tests write their screenshots and contact sheets.
func (w *Workspace) Screens() string { return filepath.Join(w.CodRea(), "work", "screens") }

// Build is one of cod_rea's CMake build trees.
type Build struct {
	// Name is what the user types: "headless" or "video".
	Name string
	// Dir is the folder name under cod_rea.
	Dir string
	// Flags are the extra cmake configure flags.
	Flags []string
	// Summary is the one-line description.
	Summary string
}

// Builds are the two trees, headless first (the tests and ctest use it).
var Builds = []Build{
	{Name: "headless", Dir: "build", Summary: "cod_client (headless), cod_server, cod_probe, the ctests"},
	{Name: "video", Dir: "build-video", Flags: []string{"-DCOD_CLIENT_VIDEO=ON"}, Summary: "cod_client with the SDL window, GL renderer and sound"},
}

// LookupBuild finds a build by name or folder.
func LookupBuild(name string) *Build {
	for i := range Builds {
		if Builds[i].Name == name || Builds[i].Dir == name {
			return &Builds[i]
		}
	}
	return nil
}

// BuildDir is a build tree's absolute folder.
func (w *Workspace) BuildDir(b *Build) string { return filepath.Join(w.CodRea(), b.Dir) }

// ClientBinary is the cod_client of a build tree.
func (w *Workspace) ClientBinary(b *Build) string { return filepath.Join(w.BuildDir(b), "cod_client") }

// SourcesNewest is the newest modification time among cod_rea's sources: src/ and the
// top CMakeLists.txt. A binary older than this is stale.
func (w *Workspace) SourcesNewest() (time.Time, string) {
	var newest time.Time
	var which string
	consider := func(path string, t time.Time) {
		if t.After(newest) {
			newest, which = t, path
		}
	}
	if st, err := os.Stat(filepath.Join(w.CodRea(), "CMakeLists.txt")); err == nil {
		consider("CMakeLists.txt", st.ModTime())
	}
	src := filepath.Join(w.CodRea(), "src")
	_ = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch filepath.Ext(path) {
		case ".c", ".h", ".cpp", ".txt", ".cmake":
		default:
			return nil
		}
		if info, err := d.Info(); err == nil {
			rel, _ := filepath.Rel(w.CodRea(), path)
			consider(rel, info.ModTime())
		}
		return nil
	})
	return newest, which
}

// BuildState is how a build tree's client relates to the sources.
type BuildState struct {
	Build   *Build
	Binary  string
	Exists  bool
	Built   time.Time
	Stale   bool
	Newest  time.Time
	NewFile string
}

// State reports whether a build's cod_client exists and is newer than the sources.
func (w *Workspace) State(b *Build) BuildState {
	s := BuildState{Build: b, Binary: w.ClientBinary(b)}
	if st, err := os.Stat(s.Binary); err == nil {
		s.Exists, s.Built = true, st.ModTime()
	}
	s.Newest, s.NewFile = w.SourcesNewest()
	s.Stale = s.Exists && s.Newest.After(s.Built)
	return s
}

// Test is one live test script in cod_rea/tools, or the ctest suite.
type Test struct {
	Name    string
	Script  string
	Summary string
	Usage   string
	// StartsServers is set when the script starts a flavor server itself, which replaces
	// one this tool started (run.sh removes a container of the same name first).
	StartsServers bool
}

// Tests lists ctest plus every tools/*test*.sh, with the summary taken from the
// script's header comment.
func (w *Workspace) Tests() []Test {
	out := []Test{{Name: "ctest", Summary: "the unit and oracle tests (ctest in build/)", Usage: "ctest [-R regex]"}}
	matches, _ := filepath.Glob(filepath.Join(w.Tools(), "*test*.sh"))
	sort.Strings(matches)
	for _, path := range matches {
		name := strings.TrimSuffix(filepath.Base(path), ".sh")
		t := Test{Name: name, Script: path}
		t.Summary, t.Usage, t.StartsServers = describe(path, name)
		out = append(out, t)
	}
	return out
}

// LookupTest finds a test by name, with or without .sh.
func (w *Workspace) LookupTest(name string) *Test {
	name = strings.TrimSuffix(name, ".sh")
	for _, t := range w.Tests() {
		if t.Name == name {
			return &t
		}
	}
	return nil
}

// describe reads a script's header: the first comment sentence as the summary, a line
// with the script's own name as the usage, and whether it starts flavor servers.
func describe(path, name string) (summary, usage string, servers bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	inHeader, headDone := true, false
	var head []string
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, "run.sh") || strings.Contains(line, "flavors/server-") {
			servers = true
		}
		if !inHeader {
			continue
		}
		if strings.HasPrefix(line, "#!") {
			continue
		}
		if !strings.HasPrefix(line, "#") {
			inHeader = false
			continue
		}
		text := strings.TrimSpace(strings.TrimPrefix(line, "#"))
		if usage == "" && strings.Contains(text, name+".sh") {
			usage = text
			if i := strings.Index(usage, name+".sh"); i >= 0 {
				usage = usage[i:]
			}
			if i := strings.Index(usage, "#"); i > 0 {
				usage = strings.TrimSpace(usage[:i])
			}
			if i := strings.Index(usage, ": "); i > 0 {
				// "svtest.sh [seconds]: live check of ..." — the summary starts after it.
				head = append(head, strings.TrimSpace(usage[i+1:]))
				usage = strings.TrimSpace(usage[:i])
			}
			continue
		}
		if headDone {
			continue
		}
		// The summary is the first paragraph; a blank comment line or a numbered list
		// ends it.
		if text == "" || (len(text) > 1 && text[0] >= '0' && text[0] <= '9' && text[1] == '.') {
			headDone = len(head) > 0
			continue
		}
		head = append(head, text)
	}
	summary = firstSentence(strings.Join(head, " "))
	return summary, strings.TrimSuffix(usage, "."), servers
}

func firstSentence(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ". "); i > 0 {
		s = s[:i]
	}
	s = strings.TrimSuffix(strings.TrimSuffix(s, ":"), ".")
	if r := []rune(s); len(r) > 90 {
		s = string(r[:87]) + "..."
	}
	return s
}
