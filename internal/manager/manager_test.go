package manager

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bejo-dev/worktree-manager/internal/db"
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

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setupRepo creates a working git repo with an origin remote and an initial
// commit on main. Returns the working repo path.
func setupRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bare := filepath.Join(dir, "origin.git")
	run(t, dir, "git", "init", "--bare", "-b", "main", bare)
	run(t, dir, "git", "clone", bare, "work")
	work := filepath.Join(dir, "work")
	run(t, work, "git", "config", "user.email", "t@t")
	run(t, work, "git", "config", "user.name", "test")
	writeFile(t, work, "README.md", "# test\n")
	run(t, work, "git", "add", ".")
	run(t, work, "git", "commit", "-m", "init")
	run(t, work, "git", "push", "origin", "main")
	if r, err := filepath.EvalSymlinks(work); err == nil {
		return r
	}
	return work
}

func newManagerDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestAcquireCreatesWorktree(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := New(d, os.Stderr)

	res, err := m.Acquire(repo, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if res.WorktreePath == "" {
		t.Fatal("empty path")
	}
	if !res.Created {
		t.Fatal("expected created=true")
	}
	if _, err := os.Stat(res.WorktreePath); err != nil {
		t.Fatalf("worktree dir missing: %v", err)
	}

	// stdout contract: only the path; here we just verify it's absolute.
	if !filepath.IsAbs(res.WorktreePath) {
		t.Fatalf("expected absolute path, got %q", res.WorktreePath)
	}

	// The worktree should be ALLOCATED.
	wt, err := d.GetWorktreeByPath(res.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	if wt == nil || wt.Status != db.StatusAllocated {
		t.Fatalf("expected ALLOCATED, got %+v", wt)
	}
	if wt.TaskID != "task-1" {
		t.Fatalf("expected task-1, got %q", wt.TaskID)
	}
}

func TestAcquireReusesFreeWorktree(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := New(d, os.Stderr)

	r1, err := m.Acquire(repo, "task-A")
	if err != nil {
		t.Fatalf("Acquire1: %v", err)
	}
	if err := m.Release(r1.WorktreePath); err != nil {
		t.Fatalf("Release: %v", err)
	}
	r2, err := m.Acquire(repo, "task-B")
	if err != nil {
		t.Fatalf("Acquire2: %v", err)
	}
	if r1.WorktreePath != r2.WorktreePath {
		t.Fatalf("expected reuse of same worktree: %s != %s", r1.WorktreePath, r2.WorktreePath)
	}
	if r2.BranchName != "task-B" {
		t.Fatalf("expected branch to follow task id, got %q", r2.BranchName)
	}
	if r2.Created {
		t.Fatal("expected created=false on reuse")
	}
}

func TestAcquireNeverAllocatesSameWorktreeTwice(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := New(d, os.Stderr)

	r1, err := m.Acquire(repo, "task-1")
	if err != nil {
		t.Fatalf("Acquire1: %v", err)
	}
	// A second acquire must create a NEW worktree since the first is
	// ALLOCATED.
	r2, err := m.Acquire(repo, "task-2")
	if err != nil {
		t.Fatalf("Acquire2: %v", err)
	}
	if r1.WorktreePath == r2.WorktreePath {
		t.Fatal("same worktree allocated twice")
	}
}

func TestReleaseResetsAndCleans(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := New(d, os.Stderr)

	r, err := m.Acquire(repo, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	wtPath := r.WorktreePath

	// Make changes: modify tracked file + add untracked.
	writeFile(t, wtPath, "README.md", "MODIFIED")
	writeFile(t, wtPath, "junk.txt", "junk")

	if err := m.Release(wtPath); err != nil {
		t.Fatalf("Release: %v", err)
	}

	// README should be back to original.
	data, _ := os.ReadFile(filepath.Join(wtPath, "README.md"))
	if string(data) != "# test\n" {
		t.Fatalf("expected reset, got %q", data)
	}
	// junk should be gone.
	if _, err := os.Stat(filepath.Join(wtPath, "junk.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected junk gone, got %v", err)
	}

	// Status should be FREE.
	wt, _ := d.GetWorktreeByPath(wtPath)
	if wt == nil || wt.Status != db.StatusFree {
		t.Fatalf("expected FREE, got %+v", wt)
	}
	if wt.TaskID != "" {
		t.Fatalf("expected empty task, got %q", wt.TaskID)
	}
}

func TestReleaseUnmanagedWorktreeFails(t *testing.T) {
	d := newManagerDB(t)
	m := New(d, os.Stderr)

	err := m.Release("/some/random/path")
	if err == nil {
		t.Fatal("expected error for unmanaged worktree")
	}
}

func TestAcquireNoTaskID(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := New(d, os.Stderr)

	res, err := m.Acquire(repo, "")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	wt, _ := d.GetWorktreeByPath(res.WorktreePath)
	if wt == nil || wt.Status != db.StatusAllocated {
		t.Fatalf("expected ALLOCATED, got %+v", wt)
	}
	if wt.TaskID == "" {
		t.Fatal("expected generated task name")
	}
	parts := strings.Split(wt.TaskID, "-")
	if len(parts) != 3 {
		t.Fatalf("expected three-word generated name, got %q", wt.TaskID)
	}
	for i, word := range parts {
		found := false
		for _, candidate := range generatedNamePools[i] {
			if word == candidate {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("word %q is not from pool %d", word, i+1)
		}
	}
	if filepath.Base(res.WorktreePath) != wt.TaskID {
		t.Fatalf("expected folder name %q, got %q", wt.TaskID, filepath.Base(res.WorktreePath))
	}
	if wt.BranchName != wt.TaskID {
		t.Fatalf("expected generated branch name, got %q", wt.BranchName)
	}
}

func TestList(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := New(d, os.Stderr)

	m.Acquire(repo, "task-1")
	m.Acquire(repo, "task-2")

	items, err := m.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 got %d", len(items))
	}
	for _, it := range items {
		if it.Status != db.StatusAllocated {
			t.Fatalf("expected ALLOCATED, got %s", it.Status)
		}
		if it.Repository == "" {
			t.Fatal("expected repository path")
		}
	}
}

func TestVerifyClean(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := New(d, os.Stderr)

	r, _ := m.Acquire(repo, "task-1")
	_ = r

	results, err := m.Verify()
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 got %d", len(results))
	}
	for _, vr := range results {
		if !vr.Exists {
			t.Fatalf("worktree should exist: %s", vr.Path)
		}
		if len(vr.Issues) != 0 {
			t.Fatalf("unexpected issues: %v", vr.Issues)
		}
	}
}

func TestAcquireBranchFollowsTaskID(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := New(d, os.Stderr)

	r1, _ := m.Acquire(repo, "task-1")
	m.Release(r1.WorktreePath)
	r2, _ := m.Acquire(repo, "task-2")

	if r1.BranchName != "task-1" {
		t.Fatalf("expected first branch to follow task id, got %q", r1.BranchName)
	}
	if r2.BranchName != "task-2" {
		t.Fatalf("expected reused branch to follow task id, got %q", r2.BranchName)
	}
}

func TestAcquireFetchesLatest(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := New(d, os.Stderr)

	// Acquire and release a worktree.
	r1, _ := m.Acquire(repo, "task-1")
	m.Release(r1.WorktreePath)

	// Push a new commit to origin from a separate clone.
	dir := filepath.Dir(repo)
	extra := filepath.Join(dir, "extra")
	run(t, dir, "git", "clone", filepath.Join(dir, "origin.git"), "extra")
	run(t, extra, "git", "config", "user.email", "t@t")
	run(t, extra, "git", "config", "user.name", "test")
	writeFile(t, extra, "new.txt", "new")
	run(t, extra, "git", "add", ".")
	run(t, extra, "git", "commit", "-m", "second")
	run(t, extra, "git", "push", "origin", "main")

	// Acquire again; the worktree should have the new commit.
	r2, _ := m.Acquire(repo, "task-2")
	data, err := os.ReadFile(filepath.Join(r2.WorktreePath, "new.txt"))
	if err != nil {
		t.Fatalf("new file missing after acquire: %v", err)
	}
	if string(data) != "new" {
		t.Fatalf("unexpected content: %q", data)
	}
}
