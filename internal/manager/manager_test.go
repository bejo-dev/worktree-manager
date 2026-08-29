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

// setupRepoWithSubmodule creates a superproject whose initial commit records
// the first commit of a local submodule.
func setupRepoWithSubmodule(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	submoduleBare := filepath.Join(dir, "submodule.git")
	run(t, dir, "git", "init", "--bare", "-b", "main", submoduleBare)
	run(t, dir, "git", "clone", submoduleBare, "submodule-seed")
	submoduleSeed := filepath.Join(dir, "submodule-seed")
	run(t, submoduleSeed, "git", "config", "user.email", "t@t")
	run(t, submoduleSeed, "git", "config", "user.name", "test")
	writeFile(t, submoduleSeed, "README.md", "submodule A\n")
	run(t, submoduleSeed, "git", "add", ".")
	run(t, submoduleSeed, "git", "commit", "-m", "submodule A")
	run(t, submoduleSeed, "git", "push", "origin", "main")

	superBare := filepath.Join(dir, "origin.git")
	run(t, dir, "git", "init", "--bare", "-b", "main", superBare)
	run(t, dir, "git", "clone", superBare, "work")
	work := filepath.Join(dir, "work")
	run(t, work, "git", "config", "user.email", "t@t")
	run(t, work, "git", "config", "user.name", "test")
	// Git blocks the file protocol by default. Keep this test's local remote
	// usable by the production submodule commands without changing global Git
	// configuration.
	run(t, work, "git", "config", "protocol.file.allow", "always")
	run(t, work, "git", "-c", "protocol.file.allow=always", "submodule", "add", submoduleBare, "core")
	run(t, work, "git", "commit", "-m", "add submodule")
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

func newTestManager(t *testing.T, database *db.DB) *Manager {
	return newTestManagerAt(t, database, t.TempDir())
}

func newTestManagerAt(t *testing.T, database *db.DB, baseDir string) *Manager {
	t.Helper()
	m, err := NewWithBaseDir(database, os.Stderr, baseDir)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAcquireCreatesWorktree(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

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

func TestAcquireBranchNameAllowsSlash(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	baseDir := t.TempDir()
	m := newTestManagerAt(t, d, baseDir)
	canonicalBaseDir, err := canonicalPath(baseDir)
	if err != nil {
		t.Fatal(err)
	}

	res, err := m.Acquire(repo, "BenE/add-unit-menu")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if res.BranchName != "BenE/add-unit-menu" {
		t.Fatalf("expected branch name to be preserved, got %q", res.BranchName)
	}
	if !strings.HasPrefix(res.WorktreePath, filepath.Join(canonicalBaseDir, "worktree-manager")+string(filepath.Separator)) {
		t.Fatalf("expected path below custom base directory, got %q", res.WorktreePath)
	}
	if current := strings.TrimSpace(run(t, res.WorktreePath, "git", "branch", "--show-current")); current != res.BranchName {
		t.Fatalf("expected checked-out branch %q, got %q", res.BranchName, current)
	}
}

func TestDoctorRepairsLegacyBranchName(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

	res, err := m.Acquire(repo, "BenE/add-unit-menu")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	wt, err := d.GetWorktreeByPath(res.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	legacyBranch := "wm/pool-legacy"
	run(t, wt.Path, "git", "branch", "-M", legacyBranch)
	tx, err := d.BeginTx()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.UpdateWorktreeIdentity(tx, wt.ID, legacyBranch, "BenE/add-unit-menu"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetWorktreeStatus(tx, wt.ID, db.StatusAllocated, "BenE/add-unit-menu"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	report, err := m.Doctor()
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if report.Checked != 1 || report.Repaired != 1 || len(report.Issues) != 0 {
		t.Fatalf("unexpected doctor report: %+v", report)
	}
	updated, _ := d.GetWorktreeByPath(res.WorktreePath)
	if updated.BranchName != "BenE/add-unit-menu" {
		t.Fatalf("expected migrated branch in database, got %q", updated.BranchName)
	}
	if current := strings.TrimSpace(run(t, wt.Path, "git", "branch", "--show-current")); current != "BenE/add-unit-menu" {
		t.Fatalf("expected migrated Git branch, got %q", current)
	}
}

func TestAcquireReusesFreeWorktree(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

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
		t.Fatalf("expected branch to follow branch name, got %q", r2.BranchName)
	}
	if r2.Created {
		t.Fatal("expected created=false on reuse")
	}
}

func TestAcquireNeverAllocatesSameWorktreeTwice(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

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

func TestReleaseAdoptsWorktreeFromAnotherDatabase(t *testing.T) {
	repo := setupRepo(t)
	first := newManagerDB(t)
	baseDir := t.TempDir()
	created, err := newTestManagerAt(t, first, baseDir).Acquire(repo, "cross-db-task")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	second := newManagerDB(t)
	if err := newTestManagerAt(t, second, baseDir).Release(created.WorktreePath); err != nil {
		t.Fatalf("Release from another database: %v", err)
	}
	wt, err := second.GetWorktreeByPath(created.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	if wt == nil || wt.Status != db.StatusFree {
		t.Fatalf("expected adopted worktree to be FREE, got %+v", wt)
	}
}

func TestAcquireRejectsBranchAlreadyUsedByGitWorktree(t *testing.T) {
	repo := setupRepo(t)
	first := newManagerDB(t)
	baseDir := t.TempDir()
	if _, err := newTestManagerAt(t, first, baseDir).Acquire(repo, "same-branch"); err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	second := newManagerDB(t)
	_, err := newTestManagerAt(t, second, baseDir).Acquire(repo, "same-branch")
	if err == nil || !strings.Contains(err.Error(), "already assigned") {
		t.Fatalf("expected branch collision, got %v", err)
	}
}

func TestReleaseResetsAndCleans(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

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
	if wt.BranchName != "main" {
		t.Fatalf("expected default branch to be recorded, got %q", wt.BranchName)
	}
	if current := strings.TrimSpace(run(t, wtPath, "git", "branch", "--show-current")); current != "" {
		t.Fatalf("expected released worktree to be detached, got branch %q", current)
	}
	if _, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/task-1").CombinedOutput(); err == nil {
		t.Fatal("expected released branch to be deleted")
	}
}

func TestReleaseFetchesLatestDefaultBranch(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

	r, err := m.Acquire(repo, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// Advance origin after the worktree is acquired. Release must fetch this
	// commit before resetting the worktree to the default branch.
	dir := filepath.Dir(repo)
	extra := filepath.Join(dir, "release-extra")
	run(t, dir, "git", "clone", filepath.Join(dir, "origin.git"), extra)
	run(t, extra, "git", "config", "user.email", "t@t")
	run(t, extra, "git", "config", "user.name", "test")
	writeFile(t, extra, "latest.txt", "latest")
	run(t, extra, "git", "add", ".")
	run(t, extra, "git", "commit", "-m", "latest")
	run(t, extra, "git", "push", "origin", "main")

	if err := m.Release(r.WorktreePath); err != nil {
		t.Fatalf("Release: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(r.WorktreePath, "latest.txt"))
	if err != nil {
		t.Fatalf("latest default-branch file missing after release: %v", err)
	}
	if string(data) != "latest" {
		t.Fatalf("unexpected latest file content: %q", data)
	}
	if head := strings.TrimSpace(run(t, r.WorktreePath, "git", "rev-parse", "HEAD")); head != strings.TrimSpace(run(t, repo, "git", "rev-parse", "origin/main")) {
		t.Fatalf("released worktree is not at origin/main: %s", head)
	}
	if status := strings.TrimSpace(run(t, r.WorktreePath, "git", "status", "--porcelain", "--untracked-files=all", "--ignored")); status != "" {
		t.Fatalf("released worktree is not clean:\n%s", status)
	}
}

func TestReleaseAlignsSubmoduleWithSuperproject(t *testing.T) {
	repo := setupRepoWithSubmodule(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

	result, err := m.Acquire(repo, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	run(t, result.WorktreePath, "git", "-c", "protocol.file.allow=always", "submodule", "update", "--init", "--recursive")
	worktreeSubmodule := filepath.Join(result.WorktreePath, "core")
	initialCommit := strings.TrimSpace(run(t, worktreeSubmodule, "git", "rev-parse", "HEAD"))

	dir := filepath.Dir(repo)
	extra := filepath.Join(dir, "submodule-extra")
	run(t, dir, "git", "clone", filepath.Join(dir, "submodule.git"), extra)
	run(t, extra, "git", "config", "user.email", "t@t")
	run(t, extra, "git", "config", "user.name", "test")
	writeFile(t, extra, "README.md", "submodule B\n")
	run(t, extra, "git", "add", ".")
	run(t, extra, "git", "commit", "-m", "submodule B")
	run(t, extra, "git", "push", "origin", "main")
	run(t, result.WorktreePath, "git", "-c", "protocol.file.allow=always", "-C", "core", "fetch", "origin", "main")

	run(t, repo, "git", "-c", "protocol.file.allow=always", "-C", "core", "fetch", "origin", "main")
	run(t, repo, "git", "-C", "core", "checkout", "--detach", "origin/main")
	updatedCommit := strings.TrimSpace(run(t, repo, "git", "-C", "core", "rev-parse", "HEAD"))
	if updatedCommit == initialCommit {
		t.Fatal("expected submodule to advance to a new commit")
	}
	run(t, repo, "git", "add", "core")
	run(t, repo, "git", "commit", "-m", "advance submodule")
	run(t, repo, "git", "push", "origin", "main")

	if err := m.Release(result.WorktreePath); err != nil {
		t.Fatalf("Release: %v", err)
	}
	gotCommit := strings.TrimSpace(run(t, worktreeSubmodule, "git", "rev-parse", "HEAD"))
	if gotCommit != updatedCommit {
		t.Fatalf("submodule is at %s, want superproject commit %s", gotCommit, updatedCommit)
	}
	if status := strings.TrimSpace(run(t, result.WorktreePath, "git", "status", "--porcelain", "--untracked-files=all", "--ignored")); status != "" {
		t.Fatalf("released worktree is not clean:\n%s", status)
	}
	worktree, err := d.GetWorktreeByPath(result.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	if worktree == nil || worktree.Status != db.StatusFree {
		t.Fatalf("expected released worktree to be FREE, got %+v", worktree)
	}
}

func TestAcquireFailsWhenFetchFails(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)
	run(t, repo, "git", "remote", "set-url", "origin", filepath.Join(filepath.Dir(repo), "missing.git"))

	if _, err := m.Acquire(repo, "task-1"); err == nil || !strings.Contains(err.Error(), "fetch origin") {
		t.Fatalf("expected fetch failure, got %v", err)
	}
}

func TestAcquireMarksWorktreeBrokenWhenDependencyInstallFails(t *testing.T) {
	repo := setupRepo(t)
	writeFile(t, repo, "go.mod", "this is not a go module\n")
	run(t, repo, "git", "add", "go.mod")
	run(t, repo, "git", "commit", "-m", "add invalid module")
	run(t, repo, "git", "push", "origin", "main")

	d := newManagerDB(t)
	m := newTestManager(t, d)
	if _, err := m.Acquire(repo, "task-1"); err == nil || !strings.Contains(err.Error(), "install dependencies") {
		t.Fatalf("expected dependency installation failure, got %v", err)
	}

	worktrees, err := d.ListAllWorktrees()
	if err != nil {
		t.Fatal(err)
	}
	if len(worktrees) != 1 || worktrees[0].Status != db.StatusBroken {
		t.Fatalf("expected one BROKEN worktree, got %+v", worktrees)
	}
}

func TestReleaseDoesNotReturnStaleWorktreeWhenFetchFails(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

	r, err := m.Acquire(repo, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	run(t, repo, "git", "remote", "set-url", "origin", filepath.Join(filepath.Dir(repo), "missing.git"))

	if err := m.Release(r.WorktreePath); err == nil || !strings.Contains(err.Error(), "fetch origin") {
		t.Fatalf("expected fetch failure, got %v", err)
	}
	wt, err := d.GetWorktreeByPath(r.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	if wt == nil || wt.Status != db.StatusAllocated {
		t.Fatalf("expected worktree to remain allocated after fetch failure, got %+v", wt)
	}
}

func TestReleaseUnmanagedWorktreeFails(t *testing.T) {
	d := newManagerDB(t)
	m := newTestManager(t, d)

	err := m.Release("/some/random/path")
	if err == nil {
		t.Fatal("expected error for unmanaged worktree")
	}
}

func TestDoctorRecoversDetachedBrokenWorktree(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

	result, err := m.Acquire(repo, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	run(t, result.WorktreePath, "git", "checkout", "--detach", "HEAD")
	worktree, err := d.GetWorktreeByPath(result.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := d.BeginTx()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.MarkBroken(tx, worktree.ID); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	writeFile(t, repo, "latest.txt", "latest")
	run(t, repo, "git", "add", "latest.txt")
	run(t, repo, "git", "commit", "-m", "latest")
	run(t, repo, "git", "push", "origin", "main")

	report, err := m.Doctor()
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if report.Checked != 1 || report.Repaired != 1 || len(report.Issues) != 0 {
		t.Fatalf("unexpected doctor report: %+v", report)
	}
	updated, err := d.GetWorktreeByPath(result.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	if updated == nil || updated.Status != db.StatusFree {
		t.Fatalf("expected recovered worktree to be FREE, got %+v", updated)
	}
	if updated.BranchName != "main" || updated.TaskID != "" {
		t.Fatalf("expected recovered worktree ownership to be cleared, got %+v", updated)
	}
	if current := strings.TrimSpace(run(t, result.WorktreePath, "git", "branch", "--show-current")); current != "" {
		t.Fatalf("expected recovered worktree to remain detached, got branch %q", current)
	}
	if _, err := exec.Command("git", "-C", repo, "rev-parse", "--verify", "--quiet", "refs/heads/task-1").CombinedOutput(); err == nil {
		t.Fatal("expected recovered task branch to be deleted")
	}
	if data, err := os.ReadFile(filepath.Join(result.WorktreePath, "latest.txt")); err != nil || string(data) != "latest" {
		t.Fatalf("expected recovery to reset to latest origin/main, got %q (%v)", data, err)
	}
}

func TestDoctorDoesNotRecoverDirtyDetachedBrokenWorktree(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

	result, err := m.Acquire(repo, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	run(t, result.WorktreePath, "git", "checkout", "--detach", "HEAD")
	writeFile(t, result.WorktreePath, "uncommitted.txt", "keep for review")
	worktree, err := d.GetWorktreeByPath(result.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := d.BeginTx()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.MarkBroken(tx, worktree.ID); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	report, err := m.Doctor()
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if report.Checked != 1 || report.Repaired != 0 || len(report.Issues) != 1 {
		t.Fatalf("unexpected doctor report: %+v", report)
	}
	if !strings.Contains(report.Issues[0], "dirty") {
		t.Fatalf("expected dirty-worktree issue, got %q", report.Issues[0])
	}
	updated, err := d.GetWorktreeByPath(result.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	if updated == nil || updated.Status != db.StatusBroken {
		t.Fatalf("expected worktree to remain BROKEN, got %+v", updated)
	}
}

func TestVerifyClassifiesRecoverableBrokenWorktree(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

	result, err := m.Acquire(repo, "task-1")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	run(t, result.WorktreePath, "git", "checkout", "--detach", "HEAD")
	worktree, err := d.GetWorktreeByPath(result.WorktreePath)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := d.BeginTx()
	if err != nil {
		t.Fatal(err)
	}
	if err := d.MarkBroken(tx, worktree.ID); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	results, err := m.Verify()
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Exists || !results[0].Clean {
		t.Fatalf("expected existing clean worktree, got %+v", results[0])
	}
	found := false
	for _, issue := range results[0].Issues {
		if strings.Contains(issue, "recoverable BROKEN worktree") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected recoverable broken-worktree issue, got %v", results[0].Issues)
	}
}

func TestVerifyDistinguishesStaleMetadataAndMissingMarker(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

	markerMissing, err := m.Acquire(repo, "marker-missing")
	if err != nil {
		t.Fatalf("Acquire marker-missing: %v", err)
	}
	pathMissing, err := m.Acquire(repo, "path-missing")
	if err != nil {
		t.Fatalf("Acquire path-missing: %v", err)
	}
	if err := os.Remove(filepath.Join(markerMissing.WorktreePath, ".git")); err != nil {
		t.Fatalf("remove worktree marker: %v", err)
	}
	if err := os.RemoveAll(pathMissing.WorktreePath); err != nil {
		t.Fatalf("remove worktree path: %v", err)
	}

	for _, path := range []string{markerMissing.WorktreePath, pathMissing.WorktreePath} {
		worktree, err := d.GetWorktreeByPath(path)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := d.BeginTx()
		if err != nil {
			t.Fatal(err)
		}
		if err := d.MarkBroken(tx, worktree.ID); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	results, err := m.Verify()
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	var sawStaleMetadata, sawMissingMarker bool
	for _, result := range results {
		for _, issue := range result.Issues {
			if strings.Contains(issue, "stale Git metadata") {
				sawStaleMetadata = true
			}
			if strings.Contains(issue, "missing worktree .git marker") {
				sawMissingMarker = true
			}
		}
	}
	if !sawStaleMetadata || !sawMissingMarker {
		t.Fatalf("expected stale metadata and missing marker issues, got %+v", results)
	}
}

func TestAcquireNoTaskID(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

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
	if !strings.Contains(res.WorktreePath, filepath.Join("worktree-manager", "repo-")) {
		t.Fatalf("expected pool folder, got %q", res.WorktreePath)
	}
	if wt.BranchName != wt.TaskID {
		t.Fatalf("expected generated branch name, got %q", wt.BranchName)
	}
}

func TestDefaultWorktreeBaseDir(t *testing.T) {
	d := newManagerDB(t)
	m := New(d, os.Stderr)
	path := m.worktreePath(&db.Repository{RootPath: "/repo"}, "pool-1-1")
	wantPrefix := filepath.Join(DefaultWorktreeBaseDir, "worktree-manager", repositoryDirectoryName("/repo")) + string(filepath.Separator)
	if !strings.HasPrefix(path, wantPrefix) {
		t.Fatalf("expected default base directory %q, got %q", DefaultWorktreeBaseDir, path)
	}
}

func TestWorktreePathUsesRepositoryName(t *testing.T) {
	d := newManagerDB(t)
	baseDir := t.TempDir()
	m := newTestManagerAt(t, d, baseDir)
	canonicalBaseDir, err := canonicalPath(baseDir)
	if err != nil {
		t.Fatal(err)
	}

	path := m.worktreePath(&db.Repository{RootPath: "/projects/worktree-manager"}, "pool-1-1")
	want := filepath.Join(canonicalBaseDir, "worktree-manager", "repo-worktree-manager", "pool-1-1")
	if path != want {
		t.Fatalf("expected repository-named pool path %q, got %q", want, path)
	}
}

func TestList(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

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

func TestRemoveAllWorktrees(t *testing.T) {
	firstRepository := setupRepo(t)
	secondRepository := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

	first, err := m.Acquire(firstRepository, "first-task")
	if err != nil {
		t.Fatalf("Acquire first worktree: %v", err)
	}
	second, err := m.Acquire(secondRepository, "second-task")
	if err != nil {
		t.Fatalf("Acquire second worktree: %v", err)
	}

	removed, err := m.RemoveAllWorktrees()
	if err != nil {
		t.Fatalf("RemoveAllWorktrees: %v", err)
	}
	if removed != 2 {
		t.Fatalf("removed %d worktrees, want 2", removed)
	}
	for _, path := range []string{first.WorktreePath, second.WorktreePath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected removed worktree path %s, got %v", path, err)
		}
	}
}

func TestRemoveAllWorktreesRejectsPathOutsidePool(t *testing.T) {
	repository := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

	tx, err := d.BeginTx()
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	registeredRepository, err := d.GetOrCreateRepository(tx, repository, "main")
	if err != nil {
		t.Fatalf("GetOrCreateRepository: %v", err)
	}
	unsafePath := t.TempDir()
	if _, err := d.InsertWorktree(tx, registeredRepository.ID, unsafePath, "unsafe", db.StatusAllocated); err != nil {
		t.Fatalf("InsertWorktree: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	if _, err := m.RemoveAllWorktrees(); err == nil || !strings.Contains(err.Error(), "outside the manager pool") {
		t.Fatalf("expected pool-path validation error, got %v", err)
	}
	if _, err := os.Stat(unsafePath); err != nil {
		t.Fatalf("expected unsafe path to remain, got %v", err)
	}
}

func TestRemoveAllWorktreesPrunesMissingWorktree(t *testing.T) {
	repository := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

	worktree, err := m.Acquire(repository, "missing-task")
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.RemoveAll(worktree.WorktreePath); err != nil {
		t.Fatalf("RemoveAll test worktree: %v", err)
	}

	removed, err := m.RemoveAllWorktrees()
	if err != nil {
		t.Fatalf("RemoveAllWorktrees: %v", err)
	}
	if removed != 1 {
		t.Fatalf("removed %d worktrees, want 1", removed)
	}
}

func TestVerifyClean(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

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
	m := newTestManager(t, d)

	r1, _ := m.Acquire(repo, "task-1")
	m.Release(r1.WorktreePath)
	r2, _ := m.Acquire(repo, "task-2")

	if r1.BranchName != "task-1" {
		t.Fatalf("expected first branch to follow branch name, got %q", r1.BranchName)
	}
	if r2.BranchName != "task-2" {
		t.Fatalf("expected reused branch to follow branch name, got %q", r2.BranchName)
	}
}

func TestAcquireFetchesLatest(t *testing.T) {
	repo := setupRepo(t)
	d := newManagerDB(t)
	m := newTestManager(t, d)

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
