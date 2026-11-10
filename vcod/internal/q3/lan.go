package q3

import (
	"context"
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

// LANPorts are the ports a LAN scan asks on: the four from 28960, as the game's own
// "Local" browser tab does (CL_LocalServers_f broadcasts getinfo to PORT_SERVER..+3).
var LANPorts = []int{28960, 28961, 28962, 28963}

// Info is a server's answer to getinfo: the short key list the browser shows.
type Info struct {
	Addr  string            // host:port the answer came from
	Vars  map[string]string // lowercased keys
	RTT   time.Duration
	Local bool // the answer came from one of this machine's own addresses
}

// Get reads an info key.
func (i *Info) Get(key string) string { return i.Vars[strings.ToLower(key)] }

// Protocol is the server's network protocol: 6 for 1.5, 10 for COD1.6X, 0 if unsaid.
func (i *Info) Protocol() int {
	n, _ := strconv.Atoi(i.Get("protocol"))
	return n
}

// Clients is the player count; CoD1 leaves the key out when the server is empty.
func (i *Info) Clients() int {
	n, _ := strconv.Atoi(i.Get("clients"))
	return n
}

// MaxClients is sv_maxclients.
func (i *Info) MaxClients() int {
	n, _ := strconv.Atoi(i.Get("sv_maxclients"))
	return n
}

// Port is the port part of Addr.
func (i *Info) Port() int {
	_, p, _ := net.SplitHostPort(i.Addr)
	n, _ := strconv.Atoi(p)
	return n
}

// ParseInfo decodes an infoResponse packet.
func ParseInfo(b []byte) (map[string]string, error) {
	body, ok := strip(b, "infoResponse")
	if !ok {
		return nil, errors.New("not an infoResponse")
	}
	line, _, _ := strings.Cut(body, "\n")
	vars := map[string]string{}
	parts := strings.Split(strings.TrimPrefix(line, "\\"), "\\")
	for i := 0; i+1 < len(parts); i += 2 {
		vars[strings.ToLower(parts[i])] = parts[i+1]
	}
	return vars, nil
}

// GetInfo asks one server (host:port) for its info.
func GetInfo(ctx context.Context, addr string, timeout time.Duration) (*Info, error) {
	start := time.Now()
	packets, err := exchange(ctx, addr, "getinfo xxx", timeout, true)
	if err != nil {
		return nil, err
	}
	vars, err := ParseInfo(packets[0])
	if err != nil {
		return nil, err
	}
	return &Info{Addr: addr, Vars: vars, RTT: time.Since(start), Local: isOwnAddr(addr)}, nil
}

// Discover finds the servers on the local network: it broadcasts getinfo on ports
// (LANPorts when empty) to the limited broadcast address and to every up IPv4
// interface's own broadcast address, and also asks extra (host:port) directly. The
// requests go out twice, as the game sends them, because UDP broadcasts get lost. It
// collects answers until timeout and returns them sorted by address.
//
// Both broadcast kinds go out because the systems differ: Linux sends 255.255.255.255
// through every route that takes it, macOS only through the primary interface, so a
// second network (a VPN, a wired link next to Wi-Fi) needs its directed broadcast.
func Discover(ctx context.Context, ports []int, extra []string, timeout time.Duration) ([]Info, error) {
	if len(ports) == 0 {
		ports = LANPorts
	}
	// Go sets SO_BROADCAST on its UDP sockets, so a plain listener can broadcast.
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var targets []*net.UDPAddr
	seen := map[string]bool{}
	add := func(ip net.IP, port int) {
		a := &net.UDPAddr{IP: ip, Port: port}
		if !seen[a.String()] {
			seen[a.String()] = true
			targets = append(targets, a)
		}
	}
	for _, bc := range append([]net.IP{net.IPv4bcast}, broadcastAddrs()...) {
		for _, p := range ports {
			add(bc, p)
		}
	}
	// The machine's own servers: broadcasts do not always loop back (macOS, no network).
	for _, p := range ports {
		add(net.IPv4(127, 0, 0, 1), p)
	}
	for _, e := range extra {
		if a, err := net.ResolveUDPAddr("udp4", e); err == nil {
			add(a.IP, a.Port)
		}
	}

	request := append(append([]byte{}, oob...), []byte("getinfo xxx")...)
	start := time.Now()
	sent := 0
	for round := 0; round < 2; round++ {
		for _, t := range targets {
			// One target failing (no route on a down link) must not end the scan.
			if _, err := conn.WriteToUDP(request, t); err == nil {
				sent++
			}
		}
		if round == 0 {
			time.Sleep(30 * time.Millisecond)
		}
	}
	if sent == 0 {
		return nil, errors.New("could not send to any address (no IPv4 network?)")
	}

	deadline := start.Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	found := map[string]*Info{}
	buf := make([]byte, 65536)
	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		_ = conn.SetReadDeadline(deadline)
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				break
			}
			// ICMP port unreachable from 127.0.0.1 surfaces here; keep listening.
			if time.Now().After(deadline) {
				break
			}
			continue
		}
		vars, err := ParseInfo(buf[:n])
		if err != nil {
			continue
		}
		addr := from.String()
		if _, dup := found[addr]; dup {
			continue
		}
		found[addr] = &Info{Addr: addr, Vars: vars, RTT: time.Since(start), Local: isOwnAddr(addr)}
	}
	return mergeLocal(found), nil
}

// mergeLocal drops a 127.0.0.1 answer when the same server also answered from one of
// this machine's network addresses, so a local server is listed once, under the
// address other machines reach it by.
func mergeLocal(found map[string]*Info) []Info {
	ports := map[int]bool{}
	for _, i := range found {
		if i.Local && !isLoopback(i.Addr) {
			ports[i.Port()] = true
		}
	}
	var out []Info
	for _, i := range found {
		if isLoopback(i.Addr) && ports[i.Port()] {
			continue
		}
		out = append(out, *i)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Local != out[b].Local {
			return out[a].Local
		}
		return lessAddr(out[a].Addr, out[b].Addr)
	})
	return out
}

func lessAddr(a, b string) bool {
	ha, pa, _ := net.SplitHostPort(a)
	hb, pb, _ := net.SplitHostPort(b)
	if ha != hb {
		ia, ib := net.ParseIP(ha).To4(), net.ParseIP(hb).To4()
		if ia != nil && ib != nil {
			return string(ia) < string(ib)
		}
		return ha < hb
	}
	na, _ := strconv.Atoi(pa)
	nb, _ := strconv.Atoi(pb)
	return na < nb
}

func isLoopback(addr string) bool {
	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// isOwnAddr reports whether addr's host is one of this machine's addresses.
func isOwnAddr(addr string) bool {
	h, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(h)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.Equal(ip) {
			return true
		}
	}
	return false
}

// broadcastAddrs is the directed broadcast address of every up, non-loopback IPv4
// interface that can broadcast.
func broadcastAddrs() []net.IP {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			if bc := directedBroadcast(n); bc != nil {
				out = append(out, bc)
			}
		}
	}
	return out
}

// directedBroadcast is a network's broadcast address, nil for IPv6 or a /31-/32.
func directedBroadcast(n *net.IPNet) net.IP {
	ip := n.IP.To4()
	if ip == nil || len(n.Mask) != net.IPv4len {
		return nil
	}
	if ones, _ := n.Mask.Size(); ones >= 31 {
		return nil
	}
	bc := make(net.IP, net.IPv4len)
	for i := range ip {
		bc[i] = ip[i] | ^n.Mask[i]
	}
	return bc
}
