package ws

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDescribe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "svtest.sh")
	body := `#!/bin/sh
# svtest.sh [seconds]: live check of cod_server with the stub game module. It starts
# flavors/server-1.5 as well.
#
#   1. the first step
#   2. the second
set -eu
sh "$ROOT/server/run.sh" ../flavors/server-1.5
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	summary, usage, servers := describe(path, "svtest")
	if usage != "svtest.sh [seconds]" {
		t.Errorf("usage = %q", usage)
	}
	if summary != "live check of cod_server with the stub game module" {
		t.Errorf("summary = %q", summary)
	}
	if !servers {
		t.Error("StartsServers not set")
	}
}

func TestLookupBuild(t *testing.T) {
	if b := LookupBuild("build-video"); b == nil || b.Name != "video" {
		t.Fatalf("LookupBuild(build-video) = %v", b)
	}
	if LookupBuild("nope") != nil {
		t.Fatal("found nope")
	}
}
