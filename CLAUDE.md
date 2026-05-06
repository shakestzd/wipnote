@AGENTS.md

# wipnote — Claude Code

Local-first observability and coordination platform for AI-assisted development.

---

## Build

**Always use `wipnote build`, never `go build` directly.**

```bash
wipnote build      # outputs to plugin/hooks/bin/wipnote (on your PATH)
plugin/build.sh      # equivalent
```

Running `go build -o wipnote ./cmd/wipnote/` puts the binary in CWD, not on your PATH.

---

## Quality Gates

```bash
go build ./... && go vet ./... && go test ./...
# Commit only when ALL pass
```

Use `/wipnote:code-quality-skill` for the complete pre-commit workflow.

---

## Deploy

```bash
./scripts/deploy-all.sh X.Y.Z --no-confirm   # full pipeline
```

Or `/wipnote:deploy X.Y.Z`. CLI binary and plugin are independent installs — the deploy script updates both. Never update one without the other.

---

## Dev Mode

```bash
wipnote claude --dev   # loads plugin from source via --plugin-dir
```

Dev mode uninstalls the marketplace plugin, clears cache, and launches with `claude --plugin-dir plugin/`. This ensures agents, skills, tools, and hooks all load from your local source — not stale marketplace copies. The marketplace plugin is reinstalled on exit.

**Why full removal is required:** Disabling a marketplace plugin only affects hooks. Agent definitions and skill content continue loading from `~/.claude/plugins/marketplaces/`, silently shadowing dev source changes.

## Resuming a Specific Session

`wipnote claude`, `wipnote yolo`, and `wipnote dev` all accept `--resume <session-id>` to resume a specific prior Claude Code session. On exit, Claude Code prints a line like `claude --resume d846b50d-…`; pass that ID through the wipnote launcher to get the wipnote plugin, system prompt, and (in `--dev`) local `--plugin-dir` applied to the resumed session:

```bash
wipnote claude --resume d846b50d-9ce4-45c1-8ad2-0f84da537efd
wipnote claude --dev --resume <session-id>
wipnote yolo --dev --resume <session-id>
wipnote dev --resume <session-id>
```

`--resume <id>` differs from `--continue` (which resumes the most recent session). If both are passed, `--resume <id>` wins.

## Dev Mode in Codespaces

Codespaces clients disconnect on idle, browser refresh, or network blips — killing long dev sessions. Wrap dev in tmux:

    wipnote claude --dev --tmux

First run creates a tmux session named `wipnote-dev`. On disconnect, detach instead of dying. Re-run the same command to reattach to the surviving session. Manually detach with `Ctrl-b d`; kill the session with `tmux kill-session -t wipnote-dev`.

## Yolo Mode in Codespaces

Codespaces clients disconnect on idle, browser refresh, or network blips — killing long yolo runs. Wrap yolo in tmux:

    wipnote yolo --dev --tmux

First run creates a tmux session named `wipnote-yolo`. On disconnect, detach instead of dying. Re-run the same command to reattach to the surviving session. Manually detach with `Ctrl-b d`; kill the session with `tmux kill-session -t wipnote-yolo`.

---

## Plugin Source — Single Source of Truth

wipnote ships one plugin to multiple AI-tool targets (Claude Code, Codex CLI, Gemini CLI) from
a single source. Each target's plugin tree is **generated** — never hand-edit it.

**Layering (edit the left column only):**

| Layer | What lives there | Edit? |
|-------|------------------|-------|
| `packages/plugin-core/manifest.json` | Canonical plugin metadata + per-target output paths + hook event matrix (which events target Claude, Codex, Gemini, or any combination) | YES |
| `plugin/commands/`, `plugin/agents/`, `plugin/skills/`, `plugin/templates/`, `plugin/static/`, `plugin/config/` | Shared markdown/static assets — copied verbatim into every target tree | YES |
| `cmd/`, `internal/` | Go CLI + hook handlers (all hooks are thin wrappers over `wipnote hook <handler>`) | YES |
| `plugin/.claude-plugin/plugin.json`, `plugin/hooks/hooks.json` | Generated Claude Code tree | NO — regenerate |
| `packages/codex-plugin/` | Generated Codex CLI tree | NO — regenerate |
| `packages/gemini-extension/` | Generated Gemini CLI tree | NO — regenerate |
| `.claude/` (anything) | Auto-synced from `plugin/` — changes are lost | NO |

**Regenerate after every manifest or asset edit:**

```bash
wipnote plugin build-ports              # all targets
wipnote plugin build-ports --target codex
wipnote plugin build-ports --target claude
wipnote plugin build-ports --target gemini
```

See `packages/plugin-core/README.md` for the per-task recipes (new command, new hook,
new target) and `.claude/rules/plugin-development.md` for full plugin structure reference.

---

## Orchestration

Delegate ALL operations except `Task()`, `AskUserQuestion()`, `TodoWrite()`, SDK operations.

Use `/wipnote:orchestrator-directives-skill` for delegation patterns and model selection.

---

## Quick Commands

| Task | Command |
|------|---------|
| View work | `wipnote snapshot --summary` |
| Run tests | `go test ./...` |
| Build binary | `wipnote build` |
| Deploy | `./scripts/deploy-all.sh VERSION --no-confirm` |
| Dashboard | `wipnote serve` |
| Status | `wipnote status` |
| Self-update | `wipnote upgrade` |

---

## Monitoring Claude Code Upstream

Claude Code is our only integration surface. Plugins, hooks, skills, slash commands, and observability (logging, events, sessions) are how wipnote influences behavior — if upstream changes those contracts, our plugin either breaks silently or misses new capabilities.

**Periodically review the Claude Code docs for changes to:**

- **Plugin system** — manifest format, `plugin.json` schema, marketplace structure
- **Hooks** — event names, payload shapes, exit-code semantics, new hook types
- **Skills** — frontmatter fields, activation triggers, invocation patterns
- **Agent teams** — still experimental as of last review; watch for graduation out of `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1` (v2.1.32+), API stability, nested-team support
- **Observability** — session metadata, telemetry, cost/token reporting, transcript format

**Upstream sources to monitor:**

- https://code.claude.com/docs/en/plugins
- https://code.claude.com/docs/en/hooks
- https://code.claude.com/docs/en/skills
- https://code.claude.com/docs/en/agent-teams
- Claude Code release notes / changelog

When upstream contracts change, the fix lands in `plugin/hooks/hooks.json`, `internal/hooks/`, `plugin/skills/`, or `cmd/wipnote/prompts/system-prompt.md` — not in AGENTS.md or CLAUDE.md (which are user-facing project docs, not plugin surfaces).

---

## Dogfooding

This project uses wipnote to develop itself. `.htmlgraph/` contains real work items — not demos.
