# Orchestrator Directives - Reference

Long-form detail for the decision rules in [SKILL.md](./SKILL.md): spawner commands, brief and delegation examples, budget-pause and completion-gate procedures, git isolation, architectural memory, and agent teams.

## Spawner details (external CLIs)

External CLIs are optional sidecars; fall back to the in-harness agent when one is missing or fails. For nested `codex exec`, choose a small/fast model with `-m <model>` (check `codex --help` for current names; the flagship default is slower and costlier than most delegated tasks need).

Run headless sidecars lean, so they load nothing the task does not need: no saved session, no hooks, apps, browser or computer use for `codex exec`; `--safe-mode --no-session-persistence` for `claude -p` (`--safe-mode` needs Claude Code 2.1.302 or later and keeps the login; `--bare` needs an API key).

### Antigravity CLI — agy (research, exploration)
```bash
agy -p "Analyze codebase for:
- All authentication patterns
- Session management
- JWT usage" --dangerously-skip-permissions 2>&1
```
Best for file searching, pattern analysis, documentation research, and unfamiliar systems. If agy fails or is missing, use `researcher` (or the native code explorer — `Explore` on Claude Code, `explorer` on Codex — for pure code reading).

### Codex CLI (code)
```bash
codex exec "Implement OAuth authentication:
- Add JWT token generation
- Include error handling
- Write unit tests" --full-auto --json -m <fast-model> -C . \
  --ephemeral --disable hooks --disable apps --disable browser_use --disable computer_use 2>&1
```
In Codex sessions, prefer the native `wipnote-feature-coder` / `wipnote-patch-coder` / `wipnote-test-runner` agents and the built-in `explorer` role for read-only exploration. Use nested `codex exec` only when native subagents are unavailable, then fall back to feature-coder.

### Copilot CLI (git)
```bash
copilot -p "Commit changes:
- Message: 'feat: add OAuth authentication'
- Files: src/auth/*.py, tests/test_auth.py
- Do NOT push" --allow-all-tools --no-color --add-dir . 2>&1
```
Best for commits, PRs, branch management and conflict resolution. If copilot fails or is missing, delegate to `patch-coder`.

### In-harness coders
```python
Use Gemini agent invocation with:
    agent="@feature-coder",
    description="Implement feature X",
    message="Feature: feat-123. Run `wipnote feature start feat-123`, then ...",
```
`feature-coder` is the balanced default; `architect-coder` is for deep reasoning, architecture decisions and ambiguous scope, when quality matters more than cost.

---

## Delegation patterns

**Exploration first, then implementation.** Later tasks that depend on earlier results run as a chain: (1) dispatch the research (native Explore or the Codex `explorer` role for code reading, researcher or agy for docs and analysis), asking for `file:line` findings; (2) read its result; (3) brief the coder with the findings pasted in; (4) commit via the copilot CLI, with patch-coder as fallback.

**Parallel delegation.** Before multi-task work, check the dependency graph and file overlap. Independent tasks with no overlap run as parallel agents in worktrees, all dispatched in one message:

```python
Use Gemini agent invocation with:
    agent="@feature-coder",
    description="Feature A",
    message="Feature: feat-aaa. ...",
    isolation="worktree",
    run_in_background=True,

Use Gemini agent invocation with:
    agent="@patch-coder",
    description="Feature B (simple)",
    message="Feature: feat-bbb. ...",
    isolation="worktree",
    run_in_background=True,
```
Time is max(T1, T2) instead of T1+T2, and each task uses the cheapest fitting agent. Afterwards merge the worktree branches, run the gate, and clean up.

**Retrieving findings.** Subagents that investigate can record a spike; read it with `wipnote spike list` and `wipnote spike show <id>`, then paste the relevant part into the next brief.

**Let subagents handle retries.** Do not retry a failing `git commit` yourself; each retry floods your context. Brief the subagent to pull, resolve conflicts, handle pre-commit hooks, retry, and report one final success or failure.

**Debugging third-party libraries.** (1) Reproduce the failure with a direct Bash run. (2) Delegate an official-docs search. (3) Delegate a GitHub issues search. (4) Only then delegate source reading. Source reading first is slow and usually unnecessary.

---

## Subagent budget-pause handling

Some runtimes (notably the VS Code devcontainer / agent-teams runtime) pause a delegated subagent at a low tool budget and return an intermediate message: it ends mid-step or mid-sentence ("let me now…"), with no completion summary, SHA, or deliverable list. That is a pause, not a failure or a finish.

- Do not treat it as done, and do not re-dispatch a fresh agent (that loses all prior context).
- Resume the same agent with a restated, explicit finish line:
  - **Claude Code:** `SendMessage({ to: <agentId> }, "Continue and finish. Remaining: <exact deliverables>. Report final status with commit SHA or summary.")`, using the `agentId` from the original result.
  - **Codex / Antigravity:** use the harness's own mechanism for re-engaging the same spawned agent.
- Expect 2-3 resumes before a genuine final report.

## Completion gate for subagent-delegated code

Completing an item whose code a subagent wrote is double-gated, because subagent gate records are session-local and subagent commits are not auto-linked (bug-3718b630):

1. Run your own gate in the orchestrator session: `wipnote check --gate --work-item <id>`.
2. Complete with the real commit SHAs in the rationale, as a separate call:
```bash
git log --oneline --grep="feat-abc" | head -3
wipnote feature complete feat-abc --accepted-advisory \
  "Subagent implementation validated. Commits: a1b2c3d, x9y8z7w. Quality gate passed."
```
Cite SHAs from `git log`, never host paths. The durable fix belongs in the binary and hooks.

## Known issues / environment

- **Codex exec sandbox failures (bwrap/bubblewrap):** in devcontainers and Codespaces, nested `codex exec` can fail immediately ("bwrap", "bubblewrap", "Operation not permitted", "cannot create namespace"). Treat the environment as permanently incompatible for the session and delegate to in-harness agents; do not retry.
- **Full Go suite is silent for minutes from cold:** `go test ./...` buffers per package. Budget 10+ minutes, or stream progress with `scripts/go-test-streaming.sh` / `go test -json`.

---

## Multi-agent git isolation

- **Source edits** by concurrent agents happen in per-agent worktrees or clones, never the shared checkout: `wipnote yolo --feature <feat-id>` creates a managed linked worktree (or `git worktree add .claude/worktrees/<id> -b <branch>`).
- **Metadata commits** (wipnote HTML, session data) are serialized by a repo-scoped advisory lock in the per-user cache directory, safe from any worktree.
- **Stale git locks:** clean only with `wipnote launcher git-lock --fix` (age threshold plus no-live-writer check). Never remove `.git/index.lock` by hand without confirming no writer.
- **Diagnosis:** `wipnote launcher doctor` warns when you are in the primary worktree.

---

## Architectural memory

`.wipnote/arch/` holds architectural fact cards (`hazard`, `invariant`, `subsystem-map`, `decision`), relevance-filtered into subagent prompts under a word budget. They save each coder the 15-25 minute tax of re-deriving the same facts.

### Dispatch-time ritual (every subagent dispatch)

```bash
wipnote arch resolve --for <work-item-id>
# or for paths:
wipnote arch resolve --for "cmd/wipnote/arch_cmds.go,internal/arch/"
```
Paste the output verbatim under `## Architectural context`. It is budget-capped (~450 words) and marks UNVERIFIED cards, which the subagent must treat as advisory and verify in code. If it prints "No arch cards matched.", omit the heading.

### Post-completion distillation

- **At completion (recommended):** `wipnote feature complete <id> --learning "Body, max 120 words." --learning-kind invariant` (or `hazard`, `decision`, `subsystem-map`). The learning is validated before the item is marked done; a failed validation aborts the completion.
- **Manual add:** `wipnote arch add <slug> --kind invariant --body "..." --paths "cmd/wipnote/**" --links <work-item-id> --created-by "agent"`.

### Post-completion nudge

After completion, wipnote prints drift-suspect cards whose globs overlap the touched paths. Re-pin with `wipnote arch verify <slug>`, or fix with `wipnote arch edit <slug> --body "..."` and then verify.

### Trust model

Active cards are authoritative. UNVERIFIED cards (drift marker or empty `verified_at`) are included but flagged for verification. Retired or superseded cards are excluded by default.

---

## Agent teams vs subagents

Claude Code 2.1.32+ has experimental **agent teams** (`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`): independent instances self-claim work from a shared task list and message each other.

| Dimension | Agent teams | Subagents |
|-----------|-------------|-----------|
| Ownership | parallel self-claiming | orchestrator dispatches |
| Communication | teammates message each other | report back to orchestrator only |
| Best for | competing-hypothesis debugging, multi-lens review, feature ownership splitting | sequential chains, research then implement, isolated tasks |
| wipnote tracking | automatic via TeammateIdle/TaskCreated/TaskCompleted | orchestrator attributes with `wipnote feature start/complete` |
| Cost | N teammates × full session | orchestrator + N smaller calls |

Spawn a team in natural language ("Create an agent team to <work and how to divide it>"); there is no SDK API. wipnote hooks no-op when no team is active.

**Caveats:** `skills:` and `mcpServers:` frontmatter are not applied to teammates; teammates cannot be `/resume`d, so a teammate blocked by a quality gate is stranded; one team per session and no nested teams; `/wipnote:execute` still uses subagents with worktree isolation.

**Example prompts:** "Create an agent team: one teammate reviews for correctness, one for performance, one for security, each writing findings to review.md under their own heading." / "Create an agent team for track trk-XXXX; each teammate claims one unblocked feature and uses `wipnote feature start/complete` for attribution."

**What wipnote captures:** `teammate_name` on every team hook event, feature steps prefixed `[teammate-name]`, and an opt-in completion gate (`"block_task_completion_on_quality_failure": true` in `.wipnote/config.json`). Because blocked teammates cannot resume, stderr then prints the manual recovery command `wipnote feature complete <feature-id>`.
