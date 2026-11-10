// Package version reports which build this is.
//
// A build is named by a hash of its own sources rather than a git commit, so a build
// from a working tree with local edits still gets a distinct name: build.sh stamps the first seven hex digits of SourceHash into Commit. The
// same hash computed over the source folder later tells `vcod version` whether the
// sources have moved on since this binary was built.
package version

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Commit is stamped at build time with -ldflags "-X vcod/internal/version.Commit=<hash>".
var Commit string

var (
	once    sync.Once
	current string
)

// RecordFile is written beside the installed binary with the source hash it was built
// from.
const RecordFile = ".version"

// Current is the version string: the stamped source hash, or "dev" for an unstamped build.
func Current() string {
	once.Do(func() {
		current = strings.TrimSpace(Commit)
		if current == "" {
			current = "dev"
		}
	})
	return current
}

// ReadRecord returns the version recorded in dir, or "".
func ReadRecord(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, RecordFile))
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(b))
	if v == "" || len(v) > 64 || strings.ContainsAny(v, " \t\r\n") {
		return ""
	}
	return v
}

// SourceHash is the hash build.sh stamps, computed over a source folder: every .go file
// plus go.mod and go.sum, outside .gocache and dist, sorted bytewise by "./"-relative
// path, each contributing its path, a newline, then its content. build.sh spells the same
// recipe in shell; version_test pins the two together.
func SourceHash(dir string) (string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if d.IsDir() {
			if rel == ".gocache" || rel == "dist" || rel == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if strings.HasSuffix(name, ".go") || name == "go.mod" || name == "go.sum" {
			files = append(files, "./"+filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return "", err
		}
		h.Write([]byte(f + "\n"))
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:7], nil
}
