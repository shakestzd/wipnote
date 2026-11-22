# wipnote

This repo is tracked by wipnote. Its core job is **causal lineage**: `wipnote lineage|trace|history <id>` show why code exists (work items, commits, sessions, agent spawns). Keep that record accurate.

## Track the work

- Before changing code, start the matching work item: `wipnote feature start <id>` (or `bug`/`spike`). If none matches, run `wipnote relevant <topic>`, then create one: `wipnote feature create "title" --track <trk-id> --description "..." --why "<why>" --done-when "<done condition>"`.
- Name the item in every commit subject or a `Refs: <id>` trailer, and in every subagent brief.
- A code comment that cites an item id also states the reason in plain words. Bad: `// See bug-eb141e88.`
- When the work is done: `wipnote check --gate --work-item <id>`, then `wipnote feature complete <id>` as a separate call. Add `--learning "<fact>"` for a durable lesson.

## Delegate when it helps

Do small, clear changes yourself. Use a subagent for parallel, independent work or wide exploration. Brief it with the work item ID and a first command (`wipnote context-pack <id>` prints a brief). To hand work to another harness, or to a worker inside Herdr, use `wipnote delegate`: `wipnote delegate --help` has the steps. Do not call `codex exec`, `claude -p` or `ccx task` directly.

A subagent that returns mid-step with no commit SHA hit a budget pause. Resume it with the remaining deliverables. If you stop making progress, stop and report what you know.

## Ask the owner

Ask only for a decision the code and the brief cannot settle: look up facts yourself, and for a choice you can undo, choose and say what you chose. Use the harness question tool, list the recommended option first, and record the answer with `wipnote question answer <question-id> <option-key>`.

## Write plainly

Sentences hold 25 words or fewer and steps 20 or fewer. Use active voice, no semicolons, and a verb to start each step. Document what was given, say what you did not check, and post or publish only when asked. Run `wipnote prose check <file>` on a pull request body, issue or README before it is posted.

## Hard rules

- Never hand-edit the store (`.wipnote/`, or the path `wipnote status` prints); use the CLI. Plan YAML changes go through `wipnote plan rewrite-yaml`.
- Never write a project's AGENTS.md, CLAUDE.md or GEMINI.md.

`wipnote help --compact` prints the CLI.

For multi-step work use TaskCreate and TaskUpdate: the task hooks mirror the task list onto the active item.
