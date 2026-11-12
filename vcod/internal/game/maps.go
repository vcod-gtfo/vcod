package game

import (
	"archive/zip"
	"bufio"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MapInfo is one map an install can load: a maps/mp/<name>.bsp in a pk3 or loose
// under main/, with the gametypes its .arena file lists.
type MapInfo struct {
	Name      string   `json:"name"`
	LongName  string   `json:"longname,omitempty"`
	Gametypes []string `json:"gametypes"`
	// Source is the pk3 (or "loose") the effective .bsp comes from: the last one in
	// load order, as the game picks it.
	Source string `json:"source"`
	Stock  bool   `json:"stock"`
}

// arenaEntry is one { map … gametype … } block of an .arena file.
type arenaEntry struct {
	name, longname string
	gametypes      []string
}

// ListMaps scans a game folder (e.g. flavors/server-1.5/main) the way the engine's
// filesystem sees it: pk3s in alphabetical order, a later one overriding an earlier,
// then loose files. Maps without an .arena entry are listed with no gametypes.
func ListMaps(dir string) ([]MapInfo, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var pk3s []string
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".pk3") {
			pk3s = append(pk3s, e.Name())
		}
	}
	sort.Strings(pk3s)

	bsps := map[string]string{} // lower-case name → source
	arenas := map[string]arenaEntry{}
	for _, p := range pk3s {
		zr, err := zip.OpenReader(filepath.Join(dir, p))
		if err != nil {
			continue // a broken pk3 is skipped, as the engine does
		}
		for _, f := range zr.File {
			lower := strings.ToLower(f.Name)
			switch {
			case strings.HasPrefix(lower, "maps/mp/") && strings.HasSuffix(lower, ".bsp") && !strings.Contains(lower[len("maps/mp/"):], "/"):
				bsps[strings.TrimSuffix(lower[len("maps/mp/"):], ".bsp")] = p
			case strings.HasSuffix(lower, ".arena") && (strings.HasPrefix(lower, "mp/") || strings.HasPrefix(lower, "scripts/")):
				if rc, err := f.Open(); err == nil {
					for _, a := range parseArena(rc) {
						arenas[strings.ToLower(a.name)] = a
					}
					rc.Close()
				}
			}
		}
		zr.Close()
	}
	// Loose files come after every pk3.
	for _, sub := range []string{"maps/mp", "maps/MP"} {
		if ls, err := os.ReadDir(filepath.Join(dir, sub)); err == nil {
			for _, e := range ls {
				if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".bsp") {
					bsps[strings.ToLower(strings.TrimSuffix(e.Name(), filepath.Ext(e.Name())))] = "loose"
				}
			}
		}
	}

	stock := map[string]bool{}
	for _, m := range Maps {
		stock[m] = true
	}
	out := make([]MapInfo, 0, len(bsps))
	for name, src := range bsps {
		mi := MapInfo{Name: name, Source: src, Stock: stock[name], Gametypes: []string{}}
		if a, ok := arenas[name]; ok {
			mi.Name = a.name
			mi.LongName = a.longname
			mi.Gametypes = a.gametypes
		}
		out = append(out, mi)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Stock != out[j].Stock {
			return out[i].Stock
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// Supports reports whether the map's .arena lists the gametype.
func (m MapInfo) Supports(gt string) bool {
	for _, g := range m.Gametypes {
		if strings.EqualFold(g, gt) {
			return true
		}
	}
	return false
}

// parseArena reads the { key "value" … } blocks of an .arena file.
func parseArena(r io.Reader) []arenaEntry {
	var out []arenaEntry
	var cur *arenaEntry
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		switch {
		case line == "":
		case strings.HasPrefix(line, "{"):
			cur = &arenaEntry{}
		case strings.HasPrefix(line, "}"):
			if cur != nil && cur.name != "" {
				out = append(out, *cur)
			}
			cur = nil
		case cur != nil:
			key, val := splitKV(line)
			switch strings.ToLower(key) {
			case "map":
				cur.name = val
			case "longname":
				cur.longname = val
			case "gametype":
				cur.gametypes = strings.Fields(strings.ToLower(val))
			}
		}
	}
	return out
}

func splitKV(line string) (string, string) {
	f := strings.Fields(line)
	if len(f) == 0 {
		return "", ""
	}
	val := strings.TrimSpace(line[len(f[0]):])
	return f[0], strings.Trim(val, "\"")
}
