// Package fetch downloads the third-party files the game needs and git doesn't carry:
// the Linux 1.5 dedicated server (Activision's release, mirrored by icculus) and the
// COD1.6X client release. Every download is checked against a pinned SHA-256, so a
// changed or truncated file is never used.
package fetch

import (
	"archive/tar"
	"compress/bzip2"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// File is one pinned download.
type File struct {
	// URL is where it comes from.
	URL string
	// SHA256 is the hex digest the download must have.
	SHA256 string
}

// LnxdedBins is the 2004-12-21 fixed build of the Linux CoD and UO servers. Its
// cod_lnxded and main/game.mp.i386.so are what the server flavors run, and what
// cod1plus.so is reverse engineered against.
var LnxdedBins = File{
	URL:    "https://treefort.icculus.org/cod/COD-CODUO-lnxded-bins-12212004.tar.bz2",
	SHA256: "4d1a780b27cfe9d619cc51f468c22fbf3e80e4832f44e54d69e59794b4c5fb33",
}

// LnxdedMembers are the files taken from LnxdedBins.
var LnxdedMembers = []string{"cod_lnxded", "main/game.mp.i386.so"}

// Cod1x is the COD1.6X 1.6.12 client release that the client-1.6x flavor is built from.
var Cod1x = File{
	URL:    "https://github.com/cod1plus/client/releases/download/1.6.12/cod1reloaded-1.6.12.zip",
	SHA256: "bbf1e3ea6eb85b6de21c0d224a4a89eb938253846081aca9ea18a8df2a8a8361",
}

// Cod1plusRepo is the server half of COD1.6X; the server-1.6x flavor builds cod1plus.so
// from it. Cod1plusCommit is the revision the port was tested against.
const (
	Cod1plusRepo   = "https://github.com/cod1plus/cod1plushookserver.git"
	Cod1plusCommit = "1193f66201deab072d2b9759c14daf430ffe2e8f"
)

// Progress is told how many bytes have arrived out of total (-1 when unknown).
type Progress func(done, total int64)

// Download fetches f to dest, verifying its digest. It writes to a temporary file next to
// dest and renames it into place, so dest is either the verified file or absent. A dest
// that already has the right digest is kept without downloading.
func Download(ctx context.Context, f File, dest string, progress Progress) error {
	if ok, _ := Verify(dest, f.SHA256); ok {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "vcod")
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", f.URL, resp.Status)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".download-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	var r io.Reader = resp.Body
	if progress != nil {
		r = &counter{r: r, total: resp.ContentLength, fn: progress}
	}
	if _, err := io.Copy(io.MultiWriter(tmp, h), r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != f.SHA256 {
		return fmt.Errorf("%s: SHA-256 is %s, expected %s", f.URL, got, f.SHA256)
	}
	return os.Rename(tmp.Name(), dest)
}

// Verify reports whether path exists and has the digest want.
func Verify(path, want string) (bool, error) {
	fh, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer fh.Close()
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return false, err
	}
	return hex.EncodeToString(h.Sum(nil)) == want, nil
}

// ExtractTarBz2 copies the named members of a .tar.bz2 into dir, keeping their relative
// paths and executable bits. Every member must be found.
func ExtractTarBz2(archive, dir string, members []string) error {
	fh, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer fh.Close()
	want := map[string]bool{}
	for _, m := range members {
		want[m] = true
	}
	tr := tar.NewReader(bzip2.NewReader(fh))
	for len(want) > 0 {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("%s: %w", archive, err)
		}
		name := filepath.ToSlash(filepath.Clean(hdr.Name))
		if !want[name] || hdr.Typeflag != tar.TypeReg {
			continue
		}
		if err := writeFile(filepath.Join(dir, filepath.FromSlash(name)), tr, os.FileMode(hdr.Mode)&0o755|0o600, hdr.ModTime); err != nil {
			return err
		}
		delete(want, name)
	}
	for m := range want {
		return fmt.Errorf("%s has no %s", archive, m)
	}
	return nil
}

func writeFile(path string, r io.Reader, mode os.FileMode, mtime time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	_ = os.Chtimes(tmp, mtime, mtime)
	return os.Rename(tmp, path)
}

type counter struct {
	r     io.Reader
	done  int64
	total int64
	last  time.Time
	fn    Progress
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.done += int64(n)
	if now := time.Now(); err == io.EOF || now.Sub(c.last) > 500*time.Millisecond {
		c.last = now
		c.fn(c.done, c.total)
	}
	return n, err
}
