# vcod

Start, stop and test Call of Duty (2003) servers and clients in this workspace. It is
one static Go binary (Bubble Tea, Bubbles, Lip Gloss).

- A bare `vcod` in a terminal opens a full-screen **shell**. Every command streams
  into a tab, so a build, a live test and a server console can be watched side by side.
- Every command also runs on its own (`vcod status --json`, `vcod play`, …). That
  is how scripts and agents use it.

## Install

```sh
sh vcod/build.sh install     # builds in a golang:1.27-alpine podman container, then installs
```

This installs `~/.vcod/vcod`, the `~/.local/bin/vcod` symlink, bash/zsh
completions, and a record of the source folder so that `vcod update` can rebuild.
An old `codctl` install is carried over: its config, jobs and client homes move to the
vcod folders, and `~/.codctl`, its symlink and its rc lines are removed.
Go does not need to be installed. Without installing, `vcod/vcod` works from the
source folder after `sh vcod/build.sh build`.

## Updating

The workspace is the git repository `git@github.com:hatspencer/vcod.git`, and this folder
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
| `build [headless\|video]` | cmake configure (first time only) and `ninja -C cod_rea/build[-video]` |
| `test [name]` | ctest in `cod_rea/build`, or `sh tools/<name>.sh` from `cod_rea/` |
| `rcon <cmd>`, `status`, `logs` | Q3 out-of-band UDP (`getstatus`, `rcon`), plus the job logs and the server's `games_mp.log` / `console_mp_server.log` |
| `flavors [make\|rebuild]` | `cod_rea/server/make_flavors.sh` |

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
sh build.sh check    # gofmt, go vet, go test (in podman)
sh build.sh build    # ./vcod, stamped with the source hash
sh build.sh hash     # the version a build would get
```

The version is the first 7 hex digits of a sha256 over the Go sources rather than a git
commit, so a build with local edits gets its own name. `internal/version` computes the same hash in Go so that
`vcod version` can tell whether the sources have moved on. `version_test` pins the two
together.

Layout:

- `cmd/<command>`: each command registers itself from `init`.
- `internal/cli`: the dispatcher and global flags.
- `internal/game`: servers, clients and arg parsing.
- `internal/jobs`: detached processes.
- `internal/q3`: the UDP protocol.
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
