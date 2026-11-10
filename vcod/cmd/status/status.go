// Package statuscmd is `vcod status`: everything that is running, and how fresh the
// builds are.
package statuscmd

import (
	"encoding/json"
	"os"

	"vcod/internal/app"
	"vcod/internal/config"
	"vcod/internal/game"
	"vcod/internal/jobs"
	"vcod/internal/registry"
	"vcod/internal/report"
	"vcod/internal/ui"
	"vcod/internal/ws"
)

const help = `Usage:
  vcod status            servers, clients and builds at a glance
  vcod status --json     the same as one JSON object, for scripts and agents

Servers are found two ways: the jobs vcod started, and the podman containers
run.sh names (cod_rea_server_1_5, cod_rea_server_1_6x), so a server a test or a manual
run.sh started shows up too, marked "outside vcod". Each one that is up is asked
for its status over UDP: map, gametype, players.

Builds are "stale" when a file under cod_rea/src (or CMakeLists.txt) is newer than
the tree's cod_client.`

func init() {
	registry.Register(registry.Command{
		Name:        "status",
		Summary:     "Servers, clients and builds at a glance",
		Args:        "[--json]",
		Help:        help,
		Group:       registry.GroupPlay,
		Order:       40,
		NeedsConfig: true,
		Complete:    func([]string) []string { return []string{"--json"} },
		Run:         run,
	})
}

func run(ctx *app.Context, args []string) int {
	for _, a := range args {
		if a == "--json" {
			ctx.JSON = true
		} else {
			return ctx.Usage("status takes only --json")
		}
	}
	servers := game.Servers(ctx.Ctx, ctx.Cfg)
	if ctx.JSON {
		return printJSON(ctx, servers)
	}
	ctx.Out.Section("servers", "default flavor "+string(ctx.Cfg.Flavor))
	ctx.Out.Listing(report.ServerRows(servers))
	ctx.Out.Section("clients", "")
	ctx.Out.Listing(report.ClientRows())
	if w, err := ctx.Workspace(); err == nil {
		ctx.Out.Section("builds", "")
		ctx.Out.Listing(report.BuildRows(w))
	} else {
		ctx.Out.Plain("")
		ctx.Errorf("%v", err)
	}
	ctx.Out.Plain("")
	return 0
}

type jsonServer struct {
	Flavor    string            `json:"flavor"`
	Up        bool              `json:"up"`
	Answering bool              `json:"answering"`
	ByVcod    bool              `json:"started_by_vcod"`
	Addr      string            `json:"addr"`
	Container string            `json:"container"`
	State     string            `json:"container_state"`
	Map       string            `json:"map,omitempty"`
	Gametype  string            `json:"gametype,omitempty"`
	Players   []string          `json:"players,omitempty"`
	Job       *jobs.Job         `json:"job,omitempty"`
	Vars      map[string]string `json:"vars,omitempty"`
}

type jsonBuild struct {
	Name   string `json:"name"`
	Binary string `json:"binary"`
	Exists bool   `json:"exists"`
	Stale  bool   `json:"stale"`
	Built  string `json:"built,omitempty"`
	Newest string `json:"newest_source,omitempty"`
}

func printJSON(ctx *app.Context, servers []game.Server) int {
	out := struct {
		Flavor    string       `json:"flavor"`
		Workspace string       `json:"workspace"`
		Servers   []jsonServer `json:"servers"`
		Clients   []*jobs.Job  `json:"clients"`
		Builds    []jsonBuild  `json:"builds"`
	}{Flavor: string(ctx.Cfg.Flavor), Workspace: ctx.Cfg.Workspace}
	for _, s := range servers {
		js := jsonServer{Flavor: string(s.Flavor), Up: s.Running(), Addr: s.Addr, Container: s.Container,
			State: s.State, ByVcod: s.Job.Running()}
		if s.Job != nil {
			js.Job = s.Job
		}
		if s.Status != nil {
			js.Answering = true
			js.Map, js.Gametype = s.Status.Get("mapname"), s.Status.Get("g_gametype")
			js.Vars = s.Status.Vars
			for _, p := range s.Status.Players {
				js.Players = append(js.Players, p.Name)
			}
		}
		out.Servers = append(out.Servers, js)
	}
	for _, f := range config.Flavors {
		if j := jobs.Get(game.ClientJob(f)); j != nil {
			out.Clients = append(out.Clients, j)
		}
	}
	if w, err := ctx.Workspace(); err == nil {
		for i := range ws.Builds {
			st := w.State(&ws.Builds[i])
			jb := jsonBuild{Name: st.Build.Name, Binary: st.Binary, Exists: st.Exists, Stale: st.Stale, Newest: st.NewFile}
			if st.Exists {
				jb.Built = st.Built.Format("2006-01-02T15:04:05Z07:00")
			}
			out.Builds = append(out.Builds, jb)
		}
	}
	enc := json.NewEncoder(os.Stdout)
	if ctx.Out != ui.Std {
		// Inside the shell: through the printer, so it lands in the tab.
		b, _ := json.MarshalIndent(out, "", "  ")
		ctx.Out.Plain("%s", b)
		return 0
	}
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return ctx.Fail(err)
	}
	return 0
}
