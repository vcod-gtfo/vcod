// Package app is the per-invocation context: the resolved configuration, the printer,
// the cancellation context, and whether write mode is on.
package app

import (
	"context"
	"errors"

	"vcod/internal/config"
	"vcod/internal/prompt"
	"vcod/internal/ui"
	"vcod/internal/ws"
)

// Context is everything a command is handed.
type Context struct {
	// Cfg is nil for the commands registered with NeedsConfig false.
	Cfg *config.Config
	// WS is the workspace layout; nil when Cfg is nil or no workspace was found.
	WS *ws.Workspace
	// Out is where status lines go.
	Out *ui.Printer
	// Ctx carries cancellation; the dispatcher wires it to SIGINT.
	Ctx context.Context
	// Version is the build this binary reports.
	Version string
	// Root is the install directory.
	Root string
	// NoColor records the global flag.
	NoColor bool
	// Write is write mode: --write on the command line, ctrl+x in the shell. Safe is the
	// default, and the dispatcher refuses a registry.Command with Writes set while this
	// is false.
	Write bool
	// JSON asks a command for machine-readable output where it offers one.
	JSON bool
	// Prompt answers the questions a command asks; nil means the terminal.
	Prompt *prompt.Reader
	// Stream, when set, receives the output of a child process line by line instead of
	// the command's stdout (the shell sets it so a test's output lands in its tab).
	Stream func(line string)
}

// New builds a context.
func New(cfg *config.Config, version, root string) *Context {
	c := &Context{Cfg: cfg, Out: ui.Std, Ctx: context.Background(), Version: version, Root: root}
	if cfg != nil && config.IsWorkspace(cfg.Workspace) {
		c.WS = ws.New(cfg.Workspace)
	}
	return c
}

// ErrReadOnly is the refusal a destructive command gets in safe mode.
var ErrReadOnly = errors.New("safe mode: this deletes or rebuilds something. Re-run with --write (-w), or press ctrl+x in the shell to unlock")

// In is where a command asks its questions.
func (c *Context) In() *prompt.Reader {
	if c.Prompt != nil {
		return c.Prompt
	}
	return prompt.New().WithContext(c.Ctx)
}

// Workspace returns the workspace layout, or the error explaining how to set it.
func (c *Context) Workspace() (*ws.Workspace, error) {
	if c.Cfg == nil {
		return nil, errors.New("no configuration loaded")
	}
	if err := c.Cfg.RequireWorkspace(); err != nil {
		return nil, err
	}
	if c.WS == nil {
		c.WS = ws.New(c.Cfg.Workspace)
	}
	return c.WS, nil
}

// Errorf prints an error where this command's output goes.
func (c *Context) Errorf(format string, a ...any) { c.Out.Errorf(format, a...) }

// Hint prints a suggestion under an error.
func (c *Context) Hint(format string, a ...any) { c.Out.Hint(format, a...) }

// Usage prints a usage error and returns exit code 1.
func (c *Context) Usage(format string, a ...any) int {
	c.Out.Errorf(format, a...)
	return 1
}

// Fail prints an error and returns the exit code a command should hand back.
func (c *Context) Fail(err error) int {
	if err == nil {
		return 0
	}
	c.Out.Errorf("%s", err.Error())
	return 1
}

// Flavor is the flavor this invocation works on.
func (c *Context) Flavor() config.Flavor {
	if c.Cfg == nil {
		return config.F15
	}
	return c.Cfg.Flavor
}
