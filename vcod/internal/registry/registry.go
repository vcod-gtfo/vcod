// Package registry is the command table. A command registers itself from an init
// function, carrying its own help text and completion candidates, so the dispatcher,
// the help listing and the generated shell completion cannot fall out of step.
package registry

import (
	"fmt"
	"sort"

	"vcod/internal/app"
)

// Group is a heading in the help listing.
type Group string

// The help groupings, in the order they are printed.
const (
	GroupPlay        Group = "Play"
	GroupTesting     Group = "Build and test"
	GroupSetup       Group = "Setup and health"
	GroupEnvironment Group = "Environment"
)

// GroupOrder is the print order for help.
var GroupOrder = []Group{GroupPlay, GroupTesting, GroupSetup, GroupEnvironment}

// Command is one subcommand.
type Command struct {
	// Name is what the user types.
	Name string
	// Summary is the one-line description in the help listing (keep under ~56 chars).
	Summary string
	// Args is the argument sketch shown next to Name.
	Args string
	// Help is the full help text for `vcod help <name>`.
	Help string
	// Group places the command in the help listing.
	Group Group
	// Order breaks ties within a group.
	Order int
	// NeedsConfig is false for commands that must work with no configuration at all.
	// With it set the dispatcher also resolves the workspace.
	NeedsConfig bool
	// Writes marks a command that deletes or rebuilds something (a flavor, a build
	// tree). The dispatcher refuses it in safe mode before the command runs, so no
	// command can forget the check. Commands whose destructive part is one subcommand
	// (flavors rebuild, build --clean) check ctx.Write themselves.
	Writes bool
	// Complete offers completion candidates for the command's own arguments.
	Complete func(words []string) []string
	// Dynamic is a vcod command line whose output, one candidate per line, adds
	// completions that change on disk (demo names). The generated shell scripts run it
	// at each completion, so its output must stay quick and plain.
	Dynamic string
	// Run is the command. It returns the process exit code.
	Run func(ctx *app.Context, args []string) int
}

var commands = map[string]*Command{}

var aliases = map[string]string{
	"ps":    "status",
	"st":    "status",
	"srv":   "server",
	"cl":    "client",
	"log":   "logs",
	"tests": "test",
	"shots": "screens",
}

// Aliases returns the alternative names, sorted.
func Aliases() []string {
	out := make([]string, 0, len(aliases))
	for name := range aliases {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Register adds a command. It panics on a duplicate name: a programming error
// discoverable at startup.
func Register(c Command) {
	if c.Name == "" {
		panic("registry: command with no name")
	}
	if _, dup := commands[c.Name]; dup {
		panic(fmt.Sprintf("registry: duplicate command %q", c.Name))
	}
	if c.Run == nil {
		panic(fmt.Sprintf("registry: command %q has no Run", c.Name))
	}
	cp := c
	commands[c.Name] = &cp
}

// Lookup finds a command by name or alias.
func Lookup(name string) *Command {
	if c, ok := commands[name]; ok {
		return c
	}
	if target, ok := aliases[name]; ok {
		return commands[target]
	}
	return nil
}

// All returns every command sorted by group, then Order, then name.
func All() []*Command {
	out := make([]*Command, 0, len(commands))
	for _, c := range commands {
		out = append(out, c)
	}
	groupIndex := func(g Group) int {
		for i, x := range GroupOrder {
			if x == g {
				return i
			}
		}
		return len(GroupOrder)
	}
	sort.Slice(out, func(i, j int) bool {
		gi, gj := groupIndex(out[i].Group), groupIndex(out[j].Group)
		if gi != gj {
			return gi < gj
		}
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Names returns every command name, sorted.
func Names() []string {
	out := make([]string, 0, len(commands))
	for name := range commands {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// InGroup returns the commands in one group, in print order.
func InGroup(g Group) []*Command {
	var out []*Command
	for _, c := range All() {
		if c.Group == g {
			out = append(out, c)
		}
	}
	return out
}
