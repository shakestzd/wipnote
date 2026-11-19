---
paths:
  - "cmd/wipnote/workitem_commit.go"
  - "scripts/worktree-*.sh"
  - "cmd/wipnote/yolo*.go"
---

# Worktrees and .wipnote/ Artifact Commits

Conductor-managed worktrees, and any worktree that `wipnote yolo` creates, install a per-worktree gitignore entry. It hides `.wipnote/` from the worktree's own `git status`. That is intentional noise reduction.

The artifact is still committed. `wipnote feature complete`, and the `bug` and `spike` forms, call `commitWipnoteArtifact` (`cmd/wipnote/workitem_commit.go`). It runs `git -C <repoRoot>` with an explicit absolute path. That stages and commits the HTML in the main repository and bypasses the exclusion.

**Conductor limit.** Conductor archives worktrees without running `wipnote * complete`. Run `wipnote feature complete <id>` (or `bug`, `spike`) inside the workspace before Conductor tears it down. wipnote cannot intercept the archive step.
