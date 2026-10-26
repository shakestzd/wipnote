# Third-Party Notices

This project incorporates material from third-party sources. Each entry below
identifies the source, license, and the scope of the included material.

## OpenSpec — Fission AI

- Source: https://github.com/Fission-AI/OpenSpec
- License: MIT
- Scope: Spec template structure (RFC 2119 Requirement + Given/When/Then
  Scenario format) used by `wipnote spec generate`. The template fragment is
  copied verbatim with attribution into `plugin/templates/spec-template.md`.

## GitHub CLI — GitHub Inc.

- Source: https://github.com/cli/cli (`pkg/cmd/pr/checks/aggregate.go`, trunk
  at commit 4f2cfe92e3499f8581bf39db77e0a1431fa7b55c)
- License: MIT
- Copyright: Copyright (c) 2019 GitHub Inc.
- Scope: The check state-to-bucket mapping and the duplicate-run elimination
  (latest run per name, workflow and event) used by `wipnote ci status`. The
  logic is adapted with attribution into `internal/gitwatch/aggregate.go`;
  STARTUP_FAILURE is mapped to fail rather than pending.

## Git Town — the Git Town creators

- Source: https://github.com/git-town/git-town (`internal/git/commands.go` and
  `internal/git/gitdomain/sync_status.go`, main at commit
  d0cc7951b01e74ba36ba057c0f31fd5eee6e83b8)
- License: MIT
- Copyright: Copyright (c) 2014-2024 the Git Town creators
- Scope: The NUL-separated `git for-each-ref` format (branch, sha, HEAD,
  worktree path, upstream, `upstream:track,nobracket`), `determineSyncStatus`
  and the sync-status values used by `wipnote ci status` branch health. The
  logic is adapted with attribution into `internal/gitwatch/branch.go`; an
  unknown track maps to "unknown" rather than a panic.

## lazygit — Jesse Duffield

- Source: https://github.com/jesseduffield/lazygit
  (`pkg/commands/git_commands/ahead_behind.go`, master at commit
  4e1eebe278790f257a875da6633234391a5a85b3)
- License: MIT
- Copyright: Copyright (c) 2018 Jesse Duffield
- Scope: Parsing of the `%(ahead-behind:<base>)` for-each-ref field (git 2.41
  or newer) used for ahead/behind counts. The logic is adapted with
  attribution into `internal/gitwatch/branch.go`, for a single base.

## gh-poi — Seito Tanaka

- Source: https://github.com/seachicken/gh-poi (`conn/command.go` and
  `cmd/root.go`, main at commit abf237be0d79d754922f3cf72a905f9854d2b096)
- License: MIT
- Copyright: Copyright (c) 2021 Seito Tanaka
- Scope: `parseWorktrees` (`git worktree list --porcelain`) and the
  `isFullyMerged` rule, including that a branch with no commits of its own is
  not merged, used for stale worktree reporting. Adapted with attribution into
  `internal/gitwatch/worktrees.go`; merged and squash-merged are decided from
  local git rather than a PR's commit list.
