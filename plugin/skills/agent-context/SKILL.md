---
name: agent-context
description: "Rules shared by wipnote agents: work attribution, safety rules, research routing, truncation-resilient reporting. Preloaded into coder agents via skills:."
user-invocable: false
---

# Shared Agent Context

## Work attribution and completion

Use the work item ID from your task prompt (e.g. "Feature: feat-580dc00b") with `wipnote feature start <id>` (or `bug`/`spike`); do not create a new one. With no ID, run `wipnote relevant "<topic>"` first (it searches completed tracks, plans and features too) and create an item only if nothing covers the scope. Bugs need an owning track; `--standalone` is for features only. When your gate passes, run `wipnote <kind> complete <id>` before reporting back. If wipnote is unavailable, proceed and report.

## Safety

- Never edit `.wipnote/` files directly; use the CLI. Plan YAML goes through `wipnote plan rewrite-yaml <plan-id> --file <tmp>` (validates and writes atomically).
- Batch wipnote CLI calls with `&&`; each Bash call costs a turn. Split only when a later command needs an earlier one's output.
- Run `wipnote arch resolve --for <work-item-id>` once at start; treat `UNVERIFIED:` cards as advisory and verify in code.

## Research routing

- **For coder and researcher agents:** external libraries, harness contracts and API details go to web search/fetch and `gh search issues`/`gh api` first; official docs, issues and changelogs are first-class research. Search for a maintained package before building, and record the adopt-vs-build outcome.
- **For orchestrators:** Research-first does not mean orchestrator researches directly; dispatch a sidecar (native Explore or the Codex `explorer` role for code, `researcher` for docs and on Antigravity). Main-context `web.*` use is limited to explicit user browse requests, one-shot verification with no sidecar, or confirmation after a sidecar failure.
- This repo's own code: local search first. Verify any local assumption about external behaviour against official docs.

## Persist, then summarize

Final reports get truncated, so persist substance first: findings via `wipnote <kind> set-description <id> "..."`, progress via `wipnote <kind> add-step <id> "..."`, durable facts via `wipnote arch add`, code via `git commit` (only when your task includes committing and the gate passes). On long tasks (>30 calls) persist incrementally. The final message is a compact summary pointing at those artifacts.

## Principles

DRY, single responsibility, KISS, YAGNI; functions <50 lines, files <500. Prefer CLI over skills over deferred MCP tools. Never author or overwrite a project's AGENTS.md, CLAUDE.md, or GEMINI.md.
