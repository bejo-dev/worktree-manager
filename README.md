# worktree-manager

A standalone CLI binary that manages a reusable pool of git worktrees for
autonomous coding agents.

The agent workflow:

1. Before starting work:
   ```
   worktree-manager acquire [branch-name] [repo-path]
   ```
   Prints the absolute path of a ready-to-use worktree to stdout. If
   `repo-path` is omitted, the current working directory is used. The
   `branch-name` is optional (for example `BenE/add-unit-menu`) and is used as
   the branch name recorded against the worktree.
2. The agent works only inside that returned directory.
3. After the task is complete:
   ```
   worktree-manager release <worktree-path>
   ```

The tool owns all worktree lifecycle logic. State is kept in a local SQLite
database at `~/.worktree-manager/state.db`, so the agent never has to track
worktree state itself. When no branch name is supplied, a short random
three-word name is generated for the branch and internal ownership label.

## Install

### From source (requires Go 1.22+)

```sh
git clone https://github.com/bejo-dev/worktree-manager.git
cd worktree-manager
go install ./cmd/worktree-manager
```

The binary is installed to `$GOBIN` (or `$GOPATH/bin`). Make sure that
directory is on your `PATH`:

```sh
export PATH="$PATH:$(go env GOPATH)/bin"
```

### Build a standalone binary

```sh
git clone https://github.com/bejo-dev/worktree-manager.git
cd worktree-manager
go build -o worktree-manager ./cmd/worktree-manager
```

Then move `worktree-manager` anywhere on your `PATH`:

```sh
mv worktree-manager /usr/local/bin/
```

The binary is fully static (it uses the pure-Go `modernc.org/sqlite` driver, so
no CGO or system SQLite is required).

### Requirements

- `git` must be installed and on `PATH`.
- Go 1.22 or newer (only for building from source).

## Commands

### `-v, --version`

Prints the worktree-manager version (`2.0.0`).

### `-d, --database <path>`

Selects the SQLite state database. If the default location is read-only, the
command prints advice to use a writable database in the repository's
`.worktree-manager` folder, for example:

```sh
worktree-manager --database /path/to/repo/.worktree-manager/state.db acquire BenE/add-unit-menu
```

Use the same path for subsequent commands and add `.worktree-manager/` to the
repository's `.gitignore`.

### `acquire [branch-name] [repo-path]`

Returns a ready-to-use worktree for the given repository. If `repo-path` is
omitted, the current working directory is used. Output (stdout) is only the
absolute worktree path, so it can be captured by scripts:

```sh
WT=$(worktree-manager acquire BenE/add-unit-menu)
```

Arguments are positional (branch name first, then repo-path) and may also be passed
as flags so they can appear in any order:

| Flag            | Positional slot | Meaning                                  |
| --------------- | --------------- | ---------------------------------------- |
| `-b, --branch`  | `args[0]`       | branch name (e.g. `BenE/add-unit-menu`)  |
| `-r, --repo`    | `args[1]`       | repository path (default: current dir)  |

It is an error to specify the same value via both a flag and a positional
argument.

Examples:

```sh
# cwd repo, with a branch name (the common case)
worktree-manager acquire BenE/add-unit-menu

# positional: branch + explicit repo
worktree-manager acquire BenE/fix-double-layering /path/to/repo

# flags, any order
worktree-manager acquire -b BenE/improve-menu-order -r /path/to/repo
worktree-manager acquire -r /path/to/repo -b BenE/improve-menu-order

# explicit repo, no task
worktree-manager acquire -r /path/to/repo

# cwd repo, no task
worktree-manager acquire
```

The branch name is recorded against the worktree so `list` and `verify` can
show which branch holds each one. If omitted, the generated three-word name is
used as both the branch name and internal ownership label. Branch names may
include `/`, such as `BenE/add-unit-menu`.

Behavior:

1. Resolve `repo-path` to the git repository root.
2. Detect the default branch (`main`, `master`, ...).
3. Find a `FREE` worktree for that repository, preferring the
   least-recently-used one.
4. If none exists, create a new git worktree in the next reusable pool folder
   and check out the requested branch. With no branch name, a generated
   three-word name such as `soaring-quiet-fox` is used.
5. Before returning:
   - `git fetch origin`
   - reset the worktree to the latest default branch (`origin/<default>`)
   - remove untracked files (`git clean -xfd`)
6. Mark the worktree `ALLOCATED` with the branch name.
7. Print the worktree absolute path to stdout.

If a git operation fails, the worktree is marked `BROKEN` and the command
exits non-zero.

### `release <worktree-path>`

Resets a worktree and returns it to the pool.

Behavior:

1. Validate the path is a Git worktree under the repository's manager pool. If
   the selected database is missing its record, adopt the Git worktree into
   that database before continuing.
2. `git fetch origin`.
3. `git reset --hard origin/<default_branch>`.
4. Reset and clean initialized submodules, then run `git clean -xfd` in the
   worktree. This removes manager state accidentally created inside a
   submodule.
5. Clear branch ownership.
6. Mark `FREE`.

Before acquisition and listing, Git worktrees under the manager pool are
reconciled with the selected database. This prevents a worktree created with
one repository-local database from being invisible to another database and
prevents allocating a branch that Git already has checked out.

### `list`

Lists all managed worktrees across all repositories:

```
STATUS     BRANCH              REPO           PATH
ALLOCATED  BenE/add-unit-menu  /path/to/repo  /path/to/repo/.worktree-manager/wm/pool-1-1
FREE       -                    /path/to/repo  /path/to/repo/.worktree-manager/wm/pool-1-2
```

### `verify`

Verifies that every registered worktree is consistent with the actual git
state (worktree exists on disk, registered in `git worktree list`, status is
sane). Issues are printed to stderr; the command exits non-zero if any
issues are found.

### `doctor`

Repairs state created by older versions after the branch-name change. It
reconciles recorded branches with Git, migrates legacy `wm/...` branches to
their recorded branch names, and generates names for older taskless allocated
worktrees. It reports any worktree it could not repair and exits non-zero.

## State

All state lives in a SQLite database at:

```
~/.worktree-manager/state.db
```

Schema:

```sql
repositories (
  id              INTEGER PRIMARY KEY,
  root_path       TEXT UNIQUE NOT NULL,
  default_branch  TEXT NOT NULL
);

worktrees (
  id               INTEGER PRIMARY KEY,
  repository_id    INTEGER NOT NULL REFERENCES repositories(id),
  path             TEXT UNIQUE NOT NULL,
  branch_name      TEXT NOT NULL,
  status           TEXT NOT NULL DEFAULT 'FREE',  -- FREE | ALLOCATED | BROKEN
  task_id          TEXT,
  last_used        TIMESTAMP,
  last_base_commit TEXT
);
```

All worktrees are created under
`<repo>/.worktree-manager/wm/pool-<repo>-<slot>`. The checked-out branch is
named exactly after the requested branch name, or after the generated name
when omitted. When a free worktree is reused, its branch is renamed to the new
branch name.

## Guarantees

- Never manually edits `.git` internals - all operations go through the `git`
  CLI.
- Never deletes the default branch.
- Never allocates the same worktree twice (state changes happen inside SQLite
  transactions).
- If a git operation fails, the affected worktree is marked `BROKEN` and is
  not handed out again until repaired.
- Logs go to stderr; stdout contains only machine-readable output.

## Development

```sh
go test ./...          # run all unit + integration tests
go vet ./...           # lint
go build ./cmd/worktree-manager
```

Integration tests create temporary git repositories, so they require `git` on
the `PATH`.
