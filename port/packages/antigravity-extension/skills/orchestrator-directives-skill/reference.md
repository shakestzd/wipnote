# Orchestrator Directives - Complete Reference

This document contains the complete orchestration rules and patterns for wipnote project.

**Source:** `packages/claude-plugin/rules/orchestration.md`

---

## Core Philosophy

**CRITICAL: When operating in orchestrator mode, you MUST delegate ALL operations except a minimal set of strategic activities.**

**You don't know the outcome before running a tool.** What looks like "one bash call" often becomes 2, 3, 4+ calls when handling failures, conflicts, hooks, or errors. Delegation preserves strategic context by isolating tactical execution in subagent threads.

## Operations You MUST Delegate

**ALL operations EXCEPT:**
- `use the appropriate Gemini agent invocation` - Delegation itself
- `AskUserQuestion()` - Clarifying requirements with user
- `TodoWrite()` - Tracking work items
- SDK operations - Creating features, spikes, bugs, analytics

**Everything else MUST be delegated**, including:

### 1. Git Operations - ALWAYS DELEGATE

- ❌ NEVER run git commands directly (add, commit, push, branch, merge)
- ✅ ALWAYS delegate to subagent with error handling

**Why?** Git operations cascade unpredictably:
- Commit hooks may fail (need fix + retry)
- Conflicts may occur (need resolution + retry)
- Push may fail (need pull + merge + retry)
- Tests may fail in hooks (need fix + retry)

**Context cost comparison:**
```
Direct execution: 7+ tool calls
  git add → commit fails (hook) → fix code → commit → push fails → pull → push

Delegation: 2 tool calls
  use the appropriate Gemini agent invocation → Read result
```

**Delegation pattern (Bash-first):**
```bash
# Priority 1: Try copilot CLI directly
copilot -p "Stage files: CLAUDE.md, SKILL.md, git-commit-push.sh. Commit with message: 'docs: enforce strict git delegation in orchestrator directives'. Do NOT push." \
  --allow-all-tools --no-color --add-dir . 2>&1
```

```python
# Priority 2: patch-coder fallback (if copilot unavailable)
Use Gemini agent invocation with:
    agent="@patch-coder",
    description="Commit: docs: enforce strict git delegation",
    message="""
    Stage files: CLAUDE.md, SKILL.md, git-commit-push.sh
    Commit with message: "docs: enforce strict git delegation in orchestrator directives"
    Do NOT push.
    Handle any errors (pre-commit hooks, conflicts, etc).
    """,
```

### 2. Code Changes - DELEGATE Unless Trivial

- ❌ Multi-file edits
- ❌ Implementation requiring research
- ❌ Changes with testing requirements
- ✅ Single-line typo fixes (OK to do directly)

### 3. Research & Exploration - ALWAYS DELEGATE

- ❌ Large codebase searches (multiple Grep/Glob calls)
- ❌ Understanding unfamiliar systems
- ❌ Documentation research
- ✅ Single file quick lookup (OK to do directly)

### 4. Testing & Validation - ALWAYS DELEGATE

- ❌ Running test suites
- ❌ Debugging test failures
- ❌ Quality gate validation
- ✅ Checking test command exists (OK to do directly)

### 5. Build & Deployment - ALWAYS DELEGATE

- ❌ Build processes
- ❌ Package publishing
- ❌ Environment setup
- ✅ Checking deployment script exists (OK to do directly)

### 6. File Operations - DELEGATE Complex Operations

- ❌ Batch file operations (multiple files)
- ❌ Large file reading/writing
- ❌ Complex file transformations
- ✅ Reading single config file (OK to do directly)
- ✅ Writing single small file (OK to do directly)

### 7. Analysis & Computation - DELEGATE Heavy Work

- ❌ Performance profiling
- ❌ Large-scale analysis
- ❌ Complex calculations
- ✅ Simple status checks (OK to do directly)

## Why Strict Delegation Matters

### 1. Context Preservation

- Each tool call consumes tokens
- Failed operations consume MORE tokens
- Cascading failures consume MOST tokens
- Delegation isolates failure to subagent context

### 2. Parallel Efficiency

- Multiple subagents can work simultaneously
- Orchestrator stays available for decisions
- Higher throughput on independent tasks

### 3. Error Isolation

- Subagent handles retries and recovery
- Orchestrator receives clean success/failure
- No pollution of strategic context

### 4. Cognitive Clarity

- Orchestrator maintains high-level view
- Subagents handle tactical details
- Clear separation of concerns

## Decision Framework

Ask yourself:

1. **Will this likely be one tool call?**
   - If uncertain → DELEGATE
   - If certain → MAY do directly

2. **Does this require error handling?**
   - If yes → DELEGATE

3. **Could this cascade into multiple operations?**
   - If yes → DELEGATE

4. **Is this strategic (decisions) or tactical (execution)?**
   - Strategic → Do directly
   - Tactical → DELEGATE

## Orchestrator Reflection System

When orchestrator mode is enabled (strict), you'll receive reflections after direct tool execution:

```
ORCHESTRATOR REFLECTION: You executed code directly.

Ask yourself:
- Could this have been delegated to a subagent?
- Would parallel use the appropriate Gemini agent invocation calls have been faster?
- Is a work item tracking this effort?
- What if this operation fails - how many retries will consume context?
```

Use these reflections to adjust your delegation habits.

## Integration with wipnote CLI

Always use the CLI to track orchestration activities:

```bash
# Track what you delegate
wipnote feature create "Implement authentication" --track <trk-id>
wipnote feature start <feat-id>
```

```bash
# Try CLI tools directly first
agy -p "Find all auth-related code in src/: What library is used? Where is validation?" \
  --dangerously-skip-permissions 2>&1
# fallback → use @patch-coder
```
```text
# In Codex when native subagents are available
spawn native `wipnote-feature-coder` to implement the OAuth flow based on research findings

# Only if native Codex subagents are unavailable
codex exec "Implement OAuth flow based on research findings" \
  --full-auto --json -m gpt-4.1-mini -C . 2>&1
# fallback → use @feature-coder
```

**See:** `packages/go-plugin/skills/orchestrator-directives-skill/SKILL.md` for complete orchestrator patterns

## Parallel Task Coordination

**Problem:** Multiple parallel tasks need independent result tracking.

**Solution:** Dispatch all tasks in a single message — Claude Code runs them in parallel automatically.

```text
# In Codex when native subagents are available, dispatch in parallel:
- `wipnote-feature-coder` → add JWT auth to API endpoints
- `wipnote-test-runner` or `wipnote-patch-coder` → write unit + integration tests
```
```bash
# Only if native Codex subagents are unavailable, use nested codex exec
codex exec "Add JWT auth to API endpoints..." --full-auto --json -m gpt-4.1-mini -C . 2>&1
# fallback → use @feature-coder
```
```bash
codex exec "Write unit + integration tests for auth endpoints..." --full-auto --json -m gpt-4.1-mini -C . 2>&1
# fallback → use @patch-coder
```
```bash
agy -p "Update API documentation for auth endpoints..." --dangerously-skip-permissions 2>&1
# fallback → use @patch-coder
```
# All three run in parallel; each reports results independently

**Benefits:**
- True parallelism (all dispatched in one message)
- Each task runs in isolation
- Cheaper agents used for each task type

## Git Workflow Patterns

### Orchestrator Pattern (REQUIRED)

When operating as orchestrator, try the CLI directly first, then delegate to patch-coder as fallback:

```bash
# ✅ CORRECT - Priority 1: Try copilot CLI directly
copilot -p "Stage files: [list files]. Commit with message: 'chore: update session tracking'. Do NOT push." \
  --allow-all-tools --no-color --add-dir . 2>&1
```

```python
# ✅ CORRECT - Priority 2: patch-coder fallback (if copilot unavailable)
Use Gemini agent invocation with:
    agent="@patch-coder",
    description="Commit: chore: update session tracking",
    message="""
    Commit and push changes to git:

    Files to commit: [list files or use 'all changes']
    Commit message: "chore: update session tracking"

    Steps:
    1. git add [files]
    2. git commit -m "message"
    3. Handle any errors (pre-commit hooks, conflicts, push failures)
    4. Retry with fixes if needed

    Report final status: success or failure with details.
    """,
```

**Why Bash-first?** Skips the agent overhead when the CLI works — fast, transparent, cost-efficient.
**Why fallback to coder agent?** When CLI isn't installed, the coder agent handles all retries in its own context.

**Context cost:**
- Bash-copilot (success): 1 tool call
- patch-coder fallback: 2 tool calls (Agent + result review)
- Direct git without delegation: 5-10+ tool calls (with failures and retries)

## Detailed Delegation Examples

### Example 1: Feature Implementation Workflow

```bash
# 1. Create feature (orchestrator does this directly)
wipnote feature create "Add user authentication" --track <trk-id>
wipnote feature start <feat-id>
```

```bash
# 2. Research (try the agy CLI first)
agy -p "Research existing auth patterns: What library is used? Where is validation? What OAuth providers are supported?" \
  --dangerously-skip-permissions 2>&1
# fallback → use @patch-coder
```

```bash
# 3. Implement
# In Codex native sessions: spawn `wipnote-feature-coder`
# Otherwise: try codex CLI first, then fallback → use @feature-coder
```

```bash
# 4. Commit (try copilot CLI first)
copilot -p "Commit with message: 'feat: add user authentication with OAuth support'. Do NOT push." \
  --allow-all-tools --no-color --add-dir . 2>&1
# fallback → use @patch-coder
```

```bash
# 5. Mark feature complete
wipnote feature complete <feat-id>
```

### Example 2: Bug Fix Workflow

```bash
# 1. Create bug
wipnote bug create "Session timeout not working" --track <trk-id>
```

```bash
# 2. Investigate (try the agy CLI first)
agy -p "Debug session timeout: expected 30min, observed ~5min. Find config, check middleware, review logs, identify root cause." \
  --dangerously-skip-permissions 2>&1
# fallback → use @patch-coder
```

```bash
# Fix
# In Codex native sessions: spawn `wipnote-feature-coder`
# Otherwise: try codex CLI first, then fallback → use @feature-coder
```

```bash
# 3. Commit (try copilot CLI first)
copilot -p "Commit with message: 'fix: correct session timeout to 30 minutes'. Do NOT push." \
  --allow-all-tools --no-color --add-dir . 2>&1
# fallback → use @patch-coder
```

```bash
# 4. Mark bug resolved
wipnote bug complete <bug-id>
```

### Example 3: Parallel Task Coordination

```bash
# Create feature
wipnote feature create "Refactor API layer" --track <trk-id>
```

```bash
# Dispatch 3 parallel delegations in a single message
agy -p "Update API documentation to reflect new endpoints" --dangerously-skip-permissions 2>&1
# fallback → use @patch-coder
```
```text
# In Codex native sessions
spawn `wipnote-test-runner` or `wipnote-feature-coder` to update the test suite for refactored API endpoints

# Otherwise
codex exec "Update test suite for refactored API endpoints" --full-auto --json -m gpt-4.1-mini -C . 2>&1
# fallback → use @feature-coder
```
```bash
agy -p "Create migration guide for API changes" --dangerously-skip-permissions 2>&1
# fallback → use @patch-coder
```

```bash
# After all complete — commit everything (try copilot CLI first)
copilot -p "Commit all API refactoring changes with message: 'refactor: update API layer with improved endpoints'. Do NOT push." \
  --allow-all-tools --no-color --add-dir . 2>&1
# fallback → use @patch-coder
```

```bash
wipnote feature complete <feat-id>
```

## Common Anti-Patterns to Avoid

### Anti-Pattern 1: Direct Git Execution

```python
# ❌ WRONG - Orchestrator executing git directly
Bash(command="git add .")
Bash(command="git commit -m 'feat: new feature'")
Bash(command="git push origin main")

# This will likely fail due to:
# - Pre-commit hooks
# - Merge conflicts
# - Remote changes
# Each failure consumes context and requires recovery
```

```python
# ✅ CORRECT - Delegate to subagent
Use Gemini agent invocation with:
    message="""
    Commit and push changes:
    Message: "feat: new feature"
    Handle all errors (hooks, conflicts, etc)
    """,
    workflow="general-purpose"
```

### Anti-Pattern 2: Sequential When Parallel is Possible

```python
# ❌ WRONG - Sequential delegation
use the appropriate Gemini agent invocation
# Wait for result...
use the appropriate Gemini agent invocation
# Wait for result...
use the appropriate Gemini agent invocation

# Total time: T1 + T2 + T3
```

```python
# ✅ CORRECT - Parallel delegation
use the appropriate Gemini agent invocation
use the appropriate Gemini agent invocation
use the appropriate Gemini agent invocation

# Total time: max(T1, T2, T3)
```

### Anti-Pattern 3: Not Using Task IDs

```python
# ❌ WRONG - No task IDs, can't distinguish results
use the appropriate Gemini agent invocation
use the appropriate Gemini agent invocation
use the appropriate Gemini agent invocation

# Which result is which?
```

```python
# ✅ CORRECT - Use task IDs
auth_id, auth_prompt = delegate_with_id("Research auth", "...", "general-purpose")
cache_id, cache_prompt = delegate_with_id("Research caching", "...", "general-purpose")
log_id, log_prompt = delegate_with_id("Research logging", "...", "general-purpose")

use the appropriate Gemini agent invocation
use the appropriate Gemini agent invocation
use the appropriate Gemini agent invocation

# Retrieve results independently
auth_results = get_results_by_task_id(sdk, auth_id)
cache_results = get_results_by_task_id(sdk, cache_id)
log_results = get_results_by_task_id(sdk, log_id)
```

### Anti-Pattern 4: Not Tracking Work Items

```python
# ❌ WRONG - No feature/bug tracking
use the appropriate Gemini agent invocation
# No record of what was planned or completed
```

```bash
# ✅ CORRECT - Track with wipnote CLI
wipnote feature create "Implement new feature" --track <trk-id>
wipnote feature start <feat-id>
```

```python
use the appropriate Gemini agent invocation
```

```bash
# Update status after completion
wipnote feature complete <feat-id>
```

## Summary

**Key Principles:**

1. **Delegate Everything** - Except use the appropriate Gemini agent invocation, AskUserQuestion(), TodoWrite(), and CLI operations
2. **Parallel Dispatch** - Send all independent Tasks in one message
3. **Track Work** - Use wipnote CLI for all features, bugs, spikes
4. **Parallel > Sequential** - Delegate independently when possible
5. **Git = Always Delegate** - Never run git commands directly

**Benefits:**

- Context preservation (fewer tokens consumed)
- Parallel efficiency (faster completion)
- Error isolation (cleaner orchestration)
- Cognitive clarity (strategic focus)

**When in doubt, DELEGATE.**


---

## Moved from SKILL.md (patterns, examples, and long-form procedures)

The core skill keeps the decision rules; the detailed examples live below.

## Spawner details (external CLIs)

External CLIs are optional sidecars; fall back to the in-harness agent when one is missing or fails. For nested `codex exec`, choose a small/fast model with `-m <model>` (check `codex --help` for current names; the default flagship model is slower and costlier than most delegated tasks need).

### Antigravity CLI — agy (exploration)
```bash
agy -p "Analyze codebase for:
- All authentication patterns
- OAuth implementations
- Session management
- JWT usage" --dangerously-skip-permissions 2>&1
```

**If agy fails/unavailable → fallback to patch-coder**

**Best for:**
- File searching 
- Pattern analysis 
- Documentation research 
- Understanding unfamiliar systems 

### Codex CLI (code)
```bash
codex exec "Implement OAuth authentication:
- Add JWT token generation
- Include error handling
- Write unit tests" --full-auto --json -m <fast-model> -C . 2>&1
```

**In Codex, prefer native `wipnote-feature-coder` / `wipnote-patch-coder` / `wipnote-test-runner` first. If native subagents are unavailable or fail → use `codex exec`, then fallback to feature-coder.**

**Best for:**
- Code generation
- Bug fixes
- Test writing
- Refactoring
- Sandboxed execution

### Copilot CLI (git)
```bash
copilot -p "Commit changes:
- Message: 'feat: add OAuth authentication'
- Files: src/auth/*.py, tests/test_auth.py
- Do NOT push" --allow-all-tools --no-color --add-dir . 2>&1
```

**If copilot fails/unavailable → fallback to patch-coder**

**Best for:**
- Git commits 
- PR creation
- Branch management
- GitHub integration
- Resolving conflicts

### use the appropriate Gemini agent invocation with feature-coder/architect-coder (Strategic)
```python
Use Gemini agent invocation with:
    message="Design authentication architecture...",
    workflow="feature-coder"  # or "architect-coder" for deep reasoning
```

**feature-coder (Mid-tier):**
- Coordinate complex workflows
- Multi-agent orchestration
- Fallback when spawners fail

**architect-coder (Expensive):**
- Deep reasoning
- Architecture decisions
- Strategic planning
- When quality matters more than cost


---

## Delegation Patterns & Examples

<details>
<summary><strong>Basic Delegation Pattern</strong></summary>

**Simple exploration (try CLI first):**
```bash
agy -p "Search codebase for authentication patterns and summarize findings" \
  --dangerously-skip-permissions 2>&1
# fallback → use @patch-coder
```

**Code implementation (try CLI first):**
```bash
codex exec "Implement OAuth authentication endpoint with JWT support" \
  --full-auto --json -m <fast-model> -C . 2>&1
# fallback → use @feature-coder
```

**Code implementation (Codex-native preferred when available):**
```text
Spawn the native `wipnote-feature-coder` subagent for implementation work.
Use nested `codex exec` only when the native Codex subagent surface is unavailable.
```

**Git operations (try CLI first):**
```bash
copilot -p "Commit changes with message: 'feat: add OAuth authentication'. Do NOT push." \
  --allow-all-tools --no-color --add-dir . 2>&1
# fallback → use @patch-coder
```

</details>

<details>
<summary><strong>Git/Code Operations (Bash-first, patch-coder fallback)</strong></summary>

**Try the Copilot CLI directly via Bash first, then delegate to patch-coder if unavailable.**

```bash
# Priority 1: Bash-copilot (preferred)
copilot -p "Stage files: <list>. Commit with message: '<message>'. Do NOT push." \
  --allow-all-tools --no-color --add-dir . 2>&1
```

```python
# Priority 2: patch-coder fallback (if copilot fails or not installed)
Use Gemini agent invocation with:
    agent="@patch-coder",
    description="Commit and push changes",
    message="Stage files: <list>. Commit with message: 'feat: add X'. Do NOT push.",
```

**Pattern:** orchestrator tries the CLI directly, falls back to a coder agent.

</details>

<details>
<summary><strong>Code Generation (Bash-first, feature-coder fallback)</strong></summary>

**For implementation, refactoring, and structured output tasks:**

```bash
# Priority 1 outside Codex-native sessions: Bash-codex
codex exec "TASK_DESCRIPTION" --full-auto --json -m <fast-model> -C . 2>&1
```

```python
# Priority 1 in Codex-native sessions, or Priority 2 elsewhere
Use Gemini agent invocation with:
    agent="@feature-coder",
    description="Implement feature X",
    message="Add OAuth authentication to the login endpoint.",
```

**Pattern:** in Codex native multi-agent sessions, use `wipnote-feature-coder` first; otherwise try the CLI directly, then fall back to a coder agent.
Pass a small/fast model via `-m` for nested `codex exec`.

</details>

<details>
<summary><strong>Research & Analysis (Bash-first, patch-coder fallback)</strong></summary>

**For codebase exploration, documentation research, and large-context analysis:**

```bash
# Priority 1: Bash-agy (preferred )
agy -p "TASK_DESCRIPTION" --dangerously-skip-permissions 2>&1
```

```python
# Priority 2: patch-coder fallback (if agy fails or not installed)
Use Gemini agent invocation with:
    agent="@patch-coder",
    description="Research auth patterns",
    message="Analyze all authentication patterns in this codebase. Find security gaps.",
```

**Pattern:** orchestrator tries the CLI directly, falls back to a coder agent.

</details>

<details>
<summary><strong>Parallel Delegation (Multiple Independent Tasks)</strong></summary>

**Analyze parallelizability when 2+ tasks are identified.**

Before presenting recommendations or starting multi-task work, ALWAYS:
1. Check dependency graph — do any tasks depend on outputs of others?
2. Check file overlap — do tasks touch the same files/modules?
3. If independent → propose parallel worktree execution as the DEFAULT
4. If dependent → identify the critical path and parallelize what you can

**Decision matrix:**

| Dependency? | File Overlap? | Action |
|-------------|---------------|--------|
| No | No | Parallel worktrees (DEFAULT) |
| No | Yes | Sequential (same files = merge conflicts) |
| Yes | No | Pipeline (parallel where deps allow) |
| Yes | Yes | Sequential |

**Pattern: Spawn all at once in isolated worktrees**

```python
# Launch parallel agents in worktrees — one per feature
Use Gemini agent invocation with:
    agent="@feature-coder",
    description="Feature A",
    message="Implement feature A...",
    isolation="worktree",
    run_in_background=True,

Use Gemini agent invocation with:
    agent="@feature-coder",
    description="Feature B",
    message="Implement feature B...",
    isolation="worktree",
    run_in_background=True,

Use Gemini agent invocation with:
    agent="@patch-coder",
    description="Feature C (simple)",
    message="Implement feature C...",
    isolation="worktree",
    run_in_background=True,
```

**Benefits:**
- 3 tasks in parallel: time = max(T1, T2, T3) instead of T1+T2+T3
- Cost optimization: Uses cheapest model for each task
- Worktree isolation: No merge conflicts during execution
- Independent results: Each task tracked separately

**After completion:** Merge worktree branches to main, run quality gates, clean up.

</details>

<details>
<summary><strong>Sequential Delegation with Dependencies</strong></summary>

**Pattern: Chain dependent tasks in sequence**

```python
# 1. Research existing patterns (free agy research sidecar)
Bash('agy -p "Find all OAuth implementations in codebase..." --dangerously-skip-permissions 2>&1')
# fallback → use @patch-coder

# 2. Wait for research, then implement
# (In next message after reading result)
research_findings = "..."  # Read from previous task result

Use Gemini agent invocation with:
    workflow="codex",
    description="Implement OAuth based on research",
    message=f"""
    Implement OAuth using discovered patterns:
    {research_findings}
    """

# 3. Wait for implementation, then commit
Use Gemini agent invocation with:
    workflow="copilot",
    description="Commit implementation",
    message="Commit OAuth implementation..."
```

**When to use:** When later tasks depend on earlier results

</details>

<details>
<summary><strong>wipnote Result Retrieval</strong></summary>

**Subagents report findings automatically:**

When a use the appropriate Gemini agent invocation completes, findings are available via CLI:
```bash
# Check recent spikes
wipnote spike list

# View specific spike
wipnote spike show <id>
```

**Pattern: Read findings after Task completes**

```bash
# 1. Delegate exploration (try the agy CLI first)
agy -p "Find all authentication patterns..." --dangerously-skip-permissions 2>&1
# fallback → use @patch-coder
```

```bash
# 2. The subagent creates a spike with findings
# Read findings via: wipnote spike list (then spike show <id>)

# 3. Use findings in next delegation
# In Codex native sessions: spawn `wipnote-feature-coder`
# Otherwise: try codex CLI first, then fallback → use @feature-coder
```

</details>

<details>
<summary><strong>Debugging Delegation Order (Third-Party Libraries)</strong></summary>

## Debugging Delegation Order

When debugging third-party library issues, enforce this order:

1. **Reproduce the failure** — run Bash commands to confirm the error message
2. **Delegate doc search to researcher** — WebSearch for official docs (via agy or the researcher agent)
3. **Delegate GitHub issues search to researcher** — check for known issues or recent changes
4. **Only THEN delegate source code reading** — last resort if docs and issues didn't resolve it

Do NOT delegate source code reading as the first debugging step.

**Pattern:**
```bash
# Step 1: Reproduce (direct Bash)
Bash("run command that triggers the error")

# Step 2 & 3: Delegate research (try the agy CLI first )
agy -p "Search official docs and GitHub issues for: <library> <error message>" \
  --dangerously-skip-permissions 2>&1
# fallback → researcher agent with WebSearch
```

</details>

<details>
<summary><strong>Error Handling & Retries</strong></summary>

**Let subagents handle retries:**

```python
# WRONG - Don't retry directly as orchestrator
bash_result = Bash(command="git commit -m 'feat: new'")
if failed:
    # Retry directly (context pollution)
    Bash(command="git pull && git commit")  # More context used

# CORRECT - Subagent handles retries
Use Gemini agent invocation with:
    workflow="copilot",
    description="Commit changes with retry",
    message="""
    Commit changes:
    Message: "feat: new feature"

    If commit fails:
    1. Pull latest changes
    2. Resolve conflicts if any
    3. Retry commit
    4. Handle pre-commit hooks

    Report final status: success or failure
    """
```

**Benefits:**
- Subagent context handles retries (not your context)
- Cleaner error reporting
- Automatic recovery attempts
- You get clean success/failure

</details>

---

## Subagent Budget-Pause Handling

<details>
<summary><strong>Pattern A: Auto-Resume Budget-Paused Subagents</strong></summary>

In some harness environments (notably VS Code devcontainer / agent-teams runtime), a delegated subagent may pause at a low tool budget and return an INTERMEDIATE, non-final message with NO completion. This is harness/runtime behavior, not a task failure. The message may be mid-sentence, lack a final report, or trail with "let me now…" — clear signs the work is not actually finished.

**Detection & Recovery:**
1. **Detect non-final return:** Message ends mid-step, no completion summary, no final SHA or deliverable list, trailing incomplete sentence
2. **DO NOT treat as done:** This is NOT a task failure; it's a pause condition
3. **DO NOT re-dispatch a fresh agent:** Re-dispatch loses all prior context and forces the agent to restart from scratch
4. **MUST resume the SAME agent via your harness's agent-resume mechanism** with a restated, explicit finish-line. Do NOT re-dispatch (that loses context). The exact primitive depends on your harness:
   
   **Claude Code:** Use `SendMessage` to send a continuation message to the paused agent by its `agentId` (available in the original task result):
   ```
   SendMessage({ to: <agentId> }, "Continue and finish the work. You paused mid-task. Complete the remaining steps: <restate exact deliverables>. Report final status with commit SHA or summary.")
   ```
   
   **Codex CLI:** Use Codex's subagent continuation mechanism (check your Codex version's documentation for re-engaging the same spawned agent instance to continue work without re-dispatch).
   
   **Gemini CLI:** Use Gemini's agent-messaging API to continue the paused agent (refer to your Gemini CLI docs for message-passing or agent-resume mechanisms).

5. **Expect multiple resume cycles:** May require 2-3 additional messages/resumes before a genuine final report is returned

**Why this matters:**
- Harness tool budgets are per-session — temporary, not permanent
- Resuming the same agent keeps context and avoids restarting
- Multiple resumes are normal and expected in this condition

**Pattern (harness-agnostic pseudocode):**
```
use the ... workflow described here
  → returns intermediate result, no completion
  
→ Resume(same_agent, "Continue and finish: <deliverables>")
  → returns partial progress
  
→ Resume(same_agent, "Still not done. Complete: <deliverables>. Report final SHA/summary.")
  → finally returns complete result
```

</details>

<details>
<summary><strong>Pattern B: Completion Gate for Subagent-Delegated Code</strong></summary>

When code for a work item was written by a delegated subagent, completing the item (`wipnote <type> complete <id>`) is DOUBLE-GATED. The orchestrator MUST perform both:

**(a) Run its OWN session-scoped quality gate:**
```bash
wipnote check --gate
```
A subagent's gate record does NOT count toward work-item completion — gate records are session-bound. You (the orchestrator in the main session) must run the gate yourself.

**(b) Pass explicit completion rationale with committed SHAs:**
```bash
wipnote feature complete <feat-id> --accepted-advisory "Subagent implementation verified. Commits: <SHA1>, <SHA2> (cite real SHAs from git log, no host paths)."
```
Subagent commits are not auto-linked to the work item in the current schema. You must cite the real commit SHAs in the completion advisory so reviewers can trace implementation back to the work.

**Why double-gating is necessary:**

The underlying defect is tracked in **bug-3718b630**: the harness/hook system currently lacks:
- Auto-linking of subagent commits to the work item they implement
- Orchestrator-visible gate records (subagent gates are session-local, not visible to orchestrator)

Both are durable fixes that belong in the binary and hooks, not in per-user guidance. Until those fixes land, completion is gated twice: (a) verifies code quality in orchestrator context, (b) documents the subagent's commits for future traceability.

**Example:**
```bash
# Subagent finishes: feat-abc
wipnote feature show feat-abc  # check commit history
git log --oneline --grep="feat-abc" | head -3
# Output: a1b2c3d feat: implementation detail
#         x9y8z7w docs: added guide

# Orchestrator runs quality gate
wipnote check --gate
# ✓ build, vet, tests pass

# Orchestrator completes with rationale
wipnote feature complete feat-abc --accepted-advisory \
  "Subagent implementation validated. Commits: a1b2c3d, x9y8z7w. Quality gate passed."
```

</details>

---

## Known Issues / Environment

**Devcontainer subagent budget-pause behavior:** In the VS Code devcontainer runtime, delegated subagents may pause at low tool budgets and return intermediate (non-final) results. This is harness/runtime behavior, not a code error. **See Pattern A (Auto-Resume) above** for detection and recovery steps.

**Subagent commit linkage gap (bug-3718b630):** Subagent commits are not auto-linked to the work item they implement, and subagent quality-gate records are session-local and invisible to the orchestrator. This blocks full automation of completion gates. **See Pattern B (Completion Gate) above** for the interim workaround (double-gating + explicit SHAs in advisory). The durable fix belongs in the binary and hooks.

**Codex exec sandbox failures in devcontainers (bwrap/bubblewrap):** In VS Code devcontainers and GitHub Codespaces, `codex exec` may fail immediately because the container lacks bubblewrap (bwrap) privileges — the nested session cannot run even `pwd`. Failure signatures: "bwrap", "bubblewrap", "Operation not permitted", "cannot create namespace". On ANY of these in `codex exec` output, treat the environment as permanently incompatible with nested Codex execution: skip `codex exec` for the rest of the session and delegate directly to in-harness agents (e.g. `feature-coder`). Do not retry.

**Full Go suite is silent ~6 min by design:** `go test ./...` buffers per-package output; `cmd/wipnote` (~320s from cold) prints first, so the run produces no output until it completes. Do not treat silence as a stall and do not kill the run — budget ≥10 min. Need progress? `go test -json ./...` or split: `go test ./internal/... && go test ./cmd/...`. (Cached runs finish in seconds.)

---

## Advanced: Post-Compact Persistence

<details>
<summary><strong>Orchestrator Activation After Compact</strong></summary>

**How it works:**

1. Before compact, SDK sets environment variable: `CLAUDE_ORCHESTRATOR_ACTIVE=true`
2. SessionStart hook detects post-compact state
3. Orchestrator Directives Skill auto-activates
4. This skill section appears automatically (first time post-compact)

**Why:** Preserve orchestration discipline after context compact

**What you see:**
- Skill automatically activates (no manual invocation needed)
- Quick start section visible by default
- Expand detailed sections as needed
- Full guidance available without re-reading docs

**To manually trigger:**
```
/orchestrator-directives
```

**Environment variable:**
```bash
CLAUDE_ORCHESTRATOR_ACTIVE=true  # Set by SDK
```

</details>

<details>
<summary><strong>Session Continuity Across Compacts</strong></summary>

**Features preserved across compact:**
- Work items in wipnote
- Feature/spike tracking
- Delegation patterns
- Model selection guidance
- This skill's guidance

**What's lost:**
- Your context (that's why compact happens)
- Intermediate tool outputs
- Local variables

**Re-activation pattern:**

```
Before compact:
- Work on features, track in wipnote
- Delegate with clear prompts
- Use SDK to save progress

After compact:
- Orchestrator Skill auto-activates
- Re-read recent spikes for context
- Continue delegations
- Use Task IDs for parallel coordination
```

</details>

---

## Multi-agent Git Isolation

When multiple agents or CLIs work on the same repository concurrently, follow this operating model to avoid Git index contention and interleaved commits:

**Source edits** must happen in **per-agent worktrees or isolated clones**, never in the shared main checkout. Create an isolated worktree for each agent:
```bash
wipnote yolo --feature <feat-id>   # creates a managed linked worktree
# or manually: git worktree add .claude/worktrees/<id> -b <branch>
```

**Metadata commits** (wipnote HTML artifacts, session data) are automatically serialized via the repo-scoped advisory lock (`runGitMutation` in feat-3f66d83f). This lock is safe from any worktree because it lives in the per-user cache directory, not in `.git/` or `.wipnote/`.

**Git lock file cleanup** is opt-in only — never automatic. Use `wipnote launcher git-lock --fix` (requires an age threshold and a no-live-writer check) to clean stale lock files. Do NOT remove `.git/index.lock` manually unless you have confirmed no process is writing.

**Diagnosis:** Run `wipnote launcher doctor` to check whether you're in the primary worktree (warns if so) or a properly isolated linked worktree.

---

---

## Architectural Memory

wipnote maintains a queryable store of architectural facts in `.wipnote/arch/` (cards with
kinds: `hazard`, `invariant`, `subsystem-map`, `decision`). The facts are relevance-filtered
into subagent prompts under a hard word budget. Using this store saves each coder agent
the 15-25 min research tax of re-deriving the same facts from code.

### Dispatch-Time Ritual (MANDATORY for every subagent dispatch)

Before composing a subagent prompt, run:

```bash
wipnote arch resolve --for <work-item-id>
# or for path-based queries:
wipnote arch resolve --for "cmd/wipnote/arch_cmds.go,internal/arch/"
```

Paste the output verbatim into the subagent's prompt under a heading like
`## Architectural context`. The output is already budget-capped (~450 words) and
annotated with UNVERIFIED drift markers. The subagent must treat UNVERIFIED cards
as advisory only and verify assumptions in code.

If no cards match, the command prints "No arch cards matched." — skip the heading entirely.

### Post-Completion Distillation (AFTER every work item completes)

When a subagent returns or you complete a work item yourself, distill durable learnings
into arch cards using one of two paths:

**Path A — Completion-time (recommended, single step):**
```bash
wipnote feature complete <feat-id> --learning "Body text: max 120 words." \
  --learning-kind invariant   # or: hazard, decision, subsystem-map
```
The `--learning` flag validates the body BEFORE marking done. A failed validation
aborts the completion with a clear error — the learning is never silently lost.

**Path B — Manual add (for learnings discovered outside completion):**
```bash
wipnote arch add <slug> --kind invariant --body "Body text." \
  --paths "cmd/wipnote/**" --links <work-item-id> --created-by "agent"
```

### Post-Completion Nudge

After a successful completion, wipnote prints drift-suspect arch cards whose globs
overlap the item's touched paths. Act on the nudge:

```bash
wipnote arch verify <slug>      # re-pins verified_at to HEAD; card is trustworthy again
wipnote arch edit <slug> --body "Updated body."   # update stale content then verify
```

### Trust Model

- Active cards (no drift marker): authoritative — include in prompt without caveat.
- UNVERIFIED cards (drift marker or empty verified_at): advisory — include but tell the
  subagent to verify assumptions in code.
- Retired/superseded cards: excluded from resolve output by default.

---

---

## Agent Teams vs Subagents

Claude Code v2.1.32+ ships an experimental **agent teams** feature where independent Claude instances self-claim work from a shared task list and message each other directly. This section helps you decide when to use teams vs traditional subagent delegation.

### Decision Criteria

| Dimension | Agent Teams | Subagents |
|-----------|-------------|-----------|
| **Ownership** | Parallel — each teammate claims tasks independently | Sequential — orchestrator dispatches one-at-a-time |
| **Communication** | Teammates message each other directly | Subagents report back to orchestrator only |
| **Best for** | Competing-hypothesis debugging, multi-lens review, feature ownership splitting | Sequential task chains, research→implement, isolated single-task work |
| **wipnote tracking** | Automatic — TeammateIdle/TaskCreated/TaskCompleted hooks fire per teammate | Manual — orchestrator attributes via `wipnote feature start/complete` |
| **Context isolation** | Each teammate has its own context window | Subagents inherit orchestrator's context model |
| **Cost model** | N teammates × full session cost | Orchestrator + N smaller subagent calls |

### Opt-In Requirements

Agent teams require explicit opt-in:

1. **Environment variable:** `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1`
2. **Minimum version:** Claude Code **2.1.32** or later
3. The wipnote plugin works with or without teams enabled — hooks gracefully no-op when no team is active

### How to Spawn a Team

There is no SDK API for teams. Spawn via natural language:

```
Create an agent team to <describe the work and how to divide it>
```

Claude Code will create teammates, assign them work from a shared task list, and let them coordinate directly.

### Caveats

- **`skills:` and `mcpServers:` frontmatter are NOT applied to teammates** — do not rely on skill injection or MCP servers in agent definitions used as teammates. Teammates run with base capabilities only.
- **No session resume** — teammates exit via the `exit-code-2` block-and-return contract; Claude Code's `/resume` is not currently wired through this path. If a teammate is blocked (e.g., by a quality gate), the teammate is stranded. Always provide manual recovery instructions in stderr.
- **One team per session** — you cannot spawn multiple teams in a single Claude Code session.
- **No nested teams** — a teammate cannot create its own team.
- **`/wipnote:execute` is unchanged** — the parallel dispatch skill continues to use subagents with worktree isolation. This plan does not convert it to use teams.

### Example Prompts

**1. Multi-lens PR review:**
```
Create an agent team: one teammate reviews for correctness,
one for performance, one for security. Each writes findings
to a shared review.md under their section heading.
```

**2. Competing-hypothesis debugging:**
```
Create an agent team to debug the flaky test in internal/hooks/.
One teammate investigates timing issues, one investigates state
pollution, one investigates resource contention. First to find
root cause messages the others.
```

**3. Feature ownership splitting:**
```
Create an agent team for track trk-XXXX. Each teammate claims
one unblocked feature and works it to completion. Use
wipnote feature start/complete for attribution.
```

### What wipnote Captures

When agent teams are active, wipnote automatically records:

- **Teammate identity** — every TeammateIdle, TaskCreated, and TaskCompleted event includes `teammate_name`
- **Step attribution** — feature steps are prefixed with `[teammate-name]` so `wipnote snapshot` shows who did what
- **Optional quality gate** — TaskCompleted can run build/test gates before allowing task completion. Opt-in via `.wipnote/config.json`:

```json
{
  "block_task_completion_on_quality_failure": true
}
```

> **WARNING:** Enabling the quality gate can strand teammates. Blocked teammates cannot be `/resume`d. When blocking occurs, stderr includes a manual recovery command: `wipnote feature complete <feature-id>`.

