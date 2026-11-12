# vcod

Start, stop and test Call of Duty (2003) servers and clients in this workspace. It is
one static Go binary (Bubble Tea, Bubbles, Lip Gloss).

- A bare `vcod` in a terminal opens a full-screen **shell**. Every command streams
  into a tab, so a build, a live test and a server console can be watched side by side.
- Every command also runs on its own (`vcod status --json`, `vcod play`, …). That
  is how scripts and agents use it.

It runs on Linux and macOS (x86-64 and arm64).

## Install

One line, on Linux or macOS:

```sh
curl -fsSL https://gitlab.com/aenigma-group/vcod/-/raw/main/install.sh | bash
```

[`install.sh`](../install.sh) clones the workspace to `~/vcod` (or pulls it when it is
already there), builds vcod and installs it. It needs `git` and either Go or podman,
installs nothing else and never uses sudo. `VCOD_DIR`, `VCOD_REPO`, `VCOD_BRANCH`,
`VCOD_BUILD` and `VCOD_NO_RC=1` change what it does (see the script's header). The
raw URL only works while the repository is public; for a private one, clone it and run
the script from the checkout: `bash install.sh`.

From a checkout:

```sh
sh vcod/build.sh install     # build (Go in podman, or the host's Go), then install
```

`build.sh` runs Go in a `golang:1.27-alpine` podman container when podman is installed,
so Go isn't needed. Without podman it uses the host's Go (on a Mac: `brew install go`).
`VCOD_BUILD=native` or `VCOD_BUILD=podman` picks one explicitly.

This installs `~/.vcod/vcod`, the `~/.local/bin/vcod` symlink, bash/zsh
completions, and a record of the source folder so that `vcod update` can rebuild.
An old `codctl` install is carried over: its config, jobs and client homes move to the
vcod folders, and `~/.codctl`, its symlink and its rc lines are removed.
Without installing, `vcod/vcod` works from the
source folder after `sh vcod/build.sh build`.

## Getting the client or the server

A fresh checkout has the code only. `vcod get` fetches and builds the rest:

```sh
vcod get                               # what each side needs, and what is here
vcod get client --from ~/Games/CoD     # copy your CoD 1.5 install to cod/, then everything to play
vcod get client 1.6x                   # + the COD1.6X 1.6.12 release, flavors/client-1.6x, cod_client
vcod get server all                    # the Linux 1.5 server, cod1plus for 1.6x, both server flavors
```

Downloads are pinned by SHA-256 and kept in `cod_rea/vendor/`: the 2004-12-21 Linux server
build from treefort.icculus.org and the COD1.6X release from github.com/cod1plus/client. The
cod1plus server source is cloned into `cod_rea/refs/` at the tested commit. The retail game
can't be downloaded: it comes from your own install, patched to 1.5. Only missing parts
are made; nothing existing is replaced.

## Updating

The workspace is the git repository `git@gitlab.com:aenigma-group/vcod.git`, and this folder
is its `vcod/`. `vcod version` fetches `main` and says whether the checkout is behind
or the sources changed since this build. `vcod update`:

1. fast-forwards the checkout to `origin/main`. It skips this step when the checkout is
   on another branch or has commits of its own, and git refuses it when a pulled file
   would overwrite a local edit.
2. runs `build.sh install` when the sources hash differently from the running binary
   (`-f` forces it).

A binary with no source folder clones the repository into `~/.vcod/src` first.
`VCOD_REMOTE` points it at another remote.

## What it drives

| vcod | underneath |
|---|---|
| `server start [map] [gt]` | `cod_rea/server/run.sh flavors/server-<f> map gt` as a detached job (podman container `cod_rea_server_<f>`), started with `rconPassword` from the config. It waits until the server answers `getstatus`. |
| `client [host:port]` | `cod_rea/build-video/cod_client +set fs_basepath flavors/client-<f> +set fs_homepath ~/.local/share/vcod/home-<f> …` as a detached job. It auto-joins this flavor's local server when that server answers. |
| `play [map] [gt]` | `server start`, or an rcon map switch if the server is already up, then `client` joined to it. This takes the place of a listen server, which the port does not have. |
| `lan [list\|join N]` | a `getinfo` broadcast to ports 28960–28963 on every network (the game's own Local tab), listing who answers; `join` starts the client whose protocol the server speaks (6 → 1.5, 10 → 1.6x) |
| `build [headless\|video]` | cmake configure (first time only) and `ninja -C cod_rea/build[-video]` |
| `test [name]` | ctest in `cod_rea/build`, or `sh tools/<name>.sh` from `cod_rea/` |
| `rcon <cmd>`, `status`, `logs` | Q3 out-of-band UDP (`getstatus`, `rcon`), plus the job logs and the server's `games_mp.log` / `console_mp_server.log` |
| `flavors [make\|rebuild]` | `cod_rea/server/make_flavors.sh` |

## Maps

```sh
vcod maps                    # the maps this flavor's server can load: stock, then custom
vcod maps sd                 # only those whose .arena lists S&D
vcod maps --client --json    # the client install's, as JSON (--names: one name per line)
```

pk3s are read in the engine's order, so the "from" column names the pk3 whose copy the
game uses. A map without an `.arena` entry shows no gametypes. Add a map by dropping its pk3
into `flavors/server-<f>/main` (and the client's).

## LAN games

```sh
vcod lan                     # servers answering on the local network, numbered
vcod lan --json              # the same for scripts
vcod lan join 2              # join number 2 (or: vcod lan join harbor, by name/map/address)
vcod lan join --headless     # with one server there, join it; client options pass through
vcod lan --add 10.0.0.7      # also ask a server a broadcast can't reach (another subnet, VPN)
```

The scan broadcasts to `255.255.255.255` and to each interface's own broadcast address,
since macOS sends the first only through its primary interface. A server on this
machine shows once, marked "this machine", and is joined over 127.0.0.1. The client
flavor comes from the server's protocol, whatever `-F` says. Other machines find a vcod
server as long as its UDP port isn't firewalled (podman publishes it on every
interface): on Fedora, `sudo firewall-cmd --add-port=28960-28963/udp`. On macOS, allow
the incoming connection when asked.

## macOS

Everything here works on a Mac, with these differences:

- Install the tools with Homebrew: `brew install go cmake ninja sdl2 podman`.
- The servers are Linux i386 binaries, so podman runs them in its Linux VM:
  `podman machine init` once, then `podman machine start` after each reboot.
  `vcod doctor` and `vcod server start` say when the VM is down. On Apple Silicon the
  container is emulated, so it is slower. To only play, join a server on another
  machine with `vcod lan join` or `vcod client host:port`.
- `CLIENT_GPU` does nothing (macOS picks the GPU itself), and `screens open` uses
  `open` instead of `xdg-open`.
- `flavors make` clones files with `cp -c` on APFS instead of `--reflink`.

`docs/macos.md` in the workspace tracks what has and hasn't been tried on a real Mac.

## Display

`vcod display` sets how the client opens, for both flavors:

```sh
vcod display                           # what the client starts with
vcod display 1440p fullscreen fps 333  # resolution, display mode, fps cap, any mix
vcod display borderless                # window | fullscreen | borderless
vcod display fps unlocked              # 1–1000, or unlocked (the engine then runs at 1000)
vcod display presets                   # desktop 720p 900p 1080p 1440p 4k uw1440 … (or any WxH)
vcod display gpu nvidia                # auto (default) | nvidia | integrated
vcod play --res 1080p --window --maxfps 125   # override for one start
```

The settings are `CLIENT_RES`, `CLIENT_DISPLAY` and `CLIENT_MAXFPS` in the config. They
become `r_mode -1` with `r_customwidth`/`r_customheight` (or `r_mode -2` for the
desktop size), `r_fullscreen 0/1/2` and `com_maxfps`. Borderless always runs at the
desktop's size. 1.6x servers check `com_maxfps` against 60–250. A running client keeps
what it started with, so use `vcod client restart` after a change.

`CLIENT_GPU` picks the graphics chip on a Linux hybrid laptop (NVIDIA Optimus), where the
integrated chip drives the screen. `auto` asks for the NVIDIA card when the driver sees
one next to another chip. It does this through PRIME render offload:
`__NV_PRIME_RENDER_OFFLOAD=1`, plus `__GLX_VENDOR_LIBRARY_NAME=nvidia` for SDL's X11
driver or `__EGL_VENDOR_LIBRARY_FILENAMES` set to the NVIDIA vendor file for its Wayland
driver. `vcod client` prints the `GL_RENDERER` the client got and warns when it isn't the
one asked for. `vcod display --json` shows the variables it sets.

There are two flavors, `1.5` (protocol 6, port 28960) and `1.6x` (cod1plus, protocol 10,
port 28961). Pick one per command with `-F 1.6x` / `--16x`, set the default with
`vcod config set FLAVOR 1.6x`, or type `flavor 1.6x` in the shell.

Jobs live in `~/.local/state/vcod/jobs/<name>.{json,log}`. They outlive the command
and the shell that started them; `vcod stop` ends them.

The settings file is `~/.config/vcod/config` (mode 600). `vcod config` lists every key,
and `VCOD_<KEY>` overrides a key for one run.

Safe mode is the default. `flavors rebuild` and `build --clean` delete things, so they
need `--write` (in the shell, ctrl+x).

## Develop

```sh
sh build.sh check    # gofmt, go vet, go test (podman, or VCOD_BUILD=native)
sh build.sh build    # ./vcod, stamped with the source hash
sh build.sh cross    # dist/vcod-{linux,darwin}-{amd64,arm64}
sh build.sh hash     # the version a build would get
```

There is no CI: check a change with `sh build.sh check`, and that it still builds for
macOS with `sh build.sh cross`.

The version is the first 7 hex digits of a sha256 over the Go sources rather than a git
commit, so a build with local edits gets its own name. `internal/version` computes the same hash in Go so that
`vcod version` can tell whether the sources have moved on. `version_test` pins the two
together.

Layout:

- `cmd/<command>`: each command registers itself from `init`.
- `internal/cli`: the dispatcher and global flags.
- `internal/game`: servers, clients and arg parsing.
- `internal/jobs`: detached processes.
- `internal/q3`: the UDP protocol, and the LAN scan (`lan.go`).
- `internal/ws`: the workspace layout, build staleness and test discovery.
- `cmd/shell`: the TUI.

A new `cod_rea/tools/*test*.sh` appears in `vcod test` with no change here. Its summary
is the first sentence of its header comment.

Gotchas worth knowing:

- CoD1's rcon cvar is `rconPassword`, not Quake 3's `rcon_password`.
- The server silently drops an rcon that arrives within 500 ms of the last one. vcod
  spaces its calls, including across separate runs, through
  `~/.local/state/vcod/jobs/rcon-<f>.last`.
- The live test scripts that start servers go through `run.sh`, so they replace a
  server vcod started for that flavor. `vcod test` marks those scripts ⚑ and warns.
