# worktree-manager Skill

Manage a reusable pool of git worktrees for autonomous coding agents.

## Before starting work

Run this from anywhere inside the target repository:

```
worktree-manager acquire <task-id>
```

- `<task-id>` is an optional short, branch-name-like label for the work (e.g.
  `add-unit-menu`, `fix-double-layering`, `improve-menu-order`).
- If omitted, the tool generates a random three-word name and uses it for the
  task label, branch, and worktree folder.
- The current working directory is used as the repository path.
- The command prints **only** the absolute path of the ready-to-use worktree
  to stdout. Capture it:

  ```sh
  WT=$(worktree-manager acquire add-unit-menu)
  ```

- All other output goes to stderr; nothing else is on stdout.

Do **all** of your work inside the returned worktree path. Do not touch the
main checkout. The tool has already:
- fetched `origin`,
- reset the worktree to the latest default branch,
- removed untracked files,
- marked it `ALLOCATED` to your task id.

### Optional: explicit repo path

If you are not running from inside the repo, pass `-r`:

```
worktree-manager acquire -t add-unit-menu -r /path/to/repo
```

Flags and positionals can be mixed, but you may not give the same value twice
(via both a flag and a positional).

## After the task is complete

Release the worktree back to the pool:

```
worktree-manager release <worktree-path>
```

The tool will:
- fetch `origin`,
- reset the worktree to `origin/<default-branch>`,
- `git clean -xfd` (remove untracked files),
- clear task ownership,
- mark it `FREE`.

Run this once you have committed/pushed your work. Anything left uncommitted
in the worktree will be discarded on release.

## Rules

- Never manually edit `.git` internals.
- Never delete the default branch.
- Never allocate the same worktree twice (the tool enforces this).
- If a git operation fails, the worktree is marked `BROKEN` and will not be
  handed out again until repaired.
- Logs are on stderr; stdout is machine-readable.

## Other commands

- `worktree-manager list` - show all managed worktrees and their status.
- `worktree-manager verify` - check registered worktrees match git state.

## State

All state lives in `~/.worktree-manager/state.db`. You do not need to track
worktree state yourself - the tool owns it.
