# worktree-manager (short)

Before starting work, run `worktree-manager acquire [branch-name]` and work only inside the returned path; when done, run `worktree-manager release <worktree-path>`. If no branch name is supplied, a random three-word name is used for the branch and internal ownership label. Branch names may include `/`.
