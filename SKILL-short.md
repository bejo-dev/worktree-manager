# worktree-manager (short)

Before starting work, acquire a worktree from inside the target repository:

```sh
WT=$(worktree-manager acquire [branch-name])
```

Work only in `$WT`, never in the main checkout. `acquire` fetches the latest
default branch, cleans the worktree, and prints only its absolute path to
stdout. Omit the branch name to generate a random three-word name.

After committing and pushing, return it to the pool:

```sh
worktree-manager release "$WT"
```

Release resets and cleans the worktree, deletes its task branch, and discards
any uncommitted work. Run `worktree-manager --help` to see available commands
and options.
