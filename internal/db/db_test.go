package db

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestIsReadonlyError(t *testing.T) {
	if !IsReadonlyError(errors.New("attempt to write a readonly database (8)")) {
		t.Fatal("expected SQLite readonly error to be detected")
	}
	if IsReadonlyError(errors.New("constraint failed")) {
		t.Fatal("did not expect unrelated error to be detected as readonly")
	}
}

func newTestDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	d, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func mustTx(t *testing.T, d *DB) *sql.Tx {
	t.Helper()
	tx, err := d.BeginTx()
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	t.Cleanup(func() { tx.Rollback() })
	return tx
}

func TestGetOrCreateRepository(t *testing.T) {
	d := newTestDB(t)

	tx := mustTx(t, d)
	r1, err := d.GetOrCreateRepository(tx, "/repo/a", "main")
	if err != nil {
		t.Fatalf("GetOrCreate: %v", err)
	}
	if r1.ID == 0 || r1.RootPath != "/repo/a" || r1.DefaultBranch != "main" {
		t.Fatalf("unexpected repo: %+v", r1)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// Second call should return the same repo.
	tx2 := mustTx(t, d)
	r2, err := d.GetOrCreateRepository(tx2, "/repo/a", "main")
	if err != nil {
		t.Fatalf("GetOrCreate 2: %v", err)
	}
	if r2.ID != r1.ID {
		t.Fatalf("expected same id %d got %d", r1.ID, r2.ID)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
}

func TestGetRepositoryByPath(t *testing.T) {
	d := newTestDB(t)
	tx := mustTx(t, d)
	if _, err := d.GetOrCreateRepository(tx, "/repo/x", "master"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	got, err := d.GetRepositoryByPath("/repo/x")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.DefaultBranch != "master" {
		t.Fatalf("unexpected: %+v", got)
	}

	missing, err := d.GetRepositoryByPath("/nope")
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Fatalf("expected nil, got %+v", missing)
	}
}

func TestGetRepositoryByID(t *testing.T) {
	d := newTestDB(t)
	tx := mustTx(t, d)
	r, err := d.GetOrCreateRepository(tx, "/repo/y", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := d.GetRepositoryByID(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != r.ID {
		t.Fatalf("unexpected: %+v", got)
	}
	missing, err := d.GetRepositoryByID(99999)
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Fatalf("expected nil")
	}
}

func TestInsertAndFindFreeWorktree(t *testing.T) {
	d := newTestDB(t)
	tx := mustTx(t, d)
	r, err := d.GetOrCreateRepository(tx, "/repo/1", "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// Insert two free worktrees.
	tx2, err := d.BeginTx()
	if err != nil {
		t.Fatal(err)
	}
	id1, err := d.InsertWorktree(tx2, r.ID, "/path/wt1", "wm/pool-1", StatusFree)
	if err != nil {
		t.Fatalf("insert1: %v", err)
	}
	id2, err := d.InsertWorktree(tx2, r.ID, "/path/wt2", "wm/pool-2", StatusFree)
	if err != nil {
		t.Fatalf("insert2: %v", err)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatal(err)
	}
	if id1 == 0 || id2 == 0 || id1 == id2 {
		t.Fatalf("unexpected ids %d %d", id1, id2)
	}

	// Find free should return one of them.
	got, err := d.FindFreeWorktree(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected a free worktree")
	}
	if got.RepositoryID != r.ID || got.Status != StatusFree {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestFindFreeWorktreeLRU(t *testing.T) {
	d := newTestDB(t)
	tx := mustTx(t, d)
	r, _ := d.GetOrCreateRepository(tx, "/repo/2", "main")
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// Create two worktrees: wt-older and wt-newer. Set last_used so wt-older
	// is the least recently used.
	older := time.Now().Add(-2 * time.Hour)
	newer := time.Now().Add(-1 * time.Hour)

	tx2, _ := d.BeginTx()
	id1, _ := d.InsertWorktree(tx2, r.ID, "/path/older", "b1", StatusFree)
	tx2.Commit()

	tx3, _ := d.BeginTx()
	id2, _ := d.InsertWorktree(tx3, r.ID, "/path/newer", "b2", StatusFree)
	tx3.Commit()

	// Set last_used directly.
	_, _ = d.conn.Exec("UPDATE worktrees SET last_used = ? WHERE id = ?", older, id1)
	_, _ = d.conn.Exec("UPDATE worktrees SET last_used = ? WHERE id = ?", newer, id2)

	got, err := d.FindFreeWorktree(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != id1 {
		t.Fatalf("expected LRU id %d got %d", id1, got.ID)
	}
}

func TestMarkAllocatedFreeBroken(t *testing.T) {
	d := newTestDB(t)
	tx := mustTx(t, d)
	r, _ := d.GetOrCreateRepository(tx, "/repo/3", "main")
	tx.Commit()

	tx2, _ := d.BeginTx()
	id, _ := d.InsertWorktree(tx2, r.ID, "/path/x", "b", StatusFree)
	tx2.Commit()

	// Mark allocated.
	tx3, _ := d.BeginTx()
	if err := d.MarkAllocated(tx3, id, "task-1"); err != nil {
		t.Fatal(err)
	}
	tx3.Commit()
	wt, _ := d.GetWorktree(id)
	if wt.Status != StatusAllocated || wt.TaskID != "task-1" {
		t.Fatalf("unexpected: %+v", wt)
	}

	// Mark free.
	tx4, _ := d.BeginTx()
	if err := d.MarkFree(tx4, id); err != nil {
		t.Fatal(err)
	}
	tx4.Commit()
	wt, _ = d.GetWorktree(id)
	if wt.Status != StatusFree || wt.TaskID != "" {
		t.Fatalf("unexpected: %+v", wt)
	}

	// Mark broken.
	tx5, _ := d.BeginTx()
	if err := d.MarkBroken(tx5, id); err != nil {
		t.Fatal(err)
	}
	tx5.Commit()
	wt, _ = d.GetWorktree(id)
	if wt.Status != StatusBroken {
		t.Fatalf("unexpected: %+v", wt)
	}

	// Broken should not be returned by FindFreeWorktree.
	got, _ := d.FindFreeWorktree(r.ID)
	if got != nil {
		t.Fatalf("expected nil for broken, got %+v", got)
	}
}

func TestFindFreeWorktree_NoneFree(t *testing.T) {
	d := newTestDB(t)
	tx := mustTx(t, d)
	r, _ := d.GetOrCreateRepository(tx, "/repo/4", "main")
	tx.Commit()

	tx2, _ := d.BeginTx()
	d.InsertWorktree(tx2, r.ID, "/path/z", "b", StatusAllocated)
	tx2.Commit()

	got, err := d.FindFreeWorktree(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestGetWorktreeByPath(t *testing.T) {
	d := newTestDB(t)
	tx := mustTx(t, d)
	r, _ := d.GetOrCreateRepository(tx, "/repo/5", "main")
	tx.Commit()

	tx2, _ := d.BeginTx()
	d.InsertWorktree(tx2, r.ID, "/path/abc", "b", StatusFree)
	tx2.Commit()

	got, err := d.GetWorktreeByPath("/path/abc")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected worktree")
	}
	missing, _ := d.GetWorktreeByPath("/nope")
	if missing != nil {
		t.Fatal("expected nil")
	}
}

func TestUpdateBaseCommit(t *testing.T) {
	d := newTestDB(t)
	tx := mustTx(t, d)
	r, _ := d.GetOrCreateRepository(tx, "/repo/6", "main")
	tx.Commit()

	tx2, _ := d.BeginTx()
	id, _ := d.InsertWorktree(tx2, r.ID, "/path/c", "b", StatusFree)
	tx2.Commit()

	tx3, _ := d.BeginTx()
	if err := d.UpdateBaseCommit(tx3, id, "abc123"); err != nil {
		t.Fatal(err)
	}
	tx3.Commit()

	wt, _ := d.GetWorktree(id)
	if wt.LastBaseCommit != "abc123" {
		t.Fatalf("expected abc123 got %s", wt.LastBaseCommit)
	}
}

func TestNextWorktreeSlot(t *testing.T) {
	d := newTestDB(t)
	tx := mustTx(t, d)
	r, _ := d.GetOrCreateRepository(tx, "/repo/slot", "main")
	tx.Commit()

	tx1, _ := d.BeginTx()
	s1, err := d.NextWorktreeSlot(tx1, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s1 != 1 {
		t.Fatalf("expected 1 got %d", s1)
	}
	d.InsertWorktree(tx1, r.ID, "/p/1", "b1", StatusFree)
	tx1.Commit()

	tx2, _ := d.BeginTx()
	s2, err := d.NextWorktreeSlot(tx2, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s2 != 2 {
		t.Fatalf("expected 2 got %d", s2)
	}
	tx2.Rollback()
}

func TestDeleteWorktree(t *testing.T) {
	d := newTestDB(t)
	tx := mustTx(t, d)
	r, _ := d.GetOrCreateRepository(tx, "/repo/7", "main")
	tx.Commit()

	tx2, _ := d.BeginTx()
	id, _ := d.InsertWorktree(tx2, r.ID, "/path/d", "b", StatusFree)
	tx2.Commit()

	tx3, _ := d.BeginTx()
	if err := d.DeleteWorktree(tx3, id); err != nil {
		t.Fatal(err)
	}
	tx3.Commit()

	got, _ := d.GetWorktree(id)
	if got != nil {
		t.Fatal("expected nil after delete")
	}
}

func TestListWorktreesByRepo(t *testing.T) {
	d := newTestDB(t)
	tx := mustTx(t, d)
	r, _ := d.GetOrCreateRepository(tx, "/repo/8", "main")
	r2, _ := d.GetOrCreateRepository(tx, "/repo/9", "main")
	tx.Commit()

	for i, rid := range []int64{r.ID, r.ID, r2.ID} {
		tx2, _ := d.BeginTx()
		d.InsertWorktree(tx2, rid, "/p/"+itoa(i), "b"+itoa(i), StatusFree)
		tx2.Commit()
	}

	wts, err := d.ListWorktreesByRepo(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 2 {
		t.Fatalf("expected 2 got %d", len(wts))
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
