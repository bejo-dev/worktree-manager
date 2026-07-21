// Package gitops wraps git CLI operations for worktree management.
package gitops

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotARepo is returned when the path is not inside a git repository.
var ErrNotARepo = errors.New("not a git repository")

// Repo represents a git repository root.
type Repo struct {
	Root string
}

// Resolve resolves the git repository root containing the given path.
func Resolve(path string) (*Repo, error) {
	out, err := runGit(path, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, ErrNotARepo
	}
	return &Repo{Root: strings.TrimSpace(out)}, nil
}

// DefaultBranch detects the default branch of the repository.
// It tries the symbolic ref of origin HEAD, then falls back to
// common branch names main and master.
func (r *Repo) DefaultBranch() (string, error) {
	// Try symbolic ref of remote HEAD (works for repos with a remote).
	out, err := runGit(r.Root, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	if err == nil {
		// Output is like origin/main.
		s := strings.TrimSpace(out)
		if strings.HasPrefix(s, "origin/") {
			return strings.TrimPrefix(s, "origin/"), nil
		}
		return s, nil
	}

	// Fall back to checking common branch names locally.
	for _, b := range []string{"main", "master", "develop"} {
		if _, err := runGit(r.Root, "rev-parse", "--verify", "--quiet", "refs/heads/"+b); err == nil {
			return b, nil
		}
	}

	// If there's a single local branch, use it.
	out, err = runGit(r.Root, "branch", "--list", "--format=%(refname:short)")
	if err == nil {
		lines := strings.Split(strings.TrimSpace(out), "\n")
		var branches []string
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if l != "" {
				branches = append(branches, l)
			}
		}
		if len(branches) >= 1 {
			return branches[0], nil
		}
	}

	return "", errors.New("could not determine default branch")
}

// FetchOrigin runs git fetch origin.
func (r *Repo) FetchOrigin() error {
	_, err := runGit(r.Root, "fetch", "origin", "--prune", "--quiet")
	return err
}

// RevParse returns the commit hash for the given ref.
func (r *Repo) RevParse(ref string) (string, error) {
	out, err := runGit(r.Root, "rev-parse", "--verify", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// AddWorktree creates a new git worktree at path on a new branch tracking
// origin/<baseBranch>. Returns the branch name.
func (r *Repo) AddWorktree(path, branchName, baseBranch string) error {
	// Use -B to (re)create the branch from origin/<baseBranch>.
	startPoint := "origin/" + baseBranch
	if _, err := r.RevParse(startPoint); err != nil {
		// No remote, fall back to local default branch.
		startPoint = baseBranch
	}
	_, err := runGit(r.Root, "worktree", "add", "-B", branchName, path, startPoint)
	return err
}

// RenameWorktreeBranch renames the branch currently checked out at path.
func (r *Repo) RenameWorktreeBranch(path, branchName string) error {
	_, err := runGit(path, "branch", "-M", branchName)
	return err
}

// CurrentBranch returns the branch checked out at path.
func (r *Repo) CurrentBranch(path string) (string, error) {
	out, err := runGit(path, "branch", "--show-current")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// RemoveWorktree removes a git worktree by path.
func (r *Repo) RemoveWorktree(path string, force bool) error {
	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)
	_, err := runGit(r.Root, args...)
	return err
}

// HardReset resets the worktree at path to the given ref and discards local
// changes.
func (r *Repo) HardReset(path, ref string) error {
	if _, err := runGit(path, "reset", "--hard", ref); err != nil {
		return err
	}
	return nil
}

// Clean removes untracked files and directories from the worktree at path.
func (r *Repo) Clean(path string) error {
	_, err := runGit(path, "clean", "-xfd")
	return err
}

// HasRemote reports whether the repository has an origin remote configured.
func (r *Repo) HasRemote() bool {
	_, err := runGit(r.Root, "remote", "get-url", "origin")
	return err == nil
}

// PruneWorktrees prunes stale worktree metadata.
func (r *Repo) PruneWorktrees() error {
	_, err := runGit(r.Root, "worktree", "prune")
	return err
}

// WorktreeExists reports whether a worktree is registered at path.
func (r *Repo) WorktreeExists(path string) (bool, error) {
	out, err := runGit(r.Root, "worktree", "list", "--porcelain")
	if err != nil {
		return false, err
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	target, err = filepath.EvalSymlinks(target)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			p := strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
			abs, err := filepath.Abs(p)
			if err != nil {
				continue
			}
			abs, err = filepath.EvalSymlinks(abs)
			if err != nil {
				continue
			}
			if abs == target {
				return true, nil
			}
		}
	}
	return false, nil
}

// runGit executes git with the given args in the given working directory.
// It returns stdout on success, or an error combining stderr.
func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s",
			strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}
