// Package prompt is line-oriented interactive input for the wizard and the confirmation
// gates. Deliberately not Bubble Tea: a question asked in the scrollback is one the user
// can still see after answering, and Ctrl-C halfway through writes nothing.
package prompt

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"

	"vcod/internal/ui"
)

// Reader reads answers from a terminal.
type Reader struct {
	in  *bufio.Reader
	out io.Writer
	// fd is the terminal for no-echo reads; -1 when stdin is not a terminal.
	fd  int
	ctx context.Context
}

// New returns a Reader over stdin.
func New() *Reader {
	fd := -1
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fd = int(os.Stdin.Fd())
	}
	return &Reader{in: bufio.NewReader(os.Stdin), out: os.Stdout, fd: fd, ctx: context.Background()}
}

// NewFrom returns a Reader over arbitrary streams, for tests. Secrets are read with echo.
func NewFrom(in io.Reader, out io.Writer) *Reader {
	return &Reader{in: bufio.NewReader(in), out: out, fd: -1, ctx: context.Background()}
}

// WithContext makes Ctrl-C abandon a pending question.
func (r *Reader) WithContext(ctx context.Context) *Reader {
	if ctx != nil {
		r.ctx = ctx
	}
	return r
}

// Interactive reports whether stdin is a terminal.
func (r *Reader) Interactive() bool { return r.fd >= 0 }

// Ask prompts for a value, offering def as the default; an empty answer takes it.
func (r *Reader) Ask(question, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(r.out, "  %s %s ", ui.StyleWhite.Render(question), ui.StyleDim.Render("["+def+"]"))
	} else {
		fmt.Fprintf(r.out, "  %s ", ui.StyleWhite.Render(question))
	}
	line, err := r.readLine()
	if err != nil {
		return "", err
	}
	if line == "" {
		return def, nil
	}
	return line, nil
}

// AskSecret prompts without echoing. A non-empty def is shown as kept, never as its value.
func (r *Reader) AskSecret(question, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(r.out, "  %s %s ", ui.StyleWhite.Render(question), ui.StyleDim.Render("[••••••(kept)]"))
	} else {
		fmt.Fprintf(r.out, "  %s ", ui.StyleWhite.Render(question))
	}
	if r.fd < 0 {
		fmt.Fprintf(r.out, "%s ", ui.StyleYellow.Render("(not a terminal: input will be visible)"))
		line, err := r.readLine()
		if err != nil {
			return "", err
		}
		if line == "" {
			return def, nil
		}
		return line, nil
	}

	// Captured before echo goes off: ReadPassword's own restore is deferred and never
	// runs when the read is abandoned on Ctrl-C.
	state, stateErr := term.GetState(r.fd)
	type result struct {
		raw []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		raw, err := term.ReadPassword(r.fd)
		ch <- result{raw, err}
	}()
	select {
	case <-r.ctx.Done():
		if stateErr == nil {
			_ = term.Restore(r.fd, state)
		}
		fmt.Fprintln(r.out)
		return "", ErrInterrupted
	case res := <-ch:
		fmt.Fprintln(r.out)
		if res.err != nil {
			return "", res.err
		}
		value := strings.TrimSpace(string(res.raw))
		if value == "" {
			return def, nil
		}
		return value, nil
	}
}

// YesNo asks a yes/no question. def is what an empty line means; anything unrecognised
// is a no, because guessing is how a wizard does something the user did not agree to.
func (r *Reader) YesNo(question string, def bool) (bool, error) {
	hint := "[y/N]"
	if def {
		hint = "[Y/n]"
	}
	fmt.Fprintf(r.out, "  %s %s ", ui.StyleWhite.Render(question), ui.StyleDim.Render(hint))
	line, err := r.readLine()
	if err != nil {
		return false, err
	}
	switch strings.ToLower(line) {
	case "":
		return def, nil
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// Gate asks the user to type word exactly. It is the confirmation for an irreversible
// action: a y/n is a reflex, a typed word is a decision. Case matters, and an empty
// line is a refusal.
func (r *Reader) Gate(question, word string) (bool, error) {
	fmt.Fprintf(r.out, "  %s %s ", ui.StyleWhite.Render(question),
		ui.StyleDim.Render("(type "+word+" to confirm)"))
	line, err := r.readLine()
	if err != nil {
		return false, err
	}
	return line == word, nil
}

// ErrAborted is returned when the user ends input with Ctrl-D.
var ErrAborted = fmt.Errorf("aborted")

// ErrInterrupted is returned when the user presses Ctrl-C.
var ErrInterrupted = fmt.Errorf("interrupted")

// Cancelled reports whether err is either form of the user giving up.
func Cancelled(err error) bool {
	return errors.Is(err, ErrAborted) || errors.Is(err, ErrInterrupted)
}

// readLine reads one answer, abandoning it if the context is cancelled. The read runs on
// its own goroutine because a blocking read on stdin cannot be interrupted portably.
func (r *Reader) readLine() (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := r.in.ReadString('\n')
		ch <- result{line, err}
	}()
	select {
	case <-r.ctx.Done():
		fmt.Fprintln(r.out)
		return "", ErrInterrupted
	case res := <-ch:
		line, err := res.line, res.err
		if err != nil {
			if err == io.EOF && strings.TrimSpace(line) != "" {
				return strings.TrimSpace(line), nil
			}
			if err == io.EOF {
				fmt.Fprintln(r.out)
				return "", ErrAborted
			}
			return "", err
		}
		return strings.TrimSpace(line), nil
	}
}
