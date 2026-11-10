package lancmd

import (
	"testing"

	"vcod/internal/q3"
)

func info(addr, host, mapname, proto string) q3.Info {
	return q3.Info{Addr: addr, Vars: map[string]string{"hostname": host, "mapname": mapname, "protocol": proto}}
}

func TestPick(t *testing.T) {
	list := []q3.Info{
		info("192.168.1.2:28960", "^1Alpha15", "mp_harbor", "6"),
		info("192.168.1.3:28961", "Bravo16x", "mp_carentan", "10"),
	}
	if s, err := pick(list, "2"); err != nil || s.Addr != "192.168.1.3:28961" {
		t.Fatalf("by number: %v %v", s, err)
	}
	if s, err := pick(list, "alpha"); err != nil || s.Addr != "192.168.1.2:28960" {
		t.Fatalf("by name: %v %v", s, err)
	}
	if s, err := pick(list, "carentan"); err != nil || s.Get("hostname") != "Bravo16x" {
		t.Fatalf("by map: %v %v", s, err)
	}
	for _, bad := range []string{"", "3", "0", "192.168.1"} {
		if _, err := pick(list, bad); err == nil {
			t.Errorf("%q should not pick", bad)
		}
	}
	if s, err := pick(list[:1], ""); err != nil || s.Addr != "192.168.1.2:28960" {
		t.Fatalf("the only server: %v %v", s, err)
	}
	if f, ok := FlavorOf(&list[1]); !ok || f != "1.6x" {
		t.Fatalf("protocol 10 → %q", f)
	}
	if _, ok := FlavorOf(&q3.Info{Vars: map[string]string{"protocol": "1"}}); ok {
		t.Fatal("protocol 1 has no client here")
	}
}

func TestParseScan(t *testing.T) {
	so, rest, err := parseScan([]string{"2", "--timeout", "3s", "--port", "29000", "--add", "10.0.0.5", "--headless"})
	if err != nil {
		t.Fatal(err)
	}
	if so.timeout.Seconds() != 3 || len(so.ports) != 1 || so.extra[0] != "10.0.0.5:28960" {
		t.Fatalf("%+v", so)
	}
	if len(rest) != 2 || rest[0] != "2" || rest[1] != "--headless" {
		t.Fatalf("rest %v", rest)
	}
}
