package install

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Remote is the public repository vcod is published to: the SubDir folder and the
// installer, mirrored from the (private) workspace. A checkout with an origin of its
// own (the workspace) updates from that instead; see UpdateRemote.
const Remote = "https://github.com/vcod-gtfo/vcod.git"

// Branch is the branch `vcod update` follows.
const Branch = "main"

// SubDir is the folder inside the repository that holds the vcod sources.
const SubDir = "vcod"

// EnvRemote overrides the remote (a fork, or a local mirror).
const EnvRemote = "VCOD_REMOTE"

// RemoteURL is the repository to update from.
func RemoteURL() string {
	if v := os.Getenv(EnvRemote); v != "" {
		return v
	}
	return Remote
}

// UpdateRemote is the repository the checkout at root updates from: VCOD_REMOTE, else
// the checkout's own origin (the workspace, or the public mirror), else Remote.
func UpdateRemote(ctx context.Context, root string) string {
	if v := os.Getenv(EnvRemote); v != "" {
		return v
	}
	if u, err := Git(ctx, root, "remote", "get-url", "origin"); err == nil && u != "" {
		return u
	}
	return Remote
}

// CloneDir is the checkout `vcod update` makes when the install has no source folder
// of its own (a binary copied to another machine).
func (p *Paths) CloneDir() string { return filepath.Join(p.Prefix, "src") }

// Git runs git in dir and returns its trimmed stdout.
func Git(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", &GitError{Args: args, Stderr: strings.TrimSpace(string(ee.Stderr))}
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// GitError carries git's own explanation.
type GitError struct {
	Args   []string
	Stderr string
}

func (e *GitError) Error() string { return "git " + strings.Join(e.Args, " ") + ": " + e.Stderr }

// GitRoot is the top of the git checkout holding dir, or "" when dir is not in one.
func GitRoot(ctx context.Context, dir string) string {
	if _, err := exec.LookPath("git"); err != nil {
		return ""
	}
	root, err := Git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return root
}
