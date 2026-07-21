package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Status values for a worktree.
const (
	StatusFree      = "FREE"
	StatusAllocated = "ALLOCATED"
	StatusBroken    = "BROKEN"
)

// Repository represents a managed git repository.
type Repository struct {
	ID            int64
	RootPath      string
	DefaultBranch string
}

// Worktree represents a managed git worktree.
type Worktree struct {
	ID             int64
	RepositoryID   int64
	Path           string
	BranchName     string
	Status         string
	TaskID         string
	LastUsed       time.Time
	LastBaseCommit string
}

// DB wraps the sqlite connection.
type DB struct {
	conn *sql.DB
}

// DefaultStateDir returns the default state directory path.
func DefaultStateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".worktree-manager")
}

// DefaultDBPath returns the default database file path.
func DefaultDBPath() string {
	return filepath.Join(DefaultStateDir(), "state.db")
}

// IsReadonlyError reports whether err is SQLite's read-only database error.
func IsReadonlyError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "readonly database") || strings.Contains(message, "sqlite_readonly")
}

// Open opens (and migrates) the database at the given path.
func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// Enable foreign keys and a reasonable busy timeout.
	if _, err := conn.Exec("PRAGMA foreign_keys = ON; PRAGMA busy_timeout = 5000;"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("pragma: %w", err)
	}
	d := &DB{conn: conn}
	if err := d.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.conn.Close()
}

func (d *DB) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS repositories (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			root_path TEXT UNIQUE NOT NULL,
			default_branch TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS worktrees (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repository_id INTEGER NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
			path TEXT UNIQUE NOT NULL,
			branch_name TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'FREE',
			task_id TEXT,
			last_used TIMESTAMP,
			last_base_commit TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_worktrees_repository_status ON worktrees(repository_id, status);`,
		`CREATE INDEX IF NOT EXISTS idx_worktrees_path ON worktrees(path);`,
	}
	for _, s := range stmts {
		if _, err := d.conn.Exec(s); err != nil {
			return fmt.Errorf("exec migration: %w", err)
		}
	}
	return nil
}

// BeginTx starts a serializable transaction.
func (d *DB) BeginTx() (*sql.Tx, error) {
	return d.conn.Begin()
}

// GetOrCreateRepository returns the repository for rootPath, creating it if missing.
// It runs inside a transaction so callers can chain further state changes atomically.
func (d *DB) GetOrCreateRepository(tx *sql.Tx, rootPath, defaultBranch string) (*Repository, error) {
	row := tx.QueryRow(`SELECT id, root_path, default_branch FROM repositories WHERE root_path = ?`, rootPath)
	var r Repository
	err := row.Scan(&r.ID, &r.RootPath, &r.DefaultBranch)
	if err == nil {
		return &r, nil
	}
	if err != sql.ErrNoRows {
		return nil, fmt.Errorf("query repository: %w", err)
	}
	res, err := tx.Exec(`INSERT INTO repositories (root_path, default_branch) VALUES (?, ?)`, rootPath, defaultBranch)
	if err != nil {
		return nil, fmt.Errorf("insert repository: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}
	return &Repository{ID: id, RootPath: rootPath, DefaultBranch: defaultBranch}, nil
}

// GetRepositoryByPath returns the repository for the given root path.
func (d *DB) GetRepositoryByPath(rootPath string) (*Repository, error) {
	row := d.conn.QueryRow(`SELECT id, root_path, default_branch FROM repositories WHERE root_path = ?`, rootPath)
	var r Repository
	if err := row.Scan(&r.ID, &r.RootPath, &r.DefaultBranch); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// GetRepositoryByID returns the repository with the given id.
func (d *DB) GetRepositoryByID(id int64) (*Repository, error) {
	row := d.conn.QueryRow(`SELECT id, root_path, default_branch FROM repositories WHERE id = ?`, id)
	var r Repository
	if err := row.Scan(&r.ID, &r.RootPath, &r.DefaultBranch); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &r, nil
}

// FindFreeWorktree returns the least-recently-used FREE worktree for a repository.
func (d *DB) FindFreeWorktree(repoID int64) (*Worktree, error) {
	row := d.conn.QueryRow(`SELECT id, repository_id, path, branch_name, status, task_id, last_used, last_base_commit
		FROM worktrees
		WHERE repository_id = ? AND status = ?
		ORDER BY last_used IS NULL DESC, last_used ASC
		LIMIT 1`, repoID, StatusFree)
	w, err := scanWorktree(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return w, nil
}

// GetWorktreeByPath returns the worktree registered at the given path.
func (d *DB) GetWorktreeByPath(path string) (*Worktree, error) {
	row := d.conn.QueryRow(`SELECT id, repository_id, path, branch_name, status, task_id, last_used, last_base_commit
		FROM worktrees WHERE path = ?`, path)
	w, err := scanWorktree(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return w, nil
}

// GetWorktree returns a worktree by id.
func (d *DB) GetWorktree(id int64) (*Worktree, error) {
	row := d.conn.QueryRow(`SELECT id, repository_id, path, branch_name, status, task_id, last_used, last_base_commit
		FROM worktrees WHERE id = ?`, id)
	w, err := scanWorktree(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return w, nil
}

// ListWorktreesByRepo returns all worktrees for a repository.
func (d *DB) ListWorktreesByRepo(repoID int64) ([]*Worktree, error) {
	rows, err := d.conn.Query(`SELECT id, repository_id, path, branch_name, status, task_id, last_used, last_base_commit
		FROM worktrees WHERE repository_id = ? ORDER BY id`, repoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Worktree
	for rows.Next() {
		w, err := scanWorktreeRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ListAllWorktrees returns all worktrees across all repositories.
func (d *DB) ListAllWorktrees() ([]*Worktree, error) {
	rows, err := d.conn.Query(`SELECT id, repository_id, path, branch_name, status, task_id, last_used, last_base_commit
		FROM worktrees ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Worktree
	for rows.Next() {
		w, err := scanWorktreeRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// ListAllRepositories returns all registered repositories.
func (d *DB) ListAllRepositories() ([]*Repository, error) {
	rows, err := d.conn.Query(`SELECT id, root_path, default_branch FROM repositories ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Repository
	for rows.Next() {
		var r Repository
		if err := rows.Scan(&r.ID, &r.RootPath, &r.DefaultBranch); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, rows.Err()
}

// InsertWorktree registers a new worktree. Returns the inserted id.
func (d *DB) InsertWorktree(tx *sql.Tx, repoID int64, path, branchName, status string) (int64, error) {
	res, err := tx.Exec(`INSERT INTO worktrees (repository_id, path, branch_name, status, last_used)
		VALUES (?, ?, ?, ?, ?)`, repoID, path, branchName, status, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateWorktreePathBranch sets the path and branch name for a worktree (used
// after the worktree id is known so the branch can be derived deterministically).
func (d *DB) UpdateWorktreePathBranch(tx *sql.Tx, id int64, path, branchName string) error {
	_, err := tx.Exec(`UPDATE worktrees SET path = ?, branch_name = ? WHERE id = ?`, path, branchName, id)
	return err
}

// UpdateWorktreeIdentity updates the branch and internal ownership label.
func (d *DB) UpdateWorktreeIdentity(tx *sql.Tx, id int64, branchName, owner string) error {
	_, err := tx.Exec(`UPDATE worktrees SET branch_name = ?, task_id = ? WHERE id = ?`,
		branchName, nullable(owner), id)
	return err
}

// NextWorktreeSlot returns the next slot number for a repository. It is the
// maximum existing worktree id for that repository plus one (or 1 if none).
// Call inside a transaction for consistency.
func (d *DB) NextWorktreeSlot(tx *sql.Tx, repoID int64) (int64, error) {
	var maxID sql.NullInt64
	err := tx.QueryRow(`SELECT MAX(id) FROM worktrees WHERE repository_id = ?`, repoID).Scan(&maxID)
	if err != nil {
		return 0, err
	}
	if !maxID.Valid {
		return 1, nil
	}
	return maxID.Int64 + 1, nil
}

// SetWorktreeStatus updates status (and optionally task id) inside a transaction.
func (d *DB) SetWorktreeStatus(tx *sql.Tx, id int64, status, taskID string) error {
	_, err := tx.Exec(`UPDATE worktrees SET status = ?, task_id = ?, last_used = ? WHERE id = ?`,
		status, nullable(taskID), time.Now().UTC(), id)
	return err
}

// MarkAllocated marks the worktree ALLOCATED for a task.
func (d *DB) MarkAllocated(tx *sql.Tx, id int64, taskID string) error {
	return d.SetWorktreeStatus(tx, id, StatusAllocated, taskID)
}

// MarkFree marks the worktree FREE and clears task ownership.
func (d *DB) MarkFree(tx *sql.Tx, id int64) error {
	return d.SetWorktreeStatus(tx, id, StatusFree, "")
}

// MarkBroken marks the worktree BROKEN.
func (d *DB) MarkBroken(tx *sql.Tx, id int64) error {
	return d.SetWorktreeStatus(tx, id, StatusBroken, "")
}

// UpdateBaseCommit records the base commit the worktree was synced to.
func (d *DB) UpdateBaseCommit(tx *sql.Tx, id int64, commit string) error {
	_, err := tx.Exec(`UPDATE worktrees SET last_base_commit = ? WHERE id = ?`, commit, id)
	return err
}

// CountAllocated returns the number of allocated worktrees for a repository.
func (d *DB) CountAllocated(repoID int64) (int, error) {
	var n int
	err := d.conn.QueryRow(`SELECT COUNT(*) FROM worktrees WHERE repository_id = ? AND status = ?`,
		repoID, StatusAllocated).Scan(&n)
	return n, err
}

// DeleteWorktree removes a worktree record by id.
func (d *DB) DeleteWorktree(tx *sql.Tx, id int64) error {
	_, err := tx.Exec(`DELETE FROM worktrees WHERE id = ?`, id)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanWorktree(s scanner) (*Worktree, error) {
	w := &Worktree{}
	var taskID sql.NullString
	var lastUsed sql.NullTime
	var baseCommit sql.NullString
	if err := s.Scan(&w.ID, &w.RepositoryID, &w.Path, &w.BranchName, &w.Status, &taskID, &lastUsed, &baseCommit); err != nil {
		return nil, err
	}
	w.TaskID = taskID.String
	if lastUsed.Valid {
		w.LastUsed = lastUsed.Time
	}
	w.LastBaseCommit = baseCommit.String
	return w, nil
}

func scanWorktreeRows(rows *sql.Rows) (*Worktree, error) {
	return scanWorktree(rows)
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
