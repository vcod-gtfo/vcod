package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFlavor(t *testing.T) {
	for in, want := range map[string]Flavor{"1.5": F15, "15": F15, "1.6x": F16x, "16x": F16x, "server-1.6x": F16x, "client-1.5": F15} {
		got, err := ParseFlavor(in)
		if err != nil || got != want {
			t.Errorf("ParseFlavor(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ParseFlavor("1.1"); err == nil {
		t.Error("accepted 1.1")
	}
}

func TestSaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := Save(path, map[string]string{KeyMap: "mp_dawnville", KeyFlavor: "1.6x"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VCOD_MAP", "")
	c, err := LoadFrom(path, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Get(KeyMap) != "mp_dawnville" || c.Flavor != F16x {
		t.Fatalf("map %q flavor %q", c.Get(KeyMap), c.Flavor)
	}
	if c.Get(KeyGametype) != "tdm" {
		t.Fatalf("default gametype = %q", c.Get(KeyGametype))
	}
	t.Setenv("VCOD_MAP", "mp_brecourt")
	if c.Get(KeyMap) != "mp_brecourt" {
		t.Fatal("environment did not override")
	}
	if err := Save(path, map[string]string{KeyMap: ""}); err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", st.Mode().Perm())
	}
	c, _ = LoadFrom(path, "1.5", true)
	if c.FileValue(KeyMap) != "" || c.Flavor != F15 {
		t.Fatalf("unset or flag failed: %q %q", c.FileValue(KeyMap), c.Flavor)
	}
}

func TestLegacyClientKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	if err := os.WriteFile(path, []byte("CLIENT_FULLSCREEN=1\nCLIENT_MODE=6\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{KeyClientRes, KeyClientDisplay, KeyClientMaxFPS} {
		t.Setenv("VCOD_"+k, "")
	}
	c, err := LoadFrom(path, "", false)
	if err != nil {
		t.Fatal(err)
	}
	v, err := c.Video()
	if err != nil {
		t.Fatal(err)
	}
	if v.Display != "fullscreen" || v.Res.Name != "1024x768" || v.MaxFPS != 125 {
		t.Fatalf("got %+v", v)
	}
	t.Setenv("VCOD_CLIENT_MAXFPS", "lots")
	if v, err := c.Video(); err == nil || v.MaxFPS != 125 {
		t.Fatalf("bad fps not reported: %+v %v", v, err)
	}
}
