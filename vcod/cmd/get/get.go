// Package getcmd is `vcod get`: fetch and build whatever is missing to play (the client)
// or to host (the server) on this machine.
package getcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	buildcmd "vcod/cmd/build"
	"vcod/internal/app"
	"vcod/internal/config"
	"vcod/internal/fetch"
	"vcod/internal/game"
	"vcod/internal/registry"
	"vcod/internal/run"
	"vcod/internal/ui"
	"vcod/internal/ws"
)

const help = `Usage:
  vcod get                          what the client and the server need, and what is here
  vcod get client [1.5|1.6x|all]    everything to play: the client flavor and cod_client
  vcod get server [1.5|1.6x|all]    everything to host: the Linux server and its flavor
  vcod get client --from /path/to/CoD   copy your retail CoD 1.5 install into cod/ first

Without a flavor argument it works on -F (default 1.5). Only missing parts are fetched
or built; a second run does nothing but the incremental build.

client:
  1. the retail game in cod/ (your own install patched to 1.5; --from copies it in)
  2. 1.6x only: the COD1.6X 1.6.12 release, downloaded from github.com/cod1plus/client
  3. the flavor folder flavors/client-<f> (cod_rea/server/make_flavors.sh)
  4. cod_client with the window, GL and sound (vcod build video: cmake, ninja, SDL2)

server:
  1. the retail game in cod/ (the servers load its paks)
  2. podman, which runs the i386 servers
  3. cod_lnxded and game.mp.i386.so 1.5 (2004-12-21 fix build), downloaded from
     treefort.icculus.org
  4. 1.6x only: the cod1plus server source, cloned into cod_rea/refs/
  5. the flavor folder flavors/server-<f> (for 1.6x this builds cod1plus.so)

Downloads are checked against pinned SHA-256 digests and kept in cod_rea/vendor/.
Nothing that exists is deleted or replaced; rebuild a flavor with vcod --write flavors
rebuild.`

func init() {
	registry.Register(registry.Command{
		Name:        "get",
		Summary:     "Fetch and build what the client or the server needs",
		Args:        "[client|server] [1.5|1.6x|all] [--from DIR]",
		Help:        help,
		Group:       registry.GroupSetup,
		Order:       15,
		NeedsConfig: true,
		Complete: func([]string) []string {
			return []string{"client", "server", "1.5", "1.6x", "all", "--from"}
		},
		Run: runGet,
	})
}

func runGet(ctx *app.Context, args []string) int {
	w, err := ctx.Workspace()
	if err != nil {
		return ctx.Fail(err)
	}
	var side, from string
	var flavors []config.Flavor
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "client" || a == "server":
			if side != "" {
				return ctx.Usage("client or server, not both")
			}
			side = a
		case a == "all" || a == "both":
			flavors = config.Flavors
		case a == "--from":
			if i+1 >= len(args) {
				return ctx.Usage("--from needs the folder of a CoD 1.5 install")
			}
			i++
			from = args[i]
		case strings.HasPrefix(a, "--from="):
			from = strings.TrimPrefix(a, "--from=")
		default:
			f, err := config.ParseFlavor(a)
			if err != nil {
				return ctx.Usage("unknown argument %q — client, server, 1.5, 1.6x or all", a)
			}
			flavors = append(flavors, f)
		}
	}
	if side == "" {
		if from != "" || flavors != nil {
			return ctx.Usage("say what to get: vcod get client or vcod get server")
		}
		return overview(ctx, w)
	}
	if flavors == nil {
		flavors = []config.Flavor{ctx.Flavor()}
	}

	ctx.Out.Section("get "+side, flavorList(flavors))
	if code := retail(ctx, w, from); code != 0 {
		return code
	}
	if side == "client" {
		return getClient(ctx, w, flavors)
	}
	return getServer(ctx, w, flavors)
}

func flavorList(fs []config.Flavor) string {
	s := make([]string, len(fs))
	for i, f := range fs {
		s[i] = string(f)
	}
	return strings.Join(s, ", ")
}

// Retail is the game install the flavors are copied from.
func retailDir(w *ws.Workspace) string { return filepath.Join(w.Root, "cod") }

// hasRetail reports whether dir holds CoD patched to 1.5: pakb.pk3 is the 1.5 patch.
func hasRetail(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "main", "pakb.pk3"))
	return err == nil
}

func retail(ctx *app.Context, w *ws.Workspace, from string) int {
	dst := retailDir(w)
	if hasRetail(dst) {
		if from != "" {
			ctx.Out.Note("cod/ already holds a 1.5 install; --from ignored (nothing is replaced)")
		}
		ctx.Out.OK("retail game in %s", dst)
		return 0
	}
	if from == "" {
		ctx.Errorf("no CoD 1.5 install in %s (cod/main/pakb.pk3 is the 1.5 patch)", dst)
		ctx.Hint("vcod get ... --from /path/to/Call\\ of\\ Duty   (your own install, patched to 1.5)")
		return 1
	}
	if abs, err := filepath.Abs(from); err == nil {
		from = abs
	}
	if !hasRetail(from) {
		ctx.Errorf("%s is not CoD patched to 1.5: it has no main/pakb.pk3", from)
		return 1
	}
	if _, err := os.Stat(dst); err == nil {
		ctx.Errorf("%s exists but has no main/pakb.pk3; move it aside, then run this again", dst)
		return 1
	}
	ctx.Out.Info("copying %s to %s", from, dst)
	if code := child(ctx, w.Root, "cp", "-R", from, dst); code != 0 {
		ctx.Out.Bad("copy failed (exit %d)", code)
		return code
	}
	ctx.Out.OK("retail game in %s", dst)
	return 0
}

func cod1xZip(w *ws.Workspace) string {
	return filepath.Join(w.CodRea(), "vendor", "cod1x-1.6.12", "cod1reloaded-1.6.12.zip")
}

func lnxdedDir(w *ws.Workspace) string {
	return filepath.Join(w.CodRea(), "vendor", "lnxded", "1.5-20041221")
}

func cod1plusSrc(w *ws.Workspace) string {
	return filepath.Join(w.CodRea(), "refs", "cod1plus_cod1plushookserver")
}

func getClient(ctx *app.Context, w *ws.Workspace, flavors []config.Flavor) int {
	for _, f := range flavors {
		if f == config.F16x {
			if code := download(ctx, fetch.Cod1x, cod1xZip(w), "COD1.6X 1.6.12"); code != 0 {
				return code
			}
		}
		if code := flavor(ctx, w, "client-"+string(f)); code != 0 {
			return code
		}
	}
	video := ws.LookupBuild("video")
	if code := buildcmd.Build(ctx, w, []*ws.Build{video}, false); code != 0 {
		return code
	}
	ctx.Out.Plain("")
	ctx.Out.OK("client ready: %s", w.ClientBinary(video))
	ctx.Out.Note("play with %s, or join one with %s", ui.StyleCyan.Render("vcod play"), ui.StyleCyan.Render("vcod lan join"))
	return 0
}

func getServer(ctx *app.Context, w *ws.Workspace, flavors []config.Flavor) int {
	if err := game.PodmanReady(); err != nil {
		return ctx.Fail(err)
	}
	ctx.Out.OK("podman")
	if code := lnxded(ctx, w); code != 0 {
		return code
	}
	for _, f := range flavors {
		if f == config.F16x {
			if code := cod1plus(ctx, w); code != 0 {
				return code
			}
		}
		if code := flavor(ctx, w, "server-"+string(f)); code != 0 {
			return code
		}
	}
	ctx.Out.Plain("")
	ctx.Out.OK("server ready")
	ctx.Out.Note("start it with %s", ui.StyleCyan.Render("vcod server start"))
	return 0
}

// lnxded puts cod_lnxded and game.mp.i386.so in vendor/lnxded/1.5-20041221, downloading
// the archive they come from when needed.
func lnxded(ctx *app.Context, w *ws.Workspace) int {
	dir := lnxdedDir(w)
	missing := false
	for _, m := range fetch.LnxdedMembers {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(m))); err != nil {
			missing = true
		}
	}
	if !missing {
		ctx.Out.OK("Linux 1.5 server in %s", dir)
		return 0
	}
	archive := filepath.Join(filepath.Dir(dir), filepath.Base(fetch.LnxdedBins.URL))
	if code := download(ctx, fetch.LnxdedBins, archive, "Linux 1.5 server"); code != 0 {
		return code
	}
	if err := fetch.ExtractTarBz2(archive, dir, fetch.LnxdedMembers); err != nil {
		return ctx.Fail(err)
	}
	ctx.Out.OK("Linux 1.5 server unpacked to %s", dir)
	return 0
}

// cod1plus clones the cod1plus server source at the tested revision.
func cod1plus(ctx *app.Context, w *ws.Workspace) int {
	dir := cod1plusSrc(w)
	if _, err := os.Stat(filepath.Join(dir, "scripts", "build.sh")); err == nil {
		ctx.Out.OK("cod1plus source in %s", dir)
		return 0
	}
	if _, ok := run.LookPath("git"); !ok {
		ctx.Errorf("git is needed to fetch the cod1plus server source")
		return 1
	}
	if _, err := os.Stat(dir); err == nil {
		ctx.Errorf("%s exists but has no scripts/build.sh; move it aside, then run this again", dir)
		return 1
	}
	ctx.Out.Info("cloning %s", fetch.Cod1plusRepo)
	if code := child(ctx, w.Root, "git", "clone", "-q", fetch.Cod1plusRepo, dir); code != 0 {
		ctx.Out.Bad("git clone failed (exit %d)", code)
		return code
	}
	if code := child(ctx, dir, "git", "-c", "advice.detachedHead=false", "checkout", "-q", fetch.Cod1plusCommit); code != 0 {
		ctx.Out.Bad("git checkout %s failed (exit %d)", fetch.Cod1plusCommit[:12], code)
		return code
	}
	ctx.Out.OK("cod1plus source at %s", fetch.Cod1plusCommit[:12])
	return 0
}

// flavor builds one flavor folder when it is missing.
func flavor(ctx *app.Context, w *ws.Workspace, name string) int {
	dir := filepath.Join(w.Flavors(), name)
	if _, err := os.Stat(dir); err == nil {
		ctx.Out.OK("flavor %s", name)
		return 0
	}
	ctx.Out.Info("building flavor %s", name)
	if code := child(ctx, w.Root, "sh", w.MakeFlavors(), name); code != 0 {
		ctx.Out.Bad("make_flavors.sh %s failed (exit %d)", name, code)
		return code
	}
	ctx.Out.OK("flavor %s", name)
	return 0
}

func download(ctx *app.Context, f fetch.File, dest, what string) int {
	if ok, _ := fetch.Verify(dest, f.SHA256); ok {
		ctx.Out.OK("%s in %s", what, dest)
		return 0
	}
	ctx.Out.Info("downloading %s from %s", what, f.URL)
	err := fetch.Download(ctx.Ctx, f, dest, func(done, total int64) {
		if ctx.Stream != nil {
			return
		}
		if total > 0 {
			fmt.Fprintf(os.Stderr, "\r  %s / %s", mb(done), mb(total))
		} else {
			fmt.Fprintf(os.Stderr, "\r  %s", mb(done))
		}
	})
	if ctx.Stream == nil {
		fmt.Fprintln(os.Stderr)
	}
	if err != nil {
		return ctx.Fail(err)
	}
	ctx.Out.OK("%s in %s", what, dest)
	return 0
}

func mb(n int64) string { return fmt.Sprintf("%.1f MB", float64(n)/(1<<20)) }

func child(ctx *app.Context, dir string, args ...string) int {
	code, err := run.Run(ctx.Ctx, run.Spec{Args: args, Dir: dir, Stream: ctx.Stream})
	if err != nil {
		ctx.Errorf("%v", err)
		return 1
	}
	return code
}

// overview lists each part per side and whether it is here.
func overview(ctx *app.Context, w *ws.Workspace) int {
	mark := func(ok bool) string {
		if ok {
			return ui.StyleGreen.Render("✔")
		}
		return ui.StyleYellow.Render("▲")
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	video := ws.LookupBuild("video")
	lnx := exists(filepath.Join(lnxdedDir(w), "cod_lnxded")) && exists(filepath.Join(lnxdedDir(w), "main", "game.mp.i386.so"))
	podman := game.PodmanReady() == nil

	ctx.Out.Section("get", w.Root)
	row := func(ok bool, what, where string) []string {
		return []string{mark(ok), ui.StyleWhite.Render(what), ui.StyleDim.Render(where)}
	}
	ctx.Out.Plain("  %s", ui.StyleCyan.Render("client")+ui.StyleDim.Render("  vcod get client [1.5|1.6x|all]"))
	ctx.Out.Listing([][]string{
		row(hasRetail(retailDir(w)), "retail CoD 1.5", "cod/"),
		row(exists(cod1xZip(w)), "COD1.6X 1.6.12 (1.6x)", "cod_rea/vendor/cod1x-1.6.12/"),
		row(exists(w.ClientDir("1.5")), "flavor client-1.5", "flavors/client-1.5"),
		row(exists(w.ClientDir("1.6x")), "flavor client-1.6x", "flavors/client-1.6x"),
		row(exists(w.ClientBinary(video)), "cod_client (video)", "cod_rea/"+video.Dir),
	})
	ctx.Out.Plain("")
	ctx.Out.Plain("  %s", ui.StyleCyan.Render("server")+ui.StyleDim.Render("  vcod get server [1.5|1.6x|all]"))
	ctx.Out.Listing([][]string{
		row(hasRetail(retailDir(w)), "retail CoD 1.5", "cod/"),
		row(podman, "podman", "runs the i386 servers"),
		row(lnx, "cod_lnxded 1.5", "cod_rea/vendor/lnxded/1.5-20041221/"),
		row(exists(filepath.Join(cod1plusSrc(w), "scripts", "build.sh")), "cod1plus source (1.6x)", "cod_rea/refs/cod1plus_cod1plushookserver"),
		row(exists(w.ServerDir("1.5")), "flavor server-1.5", "flavors/server-1.5"),
		row(exists(w.ServerDir("1.6x")), "flavor server-1.6x", "flavors/server-1.6x"),
	})
	ctx.Out.Plain("")
	return 0
}
