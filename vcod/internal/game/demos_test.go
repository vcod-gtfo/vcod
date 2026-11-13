package game

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestListDemos(t *testing.T) {
	home := t.TempDir()
	if d, err := ListDemos(home); err != nil || d != nil {
		t.Fatalf("no demos folder: got %v, %v", d, err)
	}
	dir := DemosDir(home)
	os.MkdirAll(dir, 0o755)
	write := func(name string, age time.Duration) {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("x"), 0o644)
		at := time.Now().Add(-age)
		os.Chtimes(p, at, at)
	}
	write("old.dm_2", 2*time.Hour)
	write("new.dm_2", time.Hour)
	write("other.dm_6", 0)
	write("notes.txt", 0)

	demos, err := ListDemos(home)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range demos {
		names = append(names, d.Name)
	}
	if len(names) != 3 || names[0] != "other" || names[1] != "new" || names[2] != "old" {
		t.Fatalf("names, newest first: %v", names)
	}
	if demos[0].Playable() || !demos[1].Playable() {
		t.Fatalf("only .dm_2 is playable: %+v", demos[:2])
	}
}
