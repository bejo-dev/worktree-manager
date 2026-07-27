# worktree-manager Skill

Manage a reusable pool of git worktrees for autonomous coding agents.

## Before starting work

Run this from anywhere inside the target repository:

```
worktree-manager acquire [branch-name]
```

- `<branch-name>` is optional (e.g. `BenE/add-unit-menu`).
- If omitted, the tool generates a random three-word name and uses it for the
  branch and internal ownership label.
- The current working directory is used as the repository path.
- Worktrees are created below `/private/tmp` by default. To use another base
  directory, invoke the command as `worktree-manager --base-dir /path/to/worktrees acquire ...`.
- The command prints **only** the absolute path of the ready-to-use worktree
  to stdout. Capture it:

  ```sh
  WT=$(worktree-manager acquire BenE/add-unit-menu)
  ```

- All other output goes to stderr; nothing else is on stdout.

Do **all** of your work inside the returned worktree path. Do not touch the
main checkout. The tool has already:
- fetched `origin`,
- reset the worktree to the latest default branch,
- removed untracked files,
- marked it `ALLOCATED` to your branch name.

### Optional: explicit repo path

If you are not running from inside the repo, pass `-r`:

```
worktree-manager acquire -b BenE/add-unit-menu -r /path/to/repo
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
- detach at the refreshed default-branch commit,
- delete the released task branch and clear its ownership,
- verify that `HEAD` matches the fetched default branch and the worktree is clean,
- mark it `FREE`.

After a successful release, the pool worktree is a clean detached snapshot of
the default branch from the successful fetch. The primary checkout is not
modified.

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
- `worktree-manager doctor` - repair state created by older versions.

If SQLite reports a read-only database, pass
`--database /path/to/repo/.worktree-manager/state.db` and add
`.worktree-manager/` to that repository's `.gitignore`. Use the same database
path for subsequent commands.

## State

By default, all state lives in `/private/tmp/worktree-manager/state.db`. You do not need to track
worktree state yourself - the tool owns it.
