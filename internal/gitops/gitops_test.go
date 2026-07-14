package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func run(t *testing.T, dir string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

// initBareRepo creates a bare repo seeded with an initial commit on main,
// returns the path to the bare repo.
func initBareRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bare := filepath.Join(dir, "origin.git")
	run(t, dir, "git", "init", "--bare", "-b", "main", bare)

	// Seed the bare repo with an initial commit via a temporary working clone.
	seed := filepath.Join(dir, "seed")
	run(t, dir, "git", "clone", bare, "seed")
	run(t, seed, "git", "config", "user.email", "t@t")
	run(t, seed, "git", "config", "user.name", "test")
	writeFile(t, seed, "README.md", "# test\n")
	run(t, seed, "git", "add", ".")
	run(t, seed, "git", "commit", "-m", "init")
	run(t, seed, "git", "push", "origin", "main")
	return bare
}

// cloneWorking clones the bare repo into a fresh working copy.
func cloneWorking(t *testing.T, bare string) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "clone", bare, "work")
	work := filepath.Join(dir, "work")
	run(t, work, "git", "config", "user.email", "t@t")
	run(t, work, "git", "config", "user.name", "test")
	return work
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolve(t *testing.T) {
	bare := initBareRepo(t)
	work := cloneWorking(t, bare)

	repo, err := Resolve(work)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want, _ := filepath.EvalSymlinks(work)
	if repo.Root != want {
		t.Fatalf("expected %s got %s", want, repo.Root)
	}

	if _, err := Resolve(t.TempDir()); err != ErrNotARepo {
		t.Fatalf("expected ErrNotARepo got %v", err)
	}
}

func TestDefaultBranch(t *testing.T) {
	bare := initBareRepo(t)
	work := cloneWorking(t, bare)

	repo, err := Resolve(work)
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.DefaultBranch()
	if err != nil {
		t.Fatalf("DefaultBranch: %v", err)
	}
	if b != "main" {
		t.Fatalf("expected main got %s", b)
	}
}

func TestAddWorktreeAndCleanReset(t *testing.T) {
	bare := initBareRepo(t)
	work := cloneWorking(t, bare)

	repo, err := Resolve(work)
	if err != nil {
		t.Fatal(err)
	}
	defaultBranch, _ := repo.DefaultBranch()

	wtPath := filepath.Join(work, ".worktree-manager", "wm/pool-test")
	if err := repo.AddWorktree(wtPath, "wm/pool-test", defaultBranch); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree path missing: %v", err)
	}

	// Create untracked file, then clean.
	writeFile(t, wtPath, "junk.txt", "junk")
	if err := repo.Clean(wtPath); err != nil {
		t.Fatalf("Clean: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wtPath, "junk.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected junk.txt gone, got %v", err)
	}

	// Make a tracked change then hard reset.
	writeFile(t, wtPath, "README.md", "modified")
	repo.HardReset(wtPath, "origin/"+defaultBranch)
	data, _ := os.ReadFile(filepath.Join(wtPath, "README.md"))
	if string(data) != "# test\n" {
		t.Fatalf("expected reset to original, got %q", data)
	}
}

func TestWorktreeExists(t *testing.T) {
	bare := initBareRepo(t)
	work := cloneWorking(t, bare)
	repo, _ := Resolve(work)

	wtPath := filepath.Join(work, ".worktree-manager", "wt1")
	repo.AddWorktree(wtPath, "wt1-branch", "main")

	exists, err := repo.WorktreeExists(wtPath)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("expected exists")
	}
	exists, _ = repo.WorktreeExists("/nonexistent/path")
	if exists {
		t.Fatal("expected not exists")
	}
}
