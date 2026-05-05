---
name: agent-context
description: Shared agent context — work attribution, safety rules, and development principles. Loaded by all plugin agents via skills: frontmatter.
---

# Shared Agent Context

## Work Attribution

The orchestrator always provides the work item ID in your task prompt (e.g., "Feature: feat-580dc00b"). Use it:

```bash
erinn feature start <id>   # or bug start / spike start
```

**Rules:**
1. Look for a feature/bug/spike ID in the task prompt first
2. If found, run `start` on it — do NOT create a new one
3. Only create a new work item if the prompt genuinely contains no ID
4. If erinn is unavailable, proceed — attribution is not a blocker

## Work Completion

When your task is done and quality gates pass:
1. Run `erinn feature complete <id>` (or `bug complete`, `spike complete`)
2. Do this BEFORE reporting back to the orchestrator
3. If the CLI is unavailable, report completion — the orchestrator will handle it

## Safety Rules

**FORBIDDEN:** Never edit `.htmlgraph/` files directly. Use the CLI:
- `erinn feature complete <id>` not `Edit(".htmlgraph/features/...")`
- `erinn bug create "title"` not `Write(".htmlgraph/bugs/...")`

**BATCH erinn CLI calls.** Each Bash tool call spends one turn from the user's quota. Chain commands with `&&` into a single invocation whenever possible. Do this (1 call):
```bash
erinn bug create "A" --track trk-xxx && \
erinn bug create "B" --track trk-xxx && \
erinn link add feat-aaa bug-new --rel caused_by
```
Never 3 separate Bash calls for the same thing. Only break into multiple calls when a later command must parse the output (e.g., a returned ID) of an earlier one.

### Plan YAML Updates

Plan YAML files (`.htmlgraph/plans/*.yaml`) are validated assets — never write them directly.
Use the CLI to ensure valid structure:

- **Create:** `erinn plan create-yaml "<title>"`
- **Update:** `erinn plan rewrite-yaml <plan-id> --file /tmp/updated.yaml`
- **Validate:** `erinn plan validate-yaml <plan-id>`

The `rewrite-yaml` command validates schema, checks meta.id match, and writes atomically.
Agent workflow: read plan → modify in memory → write to temp file → call rewrite-yaml.

## Development Principles

- DRY — check for existing utilities before creating new ones
- SRP — one purpose per function/module
- KISS — simplest solution that satisfies requirements
- YAGNI — only implement what is needed now
- Module limits: functions <50 lines, files <500 lines
- Research existing libraries/packages before implementing from scratch
- Check project dependencies before adding new ones

These principles are language-neutral and apply to any codebase.
