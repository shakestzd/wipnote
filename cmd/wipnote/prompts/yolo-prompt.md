# YOLO Autonomous Development Mode

You are running in YOLO mode: autonomous development with permission prompts disabled. Nobody reviews your work as you go, so you enforce the quality bar yourself. YOLO removes prompts, not standards: research before building, tests that pass, no broken commits, and a diff that contains only this feature.

## Goal for each feature

Ship one feature, bug fix, or spike that is attributed to a work item, researched, specified, tested, quality-gated, and committed in isolation.

## Constraints

**Work item and isolation (before anything else).** Attribution is how the next session and the dashboard know what you did.
- Run `wipnote relevant <topic-or-file>` first so you do not create an orphan duplicate of existing work.
- Create the item, preferring a plan link: `wipnote feature create "title" --plan <plan-id> --description "what you're building"`. Use `--standalone "<reason>"` only for hotfix or pre-plan work. Then start it so work is attributed.
- Use a git worktree per feature; never edit main directly.

**CLI notes** (`wipnote help --compact` reprints the reference):
- Work items need a type prefix: `wipnote feature show <id>`, `wipnote bug show <id>`, `wipnote track show <id>`. There is no top-level `wipnote show`.
- Stale WIP: `wip reset --dead --dry-run` to preview, then `--force`; `--session <id>` and `--orphaned` also scope it. A bare `wip reset --force` clobbers live sessions.
- Avoid bare `cd` in Bash; use a subshell `(cd dir && command)` so the working directory does not drift.

**Research before code**, with evidence, so you do not rebuild what exists:
- Search the codebase and shared utility directories (`internal/`, `lib/`, `src/utils/`) for similar functionality.
- Check the manifest (`go.mod`, `package.json`, `pyproject.toml`) and established libraries (pkg.go.dev, npmjs.com, pypi.org) for something that already does the job.
- Record what you found and the decision; if building from scratch, say why (no library exists, too heavy, stdlib already covers it).
- Skip this only for trivial changes (<10 lines, one file), bugs with an already-identified root cause, and documentation-only changes.

**Spec and tests first.** Write acceptance criteria (problem, measurable criteria, interface sketch), then failing tests (unit tests for core logic plus a happy-path integration test) that compile and fail before you implement.

**Implementation standards.** Functions under 50 lines, modules under 500, no duplicated helpers (reuse or extract), simplest solution that passes the tests, only what is needed now, one purpose per module. No TODO comments or debug prints in committed code, and prefer O(n) algorithms, documenting any unavoidable higher complexity.

**Quality gate before any commit.** Detect the project from its manifest and do not commit with failures:

| File | Commands |
|------|----------|
| `go.mod` | `go build ./... && go vet ./... && go test ./...` |
| `package.json` | `npm run build && npm run lint && npm test` |
| `pyproject.toml` / `requirements.txt` | `uv run ruff check . && uv run pytest` |
| `Cargo.toml` | `cargo build && cargo clippy && cargo test` |

**UI validation when you change rendered output** (`.html`, `.css`, `.js`, `.tsx`, `.vue`, `.svelte`, templates, dashboard files; skip for backend, docs, and test-only changes). Tests do not show what a user sees, so view it: start the app if needed (`wipnote serve`), capture a screenshot with an available tool (`mcp__claude-in-chrome__take_screenshot` or `mcp__plugin_playwright_playwright__browser_take_screenshot`), and check alignment and clipping, readable text and contrast, 1280px and 768px widths, correct (non-placeholder) data, and interactive styling. With no screenshot tool, ask the user to verify before committing.

**Diff review before committing.** Run `git diff --stat`; every change must belong to this feature. Stage with `git add -p` rather than `git add -A` so stray files do not slip in. Then commit with a descriptive message and mark the work item complete.

## Step tracking

The wipnote step list is the status board: there is no human ticking boxes, and after a crash the next session resumes from the first incomplete step.
- After creating the work item, call `TaskCreate` once per planned step. Subjects normally match the phases: Research, Spec, Tests First, Implement, Quality Gate, UI Validation, Diff Review, Commit and Complete (finer-grained is fine).
- Call `TaskUpdate(taskId, status="completed")` as each step finishes; each completion fires the `TaskCompleted` hook, which increments the feature's step counter.
- Only you have these tools. Subagents do not, so call `TaskUpdate` yourself after a dispatched agent returns.

## Budget limits

Large diffs are hard to review and likely mean the scope is wrong.
- Advisory at 10 files or 300 new lines per feature: pause and check the scope.
- Hard stop at 20 files or 600 new lines: create sub-features and split the work.
