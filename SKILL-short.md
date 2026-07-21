# worktree-manager (short)

Before starting work, run `worktree-manager acquire [task-id]` and work only inside the returned path; when done, run `worktree-manager release <worktree-path>`. If no task ID is supplied, a random three-word name is used for the task, branch, and worktree folder.
