package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/bejo-dev/worktree-manager/internal/db"
	"github.com/bejo-dev/worktree-manager/internal/manager"
)

const usage = `worktree-manager - manage a reusable pool of git worktrees

Usage:
  worktree-manager acquire <repo-path> [task-id]
  worktree-manager release <worktree-path>
  worktree-manager list
  worktree-manager verify

Commands:
  acquire   Acquire a ready-to-use worktree. Prints the absolute path to stdout.
  release   Release a worktree back to the pool.
  list      List all managed worktrees.
  verify    Verify consistency of registered worktrees with git state.
`

func main() {
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "acquire":
		if len(rest) < 1 || len(rest) > 2 {
			fmt.Fprintln(os.Stderr, "usage: worktree-manager acquire <repo-path> [task-id]")
			os.Exit(2)
		}
		repoPath := rest[0]
		taskID := ""
		if len(rest) == 2 {
			taskID = rest[1]
		}
		database, err := db.Open(db.DefaultDBPath())
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: open db: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()

		m := manager.New(database, os.Stderr)
		res, err := m.Acquire(repoPath, taskID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		// Print only the absolute path to stdout.
		fmt.Println(res.WorktreePath)

	case "release":
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, "usage: worktree-manager release <worktree-path>")
			os.Exit(2)
		}
		database, err := db.Open(db.DefaultDBPath())
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: open db: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()

		m := manager.New(database, os.Stderr)
		if err := m.Release(rest[0]); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "released")

	case "list":
		database, err := db.Open(db.DefaultDBPath())
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: open db: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()

		m := manager.New(database, os.Stderr)
		items, err := m.List()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "STATUS\tBRANCH\tTASK\tREPO\tPATH")
		for _, it := range items {
			taskID := it.TaskID
			if taskID == "" {
				taskID = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", it.Status, it.BranchName, taskID, it.Repository, it.Path)
		}
		w.Flush()

	case "verify":
		database, err := db.Open(db.DefaultDBPath())
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: open db: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()

		m := manager.New(database, os.Stderr)
		results, err := m.Verify()
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		ok := true
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "STATUS\tEXISTS\tISSUES\tBRANCH\tPATH")
		for _, r := range results {
			exists := "yes"
			if !r.Exists {
				exists = "no"
			}
			issues := "-"
			if len(r.Issues) > 0 {
				issues = fmt.Sprintf("%d", len(r.Issues))
				ok = false
				for _, iss := range r.Issues {
					fmt.Fprintf(os.Stderr, "issue: %s: %s\n", r.Path, iss)
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", r.Status, exists, issues, r.BranchName, r.Path)
		}
		w.Flush()
		if !ok {
			os.Exit(1)
		}

	case "-h", "--help", "help":
		fmt.Fprint(os.Stderr, usage)

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}
