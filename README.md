# vcod

A command-line launcher for Call of Duty (2003) multiplayer on Linux and macOS: it
installs and updates the native client, starts and joins servers, finds LAN games, and
keeps your configs, demos, downloads and mods in one place. One static binary, with a
full-screen terminal shell (`vcod`) and plain subcommands for scripts.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/vcod-gtfo/vcod/main/install.sh | bash
```

This clones the repository to `~/vcod` and builds vcod with Go or podman. Without
either, it downloads the prebuilt binary of the latest
[release](https://github.com/vcod-gtfo/vcod/releases). It never uses sudo. Then:

```sh
vcod doctor     # what is installed and what is missing
vcod lan        # servers on your network
vcod help       # every command
```

## The game

vcod does not include the game. You need your own copy of Call of Duty, patched to
1.5. The native client is built from a separate, invite-only workspace: open an
[issue](https://github.com/vcod-gtfo/vcod/issues) to ask for access.

This is a non-commercial fan project. It is not affiliated with or endorsed by
Activision. Call of Duty is a trademark of Activision Publishing, Inc.

## Bugs

Open an [issue](https://github.com/vcod-gtfo/vcod/issues) with the output of
`vcod version` and `vcod doctor`, and what you ran.

## Source

[`vcod/`](vcod) holds the Go sources; [`vcod/README.md`](vcod/README.md) documents every
command. `sh vcod/build.sh check` runs the tests. This repository is a mirror: changes
are made in the workspace and published here.

## License

vcod is released under the [MIT License](LICENSE). The license covers this repository
only: not the game, its files, or the client workspace.
