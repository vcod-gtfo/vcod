package game

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DemoInfo is one recorded demo in a client home.
type DemoInfo struct {
	Name    string    `json:"name"`    // what 'demo <name>' takes: the file name without .dm_N
	File    string    `json:"file"`    // the full path
	Size    int64     `json:"size"`    // bytes
	ModTime time.Time `json:"modtime"` // when the recording ended
}

// DemosDir is where the client of a home records and plays demos: <home>/main/demos.
func DemosDir(home string) string {
	return filepath.Join(home, "main", "demos")
}

// ListDemos lists the demos (*.dm_<n>) under a client home, newest first. A home with
// no demos folder has none, without an error.
func ListDemos(home string) ([]DemoInfo, error) {
	dir := DemosDir(home)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []DemoInfo
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if !strings.HasPrefix(strings.ToLower(ext), ".dm_") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, DemoInfo{
			Name:    strings.TrimSuffix(e.Name(), ext),
			File:    filepath.Join(dir, e.Name()),
			Size:    fi.Size(),
			ModTime: fi.ModTime(),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].ModTime.Equal(out[j].ModTime) {
			return out[i].ModTime.After(out[j].ModTime)
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// Playable reports whether the client plays the demo: retail and the port only open
// .dm_2 (a literal 2 in CoDMP, not the protocol).
func (d DemoInfo) Playable() bool {
	return strings.EqualFold(filepath.Ext(d.File), ".dm_2")
}
