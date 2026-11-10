package q3

import (
	"net"
	"testing"
)

func TestParseInfo(t *testing.T) {
	pkt := []byte("\xff\xff\xff\xffinfoResponse\n\\challenge\\xxx\\protocol\\10\\hostname\\Bravo16x\\mapname\\mp_carentan\\sv_maxclients\\16\\gametype\\dm\\pure\\1")
	vars, err := ParseInfo(pkt)
	if err != nil {
		t.Fatal(err)
	}
	i := Info{Addr: "192.168.1.5:28961", Vars: vars}
	if i.Protocol() != 10 || i.MaxClients() != 16 || i.Clients() != 0 || i.Get("mapname") != "mp_carentan" || i.Port() != 28961 {
		t.Fatalf("parsed %+v", vars)
	}
	if _, err := ParseInfo([]byte("\xff\xff\xff\xffstatusResponse\n")); err == nil {
		t.Fatal("a statusResponse parsed as info")
	}
}

func TestDirectedBroadcast(t *testing.T) {
	for _, c := range []struct{ cidr, want string }{
		{"192.168.178.34/24", "192.168.178.255"},
		{"10.1.2.3/8", "10.255.255.255"},
		{"172.16.5.9/20", "172.16.15.255"},
	} {
		ip, n, _ := net.ParseCIDR(c.cidr)
		n.IP = ip
		if got := directedBroadcast(n); got.String() != c.want {
			t.Errorf("%s: %s, want %s", c.cidr, got, c.want)
		}
	}
	_, n, _ := net.ParseCIDR("10.0.0.1/32")
	if directedBroadcast(n) != nil {
		t.Error("a /32 has no broadcast")
	}
}

func TestMergeLocalDropsLoopbackTwin(t *testing.T) {
	addrs, _ := net.InterfaceAddrs()
	var own string
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
			own = n.IP.String()
			break
		}
	}
	if own == "" {
		t.Skip("no non-loopback IPv4 address")
	}
	found := map[string]*Info{
		"127.0.0.1:28960":   {Addr: "127.0.0.1:28960", Local: true},
		own + ":28960":      {Addr: own + ":28960", Local: true},
		"127.0.0.1:28962":   {Addr: "127.0.0.1:28962", Local: true},
		"203.0.113.9:28960": {Addr: "203.0.113.9:28960"},
	}
	out := mergeLocal(found)
	if len(out) != 3 {
		t.Fatalf("got %d: %+v", len(out), out)
	}
	for _, i := range out {
		if i.Addr == "127.0.0.1:28960" {
			t.Fatal("the loopback twin was kept")
		}
	}
	if !out[0].Local || out[len(out)-1].Local {
		t.Fatalf("local servers should sort first: %+v", out)
	}
}
