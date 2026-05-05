# YOLO Autonomous Development Mode

You are running in YOLO mode — autonomous development with enforced quality guardrails.
Permission prompts are disabled. You must self-enforce quality at every step.

## Mandatory Workflow for Each Feature

### Step 0 — Work Item (BEFORE anything else)
0. **Discover first:** `erinn relevant <topic-or-file>` — surface existing features so you don't create an orphan
1. Create one of:
   - `erinn feature create "title" --plan <plan-id> --description "what you're building"` (preferred — links to plan + its track)
   - `erinn feature create "title" --standalone "<reason>"` (last resort: hotfix or pre-plan work)
2. Start: Record the active feature for attribution
3. Isolate: Use a git worktree for each feature — never edit main directly

**CLI Quick Reference** (run `erinn help --compact` to reprint):
- Work items require type prefix: `erinn feature show <id>`, `erinn bug show <id>`, `erinn track show <id>`
- NEVER use `erinn show <id>` — there is no top-level show command
- Subcommands: `create|show|start|complete|list|add-step|update|move|delete`
- Lookup: `find <query>` · `wip show` · `status` · `snapshot --summary`
- Edges: `link add <from> <to> --rel <type>`
- Quality: `check` · `health` · `spec|tdd|review|compliance <id>`
- Data: `reindex` · `ingest` · `batch apply`
- NEVER use bare `cd` in Bash — always use subshells: `(cd dir && command)`

### Step 1 — Research
Before writing any code, answer these questions with evidence:

**Mandatory searches:**
1. Grep the codebase for similar functionality: does this already exist?
   `grep -r "keyword" cmd/ internal/` or use the Grep tool
2. Check the project manifest (`go.mod`, `package.json`, `pyproject.toml`) — is there an available dependency that does this?
3. Search for established libraries (pkg.go.dev, npmjs.com, pypi.org) that solve the problem
4. Check shared utility directories (`internal/`, `lib/`, `src/utils/`) — does the project already have a utility for this?

**Document findings:**
- Record: what libraries exist, what patterns are already used, what the decision was
- If building from scratch: explicitly document WHY (no library exists / too heavy / already have stdlib)

**Skip research only for:**
- Trivial changes (<10 lines, single file)
- Bug fixes where the root cause is already identified
- Documentation-only changes

**Examples of research-first:**
- Before adding an HTTP client: check if `net/http` or `httpx` is already imported
- Before writing a parser: search the codebase for existing parsers
- Before adding a dependency: verify the stdlib does not already have an equivalent

### Step 2 — Spec
Write acceptance criteria before coding:
- What problem does this solve?
- Measurable acceptance criteria
- API surface / interface sketch

### Step 3 — Tests First (TDD)
Write failing tests before implementation:
- Unit tests for core logic
- Integration test for happy path
- Tests must compile and fail before you write implementation

### Step 4 — Implement
- Functions: <50 lines | Modules: <500 lines
- DRY: search for existing utilities before creating new ones
- KISS: simplest solution that passes tests
- YAGNI: only what is needed now
- Separation of concerns: one purpose per module

### Step 5 — Quality Gate (MANDATORY before any commit)

Detect the project type from manifest files in the repository root:

| File | Commands |
|------|----------|
| `go.mod` | `go build ./... && go vet ./... && go test ./...` |
| `package.json` | `npm run build && npm run lint && npm test` |
| `pyproject.toml` / `requirements.txt` | `uv run ruff check . && uv run pytest` |
| `Cargo.toml` | `cargo build && cargo clippy && cargo test` |

Do NOT commit with failures.

### Step 6 — UI Validation (if UI changes)

**When to trigger:** Changed any `.html`, `.css`, `.js`, `.tsx`, `.vue`, `.svelte`, template, or dashboard file — anything that renders visual output.

**Skip when:** Backend-only changes (Go hooks, CLI commands), documentation changes, or test-only changes.

**Workflow:**
1. Start the dev server if needed: `erinn serve` (or `open index.html` for static files)
2. Navigate to the affected page
3. Take a screenshot using available MCP tools:
   - Chrome DevTools: `mcp__claude-in-chrome__take_screenshot`
   - Playwright: `mcp__plugin_playwright_playwright__browser_take_screenshot`
4. Review the screenshot against the checklist below

**Validation checklist:**
- Layout: elements properly aligned, no overlapping or clipping
- Text: readable font sizes, sufficient contrast
- Responsive: check at 1280px width and 768px width
- Data: correct values displayed, no placeholder or stale content
- Interactive: buttons and links look clickable and correctly styled

**If no MCP tools available:**
- Open the file directly: `open index.html`
- Ask the user to verify visual correctness before committing

### Step 7 — Diff Review
Run `git diff --stat` before committing. Every change must belong to this feature.
Use `git add -p` — never `git add -A`.

### Step 8 — Commit and Complete
Commit with descriptive message. Mark feature done in erinn.

## Budget Limits

### Advisory (slow down and review)
- 10 files changed per feature
- 300 new lines per feature

### Hard limit (STOP and split into sub-features)
- 20 files changed per feature
- 600 new lines per feature

If approaching the advisory limit, review whether the scope is correct.
If hitting the hard limit, STOP — create sub-features and split the work.

## Code Health Rules
- No function >50 lines
- No module >500 lines
- No duplication — extract shared helpers
- No TODO comments in committed code
- No debug print statements in commits
- Prefer O(n) algorithms; document when higher complexity is unavoidable

## What YOLO Mode Does NOT Mean
- Does NOT mean skip research
- Does NOT mean commit broken code
- Does NOT mean ignore test failures
- Does NOT mean bypass code review
- It means: no permission prompts, but FULL quality enforcement
