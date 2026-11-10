// Package installcmd is `vcod install`: put the binary somewhere stable, symlink it
// into PATH, write the completion script, and record where updates come from.
//
// It never touches ~/.config/vcod or the jobs under ~/.local/state/vcod.
package installcmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"vcod/internal/app"
	"vcod/internal/completion"
	"vcod/internal/install"
	"vcod/internal/platform"
	"vcod/internal/registry"
	"vcod/internal/ui"
)

const help = `Usage:
  vcod install          install or refresh this binary at ~/.vcod
  vcod install -f       reinstall even when it is already the installed copy
  vcod install --no-rc  do not touch any shell rc file

What it does:
  copies this binary to ~/.vcod/vcod
  symlinks ~/.local/bin/vcod into your PATH
  writes ~/.vcod/vcod-completion.bash and sources it from your rc files
  records the source folder, so 'vcod update' knows what to rebuild
  records the source hash it was built from, for the update check
  moves an old codctl install over: its config, jobs and client homes, then
  removes ~/.codctl, its symlink and its rc lines

Normally run through the build script: sh vcod/build.sh install.

Overrides: VCOD_PREFIX, VCOD_BIN_DIR, VCOD_NO_RC=1.`

// EnvNoRC skips the shell rc wiring.
const EnvNoRC = "VCOD_NO_RC"

func init() {
	registry.Register(registry.Command{
		Name:        "install",
		Summary:     "Install this binary and its completions",
		Args:        "[-f] [--no-rc]",
		Help:        help,
		Group:       registry.GroupEnvironment,
		Order:       20,
		NeedsConfig: false,
		Complete:    func([]string) []string { return []string{"-f", "--no-rc"} },
		Run:         run,
	})
}

func run(ctx *app.Context, args []string) int {
	force := false
	noRC := os.Getenv(EnvNoRC) == "1"
	for _, a := range args {
		switch a {
		case "-f", "--force":
			force = true
		case "--no-rc":
			noRC = true
		default:
			return ctx.Usage("unknown argument: %s  (try: vcod help install)", a)
		}
	}
	out := ctx.Out
	out.Banner("installer")
	out.Info("os     : %s (%s)", platform.OS(), platform.Arch())

	self, err := os.Executable()
	if err != nil {
		return ctx.Fail(fmt.Errorf("cannot locate the running binary: %w", err))
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	tree := filepath.Dir(self)
	out.Info("version: %s", ctx.Version)

	paths, err := install.Resolve()
	if err != nil {
		return ctx.Fail(err)
	}
	migrateLegacy(out, paths, noRC)
	if self == paths.BinaryPath() && !force {
		out.OK("already installed at %s", paths.BinaryPath())
		out.Note("rebuild and reinstall from the sources with: vcod update")
		return 0
	}
	if self == paths.BinaryPath() {
		// Copying a file over itself truncates it.
		out.OK("%s is this binary; refreshing the links only", paths.BinaryPath())
	} else {
		if err := os.MkdirAll(paths.Prefix, 0o755); err != nil {
			return ctx.Fail(err)
		}
		if err := InstallBinary(self, paths.BinaryPath()); err != nil {
			return ctx.Fail(err)
		}
		out.OK("installed %s", paths.BinaryPath())
	}
	if err := os.WriteFile(paths.VersionFile(), []byte(ctx.Version+"\n"), 0o644); err != nil {
		return ctx.Fail(err)
	}
	if install.IsSourceTree(tree) {
		if err := os.WriteFile(paths.SourceFile(), []byte(tree+"\n"), 0o644); err == nil {
			out.OK("update source: %s", tree)
		}
	} else if paths.Source() == "" {
		out.Note("no source folder recorded — 'vcod update' will need %s set", install.EnvSource)
	}

	if err := os.MkdirAll(paths.BinDir, 0o755); err != nil {
		return ctx.Fail(err)
	}
	_ = os.Remove(paths.SymlinkPath())
	if err := os.Symlink(paths.BinaryPath(), paths.SymlinkPath()); err != nil {
		return ctx.Fail(err)
	}
	out.OK("linked %s", paths.SymlinkPath())

	if err := os.WriteFile(paths.CompletionFile(), []byte(completion.Bash()), 0o644); err != nil {
		return ctx.Fail(err)
	}
	out.OK("wrote %s", paths.CompletionFile())
	if noRC {
		out.Info("skipping shell rc files")
		out.Note("add this yourself: . %s", paths.CompletionFile())
	} else {
		for _, rc := range install.RCFiles() {
			switch added, err := WireCompletion(rc, paths.CompletionFile()); {
			case err != nil:
				out.Warn("could not write %s: %v", rc, err)
			case added:
				out.OK("added completions to %s", filepath.Base(rc))
			default:
				out.Note("completions already wired into %s", filepath.Base(rc))
			}
		}
	}
	if _, err := os.Stat(paths.ConfigFile()); err == nil {
		out.OK("kept your configuration at %s", paths.ConfigFile())
	} else {
		out.Info("no configuration yet — defaults apply; %s asks for them", ui.StyleCyan.Render("vcod setup"))
	}

	out.Plain("")
	if install.InPath(paths.BinDir) {
		out.OK("run %s from anywhere", ui.StyleCyan.Render("vcod"))
		out.Note("open a new shell, or source your rc file, to pick up completions")
		return 0
	}
	out.Warn("%s is not on your PATH.", paths.BinDir)
	out.Plain("  add this to your shell profile:\n")
	out.Plain("    export PATH=\"%s:$PATH\"\n", paths.BinDir)
	out.Note("meanwhile the full path works: %s", paths.BinaryPath())
	return 0
}

// InstallBinary copies src to dst through a temporary name and renames, so an
// interrupted copy cannot leave a truncated binary.
func InstallBinary(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".new"
	outFile, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(outFile, in); err != nil {
		outFile.Close()
		os.Remove(tmp)
		return err
	}
	if err := outFile.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dst)
}

// RCComment is the comment written above the source line; purge finds the block by it.
const RCComment = "# vcod CLI completions"

// WireCompletion appends the completion source line to one shell rc file. It reports
// false with no error when the line is already there.
func WireCompletion(rc, completionFile string) (bool, error) {
	body, err := os.ReadFile(rc)
	if err != nil {
		return false, err
	}
	if strings.Contains(string(body), filepath.Base(completionFile)) {
		return false, nil
	}
	f, err := os.OpenFile(rc, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return false, err
	}
	defer f.Close()
	line := fmt.Sprintf("[ -f %s ] && . %s", completionFile, completionFile)
	if _, err := fmt.Fprintf(f, "\n%s\n%s\n", RCComment, line); err != nil {
		return false, err
	}
	return true, nil
}
