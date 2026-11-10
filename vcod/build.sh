#!/usr/bin/env sh
# Build, test or install vcod, in a container or with a Go installed here.
#
#   ./build.sh build      static binary for this machine at ./vcod
#   ./build.sh install    build, then run ./vcod install -f
#   ./build.sh check      gofmt verification, go vet and go test
#   ./build.sh test       go test only
#   ./build.sh fmt [f…]   gofmt -w the given files, else all
#   ./build.sh hash       print the source hash the binary would be stamped with
#   ./build.sh cross      dist/vcod-<os>-<arch> for linux and darwin, amd64 and arm64
#   ./build.sh shell      a shell inside the build container, in this directory
#
# VCOD_BUILD picks where Go runs. With podman (the default when podman is installed),
# the golang image runs under rootless podman as your own user (--userns=keep-id), so
# nothing root-owned lands in the tree. The module cache is ~/go/pkg/mod when it exists,
# else .gocache/mod here, so builds need no network. Override the image with
# VCOD_GO_IMAGE. With native (the default when podman is missing and go is on PATH,
# e.g. macOS with `brew install go`), the host's Go runs directly with its own caches.
set -eu

cd "$(dirname "$0")"

IMAGE=${VCOD_GO_IMAGE:-golang:1.27-alpine}
BINARY=vcod

usage() {
  sed -n '2,11p' "$0" | sed 's/^# \{0,1\}//'
  exit "${1:-0}"
}

# The version is a hash of the sources, so local edits get their own name too.
# internal/version.SourceHash computes the same thing; version_test pins the two.
srchash() {
  find . \( -name '*.go' -o -name go.mod -o -name go.sum \) \
    -not -path './.gocache/*' -not -path './dist/*' -not -path './.git/*' |
    LC_ALL=C sort | while read -r f; do printf '%s\n' "$f"; cat "$f"; done |
    sha256 | cut -c1-7
}

# sha256sum is GNU coreutils; macOS has shasum (perl) instead
sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum
  else
    shasum -a 256
  fi
}

mode() {
  case "${VCOD_BUILD:-}" in
    podman|native) echo "$VCOD_BUILD" ;;
    "")
      if command -v podman >/dev/null 2>&1; then
        echo podman
      elif command -v go >/dev/null 2>&1; then
        echo native
      else
        echo "error: need podman or a Go toolchain (dnf install podman, or brew install go)" >&2
        exit 1
      fi ;;
    *)
      echo "error: VCOD_BUILD must be podman or native, not $VCOD_BUILD" >&2
      exit 1 ;;
  esac
}

ldflags() {
  echo "-s -w -X vcod/internal/version.Commit=$(srchash)"
}

modcache() {
  if [ -d "${GOMODCACHE:-$HOME/go/pkg/mod}" ]; then
    echo "${GOMODCACHE:-$HOME/go/pkg/mod}"
  else
    mkdir -p .gocache/mod
    echo "$PWD/.gocache/mod"
  fi
}

run() {
  m=$(mode) || exit 1
  if [ "$m" = native ]; then
    CGO_ENABLED=0 GOFLAGS=-mod=readonly "$@"
    return
  fi
  command -v podman >/dev/null 2>&1 || {
    echo "error: podman is not installed (dnf install podman, or VCOD_BUILD=native)" >&2
    exit 1
  }
  mkdir -p .gocache/build .gocache/home
  # shellcheck disable=SC2086
  podman run --rm --userns=keep-id \
    -e HOME=/gohome \
    -e CGO_ENABLED=0 \
    -e GOFLAGS=-mod=readonly \
    -e GOCACHE=/gobuild \
    -e GOMODCACHE=/gomodcache \
    -e GOTOOLCHAIN=local \
    -v "$PWD:/src:z" \
    -v "$PWD/.gocache/build:/gobuild:z" \
    -v "$PWD/.gocache/home:/gohome:z" \
    -v "$(modcache):/gomodcache:z" \
    -w /src \
    ${TTY_FLAGS:-} \
    ${GOOS:+-e GOOS="$GOOS"} ${GOARCH:+-e GOARCH="$GOARCH"} \
    "$IMAGE" "$@"
}

build() {
  run go build -trimpath -ldflags "$(ldflags)" -o "./$BINARY" .
  echo "built ./$BINARY ($(srchash))"
}

cross() {
  mkdir -p dist
  for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
    (
      GOOS=${target%/*} GOARCH=${target#*/}
      export GOOS GOARCH
      run go build -trimpath -ldflags "$(ldflags)" -o "./dist/$BINARY-$GOOS-$GOARCH" .
    )
    echo "built ./dist/$BINARY-${target%/*}-${target#*/}"
  done
}

case "${1:-build}" in
  -h|--help|help) usage 0 ;;
  build) build ;;
  install)
    build
    "./$BINARY" install -f ;;
  check)
    run sh -c '
      out=$(gofmt -l $(find . -name "*.go" -not -path "./.gocache/*" -not -path "./dist/*"));
      if [ -n "$out" ]; then echo "gofmt needed:"; echo "$out"; exit 1; fi
      go vet ./... && go test ./...' ;;
  test) run go test ./... ;;
  fmt)
    shift
    if [ $# -gt 0 ]; then
      run gofmt -l -w "$@"
    else
      run sh -c 'gofmt -l -w $(find . -name "*.go" -not -path "./.gocache/*" -not -path "./dist/*")'
    fi ;;
  hash) srchash ;;
  cross) cross ;;
  shell)
    m=$(mode) || exit 1
    if [ "$m" = native ]; then
      echo "shell: only in the podman mode; the host shell already has go" >&2
      exit 1
    fi
    TTY_FLAGS="-it" run sh ;;
  *)
    echo "unknown command: $1" >&2
    usage 1 ;;
esac
