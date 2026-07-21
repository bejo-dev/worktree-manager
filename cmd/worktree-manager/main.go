package main

import (
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/bejo-dev/worktree-manager/internal/db"
	"github.com/bejo-dev/worktree-manager/internal/manager"
)

const version = "2.0.0"

const usage = `worktree-manager - manage a reusable pool of git worktrees

Usage:
  worktree-manager acquire [branch-name] [repo-path]
  worktree-manager acquire -b <branch-name> -r <repo-path>
  worktree-manager release <worktree-path>
  worktree-manager list
  worktree-manager verify
  worktree-manager doctor

Commands:
  acquire   Acquire a ready-to-use worktree. Prints the absolute path to stdout.
  release   Release a worktree back to the pool.
  list      List all managed worktrees.
  verify    Verify consistency of registered worktrees with git state.
  doctor    Repair legacy branch and ownership records.

Acquire options:
  -b, --branch <name>    Branch name (e.g. BenE/add-unit-menu).
  -r, --repo <repo-path> Repository path. Defaults to the current directory.

Global options:
  -d, --database <path>  SQLite database path (default: ~/.worktree-manager/state.db).

  branch name and repo-path may also be passed positionally (in that order). It is
  an error to specify the same value via both a flag and a positional argument.
`

func main() {
	var showVersion bool
	databasePath := db.DefaultDBPath()
	flag.BoolVar(&showVersion, "v", false, "show version")
	flag.BoolVar(&showVersion, "version", false, "show version")
	flag.StringVar(&databasePath, "d", databasePath, "SQLite database path")
	flag.StringVar(&databasePath, "database", databasePath, "SQLite database path")
	flag.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	flag.Parse()
	if showVersion {
		fmt.Println(version)
		return
	}

	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	cmd := args[0]
	rest := args[1:]

	switch cmd {
	case "acquire":
		repoPath, branchName, err := parseAcquireArgs(rest)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			fmt.Fprintln(os.Stderr, "usage: worktree-manager acquire [branch-name] [repo-path]")
			os.Exit(2)
		}
		database, err := openDatabase(databasePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: open db: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()

		m := manager.New(database, os.Stderr)
		res, err := m.Acquire(repoPath, branchName)
		if err != nil {
			printCommandError(err, databasePath)
			os.Exit(1)
		}
		// Print only the absolute path to stdout.
		fmt.Println(res.WorktreePath)

	case "release":
		if len(rest) != 1 {
			fmt.Fprintln(os.Stderr, "usage: worktree-manager release <worktree-path>")
			os.Exit(2)
		}
		database, err := openDatabase(databasePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: open db: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()

		m := manager.New(database, os.Stderr)
		if err := m.Release(rest[0]); err != nil {
			printCommandError(err, databasePath)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "released")

	case "list":
		database, err := openDatabase(databasePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: open db: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()

		m := manager.New(database, os.Stderr)
		items, err := m.List()
		if err != nil {
			printCommandError(err, databasePath)
			os.Exit(1)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "STATUS\tBRANCH\tREPO\tPATH")
		for _, it := range items {
			branchName := it.BranchName
			if branchName == "" {
				branchName = "-"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", it.Status, branchName, it.Repository, it.Path)
		}
		w.Flush()

	case "verify":
		database, err := openDatabase(databasePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: open db: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()

		m := manager.New(database, os.Stderr)
		results, err := m.Verify()
		if err != nil {
			printCommandError(err, databasePath)
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

	case "doctor":
		if len(rest) != 0 {
			fmt.Fprintln(os.Stderr, "usage: worktree-manager doctor")
			os.Exit(2)
		}
		database, err := openDatabase(databasePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: open db: %v\n", err)
			os.Exit(1)
		}
		defer database.Close()

		m := manager.New(database, os.Stderr)
		report, err := m.Doctor()
		if err != nil {
			printCommandError(err, databasePath)
			os.Exit(1)
		}
		for _, issue := range report.Issues {
			fmt.Fprintf(os.Stderr, "issue: %s\n", issue)
		}
		fmt.Fprintf(os.Stderr, "doctor: checked %d, repaired %d, issues %d\n", report.Checked, report.Repaired, len(report.Issues))
		if len(report.Issues) > 0 {
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

func printCommandError(err error, databasePath string) {
	err = databaseError(err, databasePath)
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
}

func openDatabase(path string) (*db.DB, error) {
	database, err := db.Open(path)
	if err != nil {
		return nil, databaseError(err, path)
	}
	return database, nil
}

func databaseError(err error, path string) error {
	if !db.IsReadonlyError(err) {
		return err
	}
	return fmt.Errorf("%w\n\nadvice: SQLite cannot write to %s in this environment. Retry with a database inside the repository worktree-manager folder, for example:\n  worktree-manager --database <repo-root>/.worktree-manager/state.db acquire ...\nUse the same --database path for subsequent commands, and add .worktree-manager/ to the repository's .gitignore", err, path)
}

// parseAcquireArgs parses the arguments for the acquire command. It supports
// both flags (-b/--branch, -r/--repo) and positional arguments, interpreted as
// branch name first, then repo-path. It is an error to specify the same value via
// both a flag and a positional argument.
func parseAcquireArgs(rest []string) (repoPath, branchName string, err error) {
	fs := flag.NewFlagSet("acquire", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: worktree-manager acquire [branch-name] [repo-path]")
		fmt.Fprintln(os.Stderr, "  -b, --branch <name>    branch name (e.g. BenE/add-unit-menu)")
		fmt.Fprintln(os.Stderr, "  -r, --repo <repo-path> repository path (default: current directory)")
	}
	var branchFlag, repoFlag string
	var branchSet, repoSet bool
	fs.Var(&stringValue{v: &branchFlag, set: &branchSet}, "b", "branch name")
	fs.Var(&stringValue{v: &branchFlag, set: &branchSet}, "branch", "branch name")
	fs.Var(&stringValue{v: &repoFlag, set: &repoSet}, "r", "repository path")
	fs.Var(&stringValue{v: &repoFlag, set: &repoSet}, "repo", "repository path")
	if err := fs.Parse(rest); err != nil {
		return "", "", err
	}
	args := fs.Args()
	if len(args) > 2 {
		return "", "", fmt.Errorf("too many positional arguments: got %d, want at most 2", len(args))
	}

	branchName = ""
	repoPath = "."

	// Positional interpretation: args[0] = branch name, args[1] = repo-path.
	if len(args) >= 1 {
		branchName = args[0]
	}
	if len(args) >= 2 {
		repoPath = args[1]
	}

	// Flag overrides, but conflict if the same slot is also provided positionally.
	if branchSet {
		if len(args) >= 1 {
			return "", "", fmt.Errorf("branch name specified via both -b flag and positional argument")
		}
		branchName = branchFlag
	}
	if repoSet {
		if len(args) >= 2 {
			return "", "", fmt.Errorf("repo-path specified via both -r flag and positional argument")
		}
		repoPath = repoFlag
	}

	return repoPath, branchName, nil
}

// stringValue is a flag.Value that records whether it was set. This lets us
// distinguish "flag not provided" from "flag provided with empty string".
type stringValue struct {
	v   *string
	set *bool
}

func (s *stringValue) String() string {
	if s.v == nil {
		return ""
	}
	return *s.v
}

func (s *stringValue) Set(val string) error {
	*s.v = val
	*s.set = true
	return nil
}
