package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownload(t *testing.T) {
	body := []byte("cod_lnxded")
	sum := sha256.Sum256(body)
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write(body)
	}))
	defer srv.Close()
	dest := filepath.Join(t.TempDir(), "sub", "f.bin")
	f := File{URL: srv.URL, SHA256: hex.EncodeToString(sum[:])}
	if err := Download(context.Background(), f, dest, nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(body) {
		t.Fatalf("got %q", got)
	}
	// already there with the right digest: no second request
	if err := Download(context.Background(), f, dest, nil); err != nil || hits != 1 {
		t.Fatalf("err %v, hits %d", err, hits)
	}
}

func TestDownloadBadDigest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("tampered"))
	}))
	defer srv.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "f.bin")
	if err := Download(context.Background(), File{URL: srv.URL, SHA256: "00"}, dest, nil); err == nil {
		t.Fatal("bad digest accepted")
	}
	if _, err := os.Stat(dest); err == nil {
		t.Fatal("a file that failed verification was left in place")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "*")); len(left) != 0 {
		t.Fatalf("temporary files left: %v", left)
	}
}
