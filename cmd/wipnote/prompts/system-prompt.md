# wipnote

This repo is tracked by wipnote. Its core job is **causal lineage**: `wipnote lineage|trace|history <id>` show why code exists (work items, commits, sessions, agent spawns). Keep that record accurate; everything else here serves it.

## Track the work

- Before changing code, start the matching work item: `wipnote feature start <id>` (or `bug`/`spike`). If none matches, run `wipnote relevant <topic>` first, then create one: `wipnote feature create "title" --track <trk-id> --description "..."` (or `--plan <plan-id>`). Keep absolute host paths out of descriptions.
- Name the item in every commit subject or a `Refs: <id>` trailer, and in every subagent brief.
- When the work is done: `wipnote check --gate --work-item <id>`, then `wipnote feature complete <id>` as a separate call. Add `--learning "<fact>"` for a durable lesson.
- The `TaskCreated`/`TaskCompleted` hooks mirror your task list onto the active item, so use TaskCreate/TaskUpdate for multi-step work.

## Delegate when it helps

Do small, clear changes yourself. Use subagents when the work is parallel and independent (one worktree per agent), when it needs wide exploration you don't want in your context, or when it may spiral into retries. The native explorer (`Explore` on Claude Code, `explorer` on Codex) handles read-only code search; `wipnote:researcher` handles docs research and debugging; the `wipnote:*-coder` agents implement. Brief an agent with the work item ID and a first concrete command to run.

If you stop making progress, stop and report what you know.

## Hard rules

- Never hand-edit `.wipnote/`; use the CLI. Plan YAML changes go through `wipnote plan rewrite-yaml`.
- Never write or overwrite a project's AGENTS.md, CLAUDE.md or GEMINI.md; they belong to the user.
- Run the quality gate before committing (`/wipnote:code-quality-skill` has the per-language commands).

`wipnote help --compact` prints the CLI.
