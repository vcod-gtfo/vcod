package video

import (
	"strings"
	"testing"
)

func TestParseRes(t *testing.T) {
	cases := map[string]Res{
		"1080p":     {"1080p", 1920, 1080},
		"1080P":     {"1080p", 1920, 1080},
		"1920x1080": {"1080p", 1920, 1080},
		"1366×768":  {"1366x768", 1366, 768},
		"desktop":   {"desktop", 0, 0},
		"uw1440":    {"uw1440", 3440, 1440},
	}
	for in, want := range cases {
		got, err := ParseRes(in)
		if err != nil || got != want {
			t.Fatalf("%q: got %+v, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "big", "100x100", "1920x", "x1080", "99999x2"} {
		if _, err := ParseRes(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestParseDisplayAndFPS(t *testing.T) {
	for in, want := range map[string]Display{"window": Window, "fs": Fullscreen, "2": Borderless, "Borderless": Borderless} {
		if got, err := ParseDisplay(in); err != nil || got != want {
			t.Fatalf("%q: %v %v", in, got, err)
		}
	}
	if IsDisplay("1") || !IsDisplay("fullscreen") {
		t.Fatal("IsDisplay")
	}
	for in, want := range map[string]int{"125": 125, "333fps": 333, "333": 333, "777": 777, "off": 0, "0": 0, "unlocked": 0, "Unlimited": 0} {
		if got, err := ParseFPS(in); err != nil || got != want {
			t.Fatalf("%q: %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"-1", "1001", "fast"} {
		if _, err := ParseFPS(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestCvars(t *testing.T) {
	r, _ := ParseRes("1080p")
	s := Settings{Res: r, Display: Window, MaxFPS: 125}
	got := strings.Join(s.Cvars(true), " ")
	if got != "+set r_fullscreen 0 +set r_mode -1 +set r_customwidth 1920 +set r_customheight 1080 +set com_maxfps 125" {
		t.Fatal(got)
	}
	if got := strings.Join(s.Cvars(false), " "); got != "+set com_maxfps 125" {
		t.Fatal(got)
	}
	d, _ := ParseRes("desktop")
	s = Settings{Res: d, Display: Borderless, MaxFPS: 0}
	if got := strings.Join(s.Cvars(true), " "); got != "+set r_fullscreen 2 +set r_mode -2 +set com_maxfps 0" {
		t.Fatal(got)
	}
	mode := 3
	s.Mode = &mode
	if got := strings.Join(s.Cvars(true), " "); !strings.Contains(got, "r_mode 3") || strings.Contains(got, "custom") {
		t.Fatal(got)
	}
}

func TestNotes(t *testing.T) {
	r, _ := ParseRes("720p")
	if n := (Settings{Res: r, Display: Borderless, MaxFPS: 125}).Notes(); len(n) != 1 || !strings.Contains(n[0], "borderless") {
		t.Fatal(n)
	}
	if n := (Settings{Res: r, Display: Window, MaxFPS: 333}).Notes(); len(n) != 1 || !strings.Contains(n[0], "60–250") {
		t.Fatal(n)
	}
	if n := (Settings{Res: r, Display: Window, MaxFPS: 125}).Notes(); len(n) != 0 {
		t.Fatal(n)
	}
}

func TestResFromMode(t *testing.T) {
	if ResFromMode(-2) != "desktop" || ResFromMode(6) != "1024x768" || ResFromMode(-1) != "" || ResFromMode(99) != "" {
		t.Fatal(ResFromMode(6))
	}
}
