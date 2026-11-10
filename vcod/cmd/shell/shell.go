// Package shell is `vcod shell`, the full-screen session: the wordmark, the flavor,
// the safe / write state and the servers' state across the top, tabs of output in the
// middle, and a prompt boxed between two rules at the bottom. Commands are typed
// without the vcod prefix.
//
// Every command runs in the background with its output streaming into a tab, so a
// build, a live test and a server's console can all be watched at once. Servers and
// clients are detached jobs: leaving the shell does not stop them.
package shell

import (
	tea "github.com/charmbracelet/bubbletea"

	"vcod/internal/app"
	"vcod/internal/registry"
)

const help = `Usage:
  vcod shell            open the session (also: a bare 'vcod' in a terminal)
  vcod --16x shell      with 1.6x as the flavor
  vcod -w shell         start in write mode

A full-screen session. Commands are typed at the prompt without the vcod prefix:

  ❯ play                     server + client on the default map, each log in a tab
  ❯ play mp_carentan sd      another map and gametype (switches a running server)
  ❯ flavor 1.6x              work on the 1.6x flavor from now on
  ❯ rcon status              a console command on this flavor's server
  ❯ test viewtest            a live test, streaming into a tab of its own
  ❯ build video              rebuild the windowed client
  ❯ stop                     stop every server and client

Each long command (play, server start, build, test, logs -f, flavors make) opens a tab
titled with what was typed; quick ones (status, rcon, config) answer in the tab in front.
When a command starts a server or a client, a tab following its console log opens too.
ctrl+c cancels the command running in the tab in front; rerun runs it again.

The header shows both servers' state, refreshed every few seconds: map, gametype and
players when one answers.

It starts in safe mode: flavors rebuild and build --clean are refused until write
mode is unlocked with ctrl+x (or the 'write' command); the header badge is red while
it is on.

Keys:
  tab            complete the command, a map, a gametype or a test name; on an empty
                 prompt with several tabs open, the next tab (shift+tab: the previous)
  alt+1…9        select a tab   alt+t new tab   alt+w close it   alt+←/→ cycle
  up down        history, kept across sessions
  pgup pgdn      scroll; the mouse wheel too   end   back to the bottom (follow)
  ctrl+c         cancel the tab's command, or clear the line, or leave when idle
  ctrl+x         toggle write mode
  ctrl+l         clear the tab
  ctrl+d         leave
  mouse          drag to select — copied when you let go; shift+drag for the
                 terminal's own selection

Type help inside it for the command list.`

func init() {
	registry.Register(registry.Command{
		Name:        "shell",
		Summary:     "Full-screen session: tabs for servers, clients, tests",
		Help:        help,
		Group:       registry.GroupPlay,
		Order:       1,
		NeedsConfig: true,
		Run:         run,
	})
}

func run(ctx *app.Context, args []string) int {
	if len(args) > 0 {
		return ctx.Usage("shell does not take %q  (try: vcod help shell)", args[0])
	}
	m := NewModel(ctx)
	prog := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithContext(ctx.Ctx))
	final, err := prog.Run()
	if fm, ok := final.(Model); ok {
		fm.shutdown()
	}
	if err != nil {
		if ctx.Ctx.Err() != nil {
			return 130
		}
		return ctx.Fail(err)
	}
	return 0
}
