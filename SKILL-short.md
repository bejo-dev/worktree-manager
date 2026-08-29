# worktree-manager (short)

Run worktree-manager commands with elevated filesystem permission from the
initial invocation. Git updates shared metadata under the primary repository,
and staging or committing from a linked worktree needs the same permission. In
a Codex sandbox, request `require_escalated` before running the command.

Before starting work, acquire a worktree from inside the target repository:

```sh
WT=$(worktree-manager acquire [branch-name])
```
This will return the absolute path to fresh worktree where you will do the work for this feature.

After committing and pushing, return it to the pool with:

```sh
worktree-manager release "$WT"
```
