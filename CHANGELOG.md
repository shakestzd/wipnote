# Changelog

All notable changes to wipnote will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- Aligned `README.md`, `AGENTS.md`, `CONTRIBUTING.md`, `docs/index.md`, `docs/INSTALL.md`, `docs/reference/cli.md`, `.devcontainer/README.md`, `.github/BRANCH_PROTECTION.md`, `mkdocs.yml`, and `.claude/` documentation with the current in-memory SQLite projection architecture, bundled `--plugin-dir` + `mods/` launchers, and multi-module Go layout.
- Aligned `install.sh` and `scripts/install.sh` (consistent `WIPNOTE_*` env var support, `wn` symlink, and `0.95.0` examples) and removed stale `.wipnote/wipnote.db` mentions from `wipnote agent-init` and the first-launch OTel notice.
- Removed stale pre-built `site/` output, legacy Python `examples/`, and dead Python-era / pre-rename leftovers (`.claude-plugin/plugin.json`, `.claude/projects/` htmlgraph memory file, `.claude/config/`, `.codex`, `.pre-commit-config.yaml`, `pytest.ini`, `docker-compose.yml`, `wipnote-aliases.sh`, `wipnote-deploy.toml`, `PRD.md`, `_build/esbuild-darwin-arm64`, `memory/MEMORY.md`, `benchmarks/`, `prototypes/`, `src/js/wipnote.js`, `templates/`, `tests/`, `ui-review/`, `.gotmp-exec/`).

## [0.95.0] - 2026-11-19

### Changed
- Reply block visuals (steps, options, callouts) moved to the optional `lulawrite` plugin; `wipnote-visuals` and `wipnote-orchestra` draw only `wipnote-ref` work-item cards, the turn receipt line, and the writing check (`feat-fd3637f9`).

### Fixed
- Quality gate skips nested Go modules that contain no Go source files (`bug-7d02822c`).

## [0.94.0] - 2026-11-18

### Added
- `wipnote plan next` and `wipnote execute-preview` CLI commands (`feat-bcc2297f`, `feat-d06e3fb2`).
- `wipnote prose check` and `wipnote prose rules` plain-writing checks (`feat-4151d2f0`).
- `wipnote check --gate` and `wipnote review` surface removed tests, added skips, and config edits, and run the Go gate across every module (`feat-a1daeef8`, `bug-9f3b580f`).
- Prompt guidance size guardrails (`port/pluginbuild/guidance_size_test.go`) and `SubagentStart` worker-rules delivery (`feat-9fd0cb31`, `feat-0b0112a4`, `feat-0d9b57bc`).

### Changed
- Removed shipped plugin skills and slash commands (`plugin/skills/`, `plugin/commands/`); workflows now live in CLI commands and hook-delivered rules (`feat-9796e97f`, `feat-0be892a9`, `feat-8f910039`).
- Slimmed `CLAUDE.md` to pointers backed by path-scoped `.claude/rules/` and `docs/guides/` (`feat-6f99bc47`).

## [0.93.0] - 2026-11-12

### Added
- `wipnote worktree list` and `wipnote worktree clean` (`feat-742f6838`).
- Default `--autocompact 300k` flag on Claude Code launches, configurable via `launch.claude.autoCompactWindow` (`feat-3fcabee6`).

## [0.92.0] - 2026-11-08

### Added
- `PreCompact` and `PostCompact` hooks plus `SessionStart` compact resume brief to preserve active work-item state across compaction (`feat-9bc8e611`).
- Worker session naming (`<parent> › <worker>`) and automatic naming for item-less sessions (`feat-52d2792b`, `feat-00e8112d`).

## [0.91.4] - 2026-11-04

### Added
- `wipnote delegate run --progress` streaming progress for one-shot runs.

### Fixed
- OTel collector startup/watchdog logging, shared collector port handling, and serialized `wipnote run` tee output (`0.91.1`–`0.91.3`).

## [0.85.0] - 2026-10-29

### Added
- `wipnote delegate` is the one way to hand work to another harness. Every run is a row in `.wipnote/delegations-ledger.html` (feat-89cfa21c):
  - `run`: one codex exec, codex review, claude -p or ccx task call, with its answer and a run page. `--write` gives `codex exec` a writable sandbox. `--container` runs the tool in the project's devcontainer.
  - `spawn`: an interactive Claude or Codex worker in a herdr pane. NAME is optional (it comes from the work item). `--container` starts it in the devcontainer. `--resume OLD` continues a stopped worker and needs a NAME.
  - `send`, `wait` (needs `--timeout`; a timeout exits 124), `result`, `questions`, `inbox` and `stop` for workers.
  - `list`, `tree` and `view` to see runs. The wipnote-orchestra band shows them too, inside and outside herdr.
  - `doctor` (and `doctor --container`) to check the host and the devcontainer.
  - `import-orch` to copy orch's run history into the ledger.
- Inside herdr, `wipnote delegate run -- claude ...` runs as an interactive worker you can watch and answer, with the same report as `claude -p`. Outside herdr, or with `--no-pane`, it stays `claude -p`. wipnote pre-seeds workspace trust, enforces `--max-turns` and `--max-budget-usd` itself, and names start failures such as `session_start_timeout` and `plugin_not_loaded` (feat-21ae0920).
- Workers are named after their work item, co-claim it, and can resume a stopped session with its context. Container workers keep their session, gate and claim records in their own run folder, and the host folds them into the shared ledgers.
- Lineage shows delegations: a worker's session sits under the session that started it.
- `wipnote doctor --project-thread` checks a Claude project thread for the binary, plugin, hooks and rules, and names the fix for each missing part. `scripts/cloud-env-setup.sh` installs a pinned, checksum-verified wipnote binary in a Claude cloud environment. `docs/guides/claude-projects.md` explains the setup (feat-e3f36f91).
- The `delegate` skill now covers every verb and the claim rules: the worker co-claims its item, there is one live worker per item, and you stop the worker before you complete the item. It ships to Claude Code and Codex, not to Antigravity.
- A bare `codex exec`, `codex review`, `claude -p` or `ccx task` in the Bash tool is denied with the `wipnote delegate run` form to use. A plain `ccx task` is offered the `--container` form only when the project has a devcontainer.
- A one-time notice at `wipnote claude` launch tells a user who set options for the removed mod which `delegate` settings in `~/.wipnote/config.json` they map to. wipnote does not move them.
- `wipnote upgrade` / `wipnote update` — self-update CLI from GitHub releases with atomic binary replacement and self-test (feat-d16a24ac).

### Fixed
- `wipnote delegate wait` no longer exits 0 when the worker is gone or blocked: `gone` exits 3 and `blocked` exits 4. `done` and the other settled outcomes still exit 0, a timeout still exits 124, and the JSON is unchanged (bug-da9bde6f).
- `bootstrap.sh` standalone install resolves version via `$WIPNOTE_VERSION` or GitHub API when no `plugin.json` is present (bug-b08a2ec9).

### Removed
- The `codex-herdr-pane` mod. `wipnote delegate run` does its job, `wipnote delegate view` replaces its viewer, and the wipnote-orchestra band replaces its Claude Code pane.

### Known limits
- Chained delegations skip the ledger until v2. A delegation in a pipe, after `;` or `&&`, fed on stdin or behind `dev exec` runs as typed and gets no ledger row.

## [0.13.9] - 2025-12-30

### Added
- **Orchestrator Mode Enforcement System** - Complete implementation of orchestrator pattern enforcement
  - PreToolUse hook that intercepts tool calls before execution
  - Configurable thresholds for Bash, Read, Edit, Grep, Glob operations
  - Two enforcement modes: strict (blocks) and guidance (warns only)
  - Operation classification system (allowed/warned/blocked)
  - Auto-generated delegation suggestions with examples
  - CLI commands: `orchestrator enable/disable/status`
  - Configuration via `.wipnote/orchestrator.json`
- **Comprehensive Documentation**
  - Complete orchestrator mode section in AGENTS.md
  - Standalone ORCHESTRATOR_MODE_GUIDE.md with examples
  - Updated orchestrator skill with activation notes
  - Operation reference matrix with rationales
  - Troubleshooting guide and FAQ
- **Orchestrator Tests** - Complete test suite for enforcement system
  - Threshold enforcement tests (strict and guidance modes)
  - CLI command tests
  - Configuration management tests
  - Operation classification tests
  - Delegation suggestion tests

### Changed
- Orchestrator skill now includes enforcement behavior documentation
- Session tracking improvements for orchestrator mode

### Technical Details
- PreToolUse hook: `.claude/hooks/pre-tool-use/orchestrator_enforcement.sh`
- Enforcement logic: `src/python/wipnote/orchestrator.py`
- CLI integration: `src/python/wipnote/cli.py` (orchestrator subcommand)
- Tests: `tests/unit/test_orchestrator.py`

## [0.13.8] - 2025-12-29

### Added
- Session tracking and drift detection system
- Enhanced PreToolUse hook with pattern detection
- Active learning from tool usage patterns

### Changed
- Improved session management
- Better drift detection and warnings

## [0.13.5] - 2025-12-28

### Added
- Comprehensive code quality checks in deploy script
- GitHub installation instructions for Claude plugin

### Changed
- Updated deployment automation
- Improved marketplace.json version handling

## [0.13.0] - 2025-12-27

### Added
- TrackBuilder fluent API for complex track planning
- Multi-pattern glob support for file operations
- Strategic analytics module (recommend_next_work, find_bottlenecks)

### Changed
- Enhanced SDK with builder patterns
- Improved track management capabilities

## [0.12.0] - 2025-12-26

### Added
- Session tracking with automatic hooks
- Feature-session linking
- Session analytics and insights

### Changed
- Improved SDK ergonomics
- Better context management

## [0.11.0] - 2025-12-25

### Added
- Complete SDK for AI agents
- Feature collections with fluent builders
- Track planning and management
- Dependency analytics

### Changed
- Major API refactoring for agent-first design

## [0.10.0] - 2025-12-24

### Added
- Initial public release
- Basic HTML graph functionality
- Python SDK
- REST API server
- CLI interface

### Technical
- justhtml for HTML parsing
- Pydantic models for validation
- SQLite indexing (optional)

[Unreleased]: https://github.com/shakestzd/wipnote/compare/v0.13.9...HEAD
[0.13.9]: https://github.com/shakestzd/wipnote/compare/v0.13.8...v0.13.9
[0.13.8]: https://github.com/shakestzd/wipnote/compare/v0.13.5...v0.13.8
[0.13.5]: https://github.com/shakestzd/wipnote/compare/v0.13.0...v0.13.5
[0.13.0]: https://github.com/shakestzd/wipnote/compare/v0.12.0...v0.13.0
[0.12.0]: https://github.com/shakestzd/wipnote/compare/v0.11.0...v0.12.0
[0.11.0]: https://github.com/shakestzd/wipnote/compare/v0.10.0...v0.11.0
[0.10.0]: https://github.com/shakestzd/wipnote/releases/tag/v0.10.0
