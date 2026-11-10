// Package lancmd is `vcod lan`: find the servers on the local network and join one.
package lancmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	clientcmd "vcod/cmd/client"
	"vcod/internal/app"
	"vcod/internal/config"
	"vcod/internal/game"
	"vcod/internal/q3"
	"vcod/internal/registry"
	"vcod/internal/ui"
)

const help = `Usage:
  vcod lan [list] [--json]           the servers answering on the local network
  vcod lan join [N|text] [client options]
                                     join one: its number in the list, or a piece of
                                     its name, address or map; with one server, that one

Options:
  --timeout D         how long to listen for answers (default 1.5s; e.g. 3s, 800ms)
  --port N            also scan this port (repeatable); 28960–28963 always are
  --add host:port     also ask this server directly (one a broadcast cannot reach)

The scan works as the game's own "Local" tab: it broadcasts getinfo to ports 28960–28963
on every network this machine is on, and lists whoever answers. A server's protocol
picks the client: 6 joins with the 1.5 client, 10 with the 1.6x one, whatever -F says.
Anything else after 'join' goes to the client as in 'vcod client' (--headless, --res,
--window, -- +set ...).

A server another machine should find must be reachable on its UDP port: vcod's
servers publish theirs on every interface, so only a firewall stands in the way
(firewall-cmd --add-port=28960-28963/udp on Fedora; on macOS, allow the incoming
connection when asked).`

func init() {
	registry.Register(registry.Command{
		Name:        "lan",
		Summary:     "Find servers on the local network and join one",
		Args:        "[list|join] [N|text]",
		Help:        help,
		Group:       registry.GroupPlay,
		Order:       35,
		NeedsConfig: true,
		Complete: func(prev []string) []string {
			if len(prev) > 0 && prev[0] == "join" {
				if v := clientcmd.FlagValues(prev); v != nil {
					return v
				}
				return []string{"--timeout", "--port", "--add", "--headless", "--video", "--res", "--window",
					"--fullscreen", "--borderless", "--maxfps", "--gpu", "--build"}
			}
			return []string{"list", "join", "--json", "--timeout", "--port", "--add"}
		},
		Run: run,
	})
}

type scanOpts struct {
	timeout time.Duration
	ports   []int
	extra   []string
}

// parseScan takes the scan flags out of args and returns the rest.
func parseScan(args []string) (scanOpts, []string, error) {
	o := scanOpts{timeout: 1500 * time.Millisecond}
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		switch a {
		case "--timeout", "--port", "--add":
			if i+1 >= len(args) {
				return o, nil, fmt.Errorf("%s needs a value", a)
			}
			i++
			v := args[i]
			switch a {
			case "--timeout":
				d, err := time.ParseDuration(v)
				if err != nil {
					if n, err2 := strconv.ParseFloat(v, 64); err2 == nil {
						d = time.Duration(n * float64(time.Second))
					} else {
						return o, nil, fmt.Errorf("bad timeout %q (e.g. 2s)", v)
					}
				}
				if d <= 0 || d > time.Minute {
					return o, nil, fmt.Errorf("timeout %s out of range", d)
				}
				o.timeout = d
			case "--port":
				n, err := strconv.Atoi(v)
				if err != nil || n <= 0 || n > 65535 {
					return o, nil, fmt.Errorf("bad port %q", v)
				}
				o.ports = append(o.ports, n)
			case "--add":
				if !strings.Contains(v, ":") {
					v += ":28960"
				}
				o.extra = append(o.extra, v)
			}
		default:
			rest = append(rest, a)
		}
	}
	return o, rest, nil
}

func run(ctx *app.Context, args []string) int {
	sub := "list"
	if len(args) > 0 && (args[0] == "list" || args[0] == "ls" || args[0] == "join" || args[0] == "connect") {
		sub, args = args[0], args[1:]
	}
	so, rest, err := parseScan(args)
	if err != nil {
		return ctx.Usage("%v  (try: vcod help lan)", err)
	}
	if sub == "join" || sub == "connect" {
		return join(ctx, so, rest)
	}
	for _, a := range rest {
		if a == "--json" {
			ctx.JSON = true
		} else {
			return ctx.Usage("lan list takes --json, --timeout, --port and --add, not %s", a)
		}
	}
	servers, err := scan(ctx, so)
	if err != nil {
		return ctx.Fail(err)
	}
	if ctx.JSON {
		return printJSON(ctx, servers)
	}
	ctx.Out.Section("lan servers", strconv.Itoa(len(servers))+" answering")
	if len(servers) == 0 {
		ctx.Out.Note("no server answered on ports %s within %s", portList(so), so.timeout)
		ctx.Out.Note("start one: vcod server start   ·   a firewall can hide them: vcod help lan")
		return 0
	}
	ctx.Out.Listing(rows(servers))
	ctx.Out.Plain("")
	ctx.Out.Note("join: vcod lan join N   (N from the first column, or a piece of the name)")
	return 0
}

func scan(ctx *app.Context, so scanOpts) ([]q3.Info, error) {
	ports := append(append([]int{}, q3.LANPorts...), so.ports...)
	if ctx.Cfg != nil {
		for _, f := range config.Flavors {
			ports = append(ports, ctx.Cfg.Port(f))
		}
	}
	return q3.Discover(ctx.Ctx, uniq(ports), so.extra, so.timeout)
}

func uniq(in []int) []int {
	seen := map[int]bool{}
	var out []int
	for _, n := range in {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

func portList(so scanOpts) string {
	s := "28960–28963"
	for _, p := range so.ports {
		s += ", " + strconv.Itoa(p)
	}
	return s
}

// FlavorOf is the client flavor that speaks a server's protocol.
func FlavorOf(i *q3.Info) (config.Flavor, bool) {
	switch i.Protocol() {
	case 6:
		return config.F15, true
	case 10:
		return config.F16x, true
	}
	return "", false
}

func rows(servers []q3.Info) [][]string {
	var out [][]string
	for n, s := range servers {
		flavor := "proto " + strconv.Itoa(s.Protocol())
		if f, ok := FlavorOf(&s); ok {
			flavor = ui.FlavorBadge(string(f))
		}
		where := s.Addr
		if s.Local {
			where += ui.StyleDim.Render(" (this machine)")
		}
		pure := ""
		if s.Get("pure") == "1" {
			pure = ui.StyleDim.Render(" pure")
		}
		out = append(out, []string{
			ui.StyleWhite.Render(strconv.Itoa(n + 1)),
			flavor,
			ui.StyleWhite.Render(q3.StripColors(s.Get("hostname"))),
			s.Get("mapname") + " " + s.Get("gametype") + pure,
			strconv.Itoa(s.Clients()) + "/" + strconv.Itoa(s.MaxClients()) + " players",
			where,
			ui.StyleDim.Render(strconv.Itoa(int(s.RTT.Milliseconds())) + " ms"),
		})
	}
	return out
}

type jsonServer struct {
	Addr     string            `json:"addr"`
	Hostname string            `json:"hostname"`
	Map      string            `json:"map"`
	Gametype string            `json:"gametype"`
	Clients  int               `json:"clients"`
	Max      int               `json:"max_clients"`
	Protocol int               `json:"protocol"`
	Flavor   string            `json:"flavor,omitempty"`
	Local    bool              `json:"this_machine"`
	RTTms    int64             `json:"rtt_ms"`
	Vars     map[string]string `json:"vars"`
}

func printJSON(ctx *app.Context, servers []q3.Info) int {
	out := []jsonServer{}
	for _, s := range servers {
		js := jsonServer{Addr: s.Addr, Hostname: q3.StripColors(s.Get("hostname")), Map: s.Get("mapname"),
			Gametype: s.Get("gametype"), Clients: s.Clients(), Max: s.MaxClients(), Protocol: s.Protocol(),
			Local: s.Local, RTTms: s.RTT.Milliseconds(), Vars: s.Vars}
		if f, ok := FlavorOf(&s); ok {
			js.Flavor = string(f)
		}
		out = append(out, js)
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	if ctx.Out != ui.Std {
		ctx.Out.Plain("%s", b)
		return 0
	}
	fmt.Fprintln(os.Stdout, string(b))
	return 0
}

// pick chooses the server a join means: a list number, else the one server whose name,
// address or map holds the text, else the only server there is.
func pick(servers []q3.Info, sel string) (*q3.Info, error) {
	if len(servers) == 0 {
		return nil, fmt.Errorf("no server answered on the local network")
	}
	if sel == "" {
		if len(servers) == 1 {
			return &servers[0], nil
		}
		return nil, fmt.Errorf("%d servers answered — say which: vcod lan join N (vcod lan lists them)", len(servers))
	}
	if n, err := strconv.Atoi(sel); err == nil {
		if n < 1 || n > len(servers) {
			return nil, fmt.Errorf("no server %d: the list has %d", n, len(servers))
		}
		return &servers[n-1], nil
	}
	want := strings.ToLower(sel)
	var hits []*q3.Info
	for i := range servers {
		s := &servers[i]
		hay := strings.ToLower(q3.StripColors(s.Get("hostname")) + " " + s.Addr + " " + s.Get("mapname"))
		if strings.Contains(hay, want) {
			hits = append(hits, s)
		}
	}
	switch len(hits) {
	case 0:
		return nil, fmt.Errorf("no server matches %q", sel)
	case 1:
		return hits[0], nil
	}
	return nil, fmt.Errorf("%d servers match %q — use the list number", len(hits), sel)
}

func join(ctx *app.Context, so scanOpts, args []string) int {
	sel := ""
	var clientArgs []string
	for i, a := range args {
		if a == "--" {
			clientArgs = append(clientArgs, args[i:]...)
			break
		}
		// The first bare word is the choice; the client's own flags pass through,
		// with the value of a flag that takes one.
		if sel == "" && !strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "+") &&
			(i == 0 || !game.ValueFlags[args[i-1]]) {
			sel = a
			continue
		}
		clientArgs = append(clientArgs, a)
	}
	servers, err := scan(ctx, so)
	if err != nil {
		return ctx.Fail(err)
	}
	s, err := pick(servers, sel)
	if err != nil {
		ctx.Errorf("%v", err)
		if len(servers) > 0 {
			ctx.Out.Listing(rows(servers))
		}
		return 1
	}
	f, ok := FlavorOf(s)
	if !ok {
		return ctx.Fail(fmt.Errorf("%s speaks protocol %d; the clients here speak 6 (1.5) and 10 (1.6x)", s.Addr, s.Protocol()))
	}
	addr := s.Addr
	if s.Local {
		// This machine's own server: loopback skips the firewall and the port forward.
		addr = "127.0.0.1:" + strconv.Itoa(s.Port())
	}
	ctx.Out.Info("joining %s: %s %s on %s (protocol %d → %s client)", q3.StripColors(s.Get("hostname")),
		s.Get("mapname"), s.Get("gametype"), s.Addr, s.Protocol(), f)
	// The flavor comes from the server, not -F: run the client on a copy of the context.
	cfg := *ctx.Cfg
	cfg.Flavor = f
	sub := *ctx
	sub.Cfg = &cfg
	return clientcmd.Start(&sub, append([]string{addr}, clientArgs...))
}
