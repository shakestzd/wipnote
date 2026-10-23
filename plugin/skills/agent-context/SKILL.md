---
name: agent-context
description: "Defines the shared rules for wipnote coder and researcher agents: work attribution and completion, the test-first implementation workflow, safety, research routing, and persisting results. Preloaded into those agents. Use when acting as a wipnote coder or researcher agent, or implementing a delegated feature or bug fix."
user-invocable: false
---

# Shared Agent Context

## Work attribution and completion

Use the work item ID from your task prompt (e.g. "Feature: feat-580dc00b") with `wipnote feature start <id>` (or `bug`/`spike`); do not create a new one. With no ID, run `wipnote relevant "<topic>"` first (it searches completed tracks, plans and features too) and create an item only if nothing covers the scope. Bugs need an owning track; `--standalone` is for features only. When your gate passes, run `wipnote <kind> complete <id>` before reporting back. If `complete` is refused, do not run it again for any reason (including to re-read the refusal) and do not work around it: report your commit SHAs and the refusal text you already have, and leave completion to the orchestrator. If wipnote is unavailable, proceed and report.

## Implementation workflow

When your task is to implement a feature or bug fix, copy this checklist and tick it off. `<kind>` is `feature`, `bug` or `spike`, matching the work item ID in your brief.

```
Implementation (<kind> <id>):
- [ ] 1. Item started — before reading files or writing tests
- [ ] 2. Failing tests written; they fail for the right reason
- [ ] 3. Minimum implementation; tests pass
- [ ] 4. Full quality gate passes
- [ ] 5. Committed with the work item ID
- [ ] 6. Item completed (or refusal reported)
```

1. **Start the item.** Run `wipnote <kind> start <id>` as your first command.
2. **Write failing tests first.** Express the acceptance criteria as tests. Run the test phase of the gate (code-quality-skill). Confirm they compile and fail because the implementation does not exist yet. If they fail for any other reason, fix the test and run it again. Write no implementation until this holds.
3. **Implement the minimum.** Make the tests pass and add no code that no test covers. Re-run the tests after each change until they pass.
4. **Run the full gate** from code-quality-skill. Fix every failure, including pre-existing ones in files you touched, and run it again. Continue only when every phase passes.
5. **Commit** with a conventional subject that names the item, for example `git commit -m "feat(<scope>): <description> (<id>)"`, or add a `Refs: <id>` trailer. Add the attribution trailer your harness or user instructions specify.
6. **Complete** the item per the completion rule above.

Report: files changed, lines added, test names and count, gate results, and commit SHAs.

## Safety

- Never hand-edit `.wipnote/` work-item, plan or track files; use the CLI. Appending to `.wipnote/logs/progress/<id>.md` when a brief tells you to is allowed. Plan YAML goes through `wipnote plan rewrite-yaml <plan-id> --file <tmp>` (validates and writes atomically).
- Batch wipnote CLI calls with `&&`; each Bash call costs a turn. Split only when a later command needs an earlier one's output.
- Run `wipnote arch resolve --for <work-item-id>` once at start; treat `UNVERIFIED:` cards as advisory and verify in code.

## Research routing

- **For coder and researcher agents:** external libraries, harness contracts and API details go to web search/fetch and `gh search issues`/`gh api` first; official docs, issues and changelogs are first-class research. Search for a maintained package before building, and record the adopt-vs-build outcome.
- This repo's own code: local search first. Verify any local assumption about external behaviour against official docs.

## Persist, then summarize

Final reports get truncated, so persist substance first: findings via `wipnote <kind> set-description <id> "..."`, progress via `wipnote <kind> add-step <id> "..."`, durable facts via `wipnote arch add`, code via `git commit` once the gate passes (implementation tasks always commit, per the Implementation workflow; research and other tasks commit only when the brief asks). On long tasks (>30 calls) persist incrementally. The final message is a compact summary pointing at those artifacts.

## Writing style

Write final reports, `set-description` and `add-step` text in plain English (ASD-STE100-lite): one idea per sentence, sentences under 25 words (steps under 20, no -ing or passive), active voice, commands for steps, one word for one meaning, noun clusters of three words or fewer. Never write: delve, pivotal, crucial, robust, seamless, leverage, enhance, showcase, "in order to", "serves as", "not just X, but Y", flattery or throat-clearing openers, hedge stacks, more than one em dash per paragraph, or a closing summary. Lead with the outcome, say each result once, and never copy system-reminder or harness text into a reply. Code, commands, paths and quoted output stay exact.

When a brief asks for STE, or for text that non-native readers must use (procedures, warnings), load the `simplified-technical-english` skill (`wipnote skill show simplified-technical-english`): it adds a linter and a dictionary lookup.

## Principles

Prefer CLI over skills over deferred MCP tools. Never author or overwrite a project's AGENTS.md, CLAUDE.md, or GEMINI.md.
