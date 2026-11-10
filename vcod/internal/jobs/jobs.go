// Package jobs runs the long-lived processes this tool starts — servers and clients — in
// the background, detached from the terminal, each with a record and a log under the
// state folder. A job outlives the command or the shell that started it: `vcod
// server start` returns at once, and `vcod status` finds the job again later.
//
// A job is named after what it is ("server-1.5", "client-1.6x"), so there is at most one
// of each, and starting one that is running is refused rather than doubled.
package jobs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Job is the record of one background process.
type Job struct {
	Name    string    `json:"name"`
	Kind    string    `json:"kind"` // "server" or "client"
	Flavor  string    `json:"flavor"`
	PID     int       `json:"pid"`
	Args    []string  `json:"args"`
	Dir     string    `json:"dir"`
	Log     string    `json:"log"`
	Started time.Time `json:"started"`
	// Container is the podman container a server job runs in, so stop can end it even
	// when the podman client process is gone.
	Container string `json:"container,omitempty"`
	// Port, Map and Gametype describe a server job.
	Port     int    `json:"port,omitempty"`
	Map      string `json:"map,omitempty"`
	Gametype string `json:"gametype,omitempty"`
	// Connect is the address a client job joined.
	Connect string `json:"connect,omitempty"`
	// Exit is the exit status once the process ended and someone saw it end.
	Exit *int `json:"exit,omitempty"`
	// Ended is when it was seen to end.
	Ended *time.Time `json:"ended,omitempty"`
}

// Dir is the folder holding the records and logs; set it before use (the commands set
// it from install.Paths.JobsDir).
var Dir string

func recordPath(name string) string { return filepath.Join(Dir, name+".json") }

// LogPath is the log a job writes.
func LogPath(name string) string { return filepath.Join(Dir, name+".log") }

// Running reports whether the job's process is alive (and not a zombie).
func (j *Job) Running() bool {
	if j == nil || j.PID <= 0 || j.Exit != nil {
		return false
	}
	return Alive(j.PID)
}

// Alive reports whether a pid names a live, non-zombie process.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil && !errors.Is(err, syscall.EPERM) {
		return false
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		// No procfs (not Linux): the signal probe is all there is.
		return true
	}
	// The state follows the parenthesised command name, which may itself hold spaces.
	s := string(b)
	if i := strings.LastIndexByte(s, ')'); i >= 0 && i+2 < len(s) {
		return s[i+2] != 'Z'
	}
	return true
}

// Uptime is how long the job has been running, or ran.
func (j *Job) Uptime() time.Duration {
	end := time.Now()
	if j.Ended != nil {
		end = *j.Ended
	}
	return end.Sub(j.Started)
}

// Get reads a job's record; nil when there is none.
func Get(name string) *Job {
	b, err := os.ReadFile(recordPath(name))
	if err != nil {
		return nil
	}
	var j Job
	if json.Unmarshal(b, &j) != nil {
		return nil
	}
	return &j
}

// List reads every record, sorted by name.
func List() []*Job {
	matches, _ := filepath.Glob(filepath.Join(Dir, "*.json"))
	sort.Strings(matches)
	var out []*Job
	for _, m := range matches {
		if j := Get(strings.TrimSuffix(filepath.Base(m), ".json")); j != nil {
			out = append(out, j)
		}
	}
	return out
}

func save(j *Job) error {
	if err := os.MkdirAll(Dir, 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(j, "", "  ")
	tmp := recordPath(j.Name) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, recordPath(j.Name))
}

// Spec is what Start runs.
type Spec struct {
	Job
	Env []string
}

// ErrRunning is returned by Start when a job of that name is alive.
var ErrRunning = errors.New("already running")

// Start launches a job in its own session, with stdin from /dev/null and stdout and
// stderr appended to a fresh log, and records it. The process is reaped by a goroutine
// for as long as this process lives; after that init adopts it.
func Start(spec Spec) (*Job, error) {
	if old := Get(spec.Name); old.Running() {
		return old, fmt.Errorf("%s is %w (pid %d)", spec.Name, ErrRunning, old.PID)
	}
	if err := os.MkdirAll(Dir, 0o755); err != nil {
		return nil, err
	}
	logPath := LogPath(spec.Name)
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		logf.Close()
		return nil, err
	}
	cmd := exec.Command(spec.Args[0], spec.Args[1:]...)
	cmd.Dir = spec.Dir
	cmd.Env = append(os.Environ(), spec.Env...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = devnull, logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	fmt.Fprintf(logf, "# vcod %s: %s\n", spec.Name, strings.Join(spec.Args, " "))
	if err := cmd.Start(); err != nil {
		logf.Close()
		devnull.Close()
		return nil, err
	}
	logf.Close()
	devnull.Close()

	j := spec.Job
	j.PID = cmd.Process.Pid
	j.Log = logPath
	j.Started = time.Now()
	j.Exit, j.Ended = nil, nil
	if err := save(&j); err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	go func() {
		err := cmd.Wait()
		code := 0
		if err != nil {
			code = -1
			var ee *exec.ExitError
			if errors.As(err, &ee) {
				code = ee.ExitCode()
			}
		}
		MarkEnded(j.Name, j.PID, code)
	}()
	return &j, nil
}

// MarkEnded records an exit, unless the record has since been replaced by a new run.
func MarkEnded(name string, pid, code int) {
	j := Get(name)
	if j == nil || j.PID != pid || j.Exit != nil {
		return
	}
	now := time.Now()
	j.Exit, j.Ended = &code, &now
	_ = save(j)
}

// Stop ends a job: the container first for a server (podman stop gives the server its
// SIGTERM and a grace period), then the process group. It waits up to grace for the
// process to go.
func Stop(j *Job, grace time.Duration) error {
	if j.Container != "" {
		_ = exec.Command("podman", "stop", "-t", "3", j.Container).Run()
	}
	if j.Running() {
		// The job leads its own session, so the negative pid reaches its children too.
		_ = syscall.Kill(-j.PID, syscall.SIGTERM)
		deadline := time.Now().Add(grace)
		for j.Running() && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
		if j.Running() {
			_ = syscall.Kill(-j.PID, syscall.SIGKILL)
			time.Sleep(200 * time.Millisecond)
		}
	}
	if j.Running() {
		return fmt.Errorf("%s (pid %d) did not stop", j.Name, j.PID)
	}
	MarkEnded(j.Name, j.PID, -1)
	return nil
}

// Forget removes a job's record and log, for a job that is not running.
func Forget(name string) {
	_ = os.Remove(recordPath(name))
	_ = os.Remove(LogPath(name))
}

// Tail returns the last n lines of a file.
func Tail(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	// Read a window from the end big enough for n ordinary lines, widening once if not.
	size := st.Size()
	for window := int64(n*200 + 4096); ; window *= 4 {
		start := size - window
		if start < 0 {
			start = 0
		}
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return nil, err
		}
		b, err := io.ReadAll(f)
		if err != nil {
			return nil, err
		}
		text := strings.TrimRight(strings.ReplaceAll(string(b), "\r", ""), "\n")
		lines := strings.Split(text, "\n")
		if start > 0 {
			lines = lines[1:] // the first is cut mid-line
		}
		if len(lines) >= n || start == 0 {
			if len(lines) > n {
				lines = lines[len(lines)-n:]
			}
			if len(lines) == 1 && lines[0] == "" {
				return nil, nil
			}
			return lines, nil
		}
	}
}

// Follow streams a file's new lines from offset to fn until stop is closed. It returns
// the offset reached. Partial lines wait for their newline.
func Follow(path string, offset int64, stop <-chan struct{}, fn func(lines []string)) int64 {
	var pending string
	tick := time.NewTicker(150 * time.Millisecond)
	defer tick.Stop()
	for {
		if f, err := os.Open(path); err == nil {
			if st, err := f.Stat(); err == nil && st.Size() < offset {
				offset = 0 // truncated: a new run of the job
				pending = ""
			}
			if _, err := f.Seek(offset, io.SeekStart); err == nil {
				b, _ := io.ReadAll(f)
				offset += int64(len(b))
				if len(b) > 0 {
					text := pending + strings.ReplaceAll(string(b), "\r", "")
					parts := strings.Split(text, "\n")
					pending = parts[len(parts)-1]
					if lines := parts[:len(parts)-1]; len(lines) > 0 {
						fn(lines)
					}
				}
			}
			f.Close()
		}
		select {
		case <-stop:
			if pending != "" {
				fn([]string{pending})
			}
			return offset
		case <-tick.C:
		}
	}
}
