# worktree-manager

A standalone CLI binary that manages a reusable pool of git worktrees for
autonomous coding agents.

The agent workflow:

1. Before starting work:
   ```
   worktree-manager acquire <repo-path> [task-id]
   ```
   Prints the absolute path of a ready-to-use worktree to stdout.
2. The agent works only inside that returned directory.
3. After the task is complete:
   ```
   worktree-manager release <worktree-path>
   ```

The tool owns all worktree lifecycle logic. State is kept in a local SQLite
database at `~/.worktree-manager/state.db`, so the agent never has to track
worktree state itself. All operations are deterministic.

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

### `acquire <repo-path> [task-id]`

Returns a ready-to-use worktree for the given repository. Output (stdout) is
only the absolute worktree path, so it can be captured by scripts:

```sh
WT=$(worktree-manager acquire /path/to/repo my-task-123)
```

Behavior:

1. Resolve `repo-path` to the git repository root.
2. Detect the default branch (`main`, `master`, ...).
3. Find a `FREE` worktree for that repository, preferring the
   least-recently-used one.
4. If none exists, create a new git worktree with a deterministic reusable
   branch and register it.
5. Before returning:
   - `git fetch origin`
   - reset the worktree to the latest default branch (`origin/<default>`)
   - remove untracked files (`git clean -xfd`)
6. Mark the worktree `ALLOCATED` (with the optional task id).
7. Print the worktree absolute path to stdout.

If a git operation fails, the worktree is marked `BROKEN` and the command
exits non-zero.

### `release <worktree-path>`

Resets a worktree and returns it to the pool.

Behavior:

1. Validate the path belongs to the manager.
2. `git fetch origin`.
3. `git reset --hard origin/<default_branch>`.
4. `git clean -xfd`.
5. Clear task ownership.
6. Mark `FREE`.

### `list`

Lists all managed worktrees across all repositories:

```
STATUS     BRANCH       TASK    REPO                      PATH
ALLOCATED  wm/pool-1-1  task-1  /path/to/repo             /path/to/repo/.worktree-manager/wm/pool-1-1
FREE       wm/pool-1-2  -       /path/to/repo             /path/to/repo/.worktree-manager/wm/pool-1-2
```

### `verify`

Verifies that every registered worktree is consistent with the actual git
state (worktree exists on disk, registered in `git worktree list`, status is
sane). Issues are printed to stderr; the command exits non-zero if any
issues are found.

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

Worktrees are created under `<repo>/.worktree-manager/wm/pool-<repo>-<slot>`.
Each worktree owns a stable branch (`wm/pool-<repo>-<slot>`) that is reused
across acquire/release cycles, so the same worktree keeps the same branch over
time.

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
