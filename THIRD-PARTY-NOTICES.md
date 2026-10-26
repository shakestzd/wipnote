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
