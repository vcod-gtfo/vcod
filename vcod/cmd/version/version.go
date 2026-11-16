// Package versioncmd is `vcod version`, `vcod update` and `vcod purge`.
package versioncmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"vcod/internal/app"
	"vcod/internal/install"
	"vcod/internal/platform"
	"vcod/internal/registry"
	"vcod/internal/ui"
	"vcod/internal/version"
)

func init() {
	registry.Register(registry.Command{
		Name:    "version",
		Summary: "Show the build and whether the remote moved on",
		Help:    versionHelp,
		Group:   registry.GroupEnvironment,
		Order:   10,
		Run:     runVersion,
	})
	registry.Register(registry.Command{
		Name:     "update",
		Summary:  "Pull from the remote, rebuild and reinstall",
		Args:     "[-f]",
		Help:     updateHelp,
		Group:    registry.GroupEnvironment,
		Order:    30,
		Complete: func([]string) []string { return []string{"-f"} },
		Run:      runUpdate,
	})
	registry.Register(registry.Command{
		Name:    "purge",
		Summary: "Uninstall vcod",
		Help:    purgeHelp,
		Group:   registry.GroupEnvironment,
		Order:   40,
		Run:     runPurge,
	})
}

const versionHelp = `Prints the build this binary was made from (the first seven hex digits of a hash over
vcod's Go sources), where it is installed and which source folder it updates from.

The source folder lives in a git checkout: the workspace, or a clone of
` + install.Remote + `. version fetches ` + install.Branch + ` and says how many commits the checkout is behind, and whether
the local sources hash differently from this build. Either way: vcod update.`

func runVersion(ctx *app.Context, args []string) int {
	if len(args) > 0 {
		return ctx.Usage("version takes no arguments")
	}
	out := ctx.Out
	out.Plain("  %s %s", ui.StyleBrand.Render("vcod"), ui.StyleWhite.Render(version.Current()))
	paths, err := install.Resolve()
	if err != nil {
		return ctx.Fail(err)
	}
	where := install.Root()
	if install.IsInstalled() {
		where += "  " + ui.StyleDim.Render("(installed)")
	}
	out.Note("binary  %s", where)
	src := paths.Source()
	if src == "" {
		out.Note("remote  %s (%s)", install.RemoteURL(), install.Branch)
		out.Note("source  %s", ui.StyleYellow.Render("none — vcod update clones the remote into "+paths.CloneDir()))
		return 0
	}
	out.Note("source  %s", src)
	if root := install.GitRoot(ctx.Ctx, src); root != "" {
		reportRemote(ctx, root)
	} else {
		out.Note("        %s", ui.StyleDim.Render("not a git checkout; update only rebuilds it"))
	}
	h, err := version.SourceHash(src)
	switch {
	case err != nil:
		out.Warn("could not hash the sources: %v", err)
	case h == version.Current():
		out.OK("built from the sources in the checkout")
	default:
		out.Warn("the sources changed: %s → %s   run: vcod update", ui.StyleYellow.Render(version.Current()), ui.StyleGreen.Render(h))
	}
	return 0
}

// reportRemote fetches the branch and says how far the checkout is behind it.
func reportRemote(ctx *app.Context, root string) {
	out := ctx.Out
	head, _ := install.Git(ctx.Ctx, root, "rev-parse", "--short", "HEAD")
	branch, _ := install.Git(ctx.Ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	out.Note("remote  %s (%s)", install.UpdateRemote(ctx.Ctx, root), install.Branch)
	out.Note("commit  %s on %s", head, branch)
	tctx, cancel := context.WithTimeout(ctx.Ctx, 15*time.Second)
	defer cancel()
	remote := install.UpdateRemote(ctx.Ctx, root)
	if _, err := install.Git(tctx, root, "fetch", "--quiet", remote, install.Branch); err != nil {
		out.Warn("could not fetch %s: %v", remote, err)
		return
	}
	behind, _ := install.Git(ctx.Ctx, root, "rev-list", "--count", "HEAD..FETCH_HEAD")
	ahead, _ := install.Git(ctx.Ctx, root, "rev-list", "--count", "FETCH_HEAD..HEAD")
	switch {
	case behind != "" && behind != "0":
		out.Warn("%s commit(s) behind %s   run: vcod update", behind, install.Branch)
	default:
		out.OK("checkout is up to date with %s", install.Branch)
	}
	if ahead != "" && ahead != "0" {
		out.Note("%s local commit(s) not pushed", ahead)
	}
}

const updateHelp = `Usage:
  vcod update      pull ` + install.Branch + ` from the remote, rebuild and reinstall when the sources changed
  vcod update -f   rebuild and reinstall even when they did not

The source folder is the vcod/ folder of a git checkout: the workspace, or a clone of
` + install.Remote + `. update fast-forwards that checkout to its origin's ` + install.Branch + `,
then runs 'sh <source>/build.sh install', which compiles in a golang container (podman)
and installs the result. Local edits are kept: git refuses the fast-forward when a pulled
file would overwrite one, and update then rebuilds what is there. A checkout on another
branch, or with commits of its own, is not touched.

Without a source folder (a binary copied elsewhere), update clones the remote into
~/.vcod/src first. VCOD_REMOTE overrides the remote in either case.

Your configuration and the running jobs are left alone.`

func runUpdate(ctx *app.Context, args []string) int {
	force := false
	for _, a := range args {
		if a == "-f" || a == "--force" {
			force = true
		} else {
			return ctx.Usage("unknown argument: %s", a)
		}
	}
	paths, err := install.Resolve()
	if err != nil {
		return ctx.Fail(err)
	}
	out := ctx.Out
	src := paths.Source()
	if src == "" {
		clone := paths.CloneDir()
		if install.GitRoot(ctx.Ctx, clone) == "" {
			out.Info("no source folder; cloning %s into %s", install.RemoteURL(), clone)
			if err := os.MkdirAll(filepath.Dir(clone), 0o755); err != nil {
				return ctx.Fail(err)
			}
			if _, err := install.Git(ctx.Ctx, filepath.Dir(clone), "clone", "--quiet", "--branch", install.Branch, install.RemoteURL(), clone); err != nil {
				return ctx.Fail(err)
			}
		}
		src = filepath.Join(clone, install.SubDir)
		if !install.IsSourceTree(src) {
			ctx.Errorf("%s holds no vcod sources", src)
			return 1
		}
	}
	if root := install.GitRoot(ctx.Ctx, src); root != "" {
		pull(ctx, root)
	} else {
		out.Note("%s is not a git checkout; rebuilding it as it is", src)
	}
	if h, err := version.SourceHash(src); err == nil && h == version.Current() && !force {
		out.OK("already built from the current sources (%s)", h)
		out.Note("vcod update -f rebuilds anyway")
		return 0
	}
	cmd := exec.CommandContext(ctx.Ctx, "sh", filepath.Join(src, "build.sh"), "install")
	cmd.Dir = src
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if ctx.Out != ui.Std {
		w := &ui.LineWriter{Emit: func(l string) { ctx.Out.Plain("%s", l) }}
		cmd.Stdout, cmd.Stderr = w, w
		defer w.Flush()
	}
	if err := cmd.Run(); err != nil {
		return ctx.Fail(fmt.Errorf("build.sh install: %w", err))
	}
	if ctx.Out != ui.Std {
		ctx.Out.Note("the shell keeps running the old binary until you restart it")
	}
	return 0
}

// pull fast-forwards the checkout at root to the remote branch. Every failure is a
// warning: update goes on to rebuild whatever the checkout holds.
func pull(ctx *app.Context, root string) {
	out := ctx.Out
	if b, _ := install.Git(ctx.Ctx, root, "rev-parse", "--abbrev-ref", "HEAD"); b != install.Branch {
		out.Warn("%s is on branch %q, not %s; not pulling", root, b, install.Branch)
		return
	}
	remote := install.UpdateRemote(ctx.Ctx, root)
	out.Info("fetching %s %s", remote, install.Branch)
	if _, err := install.Git(ctx.Ctx, root, "fetch", "--quiet", remote, install.Branch); err != nil {
		out.Warn("fetch failed: %v", err)
		return
	}
	before, _ := install.Git(ctx.Ctx, root, "rev-parse", "--short", "HEAD")
	if _, err := install.Git(ctx.Ctx, root, "merge", "--ff-only", "--quiet", "FETCH_HEAD"); err != nil {
		out.Warn("could not fast-forward (local commits, or local edits to pulled files): %v", err)
		return
	}
	after, _ := install.Git(ctx.Ctx, root, "rev-parse", "--short", "HEAD")
	if before == after {
		out.OK("checkout already at %s's %s (%s)", install.Branch, "head", after)
		return
	}
	n, _ := install.Git(ctx.Ctx, root, "rev-list", "--count", before+".."+after)
	out.OK("pulled %s commit(s): %s → %s", n, before, after)
}

const purgeHelp = `Removes the install tree (~/.vcod), the PATH symlink and the completion lines from
your shell rc files. It asks separately before removing the configuration and the job
records, and keeps them by default. It never deletes the source folder, and it does
not stop running servers or clients (vcod stop first).`

func runPurge(ctx *app.Context, args []string) int {
	if len(args) > 0 {
		return ctx.Usage("purge takes no arguments")
	}
	if ctx.Out != ui.Std {
		ctx.Errorf("purge asks questions on the terminal; run it outside the shell")
		return 1
	}
	out := ctx.Out
	paths, err := install.Resolve()
	if err != nil {
		return ctx.Fail(err)
	}
	out.Plain("")
	out.Warn("this will remove vcod:")
	out.Plain("    %s", paths.Prefix)
	out.Plain("    %s", paths.SymlinkPath())
	out.Plain("    the completion lines in your shell rc files")
	out.Plain("")
	if !confirm("  Are you sure? [y/N] ") {
		out.Info("aborted")
		return 0
	}
	if _, err := os.Lstat(paths.SymlinkPath()); err == nil {
		if err := os.Remove(paths.SymlinkPath()); err == nil {
			out.OK("removed %s", paths.SymlinkPath())
		}
	}
	for _, rc := range install.RCFiles() {
		if removed, err := StripRC(rc); err != nil {
			out.Warn("could not clean %s: %v", filepath.Base(rc), err)
		} else if removed {
			out.OK("cleaned %s", filepath.Base(rc))
		}
	}
	if confirm(fmt.Sprintf("\n  Also remove the configuration and job records (%s, %s)? [y/N] ", paths.ConfigDir, paths.StateDir)) {
		_ = os.RemoveAll(paths.ConfigDir)
		_ = os.RemoveAll(paths.StateDir)
		out.OK("removed %s and %s", paths.ConfigDir, paths.StateDir)
	} else {
		out.Info("keeping %s", paths.ConfigDir)
	}
	if install.IsInstalled() || install.Root() != paths.Prefix {
		if !install.IsSourceTree(paths.Prefix) {
			if err := os.RemoveAll(paths.Prefix); err == nil {
				out.OK("removed %s", paths.Prefix)
			}
		}
	}
	out.Plain("")
	out.OK("vcod has been uninstalled. Restart your shell to finish the cleanup.")
	return 0
}

// RCMarker is on every line the installer writes into a shell rc file.
const RCMarker = install.Binary + "-completion.bash"

// RCComment is the comment line the installer writes above the source line.
const RCComment = "# vcod CLI completions"

// StripRC removes the completion lines from a shell rc file.
func StripRC(path string) (bool, error) {
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
		if strings.Contains(line, RCMarker) || strings.Contains(line, RCComment) {
			removed = true
			if strings.Contains(line, RCComment) && len(kept) > 0 && kept[len(kept)-1] == "" {
				kept = kept[:len(kept)-1]
			}
			continue
		}
		kept = append(kept, line)
	}
	scanErr := sc.Err()
	f.Close()
	if scanErr != nil {
		return false, scanErr
	}
	if !removed {
		return false, nil
	}
	tmp := path + ".vcod.tmp"
	if err := os.WriteFile(tmp, []byte(strings.Join(kept, "\n")+"\n"), 0o644); err != nil {
		return false, err
	}
	return true, os.Rename(tmp, path)
}

func confirm(prompt string) bool {
	fmt.Print(prompt)
	if !platform.IsStdinTTY() {
		fmt.Println("n (not a terminal)")
		return false
	}
	sc := bufio.NewScanner(os.Stdin)
	if !sc.Scan() {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(sc.Text())) {
	case "y", "yes":
		return true
	}
	return false
}
