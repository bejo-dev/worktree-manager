# worktree-manager (short)

Before starting work, acquire a worktree from inside the target repository:

```sh
WT=$(worktree-manager acquire [branch-name])
```
This will return the absolute path to fresh worktree where you will do the work for this feature.

After committing and pushing, return it to the pool with:

```sh
worktree-manager release "$WT"
```
