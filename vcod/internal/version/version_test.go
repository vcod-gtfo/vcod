package version

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The binary is stamped by build.sh's shell spelling of the hash, and `vcod version`
// recomputes it in Go: the two must agree or every install reports an update.
func TestSourceHashMatchesBuildScript(t *testing.T) {
	root, _ := filepath.Abs("../..")
	_, errSum := exec.LookPath("sha256sum")
	_, errShasum := exec.LookPath("shasum")
	if errSum != nil && errShasum != nil {
		t.Skip("no sha256sum or shasum")
	}
	out, err := exec.Command("sh", filepath.Join(root, "build.sh"), "hash").Output()
	if err != nil {
		t.Fatalf("build.sh hash: %v", err)
	}
	got, err := SourceHash(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("SourceHash = %s, build.sh hash = %s", got, want)
	}
}

func TestCurrentDefaultsToDev(t *testing.T) {
	if Commit == "" && Current() != "dev" {
		t.Fatalf("Current() = %q", Current())
	}
}
