#!/usr/bin/env bash
# Installs vcod on Linux or macOS: clones (or updates) the vcod repository, builds vcod
# (or downloads the prebuilt binary when there is no Go and no podman) and installs it to
# ~/.vcod with a ~/.local/bin/vcod symlink.
#
#   curl -fsSL https://raw.githubusercontent.com/vcod-gtfo/vcod/main/install.sh | bash
#
# Run from a checkout (bash install.sh), it builds that checkout instead of cloning: this
# is how the private workspace installs its own vcod.
#
# Settings (environment variables):
#   VCOD_DIR      where the repository is cloned (default ~/vcod)
#   VCOD_REPO     the repository to clone (default https://github.com/vcod-gtfo/vcod.git)
#   VCOD_BRANCH   the branch (default main)
#   VCOD_BUILD    podman or native: where Go runs (default: native Go when it is
#                 installed, else podman)
#   VCOD_NO_RC=1  do not touch shell rc files
#
# It needs git, and Go, podman or curl. It installs nothing else and never uses sudo.
set -euo pipefail

DIR=${VCOD_DIR:-$HOME/vcod}
REPO=${VCOD_REPO:-https://github.com/vcod-gtfo/vcod.git}
RELEASES=https://github.com/vcod-gtfo/vcod/releases/latest/download
BRANCH=${VCOD_BRANCH:-main}

say() { printf '\033[1;32m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

pkg_hint() {
  case "$(uname -s)" in
    Darwin) echo "brew install $*" ;;
    *)
      if have dnf; then echo "sudo dnf install $*"
      elif have apt-get; then echo "sudo apt-get install $*"
      elif have pacman; then echo "sudo pacman -S $*"
      else echo "install $* with your package manager"
      fi ;;
  esac
}

# the native client's build tools, by package manager
build_hint() {
  case "$(uname -s)" in
    Darwin) echo "brew install cmake ninja sdl2" ;;
    *)
      if have dnf; then echo "sudo dnf install gcc cmake ninja-build SDL2-devel zlib-devel"
      elif have apt-get; then echo "sudo apt-get install gcc cmake ninja-build libsdl2-dev zlib1g-dev"
      elif have pacman; then echo "sudo pacman -S gcc cmake ninja sdl2 zlib"
      else echo "install gcc, cmake, ninja, SDL2 and zlib with your package manager"
      fi ;;
  esac
}

case "$(uname -s)" in
  Linux|Darwin) ;;
  *) die "vcod runs on Linux and macOS, not $(uname -s)" ;;
esac

have git || die "git is missing: $(pkg_hint git)"

# Prefer a native Go: no container, and on macOS podman's VM is not needed to build.
# Without either, take the prebuilt binary from the latest release.
if [ -z "${VCOD_BUILD:-}" ]; then
  if have go; then
    VCOD_BUILD=native
  elif have podman; then
    VCOD_BUILD=podman
  elif have curl; then
    VCOD_BUILD=prebuilt
  else
    die "vcod needs Go ($(pkg_hint go)), podman ($(pkg_hint podman)) or curl to download it"
  fi
fi
export VCOD_BUILD

# Run from a checkout: build that one.
HERE=$(cd "$(dirname "$0")" 2>/dev/null && pwd || true)
if [ -n "$HERE" ] && [ -f "$HERE/vcod/build.sh" ] && [ -d "$HERE/.git" ]; then
  DIR=$HERE
  say "using the checkout in $DIR"
elif [ -d "$DIR/.git" ]; then
  say "updating $DIR"
  git -C "$DIR" fetch --quiet origin "$BRANCH"
  if ! git -C "$DIR" merge --ff-only --quiet "origin/$BRANCH"; then
    die "$DIR has local changes or commits that block a fast-forward; update it by hand (git -C $DIR status)"
  fi
elif [ -e "$DIR" ] && [ -n "$(ls -A "$DIR" 2>/dev/null)" ]; then
  die "$DIR exists and is not a git checkout; set VCOD_DIR to another folder"
else
  say "cloning $REPO into $DIR"
  git clone --quiet --branch "$BRANCH" "$REPO" "$DIR"
fi

if [ "$VCOD_BUILD" = prebuilt ]; then
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  case "$(uname -m)" in
    x86_64|amd64) arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) die "no prebuilt vcod for $(uname -m); install Go ($(pkg_hint go)) and run this again" ;;
  esac
  say "downloading vcod-$os-$arch"
  curl -fsSL -o "$DIR/vcod/vcod" "$RELEASES/vcod-$os-$arch" ||
    die "download failed; install Go ($(pkg_hint go)) and run this again to build it"
  chmod +x "$DIR/vcod/vcod"
  if [ "$os" = darwin ]; then xattr -d com.apple.quarantine "$DIR/vcod/vcod" 2>/dev/null || true; fi
  "$DIR/vcod/vcod" install -f
else
  say "building vcod ($VCOD_BUILD Go)"
  sh "$DIR/vcod/build.sh" install
fi

BIN=${VCOD_BIN_DIR:-$HOME/.local/bin}
VCOD="$BIN/vcod"
[ -x "$VCOD" ] || VCOD="$DIR/vcod/vcod"

say "installed $VCOD"

if [ -d "$DIR/cod_rea" ]; then
  cat <<EOF

Next steps:
  1. Copy your own Call of Duty install, patched to 1.5, to $DIR/cod
  2. vcod setup                 workspace, flavor, display (point it at $DIR)
  3. vcod flavors make          the client and server installs under flavors/
  4. vcod build video           the native client; its tools:
                                $(build_hint)
  5. vcod lan                   servers on your network;  vcod lan join N  to play
     vcod doctor                checks everything above
EOF
else
  cat <<EOF

The native client and the servers are built from the cod_rea workspace, which is
invite only. Ask for access at https://github.com/vcod-gtfo/vcod/issues, then:
  git clone https://github.com/vcod-gtfo/cod_rea.git ~/cod_rea
  bash ~/cod_rea/install.sh     vcod from the workspace; it prints the next steps
You need your own Call of Duty install, patched to 1.5: the game files are never
distributed.
EOF
fi
