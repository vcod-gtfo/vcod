#!/usr/bin/env sh
# Build, test or install vcod in a container, so Go need not be installed here.
#
#   ./build.sh build      static binary for this machine at ./vcod
#   ./build.sh install    build, then run ./vcod install -f
#   ./build.sh check      gofmt verification, go vet and go test
#   ./build.sh test       go test only
#   ./build.sh fmt [f…]   gofmt -w the given files, else all
#   ./build.sh hash       print the source hash the binary would be stamped with
#   ./build.sh shell      a shell inside the build container, in this directory
#
# The Go toolchain runs in the golang image under rootless podman as your own user
# (--userns=keep-id), so nothing root-owned lands in the tree. The module cache is
# ~/go/pkg/mod when it exists, else .gocache/mod here, so builds need no network.
# Override the image with VCOD_GO_IMAGE.
set -eu

cd "$(dirname "$0")"

IMAGE=${VCOD_GO_IMAGE:-golang:1.27-alpine}
BINARY=vcod

usage() {
  sed -n '2,10p' "$0" | sed 's/^# \{0,1\}//'
  exit "${1:-0}"
}

# The version is a hash of the sources, so local edits get their own name too.
# internal/version.SourceHash computes the same thing; version_test pins the two.
srchash() {
  find . \( -name '*.go' -o -name go.mod -o -name go.sum \) \
    -not -path './.gocache/*' -not -path './dist/*' -not -path './.git/*' |
    LC_ALL=C sort | while read -r f; do printf '%s\n' "$f"; cat "$f"; done |
    sha256sum | cut -c1-7
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
  command -v podman >/dev/null 2>&1 || {
    echo "error: podman is not installed (dnf install podman)" >&2
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
    "$IMAGE" "$@"
}

build() {
  run go build -trimpath -ldflags "$(ldflags)" -o "./$BINARY" .
  echo "built ./$BINARY ($(srchash))"
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
  shell) TTY_FLAGS="-it" run sh ;;
  *)
    echo "unknown command: $1" >&2
    usage 1 ;;
esac
