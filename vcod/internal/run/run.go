// Package run executes a foreground child process — a build, a test script, ctest — for
// as long as the command waits on it. In a terminal the child writes straight to it, so
// ninja's progress line and the scripts' colours survive; inside the shell its output is
// split into lines and handed to a callback that appends them to a tab.
package run

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Spec is one child process.
type Spec struct {
	Args []string
	Dir  string
	Env  []string
	// Stream receives the output line by line; nil means the terminal.
	Stream func(line string)
}

// Env is the environment children run with: this one, with ~/.local/bin on PATH (where
// cmake and ninja live on this machine) when it is missing.
func Env(extra ...string) []string {
	env := os.Environ()
	if home, err := os.UserHomeDir(); err == nil {
		local := filepath.Join(home, ".local", "bin")
		for i, kv := range env {
			if strings.HasPrefix(kv, "PATH=") && !strings.Contains(kv, local) {
				env[i] = kv + string(os.PathListSeparator) + local
			}
		}
	}
	return append(env, extra...)
}

// LookPath is exec.LookPath over Env's PATH.
func LookPath(bin string) (string, bool) {
	for _, kv := range Env() {
		if !strings.HasPrefix(kv, "PATH=") {
			continue
		}
		for _, dir := range filepath.SplitList(strings.TrimPrefix(kv, "PATH=")) {
			p := filepath.Join(dir, bin)
			if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
				return p, true
			}
		}
	}
	return "", false
}

// Run executes the child and returns its exit code. Cancelling ctx stops it: SIGTERM to
// its process group, then SIGKILL two seconds later.
func Run(ctx context.Context, s Spec) (int, error) {
	cmd := exec.Command(s.Args[0], s.Args[1:]...)
	cmd.Dir = s.Dir
	cmd.Env = Env(s.Env...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var wg sync.WaitGroup
	if s.Stream == nil {
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		// A child in its own process group cannot read the terminal; give it none.
		cmd.Stdin = nil
	} else {
		pr, pw := io.Pipe()
		cmd.Stdout, cmd.Stderr = pw, pw
		wg.Add(1)
		go func() {
			defer wg.Done()
			sc := bufio.NewScanner(pr)
			sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
			sc.Split(scanLines)
			for sc.Scan() {
				s.Stream(sc.Text())
			}
			_, _ = io.Copy(io.Discard, pr)
		}()
		defer func() {
			pw.Close()
			wg.Wait()
		}()
	}
	if err := cmd.Start(); err != nil {
		return -1, err
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			}
		case <-done:
		}
	}()
	err := cmd.Wait()
	close(done)
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
}

// scanLines splits on \n and on a lone \r, so a progress line that rewrites itself
// (ninja's [12/340]) arrives as separate lines rather than one endless one.
func scanLines(data []byte, atEOF bool) (int, []byte, error) {
	for i, b := range data {
		if b == '\n' {
			return i + 1, trimCR(data[:i]), nil
		}
		if b == '\r' {
			if i+1 < len(data) && data[i+1] == '\n' {
				return i + 2, data[:i], nil
			}
			if i+1 < len(data) || atEOF {
				return i + 1, data[:i], nil
			}
			return 0, nil, nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func trimCR(b []byte) []byte {
	if n := len(b); n > 0 && b[n-1] == '\r' {
		return b[:n-1]
	}
	return b
}
