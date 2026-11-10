// Package q3 speaks the connectionless half of the Quake 3 protocol that CoD1 servers
// answer on their game port: getstatus (server variables and the player list) and rcon.
// Every packet starts with four 0xff bytes.
package q3

import (
	"context"
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var oob = []byte{0xff, 0xff, 0xff, 0xff}

// Player is one line of a statusResponse.
type Player struct {
	Score int
	Ping  int
	Name  string
}

// Status is a server's answer to getstatus.
type Status struct {
	Vars    map[string]string
	Players []Player
	RTT     time.Duration
}

// Get reads a server variable.
func (s *Status) Get(key string) string { return s.Vars[strings.ToLower(key)] }

// Humans counts the players with a nonzero ping; bots and the demo probe ping 999 or 0.
func (s *Status) Humans() int {
	n := 0
	for _, p := range s.Players {
		if p.Ping > 0 && p.Ping < 999 {
			n++
		}
	}
	return n
}

// Keys lists the variable names, sorted.
func (s *Status) Keys() []string {
	out := make([]string, 0, len(s.Vars))
	for k := range s.Vars {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ErrNoAnswer is returned when the server did not answer in time.
var ErrNoAnswer = errors.New("no answer")

// GetStatus asks addr ("host:port") for its status.
func GetStatus(ctx context.Context, addr string, timeout time.Duration) (*Status, error) {
	start := time.Now()
	packets, err := exchange(ctx, addr, "getstatus", timeout, true)
	if err != nil {
		return nil, err
	}
	st, err := ParseStatus(packets[0])
	if err != nil {
		return nil, err
	}
	st.RTT = time.Since(start)
	return st, nil
}

// ParseStatus decodes a statusResponse packet.
func ParseStatus(b []byte) (*Status, error) {
	body, ok := strip(b, "statusResponse")
	if !ok {
		return nil, errors.New("not a statusResponse")
	}
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	st := &Status{Vars: map[string]string{}}
	if len(lines) > 0 {
		parts := strings.Split(strings.TrimPrefix(lines[0], "\\"), "\\")
		for i := 0; i+1 < len(parts); i += 2 {
			st.Vars[strings.ToLower(parts[i])] = parts[i+1]
		}
	}
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, " ", 3)
		if len(fields) < 3 {
			continue
		}
		score, _ := strconv.Atoi(fields[0])
		ping, _ := strconv.Atoi(fields[1])
		st.Players = append(st.Players, Player{Score: score, Ping: ping, Name: strings.Trim(fields[2], `"`)})
	}
	return st, nil
}

// rconGap is how far apart two rcon packets must be: the server (Quake 3's
// SVC_RemoteCommand) silently drops one that arrives within 500 ms of the last.
const rconGap = 550 * time.Millisecond

var (
	rconMu   sync.Mutex
	rconLast time.Time
)

// Rcon runs a console command on addr with the rcon password and returns what the server
// printed. A server prints long answers over several packets; this collects them until
// the server has been quiet for a moment.
//
// Calls from this process are spaced rconGap apart. A command that meets silence (another
// process's rcon just went through) is sent once more, except a map change, which a slow
// load could make look silent and which must not run twice.
func Rcon(ctx context.Context, addr, password, command string, timeout time.Duration) (string, error) {
	packets, err := rconOnce(ctx, addr, password, command, timeout)
	if errors.Is(err, ErrNoAnswer) && !ChangesMap(command) {
		packets, err = rconOnce(ctx, addr, password, command, timeout)
	}
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, p := range packets {
		if body, ok := strip(p, "print"); ok {
			b.WriteString(body)
		}
	}
	out := b.String()
	if strings.HasPrefix(strings.TrimSpace(out), "Bad rcon") {
		return out, errors.New("the server refused the rcon password (it was started with another one, or without one)")
	}
	return out, nil
}

func rconOnce(ctx context.Context, addr, password, command string, timeout time.Duration) ([][]byte, error) {
	rconMu.Lock()
	if wait := rconGap - time.Since(rconLast); wait > 0 {
		select {
		case <-ctx.Done():
			rconMu.Unlock()
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	defer func() {
		rconLast = time.Now()
		rconMu.Unlock()
	}()
	return exchange(ctx, addr, "rcon "+password+" "+command, timeout, false)
}

// ChangesMap reports whether a console command loads or restarts the map.
func ChangesMap(command string) bool {
	f := strings.Fields(strings.ToLower(command))
	if len(f) == 0 {
		return false
	}
	switch f[0] {
	case "map", "devmap", "map_restart", "fast_restart", "map_rotate", "spdevmap":
		return true
	}
	return false
}

// exchange sends one request and gathers the answers. With first set it returns as soon
// as one packet arrives; otherwise it keeps reading until 250 ms pass without one.
func exchange(ctx context.Context, addr, request string, timeout time.Duration, first bool) ([][]byte, error) {
	conn, err := net.Dial("udp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if _, err := conn.Write(append(append([]byte{}, oob...), []byte(request)...)); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	var packets [][]byte
	buf := make([]byte, 65536)
	for {
		wait := deadline
		if len(packets) > 0 {
			if quiet := time.Now().Add(250 * time.Millisecond); quiet.Before(wait) {
				wait = quiet
			}
		}
		_ = conn.SetReadDeadline(wait)
		n, err := conn.Read(buf)
		if err != nil {
			if len(packets) > 0 {
				return packets, nil
			}
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				return nil, ErrNoAnswer
			}
			// A refused port answers with ICMP, which surfaces as a read error at once.
			return nil, ErrNoAnswer
		}
		packets = append(packets, append([]byte{}, buf[:n]...))
		if first {
			return packets, nil
		}
	}
}

// strip removes the out-of-band header and the expected command word.
func strip(b []byte, word string) (string, bool) {
	if len(b) < 4 || string(b[:4]) != string(oob) {
		return "", false
	}
	s := string(b[4:])
	if !strings.HasPrefix(s, word) {
		return "", false
	}
	s = strings.TrimPrefix(s, word)
	s = strings.TrimPrefix(s, "\n")
	s = strings.TrimPrefix(s, " ")
	return s, true
}

// StripColors removes the ^N colour codes from a name or hostname.
func StripColors(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '^' && i+1 < len(s) && s[i+1] != '^' {
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
