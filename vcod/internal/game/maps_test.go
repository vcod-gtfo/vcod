package game

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePk3(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	f.Close()
}

func TestParseArena(t *testing.T) {
	src := "{\n\tmap\t\t\"mp_a\"\n\tlongname\t\"Alpha Town\"\n\tgametype\t\"dm TDM sd\" // x\n}\n\n{\n map \"mp_b\"\n}\n"
	got := parseArena(strings.NewReader(src))
	if len(got) != 2 || got[0].name != "mp_a" || got[0].longname != "Alpha Town" ||
		strings.Join(got[0].gametypes, ",") != "dm,tdm,sd" || got[1].name != "mp_b" {
		t.Fatalf("got %+v", got)
	}
}

func TestListMaps(t *testing.T) {
	dir := t.TempDir()
	writePk3(t, filepath.Join(dir, "pak0.pk3"), map[string]string{
		"maps/MP/mp_harbor.bsp": "",
		"mp/CoDmaps.arena":      "{\nmap \"mp_harbor\"\nlongname \"mp_harbor\"\ngametype \"dm tdm\"\n}\n",
	})
	writePk3(t, filepath.Join(dir, "z_harbor.pk3"), map[string]string{"maps/mp/mp_harbor.bsp": ""})
	writePk3(t, filepath.Join(dir, "mp_custom.pk3"), map[string]string{"maps/mp/mp_custom.bsp": ""})
	os.MkdirAll(filepath.Join(dir, "maps", "mp"), 0o755)
	os.WriteFile(filepath.Join(dir, "maps", "mp", "mp_loose.bsp"), nil, 0o644)

	maps, err := ListMaps(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range maps {
		names = append(names, m.Name+"@"+m.Source)
	}
	if want := "mp_harbor@z_harbor.pk3 mp_custom@mp_custom.pk3 mp_loose@loose"; strings.Join(names, " ") != want {
		t.Fatalf("got %v, want %s", names, want)
	}
	if !maps[0].Stock || !maps[0].Supports("tdm") || maps[0].Supports("sd") || maps[1].Stock {
		t.Fatalf("got %+v", maps)
	}
}
