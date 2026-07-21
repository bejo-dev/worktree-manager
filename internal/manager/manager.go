// Package manager implements the worktree lifecycle logic.
package manager

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bejo-dev/worktree-manager/internal/db"
	"github.com/bejo-dev/worktree-manager/internal/gitops"
)

// Manager coordinates database and git operations.
type Manager struct {
	db   *db.DB
	logw io.Writer
}

// New creates a new Manager.
func New(database *db.DB, logw io.Writer) *Manager {
	return &Manager{db: database, logw: logw}
}

// AcquireResult holds the result of an acquire operation.
type AcquireResult struct {
	WorktreePath string
	BranchName   string
	Created      bool
}

// Acquire returns a ready-to-use worktree for the given repo path. If no
// branch name is supplied, it generates one and records it as the owner.
func (m *Manager) Acquire(repoPath string, branchName string) (*AcquireResult, error) {
	if branchName == "" {
		var err error
		branchName, err = randomWorktreeName()
		if err != nil {
			return nil, err
		}
	}

	repo, err := gitops.Resolve(repoPath)
	if err != nil {
		return nil, fmt.Errorf("resolve repo: %w", err)
	}

	defaultBranch, err := repo.DefaultBranch()
	if err != nil {
		return nil, fmt.Errorf("detect default branch: %w", err)
	}

	absRoot, err := filepath.Abs(repo.Root)
	if err != nil {
		return nil, fmt.Errorf("abs root: %w", err)
	}

	// Start a transaction for repository registration + worktree lookup.
	tx, err := m.db.BeginTx()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	r, err := m.db.GetOrCreateRepository(tx, absRoot, defaultBranch)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	// Look for an existing FREE worktree (LRU).
	wt, err := m.db.FindFreeWorktree(r.ID)
	if err != nil {
		return nil, fmt.Errorf("find free worktree: %w", err)
	}

	created := false
	if wt == nil {
		// Create a new worktree.
		wt, err = m.createWorktree(r, defaultBranch, branchName)
		if err != nil {
			return nil, err
		}
		created = true
	} else if wt.BranchName != branchName {
		// A free worktree keeps its folder, but its checked-out branch follows
		// the task that is acquiring it.
		if err := (&gitops.Repo{Root: r.RootPath}).RenameWorktreeBranch(wt.Path, branchName); err != nil {
			m.markBroken(wt.ID)
			return nil, fmt.Errorf("rename worktree branch: %w", err)
		}
		txRename, err := m.db.BeginTx()
		if err != nil {
			return nil, err
		}
		defer txRename.Rollback()
		if err := m.db.UpdateWorktreePathBranch(txRename, wt.ID, wt.Path, branchName); err != nil {
			return nil, fmt.Errorf("record worktree branch: %w", err)
		}
		if err := txRename.Commit(); err != nil {
			return nil, err
		}
		wt.BranchName = branchName
	}

	gr := &gitops.Repo{Root: r.RootPath}

	// Prepare the worktree: fetch, sync to default branch, clean.
	if err := m.prepareWorktree(gr, wt, defaultBranch); err != nil {
		m.markBroken(wt.ID)
		return nil, fmt.Errorf("prepare worktree: %w", err)
	}

	// Record base commit.
	if err := m.recordBaseCommit(wt.ID, gr, defaultBranch); err != nil {
		m.logf("warning: could not record base commit: %v", err)
	}

	// Mark ALLOCATED atomically.
	tx2, err := m.db.BeginTx()
	if err != nil {
		return nil, err
	}
	defer tx2.Rollback()
	if err := m.db.MarkAllocated(tx2, wt.ID, branchName); err != nil {
		return nil, fmt.Errorf("mark allocated: %w", err)
	}
	if err := tx2.Commit(); err != nil {
		return nil, err
	}

	return &AcquireResult{WorktreePath: wt.Path, BranchName: wt.BranchName, Created: created}, nil
}

// Release resets the worktree at the given path back to the default branch and
// marks it FREE.
func (m *Manager) Release(worktreePath string) error {
	abs, err := filepath.Abs(worktreePath)
	if err != nil {
		return fmt.Errorf("abs path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	wt, err := m.db.GetWorktreeByPath(abs)
	if err != nil {
		return fmt.Errorf("lookup worktree: %w", err)
	}
	if wt == nil {
		return errors.New("worktree does not belong to the manager")
	}

	repo, err := m.db.GetRepositoryByID(wt.RepositoryID)
	if err != nil {
		return fmt.Errorf("lookup repo: %w", err)
	}
	if repo == nil {
		return errors.New("repository not registered")
	}

	gr := &gitops.Repo{Root: repo.RootPath}
	defaultBranch := repo.DefaultBranch

	// Fetch origin (ignore errors if no remote).
	if gr.HasRemote() {
		if err := gr.FetchOrigin(); err != nil {
			m.logf("warning: fetch origin failed: %v", err)
		}
	}

	// Reset to origin/<default>.
	target := "origin/" + defaultBranch
	if !gr.HasRemote() {
		target = defaultBranch
	}
	if err := gr.HardReset(abs, target); err != nil {
		m.markBroken(wt.ID)
		return fmt.Errorf("reset worktree: %w", err)
	}

	// Clean untracked files.
	if err := gr.Clean(abs); err != nil {
		m.markBroken(wt.ID)
		return fmt.Errorf("clean worktree: %w", err)
	}

	// Mark FREE atomically.
	tx, err := m.db.BeginTx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := m.db.MarkFree(tx, wt.ID); err != nil {
		return fmt.Errorf("mark free: %w", err)
	}
	return tx.Commit()
}

// ListResult is a flat view used by the list command.
type ListResult struct {
	Repository     string
	DefaultBranch  string
	Path           string
	BranchName     string
	Status         string
	TaskID         string
	LastUsed       time.Time
	LastBaseCommit string
}

// List returns all worktrees across all repositories.
func (m *Manager) List() ([]ListResult, error) {
	repos, err := m.db.ListAllRepositories()
	if err != nil {
		return nil, err
	}
	repoMap := make(map[int64]*db.Repository, len(repos))
	for _, r := range repos {
		repoMap[r.ID] = r
	}
	wts, err := m.db.ListAllWorktrees()
	if err != nil {
		return nil, err
	}
	out := make([]ListResult, 0, len(wts))
	for _, w := range wts {
		r := repoMap[w.RepositoryID]
		item := ListResult{
			Path:           w.Path,
			BranchName:     w.BranchName,
			Status:         w.Status,
			TaskID:         w.TaskID,
			LastUsed:       w.LastUsed,
			LastBaseCommit: w.LastBaseCommit,
		}
		if r != nil {
			item.Repository = r.RootPath
			item.DefaultBranch = r.DefaultBranch
		}
		out = append(out, item)
	}
	return out, nil
}

// VerifyResult describes the state of a single worktree during verify.
type VerifyResult struct {
	Path       string
	BranchName string
	Status     string
	Exists     bool
	Clean      bool
	Issues     []string
}

// Verify checks that all registered worktrees are consistent with the actual
// git state. It does not modify state; it returns a list of issues per
// worktree.
func (m *Manager) Verify() ([]VerifyResult, error) {
	repos, err := m.db.ListAllRepositories()
	if err != nil {
		return nil, err
	}
	var results []VerifyResult
	for _, r := range repos {
		gr := &gitops.Repo{Root: r.RootPath}
		wts, err := m.db.ListWorktreesByRepo(r.ID)
		if err != nil {
			return nil, err
		}
		for _, w := range wts {
			vr := VerifyResult{Path: w.Path, BranchName: w.BranchName, Status: w.Status}
			exists, err := gr.WorktreeExists(w.Path)
			if err != nil {
				vr.Issues = append(vr.Issues, fmt.Sprintf("git error: %v", err))
			} else {
				vr.Exists = exists
				if !exists {
					vr.Issues = append(vr.Issues, "worktree path not registered in git")
				}
			}
			if _, err := os.Stat(w.Path); err != nil {
				vr.Issues = append(vr.Issues, fmt.Sprintf("path missing: %v", err))
			}
			// Check status consistency.
			if w.Status == db.StatusAllocated && w.BranchName == "" {
				vr.Issues = append(vr.Issues, "allocated but no branch name")
			}
			results = append(results, vr)
		}
	}
	return results, nil
}

// createWorktree creates a new git worktree in the next pool slot and checks
// out the requested branch name.
func (m *Manager) createWorktree(r *db.Repository, defaultBranch, branchName string) (*db.Worktree, error) {
	tx, err := m.db.BeginTx()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	slot, err := m.db.NextWorktreeSlot(tx, r.ID)
	if err != nil {
		return nil, fmt.Errorf("next slot: %w", err)
	}
	poolName := fmt.Sprintf("wm/pool-%d-%d", r.ID, slot)
	worktreePath := m.worktreePath(r, poolName)
	id, err := m.db.InsertWorktree(tx, r.ID, worktreePath, branchName, db.StatusFree)
	if err != nil {
		return nil, fmt.Errorf("insert worktree: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}

	gr := &gitops.Repo{Root: r.RootPath}
	if err := gr.AddWorktree(worktreePath, branchName, defaultBranch); err != nil {
		// Roll back the placeholder row.
		cleanupTx, cerr := m.db.BeginTx()
		if cerr == nil {
			_ = m.db.DeleteWorktree(cleanupTx, id)
			_ = cleanupTx.Commit()
		}
		return nil, fmt.Errorf("git worktree add: %w", err)
	}
	return &db.Worktree{
		ID:           id,
		RepositoryID: r.ID,
		Path:         worktreePath,
		BranchName:   branchName,
		Status:       db.StatusFree,
		LastUsed:     time.Now().UTC(),
	}, nil
}

// prepareWorktree fetches origin, syncs the worktree to the latest default
// branch, and cleans untracked files.
func (m *Manager) prepareWorktree(gr *gitops.Repo, wt *db.Worktree, defaultBranch string) error {
	if gr.HasRemote() {
		if err := gr.FetchOrigin(); err != nil {
			m.logf("warning: fetch origin failed: %v", err)
		}
	}
	target := "origin/" + defaultBranch
	if !gr.HasRemote() {
		target = defaultBranch
	}
	// Reset worktree branch to the latest default branch.
	if err := gr.HardReset(wt.Path, target); err != nil {
		return err
	}
	if err := gr.Clean(wt.Path); err != nil {
		return err
	}
	return nil
}

func (m *Manager) recordBaseCommit(worktreeID int64, gr *gitops.Repo, defaultBranch string) error {
	ref := "origin/" + defaultBranch
	if !gr.HasRemote() {
		ref = defaultBranch
	}
	commit, err := gr.RevParse(ref)
	if err != nil {
		return err
	}
	tx, err := m.db.BeginTx()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := m.db.UpdateBaseCommit(tx, worktreeID, commit); err != nil {
		return err
	}
	return tx.Commit()
}

// worktreePath returns the absolute path for a pool worktree.
func (m *Manager) worktreePath(r *db.Repository, branchName string) string {
	return filepath.Join(r.RootPath, ".worktree-manager", branchName)
}

func (m *Manager) markBroken(worktreeID int64) {
	tx, err := m.db.BeginTx()
	if err != nil {
		m.logf("error starting tx to mark broken: %v", err)
		return
	}
	defer tx.Rollback()
	if err := m.db.MarkBroken(tx, worktreeID); err != nil {
		m.logf("error marking worktree broken: %v", err)
		return
	}
	if err := tx.Commit(); err != nil {
		m.logf("error committing broken status: %v", err)
	}
}

func (m *Manager) logf(format string, args ...any) {
	if m.logw == nil {
		return
	}
	fmt.Fprintf(m.logw, format+"\n", args...)
}
