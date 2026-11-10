// Package config resolves which workspace, flavor, map and ports the commands use.
//
// Precedence, highest first:
//
//  1. the global flag (--flavor) for the flavor
//  2. variables exported in the shell, VCOD_<KEY> (VCOD_MAP=mp_carentan vcod play)
//  3. ~/.config/vcod/config, KEY=VALUE lines written by `vcod setup` or `config set`
//  4. built-in defaults, and for WORKSPACE the folder found around this binary or the
//     working directory
package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"vcod/internal/install"
)

// Flavor is a protocol family: retail 1.5 (protocol 6) or COD1.6X (protocol 10). Each
// has a server flavor and a client flavor under flavors/.
type Flavor string

// The flavors.
const (
	F15  Flavor = "1.5"
	F16x Flavor = "1.6x"
)

// Flavors in the order they are offered.
var Flavors = []Flavor{F15, F16x}

// ParseFlavor validates a flavor name, accepting the folder names too.
func ParseFlavor(s string) (Flavor, error) {
	v := strings.ToLower(strings.TrimSpace(s))
	v = strings.TrimPrefix(v, "server-")
	v = strings.TrimPrefix(v, "client-")
	switch v {
	case "1.5", "15", "retail", "vanilla":
		return F15, nil
	case "1.6x", "1.6", "16x", "16", "cod1x", "cod1plus":
		return F16x, nil
	}
	return "", fmt.Errorf("unknown flavor %q. Valid: 1.5, 1.6x", s)
}

// Keys the file understands, with their defaults ("" = none or computed).
const (
	KeyWorkspace       = "WORKSPACE"
	KeyFlavor          = "FLAVOR"
	KeyMap             = "MAP"
	KeyGametype        = "GAMETYPE"
	KeyPort15          = "PORT_1_5"
	KeyPort16x         = "PORT_1_6X"
	KeyRcon            = "RCON_PASSWORD"
	KeyCheats          = "CHEATS"
	KeyServerExtra     = "SERVER_EXTRA"
	KeyClientBuild     = "CLIENT_BUILD"
	KeyClientHome      = "CLIENT_HOME"
	KeyClientMode      = "CLIENT_MODE"
	KeyClientFull      = "CLIENT_FULLSCREEN"
	KeyClientExtra     = "CLIENT_EXTRA"
	KeyAllowUnversion  = "ALLOW_UNVERSIONED"
	KeyReadyTimeoutSec = "READY_TIMEOUT"
)

// Keys lists every key with its default and meaning, in the order config shows them.
var Keys = []struct{ Key, Default, Help string }{
	{KeyWorkspace, "", "the codworkspace folder (holds cod_rea/ and flavors/)"},
	{KeyFlavor, "1.5", "default flavor: 1.5 or 1.6x"},
	{KeyMap, "mp_harbor", "default map for server start and play"},
	{KeyGametype, "tdm", "default gametype: dm tdm sd re bel hq"},
	{KeyPort15, "28960", "UDP port of the 1.5 server"},
	{KeyPort16x, "28961", "UDP port of the 1.6x server"},
	{KeyRcon, "", "rconPassword the servers are started with (generated once)"},
	{KeyCheats, "0", "1 = start servers with devmap (sv_cheats 1)"},
	{KeyServerExtra, "", "more +set arguments for every server"},
	{KeyClientBuild, "video", "video (SDL window) or headless"},
	{KeyClientHome, "", "fs_homepath root; each flavor gets home-<flavor> inside"},
	{KeyClientMode, "-1", "r_mode for the video client (-1 = desktop size)"},
	{KeyClientFull, "0", "1 = fullscreen video client"},
	{KeyClientExtra, "", "more +set arguments for every client"},
	{KeyAllowUnversion, "1", "1.6x server: let clients without a 1.6X build string in"},
	{KeyReadyTimeoutSec, "180", "seconds play waits for a server to answer"},
}

// Config is every resolved setting.
type Config struct {
	Flavor         Flavor
	FlavorFromFlag bool
	// Workspace is the codworkspace root; WorkspaceSource says how it was found.
	Workspace       string
	WorkspaceSource string

	Path    string
	HasFile bool

	raw      map[string]string
	fileKeys []string
}

// Load resolves the configuration. flavorFlag is the --flavor value when fromFlag is set.
func Load(flavorFlag string, fromFlag bool) (*Config, error) {
	paths, err := install.Resolve()
	if err != nil {
		return nil, err
	}
	return LoadFrom(paths.ConfigFile(), flavorFlag, fromFlag)
}

// LoadFrom is Load against an explicit file, for tests.
func LoadFrom(path, flavorFlag string, fromFlag bool) (*Config, error) {
	c := &Config{Path: path, raw: map[string]string{}}
	if err := c.readFile(); err != nil {
		return nil, err
	}
	name := c.Get(KeyFlavor)
	if fromFlag {
		name = flavorFlag
	}
	f, err := ParseFlavor(name)
	if err != nil {
		return nil, err
	}
	c.Flavor, c.FlavorFromFlag = f, fromFlag
	c.Workspace, c.WorkspaceSource = c.findWorkspace()
	return c, nil
}

// Get reads a key: the VCOD_-prefixed environment first, then the file, then the
// default.
func (c *Config) Get(key string) string {
	if v := strings.TrimSpace(os.Getenv("VCOD_" + key)); v != "" {
		return v
	}
	if v := strings.TrimSpace(c.raw[key]); v != "" {
		return v
	}
	for _, k := range Keys {
		if k.Key == key {
			return k.Default
		}
	}
	return ""
}

// Source says where a key's value comes from: "env", "file" or "default".
func (c *Config) Source(key string) string {
	if strings.TrimSpace(os.Getenv("VCOD_"+key)) != "" {
		return "env"
	}
	if strings.TrimSpace(c.raw[key]) != "" {
		return "file"
	}
	return "default"
}

// Int reads a key as an integer, falling back to def when it is not one.
func (c *Config) Int(key string, def int) int {
	if n, err := strconv.Atoi(c.Get(key)); err == nil {
		return n
	}
	return def
}

// Bool reads a 0/1 key.
func (c *Config) Bool(key string) bool {
	switch strings.ToLower(c.Get(key)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// Port is the UDP port of a flavor's server.
func (c *Config) Port(f Flavor) int {
	if f == F16x {
		return c.Int(KeyPort16x, 28961)
	}
	return c.Int(KeyPort15, 28960)
}

// Rcon is the rcon password, generated and stored on first use so every server this
// tool starts can be reached by `vcod rcon`.
func (c *Config) Rcon() string {
	if v := c.Get(KeyRcon); v != "" {
		return v
	}
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return "vcod"
	}
	pw := "vcod" + hex.EncodeToString(b)
	if err := Save(c.Path, map[string]string{KeyRcon: pw}); err == nil {
		c.raw[KeyRcon] = pw
	}
	return pw
}

// ClientHome is the fs_homepath for a flavor's client.
func (c *Config) ClientHome(f Flavor) string {
	root := c.Get(KeyClientHome)
	if root == "" {
		if p, err := install.Resolve(); err == nil {
			root = p.DataDir
		}
	}
	return filepath.Join(expandHome(root), "home-"+string(f))
}

// FileValue reads a key from the file only.
func (c *Config) FileValue(key string) string { return c.raw[key] }

// FileKeys lists the keys the file sets, in file order.
func (c *Config) FileKeys() []string { return append([]string(nil), c.fileKeys...) }

// IsWorkspace reports whether dir holds both cod_rea/ and flavors/.
func IsWorkspace(dir string) bool {
	if dir == "" {
		return false
	}
	for _, sub := range []string{"cod_rea", "flavors"} {
		if st, err := os.Stat(filepath.Join(dir, sub)); err != nil || !st.IsDir() {
			return false
		}
	}
	return true
}

// findWorkspace resolves WORKSPACE: the setting when there is one, else the workspace
// around the vcod source tree this install was built from, around the running binary,
// or around the working directory.
func (c *Config) findWorkspace() (string, string) {
	if v := c.Get(KeyWorkspace); v != "" {
		return expandHome(v), c.Source(KeyWorkspace)
	}
	if p, err := install.Resolve(); err == nil {
		if src := p.Source(); src != "" {
			if dir := filepath.Dir(src); IsWorkspace(dir) {
				return dir, "install source"
			}
		}
	}
	if dir := walkUp(install.Root()); dir != "" {
		return dir, "binary location"
	}
	if wd, err := os.Getwd(); err == nil {
		if dir := walkUp(wd); dir != "" {
			return dir, "working directory"
		}
	}
	return "", ""
}

func walkUp(dir string) string {
	for d := dir; d != "" && d != string(os.PathSeparator); d = filepath.Dir(d) {
		if IsWorkspace(d) {
			return d
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	return ""
}

// RequireWorkspace explains how to point vcod at the workspace when none was found.
func (c *Config) RequireWorkspace() error {
	if c.Workspace == "" {
		return fmt.Errorf("no workspace found (a folder holding cod_rea/ and flavors/). Run vcod from inside it, or: vcod config set WORKSPACE <path>")
	}
	if !IsWorkspace(c.Workspace) {
		return fmt.Errorf("%s is not a workspace: it needs cod_rea/ and flavors/ (WORKSPACE from %s)", c.Workspace, c.WorkspaceSource)
	}
	return nil
}

func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func (c *Config) readFile() error {
	f, err := os.Open(c.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	c.HasFile = true
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, value, ok := parseLine(sc.Text())
		if !ok {
			continue
		}
		if _, seen := c.raw[key]; !seen {
			c.fileKeys = append(c.fileKeys, key)
		}
		c.raw[key] = value
	}
	return sc.Err()
}

// parseLine reads one KEY=VALUE line, ignoring blanks and comments. Values may be
// quoted; an `export ` prefix is tolerated so a file can double as a shell snippet.
func parseLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	line = strings.TrimPrefix(line, "export ")
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return "", "", false
	}
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		value = value[1 : len(value)-1]
	}
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

// KnownKey reports whether key is one the file understands.
func KnownKey(key string) bool {
	for _, k := range Keys {
		if k.Key == key {
			return true
		}
	}
	return false
}

// Save writes updates into the file at path, keeping every other line as it is. The
// file is assembled in a temporary file and renamed into place with mode 0600. An empty
// value removes the key.
func Save(path string, updates map[string]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var lines []string
	if b, err := os.ReadFile(path); err == nil {
		lines = strings.Split(strings.TrimRight(string(b), "\n"), "\n")
		if len(lines) == 1 && lines[0] == "" {
			lines = nil
		}
	}
	pending := map[string]string{}
	for k, v := range updates {
		pending[k] = v
	}
	kept := lines[:0]
	for _, line := range lines {
		key, _, ok := parseLine(line)
		if ok {
			if v, want := pending[key]; want {
				delete(pending, key)
				if v == "" {
					continue
				}
				line = key + "=" + v
			}
		}
		kept = append(kept, line)
	}
	lines = kept
	if len(lines) == 0 {
		lines = append(lines, "# vcod configuration — written by `vcod setup` / `vcod config set`. Mode 0600.")
	}
	keys := make([]string, 0, len(pending))
	for k := range pending {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if pending[k] != "" {
			lines = append(lines, k+"="+pending[k])
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// Mask shows enough of a secret to recognise it: the first four characters, an
// ellipsis, and the last two.
func Mask(secret string) string {
	if secret == "" {
		return ""
	}
	if len(secret) <= 8 {
		return strings.Repeat("•", len(secret))
	}
	return secret[:4] + "…" + secret[len(secret)-2:]
}
