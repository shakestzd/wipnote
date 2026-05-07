# Changelog

All notable changes to wipnote will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `wipnote upgrade` / `wipnote update` — self-update CLI from GitHub releases with atomic binary replacement and self-test (feat-d16a24ac)

### Documented
- Standalone `install.sh` one-liner is now documented in README and docs/index.md. The installer itself already existed but was not advertised.

### Fixed
- `bootstrap.sh` standalone install — now resolves version via `$WIPNOTE_VERSION` env var or GitHub API when no `plugin.json` is present (bug-b08a2ec9)

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
