// Package cli is the dispatcher: global flags, help, the read-only gate, and routing to
// a registered command.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"vcod/internal/app"
	"vcod/internal/config"
	"vcod/internal/install"
	"vcod/internal/jobs"
	"vcod/internal/platform"
	"vcod/internal/registry"
	"vcod/internal/ui"
	"vcod/internal/version"
)

// EnvWrite is the environment switch for write mode, for scripts that cannot pass a flag.
const EnvWrite = "VCOD_WRITE"

// Flags are the global flags, wherever they appeared on the command line.
type Flags struct {
	Flavor     string
	FlavorSet  bool
	Write      bool
	JSON       bool
	NoColor    bool
	Version    bool
	Help       bool
	HelpTopics []string
}

// Run is the entry point. It returns the process exit code rather than calling os.Exit,
// so the whole dispatcher is testable.
func Run(args []string) int {
	flags, rest, err := parseGlobal(args)
	if err != nil {
		ui.Errorf("%s", err.Error())
		return 1
	}
	if flags.NoColor {
		ui.InitColor(true)
	}
	if flags.Version {
		fmt.Println(version.Current())
		return 0
	}
	if flags.Help {
		return help(flags.HelpTopics)
	}
	if os.Getenv(EnvWrite) == "1" {
		flags.Write = true
	}

	if len(rest) == 0 {
		// A bare `vcod` in a terminal opens the shell; piped, in CI or under an agent it
		// prints help.
		if !platform.IsTTY() || !platform.IsStdinTTY() || os.Getenv("TERM") == "dumb" {
			usage()
			return 0
		}
		rest = []string{"shell"}
	}

	name := rest[0]
	rest = rest[1:]
	if name == "help" {
		return help(rest)
	}

	cmd := registry.Lookup(name)
	if cmd == nil {
		ui.Errorf("unknown command: %s", name)
		fmt.Fprintf(os.Stderr, "\n  Available: %s\n", strings.Join(registry.Names(), ", "))
		fmt.Fprintf(os.Stderr, "\n  Run %s for the full list.\n\n", ui.StyleCyan.Render("vcod help"))
		return 1
	}
	for _, a := range rest {
		if a == "-h" || a == "--help" {
			printHelp(cmd)
			return 0
		}
	}

	// The read-only gate lives here, before the command runs, so no command can forget
	// it. Write mode is a decision the operator makes per invocation.
	if cmd.Writes && !flags.Write {
		ui.Errorf("%s", app.ErrReadOnly.Error())
		ui.Hint("vcod --write %s %s", name, strings.Join(rest, " "))
		return 2
	}

	var cfg *config.Config
	if cmd.NeedsConfig {
		cfg, err = config.Load(flags.Flavor, flags.FlavorSet)
		if err != nil {
			ui.Errorf("%s", err.Error())
			return 1
		}
	}

	ctx := app.New(cfg, version.Current(), install.Root())
	ctx.NoColor = flags.NoColor
	ctx.Write = flags.Write
	ctx.JSON = flags.JSON
	if cfg != nil {
		if paths, err := install.Resolve(); err == nil {
			jobs.Dir = paths.JobsDir()
		}
	}

	// Ctrl-C cancels the context so an in-flight request is abandoned; the default
	// disposition is restored after the first signal so a second one kills.
	sigCtx, cancel := context.WithCancel(context.Background())
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigs
		cancel()
		signal.Stop(sigs)
	}()
	defer func() {
		signal.Stop(sigs)
		cancel()
	}()
	ctx.Ctx = sigCtx

	code := cmd.Run(ctx, rest)
	if sigCtx.Err() != nil && code != 0 {
		fmt.Fprintf(os.Stderr, "\n%s\n", ui.StyleYellow.Render("interrupted"))
		return 130
	}
	return code
}

// parseGlobal pulls the global flags out of the argument list wherever they appear, so
// both `vcod -F 1.6x play` and `vcod play -F 1.6x` work. No command uses these flags
// for itself. `--` ends flag parsing, and the rest goes to the command untouched (the
// server and client commands pass it on as +set arguments).
func parseGlobal(args []string) (Flags, []string, error) {
	var f Flags
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			out = append(out, args[i:]...)
			return f, out, nil
		case arg == "-F" || arg == "--flavor":
			if i+1 >= len(args) {
				return f, nil, fmt.Errorf("%s needs a value: 1.5 or 1.6x", arg)
			}
			f.Flavor, f.FlavorSet = args[i+1], true
			i++
		case strings.HasPrefix(arg, "--flavor="):
			_, value, _ := strings.Cut(arg, "=")
			f.Flavor, f.FlavorSet = value, true
		case arg == "--15" || arg == "--1.5":
			f.Flavor, f.FlavorSet = "1.5", true
		case arg == "--16x" || arg == "--1.6x":
			f.Flavor, f.FlavorSet = "1.6x", true
		case arg == "--json":
			f.JSON = true
		case arg == "-w" || arg == "--write":
			f.Write = true
		case arg == "--no-color":
			f.NoColor = true
		case arg == "-v" || arg == "--version":
			f.Version = true
		case arg == "-h" || arg == "--help":
			// `vcod -h users` is `vcod help users`; a -h after the command name is
			// left for the dispatcher to turn into that command's help.
			if len(out) == 0 {
				f.Help = true
				f.HelpTopics = append(f.HelpTopics, args[i+1:]...)
				return f, out, nil
			}
			out = append(out, arg)
		case len(out) == 0 && strings.HasPrefix(arg, "-"):
			return f, nil, fmt.Errorf("unknown global flag: %s  (try: vcod --help)", arg)
		default:
			out = append(out, arg)
		}
	}
	return f, out, nil
}

func help(args []string) int {
	if len(args) == 0 {
		usage()
		return 0
	}
	cmd := registry.Lookup(args[0])
	if cmd == nil {
		ui.Errorf("no such command: %s  (try: vcod help)", args[0])
		return 1
	}
	printHelp(cmd)
	return 0
}

func printHelp(c *registry.Command) {
	fmt.Println()
	fmt.Printf("  %s %s\n", ui.StyleBold.Render("vcod "+c.Name), ui.StyleDim.Render(c.Args))
	fmt.Printf("  %s\n", ui.StyleGreen.Render(c.Summary))
	if c.Writes {
		fmt.Printf("  %s\n", ui.StyleYellow.Render("Deletes or rebuilds something: needs --write (safe mode is the default)."))
	}
	fmt.Println()
	for _, line := range strings.Split(strings.TrimRight(c.Help, "\n"), "\n") {
		fmt.Printf("  %s\n", line)
	}
	fmt.Println()
}

func usage() {
	ui.Banner("")
	fmt.Println(ui.ContextLine(version.Current()))
	fmt.Println()
	fmt.Printf("Usage: %s [-F 1.5|1.6x] [--write] <command> [args]\n", ui.StyleCyan.Render("vcod"))

	// One column width for every group, sized by the longest command line that fits
	// maxLeft, so the summaries line up down the whole listing. A longer cell gets its
	// own line with the summary wrapped underneath rather than pushing every row over.
	width := 24
	for _, c := range registry.All() {
		if w := ui.VisibleWidth(commandLeft(c)); w > width && w <= maxLeft {
			width = w
		}
	}
	for _, group := range registry.GroupOrder {
		cmds := registry.InGroup(group)
		if len(cmds) == 0 {
			continue
		}
		fmt.Printf("\n%s\n", ui.StyleBold.Render(string(group)))
		for _, c := range cmds {
			marker := " "
			if c.Writes {
				marker = ui.StyleYellow.Render("✎")
			}
			left := commandLeft(c)
			if ui.VisibleWidth(left) > width {
				fmt.Printf("%s %s\n", marker, left)
				left = ""
			}
			fmt.Printf("%s %-*s %s\n", marker, width, left, c.Summary)
		}
	}

	fmt.Printf("\n%s\n", ui.StyleBold.Render("Global flags"))
	fmt.Printf("  %-*s %s\n", width, "-F, --flavor <f>", "1.5 (default) or 1.6x, for this command only")
	fmt.Printf("  %-*s %s\n", width, "    --15, --16x", "Shorthands for --flavor 1.5 and --flavor 1.6x")
	fmt.Printf("  %-*s %s\n", width, "-w, --write", "Leave safe mode: allow the ✎ commands")
	fmt.Printf("  %-*s %s\n", width, "    --json", "Machine-readable output (status, test list)")
	fmt.Printf("  %-*s %s\n", width, "    --no-color", "Plain output, no ANSI escapes")
	fmt.Printf("  %-*s %s\n", width, "-h, --help", "Help for vcod, or for a command")
	fmt.Printf("  %-*s %s\n", width, "-v, --version", "Print the version and exit")
	fmt.Println()
	fmt.Println("  Safe mode is the default. Commands marked ✎ delete or rebuild something and are")
	fmt.Println("  refused until --write is given; in the shell, ctrl+x toggles write mode.")
	fmt.Println("  A bare vcod in a terminal opens the shell: servers, clients and tests in tabs.")
	fmt.Printf("\n  Run %s first when anything misbehaves.\n\n", ui.StyleCyan.Render("vcod doctor"))
}

// maxLeft is the widest "name args" cell that still shares the summary column.
const maxLeft = 34

// commandLeft is the "name args" cell of the usage listing.
func commandLeft(c *registry.Command) string {
	if c.Args == "" {
		return c.Name
	}
	return c.Name + " " + c.Args
}
