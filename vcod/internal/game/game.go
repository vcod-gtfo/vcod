// Package game starts, stops and inspects the servers and clients, for the subcommands
// and the shell alike. Nothing here prints; the callers decide how results look.
//
// A server is a flavor folder (flavors/server-1.5, server-1.6x) run by cod_rea's
// server/run.sh in its i386 podman container. A client is cod_rea's native cod_client
// (the video build by default) pointed at a client flavor folder.
package game

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"vcod/internal/config"
	"vcod/internal/jobs"
	"vcod/internal/q3"
	"vcod/internal/ws"
)

// Gametypes are the stock gametypes.
var Gametypes = []string{"dm", "tdm", "sd", "re", "bel", "hq"}

// Maps are the stock multiplayer maps (custom ones work too; these complete).
var Maps = []string{
	"mp_bocage", "mp_brecourt", "mp_carentan", "mp_chateau", "mp_dawnville", "mp_depot",
	"mp_harbor", "mp_hurtgen", "mp_neuville", "mp_pavlov", "mp_powcamp", "mp_railyard",
	"mp_rocket", "mp_ship", "mp_stalingrad", "mp_tigertown",
}

// ServerJob and ClientJob name a flavor's jobs.
func ServerJob(f config.Flavor) string { return "server-" + string(f) }
func ClientJob(f config.Flavor) string { return "client-" + string(f) }

// Container is the podman container run.sh names a server flavor's container.
func Container(f config.Flavor) string {
	name := "server-" + string(f)
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return "cod_rea_" + b.String()
}

// ServerOpts is what a server is started with.
type ServerOpts struct {
	Flavor   config.Flavor
	Map      string
	Gametype string
	Port     int
	Cheats   bool
	Pure     bool
	Public   bool
	Extra    []string
}

// DefaultServer fills the options from the configuration.
func DefaultServer(cfg *config.Config, f config.Flavor) ServerOpts {
	return ServerOpts{
		Flavor:   f,
		Map:      cfg.Get(config.KeyMap),
		Gametype: cfg.Get(config.KeyGametype),
		Port:     cfg.Port(f),
		Cheats:   cfg.Bool(config.KeyCheats),
	}
}

// ParseServerArgs reads "[map] [gametype] [--port N] [--cheats] [--pure] [--public]
// [-- +set ...]" over defaults. Words that look like a map or a gametype fill those slots
// in any order.
func ParseServerArgs(o ServerOpts, args []string) (ServerOpts, error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			o.Extra = append(o.Extra, args[i+1:]...)
			return o, nil
		case a == "--port" || a == "-p":
			if i+1 >= len(args) {
				return o, fmt.Errorf("%s needs a number", a)
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n <= 0 || n > 65535 {
				return o, fmt.Errorf("bad port %q", args[i])
			}
			o.Port = n
		case a == "--cheats" || a == "--devmap":
			o.Cheats = true
		case a == "--no-cheats":
			o.Cheats = false
		case a == "--pure":
			o.Pure = true
		case a == "--public":
			o.Public = true
		case strings.HasPrefix(a, "+"):
			o.Extra = append(o.Extra, args[i:]...)
			return o, nil
		case strings.HasPrefix(a, "-"):
			return o, fmt.Errorf("unknown flag %s", a)
		case isGametype(a):
			o.Gametype = a
		default:
			o.Map = a
		}
	}
	return o, nil
}

func isGametype(s string) bool {
	for _, g := range Gametypes {
		if g == s {
			return true
		}
	}
	return false
}

// Addr is where a local server answers.
func (o ServerOpts) Addr() string { return "127.0.0.1:" + strconv.Itoa(o.Port) }

// ContainerState asks podman whether a container is running: "running", "" when there
// is no such container, or podman's state word.
func ContainerState(name string) string {
	out, err := exec.Command("podman", "inspect", "--format", "{{.State.Status}}", name).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// StartServer launches a server flavor as a background job.
func StartServer(cfg *config.Config, w *ws.Workspace, o ServerOpts) (*jobs.Job, error) {
	dir := w.ServerDir(string(o.Flavor))
	if _, err := os.Stat(filepath.Join(dir, "cod_lnxded")); err != nil {
		return nil, fmt.Errorf("no server flavor at %s — build it with: vcod flavors make", dir)
	}
	if _, err := exec.LookPath("podman"); err != nil {
		return nil, errors.New("podman is not installed; the servers run in its i386 container")
	}
	name := ServerJob(o.Flavor)
	if j := jobs.Get(name); j.Running() {
		return j, fmt.Errorf("%s is %w (pid %d, %s on port %d) — stop it first, or use restart", name, jobs.ErrRunning, j.PID, j.Map, j.Port)
	}
	container := Container(o.Flavor)
	if ContainerState(container) == "running" {
		return nil, fmt.Errorf("container %s is already running outside vcod (run.sh by hand, or a test) — vcod server stop %s ends it", container, o.Flavor)
	}
	// CoD1 names the cvar rconPassword (Quake 3: rcon_password).
	extra := []string{"+set", "rconPassword", cfg.Rcon()}
	if o.Cheats {
		extra = append(extra, "+set", "sv_cheats", "1")
	}
	if o.Pure {
		extra = append(extra, "+set", "sv_pure", "1")
	}
	if s := cfg.Get(config.KeyServerExtra); s != "" {
		extra = append(extra, strings.Fields(s)...)
	}
	extra = append(extra, o.Extra...)
	env := []string{
		"PORT=" + strconv.Itoa(o.Port),
		"EXTRA=" + strings.Join(extra, " "),
	}
	if o.Cheats {
		env = append(env, "MAPCMD=devmap")
	}
	if o.Public {
		env = append(env, "DEDICATED=2")
	} else {
		env = append(env, "DEDICATED=1")
	}
	if o.Flavor == config.F16x && cfg.Bool(config.KeyAllowUnversion) {
		env = append(env, "ALLOW_UNVERSIONED=1")
	}
	return jobs.Start(jobs.Spec{
		Job: jobs.Job{
			Name: name, Kind: "server", Flavor: string(o.Flavor),
			Args:      []string{"sh", w.RunScript(), dir, o.Map, o.Gametype},
			Dir:       w.CodRea(),
			Container: container, Port: o.Port, Map: o.Map, Gametype: o.Gametype,
		},
		Env: env,
	})
}

// StopServer stops a flavor's server: the job when there is one, the container either way.
func StopServer(f config.Flavor) (bool, error) {
	stopped := false
	if j := jobs.Get(ServerJob(f)); j.Running() {
		if err := jobs.Stop(j, 8*time.Second); err != nil {
			return false, err
		}
		stopped = true
	}
	c := Container(f)
	if st := ContainerState(c); st == "running" || st == "created" || st == "paused" {
		if err := exec.Command("podman", "stop", "-t", "3", c).Run(); err != nil {
			return stopped, fmt.Errorf("podman stop %s: %w", c, err)
		}
		stopped = true
	}
	return stopped, nil
}

// WaitReady polls a server until it answers getstatus. It gives up when the timeout
// passes, the context ends, or the job that should become the server exits; tick is
// called about every second with the time waited.
func WaitReady(ctx context.Context, addr string, job *jobs.Job, timeout time.Duration, tick func(time.Duration)) (*q3.Status, error) {
	start := time.Now()
	last := time.Duration(0)
	for {
		st, err := q3.GetStatus(ctx, addr, 400*time.Millisecond)
		if err == nil {
			return st, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		waited := time.Since(start)
		if job != nil && !job.Running() {
			return nil, fmt.Errorf("the server exited before it answered — see: vcod logs server %s", job.Flavor)
		}
		if waited > timeout {
			return nil, fmt.Errorf("no answer from %s after %s (the first start builds the podman image; raise READY_TIMEOUT if that is slow)", addr, waited.Round(time.Second))
		}
		if tick != nil && waited-last >= time.Second {
			last = waited
			tick(waited)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// ClientOpts is what a client is started with.
type ClientOpts struct {
	Flavor     config.Flavor
	Build      *ws.Build
	Connect    string
	Fullscreen bool
	Mode       int
	Extra      []string
}

// DefaultClient fills the options from the configuration.
func DefaultClient(cfg *config.Config, f config.Flavor) ClientOpts {
	b := ws.LookupBuild(cfg.Get(config.KeyClientBuild))
	if b == nil {
		b = ws.LookupBuild("video")
	}
	return ClientOpts{
		Flavor:     f,
		Build:      b,
		Fullscreen: cfg.Bool(config.KeyClientFull),
		Mode:       cfg.Int(config.KeyClientMode, -1),
	}
}

// ParseClientArgs reads "[host:port] [--headless|--video] [--fullscreen|--window]
// [--mode N] [-- +set ...]" over defaults.
func ParseClientArgs(o ClientOpts, args []string) (ClientOpts, error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			o.Extra = append(o.Extra, args[i+1:]...)
			return o, nil
		case a == "--headless":
			o.Build = ws.LookupBuild("headless")
		case a == "--video":
			o.Build = ws.LookupBuild("video")
		case a == "--fullscreen" || a == "--fs":
			o.Fullscreen = true
		case a == "--window" || a == "--windowed":
			o.Fullscreen = false
		case a == "--mode":
			if i+1 >= len(args) {
				return o, errors.New("--mode needs a number")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil {
				return o, fmt.Errorf("bad mode %q", args[i])
			}
			o.Mode = n
		case a == "--connect" || a == "-c":
			if i+1 >= len(args) {
				return o, errors.New("--connect needs host:port")
			}
			i++
			o.Connect = args[i]
		case strings.HasPrefix(a, "+"):
			o.Extra = append(o.Extra, args[i:]...)
			return o, nil
		case strings.HasPrefix(a, "-"):
			return o, fmt.Errorf("unknown flag %s", a)
		default:
			o.Connect = a
		}
	}
	return o, nil
}

// ClientArgs is the command line a client runs with.
func ClientArgs(cfg *config.Config, w *ws.Workspace, o ClientOpts) []string {
	args := []string{
		w.ClientBinary(o.Build),
		"+set", "fs_basepath", w.ClientDir(string(o.Flavor)),
		"+set", "fs_homepath", cfg.ClientHome(o.Flavor),
		"+set", "dedicated", "0",
		"+set", "net_ecosystem", string(o.Flavor),
	}
	if o.Build.Name == "video" {
		full := "0"
		if o.Fullscreen {
			full = "1"
		}
		args = append(args, "+set", "r_fullscreen", full, "+set", "r_mode", strconv.Itoa(o.Mode))
	}
	if s := cfg.Get(config.KeyClientExtra); s != "" {
		args = append(args, strings.Fields(s)...)
	}
	args = append(args, o.Extra...)
	if o.Connect != "" {
		args = append(args, "+connect", o.Connect)
	}
	return args
}

// StartClient launches a client as a background job.
func StartClient(cfg *config.Config, w *ws.Workspace, o ClientOpts) (*jobs.Job, error) {
	st := w.State(o.Build)
	if !st.Exists {
		return nil, fmt.Errorf("no %s client at %s — build it with: vcod build %s", o.Build.Name, st.Binary, o.Build.Name)
	}
	dir := w.ClientDir(string(o.Flavor))
	if _, err := os.Stat(dir); err != nil {
		return nil, fmt.Errorf("no client flavor at %s — build it with: vcod flavors make", dir)
	}
	if err := os.MkdirAll(cfg.ClientHome(o.Flavor), 0o755); err != nil {
		return nil, err
	}
	return jobs.Start(jobs.Spec{
		Job: jobs.Job{
			Name: ClientJob(o.Flavor), Kind: "client", Flavor: string(o.Flavor),
			Args: ClientArgs(cfg, w, o), Dir: w.BuildDir(o.Build), Connect: o.Connect,
		},
	})
}

// StopClient stops a flavor's client job.
func StopClient(f config.Flavor) (bool, error) {
	j := jobs.Get(ClientJob(f))
	if !j.Running() {
		return false, nil
	}
	return true, jobs.Stop(j, 3*time.Second)
}

// Server is what status knows about one flavor's server.
type Server struct {
	Flavor    config.Flavor
	Job       *jobs.Job
	Container string
	State     string // podman's container state, "" for none
	Addr      string
	Status    *q3.Status
}

// Running reports whether anything of this server is up.
func (s Server) Running() bool { return s.Job.Running() || s.State == "running" }

// Servers inspects both flavors' servers, querying the ones that are up.
func Servers(ctx context.Context, cfg *config.Config) []Server {
	var out []Server
	for _, f := range config.Flavors {
		s := Server{Flavor: f, Job: jobs.Get(ServerJob(f)), Container: Container(f)}
		s.State = ContainerState(s.Container)
		port := cfg.Port(f)
		if s.Job != nil && s.Job.Port > 0 {
			port = s.Job.Port
		}
		s.Addr = "127.0.0.1:" + strconv.Itoa(port)
		if s.Running() {
			if st, err := q3.GetStatus(ctx, s.Addr, 600*time.Millisecond); err == nil {
				s.Status = st
			}
		}
		out = append(out, s)
	}
	return out
}

// Rcon runs a console command on a flavor's server.
func Rcon(ctx context.Context, cfg *config.Config, f config.Flavor, command string) (string, error) {
	port := cfg.Port(f)
	if j := jobs.Get(ServerJob(f)); j != nil && j.Port > 0 && j.Running() {
		port = j.Port
	}
	// The server drops an rcon within 500 ms of the last one, from anyone; q3 spaces the
	// calls of this process, and this stamp spaces separate vcod runs, which is how an
	// agent issues them.
	stamp := filepath.Join(jobs.Dir, "rcon-"+string(f)+".last")
	if st, err := os.Stat(stamp); err == nil {
		if wait := 600*time.Millisecond - time.Since(st.ModTime()); wait > 0 && wait <= 600*time.Millisecond {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(wait):
			}
		}
	}
	defer func() {
		if jobs.Dir != "" {
			_ = os.MkdirAll(jobs.Dir, 0o755)
			_ = os.WriteFile(stamp, nil, 0o644)
			now := time.Now()
			_ = os.Chtimes(stamp, now, now)
		}
	}()
	// A map change answers only once the map has loaded.
	timeout := 2 * time.Second
	if q3.ChangesMap(command) {
		timeout = 15 * time.Second
	}
	return q3.Rcon(ctx, "127.0.0.1:"+strconv.Itoa(port), cfg.Rcon(), command, timeout)
}

// ServerLogs are the log files of a flavor's server: the job's console output, the
// server's own console log and games_mp.log.
func ServerLogs(w *ws.Workspace, f config.Flavor) map[string]string {
	dir := w.ServerDir(string(f))
	return map[string]string{
		"console": jobs.LogPath(ServerJob(f)),
		"games":   filepath.Join(dir, "main", "games_mp.log"),
		"server":  filepath.Join(dir, "main", "console_mp_server.log"),
	}
}
