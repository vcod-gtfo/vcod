package installcmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"

	"vcod/internal/install"
	"vcod/internal/ui"
)

// The tool was called codctl before it became vcod. These are that install's names.
const (
	legacyName      = "codctl"
	legacyRCMarker  = "codctl-completion.bash"
	legacyRCComment = "# codctl CLI completions"
)

// migrateLegacy carries a codctl install over to vcod: it moves the configuration, the
// job records and the client homes to their vcod folders when those do not exist yet,
// then removes the old install tree, its PATH symlink and its rc lines. Each step is
// skipped when there is nothing to do, so running it again is harmless.
func migrateLegacy(out *ui.Printer, paths *install.Paths, noRC bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	xdg := func(env, def string) string {
		if v := os.Getenv(env); v != "" {
			return v
		}
		return filepath.Join(home, def)
	}
	moves := []struct{ from, to string }{
		{filepath.Join(xdg("XDG_CONFIG_HOME", ".config"), legacyName), paths.ConfigDir},
		{filepath.Join(xdg("XDG_STATE_HOME", ".local/state"), legacyName), paths.StateDir},
		{filepath.Join(xdg("XDG_DATA_HOME", ".local/share"), legacyName), paths.DataDir},
	}
	for _, m := range moves {
		if _, err := os.Stat(m.from); err != nil {
			continue
		}
		if _, err := os.Stat(m.to); err == nil {
			out.Note("both %s and %s exist; keeping %s", m.from, m.to, m.to)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(m.to), 0o755); err != nil {
			out.Warn("could not move %s: %v", m.from, err)
			continue
		}
		if err := os.Rename(m.from, m.to); err != nil {
			out.Warn("could not move %s: %v", m.from, err)
			continue
		}
		out.OK("moved %s → %s", m.from, m.to)
	}

	oldPrefix := filepath.Join(home, "."+legacyName)
	oldLink := filepath.Join(paths.BinDir, legacyName)
	if target, err := os.Readlink(oldLink); err == nil && strings.HasPrefix(target, oldPrefix+string(filepath.Separator)) {
		if os.Remove(oldLink) == nil {
			out.OK("removed the old %s", oldLink)
		}
	}
	if !noRC {
		for _, rc := range install.RCFiles() {
			if removed, err := stripLegacyRC(rc); err != nil {
				out.Warn("could not clean %s: %v", filepath.Base(rc), err)
			} else if removed {
				out.OK("removed the codctl completion lines from %s", filepath.Base(rc))
			}
		}
	}
	if _, err := os.Stat(filepath.Join(oldPrefix, legacyName)); err == nil && !install.IsSourceTree(oldPrefix) {
		if os.RemoveAll(oldPrefix) == nil {
			out.OK("removed the old install %s", oldPrefix)
		}
	}
}

// stripLegacyRC removes the codctl completion block from one rc file.
func stripLegacyRC(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	var kept []string
	removed := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if strings.Contains(line, legacyRCMarker) || strings.Contains(line, legacyRCComment) {
			removed = true
			if strings.Contains(line, legacyRCComment) && len(kept) > 0 && kept[len(kept)-1] == "" {
				kept = kept[:len(kept)-1]
			}
			continue
		}
		kept = append(kept, line)
	}
	scanErr := sc.Err()
	f.Close()
	if scanErr != nil || !removed {
		return false, scanErr
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	tmp := path + ".vcod.tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(kept, "\n")+"\n"), info.Mode().Perm()); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, path)
}
