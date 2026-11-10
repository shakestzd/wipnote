# wipnote

This repo is tracked by wipnote. Its core job is **causal lineage**: `wipnote lineage|trace|history <id>` show why code exists (work items, commits, sessions, agent spawns). Keep that record accurate; everything else here serves it.

## Track the work

- Before changing code, start the matching work item: `wipnote feature start <id>` (or `bug`/`spike`). If none matches, run `wipnote relevant <topic>` first, then create one: `wipnote feature create "title" --track <trk-id> --description "..." --why "<why>" --done-when "<done condition>"` (or `--plan <plan-id>`). Keep absolute host paths out of descriptions.
- Name the item in every commit subject or a `Refs: <id>` trailer, and in every subagent brief.
- A code comment that cites an item id must also state the reason in plain words; the id alone means nothing to a reader without the store. Good: `// Sort edge types so a rewrite never reorders them (bug-eb141e88).` Bad: `// See bug-eb141e88.`
- When the work is done: `wipnote check --gate --work-item <id>`, then `wipnote feature complete <id>` as a separate call. Add `--learning "<fact>"` for a durable lesson.
- The `TaskCreated`/`TaskCompleted` hooks mirror your task list onto the active item, so use TaskCreate/TaskUpdate for multi-step work.

## Delegate when it helps

Do small, clear changes yourself. Use subagents when the work is parallel and independent (one worktree per agent), when it needs wide exploration you don't want in your context, or when it may spiral into retries. The native explorer (`Explore` on Claude Code, `explorer` on Codex) handles read-only code search; `wipnote:researcher` handles docs research and debugging; the `wipnote:*-coder` agents implement. Brief an agent with the work item ID and a first concrete command to run (`wipnote context-pack <id>` prints a ready brief).

Inside herdr or the Herdr app, hand coding work to a `wipnote delegate spawn` worker instead of a `wipnote:*-coder` subagent: it starts with far less context, and you set its model, effort and tools for the task. A worker does not load the project's rules by default, so put any rule that must hold in its brief. Use a subagent when spawn refuses because you are outside Herdr. Inside Herdr, a `wipnote:*-coder` subagent call is denied, with the spawn command to use instead.

To hand work to another harness, use `wipnote delegate`. Do not call `codex exec`, `codex review`, `claude -p` or `ccx task` directly: a bare call in the Bash tool is denied, and the denial names the `wipnote delegate run` form to use. For one call, run `wipnote delegate run -- <tool argv>`: it prints the answer and a run page. For an interactive Claude or Codex worker inside herdr or the Herdr app, run `wipnote delegate spawn`, then `send`, `wait --timeout` and `result`, and `stop` the worker before you complete its item. Do not start agents with `hc run` or `herdr agent start`: they skip the ledger, and the same denial applies. Every run is a row in the store's `.wipnote/delegations-ledger.html`. `wipnote skill show delegate` has the details.

When an agent returns mid-step with no summary or commit SHA, it hit a budget pause: resume the same agent with the remaining deliverables rather than starting a fresh one. If its work item is still open, link the agent's commits (`wipnote feature link-commit <id> <sha>`), run the gate yourself, and complete it.

If you stop making progress, stop and report what you know.

## Ask the owner with the question tool

When you need a decision from the owner, ask with the harness question tool, not a prose question. Batch up to four questions per call and list the recommended option first. Record each answer with `wipnote question answer <question-id> <option-key>`; `wipnote question list --open` shows what is still unanswered. On Claude Code and Codex, a Stop hook blocks once when a reply ends with a prose question while open question records exist.

## Hard rules

- Never hand-edit `.wipnote/`; use the CLI. Plan YAML changes go through `wipnote plan rewrite-yaml`. A project with no `.wipnote/` folder keeps its store under `~/.wipnote/projects/`: get the path from `wipnote status`, do not assume it.
- Never write or overwrite a project's AGENTS.md, CLAUDE.md or GEMINI.md; they belong to the user.
- Run the quality gate before committing (`/wipnote:code-quality-skill` has the per-language commands).

`wipnote help --compact` prints the CLI.
