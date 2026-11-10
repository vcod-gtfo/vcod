// Package report renders the state of servers, clients and builds, for status, the
// server and client commands, and the shell.
package report

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"vcod/internal/config"
	"vcod/internal/game"
	"vcod/internal/jobs"
	"vcod/internal/q3"
	"vcod/internal/ui"
	"vcod/internal/ws"
)

// Age renders a duration the way a person reads an uptime.
func Age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return strconv.Itoa(int(d.Hours()/24)) + "d"
}

// Since renders how long ago a time was.
func Since(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return Age(time.Since(t)) + " ago"
}

// ServerRows are the status rows for the servers.
func ServerRows(servers []game.Server) [][]string {
	var rows [][]string
	for _, s := range servers {
		badge := ui.FlavorBadge(string(s.Flavor))
		switch {
		case s.Status != nil:
			st := s.Status
			players := strconv.Itoa(len(st.Players)) + "/" + orDash(st.Get("sv_maxclients"))
			where := s.Addr
			who := "vcod"
			if s.Job == nil || !s.Job.Running() {
				who = "outside vcod"
			}
			rows = append(rows, []string{badge, ui.StyleGreen.Render("● up"),
				ui.StyleWhite.Render(st.Get("mapname")) + " " + st.Get("g_gametype"),
				players + " players", where,
				ui.StyleDim.Render(uptime(s.Job) + who)})
		case s.Running():
			what := "starting"
			if s.Job == nil || !s.Job.Running() {
				what = "container " + s.State + ", not answering"
			}
			rows = append(rows, []string{badge, ui.StyleYellow.Render("◌ " + what), jobMap(s.Job), "", s.Addr,
				ui.StyleDim.Render(uptime(s.Job))})
		default:
			rows = append(rows, []string{badge, ui.StyleDim.Render("○ down"), ended(s.Job), "", "", ""})
		}
	}
	return rows
}

func uptime(j *jobs.Job) string {
	if j == nil || !j.Running() {
		return ""
	}
	return "up " + Age(j.Uptime()) + " · "
}

func jobMap(j *jobs.Job) string {
	if j == nil {
		return ""
	}
	return j.Map + " " + j.Gametype
}

func ended(j *jobs.Job) string {
	if j == nil || j.Ended == nil {
		return ""
	}
	code := ""
	if j.Exit != nil && *j.Exit > 0 {
		code = ", exit " + strconv.Itoa(*j.Exit)
	}
	return ui.StyleDim.Render("last ran " + j.Map + " " + j.Gametype + ", ended " + Since(*j.Ended) + code)
}

// ClientRows are the status rows for the clients.
func ClientRows() [][]string {
	var rows [][]string
	for _, f := range config.Flavors {
		j := jobs.Get(game.ClientJob(f))
		badge := ui.FlavorBadge(string(f))
		switch {
		case j.Running():
			to := j.Connect
			if to == "" {
				to = "main menu"
			}
			rows = append(rows, []string{badge, ui.StyleGreen.Render("● running"), "→ " + to,
				ui.StyleDim.Render("pid " + strconv.Itoa(j.PID) + " · up " + Age(j.Uptime()))})
		case j != nil && j.Ended != nil:
			code := ""
			if j.Exit != nil && *j.Exit > 0 {
				code = ui.StyleRed.Render(" exit " + strconv.Itoa(*j.Exit))
			}
			rows = append(rows, []string{badge, ui.StyleDim.Render("○ closed"), ui.StyleDim.Render("ended "+Since(*j.Ended)) + code, ""})
		default:
			rows = append(rows, []string{badge, ui.StyleDim.Render("○ not started"), "", ""})
		}
	}
	return rows
}

// BuildRows are the status rows for the build trees.
func BuildRows(w *ws.Workspace) [][]string {
	var rows [][]string
	for i := range ws.Builds {
		b := &ws.Builds[i]
		st := w.State(b)
		var state string
		switch {
		case !st.Exists:
			state = ui.StyleYellow.Render("▲ not built") + ui.StyleDim.Render("  vcod build "+b.Name)
		case st.Stale:
			state = ui.StyleYellow.Render("▲ stale") + ui.StyleDim.Render("  "+st.NewFile+" changed "+Since(st.Newest)+" · vcod build "+b.Name)
		default:
			state = ui.StyleGreen.Render("✔ fresh") + ui.StyleDim.Render("  built "+Since(st.Built))
		}
		rows = append(rows, []string{ui.StyleWhite.Render(b.Name), ui.StyleDim.Render(b.Dir + "/cod_client"), state})
	}
	return rows
}

// Players renders a server's player list.
func Players(st *q3.Status) [][]string {
	var rows [][]string
	for _, p := range st.Players {
		rows = append(rows, []string{ui.StyleWhite.Render(q3.StripColors(p.Name)),
			ui.StyleDim.Render("score ") + strconv.Itoa(p.Score), ui.StyleDim.Render("ping ") + strconv.Itoa(p.Ping)})
	}
	return rows
}

// ServerFields are the details of one answering server.
func ServerFields(s game.Server) [][2]string {
	st := s.Status
	rows := [][2]string{
		{"address", s.Addr},
		{"hostname", q3.StripColors(st.Get("sv_hostname"))},
		{"map", st.Get("mapname") + "  " + st.Get("g_gametype")},
		{"protocol", st.Get("protocol") + "  " + st.Get("shortversion")},
		{"players", strconv.Itoa(len(st.Players)) + " of " + st.Get("sv_maxclients")},
		{"pure", onOff(st.Get("sv_pure"))},
		{"cheats", onOff(st.Get("sv_cheats"))},
		{"container", s.Container + "  " + ui.StyleDim.Render(s.State)},
	}
	if s.Job != nil && s.Job.Running() {
		rows = append(rows, [2]string{"job", "pid " + strconv.Itoa(s.Job.PID) + ", up " + Age(s.Job.Uptime()) + ", log " + s.Job.Log})
	}
	return rows
}

func onOff(v string) string {
	switch v {
	case "1":
		return "on"
	case "0", "":
		return "off"
	}
	return v
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "?"
	}
	return s
}
