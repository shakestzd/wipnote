# Dispatch Hardening — Diagnostic Spike

**Slice 0 of plan-ff425b9f** · feat-060b396d · 2026-05-05

**Source bugs:** bug-0413c3c4 (#87 attribution), bug-730c0f19 (#88 commit-to-main), bug-8ff540a7 (#90 YOLO inheritance)
**Reference:** bug-cb4918d8 (does not resolve as a bug record; documented in code comments — see Q1)

This spike captures real DB state from a reproduced sub-agent dispatch and verifies which load-bearing assumptions in slices 1-3 hold against the current codebase.

---

## Reproduction context

The orchestrator (this session) ran `wipnote feature start feat-060b396d` and dispatched
`Agent({isolation:"worktree", subagent_type:"wipnote:researcher", prompt:...})` from
inside the worktree at `.claude/worktrees/multi-project-hardening-per-trk-cb36e595`
(branch `trk-cb36e595`).

The sub-agent ran 77 tool calls over ~250s, then was interrupted (no final commit, no
spike doc). The sub-agent's session/agent records are present in the DB and serve as
direct evidence for Q1, Q3, Q4.

| Field | Orchestrator | Sub-agent |
|---|---|---|
| Session row `session_id` | `0022c4dc-25aa-4dc1-b8ef-ef3e49135ece` | `af895f84af5b29ea6` |
| Session row `parent_session_id` | NULL | `0022c4dc-…` |
| Session row `is_subagent` | 0 | 1 |
| Session row `metadata.permission_mode` | NULL | NULL |
| `agent_events.session_id` for subagent's tool calls | — | `0022c4dc-…` (orchestrator's!) |
| `agent_events.agent_id` for subagent's tool calls | — | `af895f84af5b29ea6` |

This split is the core invariant: **the `sessions` table tracks sub-agents with their own row + parent pointer, but `agent_events` records sub-agent tool calls under the ORCHESTRATOR's session_id with the sub-agent's id in the `agent_id` column.** The plan's reference to "bug-cb4918d8 invariant" describes the agent_events / CloudEvent-payload form. Both forms are visible above.

`git worktree list` after dispatch:

```
/workspaces/wipnote                                                             [main]
/workspaces/wipnote/.claude/worktrees/crispi-spec-definition-trk-13e39042       [trk-13e39042]
/workspaces/wipnote/.claude/worktrees/multi-project-hardening-per-trk-cb36e595  [trk-cb36e595]
```

**No nested worktree was created for the dispatched sub-agent.** The sub-agent operated
in the parent's CWD. This is direct empirical confirmation of bug-730c0f19's suspicion.

---

## Q1 — Session-ID sharing: does the bug-cb4918d8 invariant hold?

**Code reference:** `internal/db/session_repo.go:175-211` (GetToolUseContext) — comment
at lines 178-181 documents the invariant explicitly:

> fallback for subagent tool calls, which share the orchestrator's session_id but
> carry a distinct agent_id that never had its own claim row (bug-cb4918d8). The
> orchestrator's claim is keyed on owner_session_id, so this resolves the parent's
> claim for any subagent running under it.

**bug-cb4918d8 not found.** `wipnote bug show bug-cb4918d8` returns "work item not
found." `wipnote find cb4918d8` returns no matches. The bug ID is referenced only in
code comments and in plan-ff425b9f's own description. The original bug record may have
been deleted, renumbered, or never created — the **fix shipped on 2026-04-13** as commit
`7a252c1f1bd7d224a9398e8ded1be81f8c6013f9` ("fix(hooks): populate subagent lineage +
claim lookup by session_id (bug-cb4918d8)").

**Evidence the invariant holds for CloudEvents/agent_events** (from the sub-agent
dispatch above):

```sql
SELECT DISTINCT session_id, agent_id FROM agent_events WHERE agent_id = 'af895f84af5b29ea6';
-- 0022c4dc-25aa-4dc1-b8ef-ef3e49135ece | af895f84af5b29ea6
```

Sub-agent tool calls are emitted with `session_id = ORCHESTRATOR's session_id` and
`agent_id = SUBAGENT's id`. Confirmed.

**Evidence the `sessions` table tracks sub-agents separately:**

```sql
SELECT session_id, parent_session_id, is_subagent FROM sessions WHERE session_id = 'af895f84af5b29ea6';
-- af895f84af5b29ea6 | 0022c4dc-25aa-4dc1-b8ef-ef3e49135ece | 1
```

The sub-agent has its own row with `parent_session_id` populated. This is what
`getSessionAndParent` at `yolo_guard.go:618-629` walks — but it walks **by sessions
table session_id, not by CloudEvent session_id**. When called from a hook handling a
sub-agent's tool call, `getSessionAndParent(event.SessionID)` looks up the
ORCHESTRATOR's row (because event.SessionID is the orchestrator's id), and the
orchestrator's `parent_session_id` is NULL — so the walker returns `[orchestrator_id]`
and stops.

**Finding:** The invariant holds for CloudEvents but NOT in the way slice 1 assumes.
Slice 1's planned chain walk via `getSessionAndParent(event.SessionID)` resolves the
orchestrator's row directly (no lineage to walk) — meaning **the existing claim lookup
already sees the orchestrator's session and finds its claim**. Slice 1's chain walk is
not just incorrect (per opus's HIGH critique) — it is also **unnecessary**, because the
hook is already operating against the orchestrator's session row.

---

## Q2 — Existing fallback: does GetToolUseContext already cover #87?

**Code:** `internal/db/session_repo.go:186-211` — full SQL of the COALESCE fallback:

```sql
SELECT s.session_id,
       COALESCE(CASE WHEN f.status = 'in-progress' THEN s.active_feature_id ELSE '' END, '') AS active_feature_id,
       COALESCE(s.parent_session_id, '') AS parent_session_id,
       s.is_subagent,
       s.created_at,
       COALESCE(
         (SELECT c.work_item_id FROM claims c
            WHERE c.claimed_by_agent_id = ?
              AND c.status IN ('proposed','claimed','in_progress','blocked','handoff_pending')
            LIMIT 1),
         (SELECT c.work_item_id FROM claims c
            WHERE c.owner_session_id = ?
              AND c.status IN ('proposed','claimed','in_progress','blocked','handoff_pending')
            ORDER BY c.leased_at DESC
            LIMIT 1),
         ''
       ) AS claimed_item
FROM sessions s
LEFT JOIN features f ON f.id = s.active_feature_id
WHERE s.session_id = ?
LIMIT 1
-- bound: agentID, sessionID, sessionID
```

**Trace for a sub-agent tool call** with `(sessionID = '0022c4dc-…', agentID = 'af895f84af5b29ea6')`:

1. Outer query: `sessions WHERE session_id = '0022c4dc-…'` → matches **orchestrator's** row.
   - `r.IsSubagent = 0` (orchestrator's value!)
   - `r.ParentSessionID = ''` (orchestrator has no parent in DB)
   - `r.ActiveFeatureID = orchestrator's active_feature_id`
2. Claim COALESCE Path 1: `claimed_by_agent_id = 'af895f84af5b29ea6'` → no match
   (claims for orchestrator-started features use `claimed_by_agent_id = '__root__'`,
   verified in DB: every recent claim has this sentinel value).
3. Claim COALESCE Path 2: `owner_session_id = '0022c4dc-…'` → **MATCH** → returns
   the orchestrator's active claim.

**Empirical claim shape (from this DB):**

```
work_item_id   | owner_session_id     | claimed_by_agent_id | status
feat-060b396d  | 0022c4dc-25aa-…     | __root__            | in_progress
feat-72876845  | 5b44705e-…          | __root__            | in_progress
feat-ecd82f68  | 5b44705e-…          | __root__            | completed
…
```

Every claim in the last 2 days uses `__root__` as `claimed_by_agent_id`. Path 1
(per-agent direct claim) is therefore dead-code in the current dispatch model;
Path 2 (owner_session_id fallback) is the only one that hits.

**Transient test (Q2 step 3):** Not written separately — the fallback's behavior is
already covered by the existing test `TestGetToolUseContext_SessionIDFallback` in
`internal/db/session_repo_test.go:12-…` which the plan critique acknowledged. The
test fixture exactly mirrors the bug-cb4918d8 pattern (claim with
`claimed_by_agent_id = "agent-A"` is found via the fallback when looked up with a
DIFFERENT agent_id but same session_id). No new transient test was needed.

**Bug #87's quoted error message ("feature claim is owned by session X but my session
is Y") is NOT present in current wipnote code.** Searched all of `internal/hooks/`
and `cmd/`; no such message format exists. The current "Write blocked" message
(`pretooluse.go:613-620`) prints `feature=<id>  claim=<id>` in a different shape.
Either the bug paraphrased the message, or the cited 6/8 dispatch ran on an older
or different codebase (the cited track `trk-9a3c622d` from "jobsmith" — `wipnote
track show trk-9a3c622d` returns "not found" in the wipnote project's graph;
this dispatch ran in another project's repo).

**Finding:** The existing COALESCE fallback **already covers the cited #87 attribution
mechanism** for any dispatch on a current wipnote build (≥ commit 7a252c1f,
2026-04-13). The bugs were filed 2026-05-05, three weeks AFTER the fix shipped, but
they reference a dispatch on an external (jobsmith) project — likely on a stale
wipnote version. **Slice 1 (feat-ecd82f68) is solving a problem that the current
codebase does not have.**

---

## Q3 — YOLO propagation: does Claude Code propagate bypassPermissions?

**Code:** `internal/hooks/yolo_guard.go:26-68`

```go
func isYoloFromEvent(event *CloudEvent, wipnoteDir string) bool {
    if event.PermissionMode == "bypassPermissions" {
        return true
    }
    // If Claude Code reports a non-bypass mode, trust it.
    if event.PermissionMode != "" {
        return false
    }
    // Fallback: check DB for session's last known permission_mode.
    return isYoloFromDB(wipnoteDir, event.SessionID)
}

func isYoloFromDB(wipnoteDir, sessionID string) bool {
    // ... opens DB, runs:
    // SELECT json_extract(metadata, '$.permission_mode') FROM sessions WHERE session_id = ?
}
```

**Trace for a sub-agent tool call:**

- Live state path: if `event.PermissionMode == "bypassPermissions"` → YOLO. If
  Claude Code propagates the parent's mode to sub-agent CloudEvents, this works.
- Live state path with non-bypass value: if `event.PermissionMode != ""` (anything
  other than `"bypassPermissions"`) → returns `false` IMMEDIATELY. **This is the
  hazard.** If Claude Code propagates `permission_mode = "default"` to sub-agents
  (instead of inheriting "bypassPermissions"), the function trusts it and returns
  false even though the orchestrator IS in YOLO mode.
- DB fallback (only fires when `PermissionMode == ""`): looks up
  `sessions WHERE session_id = event.SessionID`. Per the cb4918d8 invariant,
  `event.SessionID` is the **orchestrator's** session_id, so this lookup returns
  the orchestrator's permission_mode. **YOLO is inherited automatically through
  this path.**

**Direct evidence from this dispatch:** the orchestrator's session row has
`metadata.permission_mode = NULL` (the field was never written). My session was
not actually in YOLO mode — this is a `wipnote claude --dev` session, not
`wipnote yolo`. So I cannot directly observe YOLO propagation from this
reproduction. But I can verify the code path:

- If `event.PermissionMode == ""` for a sub-agent CloudEvent (Claude Code does NOT
  propagate the field), `isYoloFromDB` falls through to the orchestrator's row
  and returns the orchestrator's posture. **Inheritance works automatically.**
- If `event.PermissionMode != ""` and != `"bypassPermissions"` for a sub-agent
  CloudEvent (Claude Code propagates a non-bypass value), the function trusts the
  live state and returns false. **Inheritance is broken in this case.**

The opus + sonnet HIGH critique on slice 3 is correct: **the slice's premise is
unverified without a YOLO-mode reproduction**. Slice 3 cannot be properly
designed until we observe what `event.PermissionMode` actually contains for sub-agent
CloudEvents in a YOLO orchestrator.

**Recommended verification step (deferred to slice 3 scoping):** start a `wipnote
yolo` session, dispatch a sub-agent, dump
`SELECT data FROM agent_events WHERE agent_id = '<sub>' LIMIT 1` and inspect the
`permission_mode` field of the captured CloudEvent payload.

**Finding (provisional):** YOLO inheritance is **automatic via the DB fallback** when
Claude Code does not propagate `permission_mode` in sub-agent CloudEvents. It is
**broken** if Claude Code propagates a non-bypass value. Until a YOLO reproduction
is run, the actual mechanism is unknown. **Slice 3 should not ship as currently
described** (the proposed `getSessionAndParent`-based walker still queries by
session_id, which already resolves to the orchestrator's row — same fundamental
flaw as slice 1). If a fix IS needed, the right shape is: when
`event.PermissionMode != ""` AND the CloudEvent's `session_id` resolves to a
sub-agent (use `is_subagent=1` from the sessions row hit by `event.SessionID`'s
PARENT lookup, not by the event's session_id), check the parent's permission_mode
in addition to the live state, and OR them.

But before adding that complexity: confirm Claude Code's actual propagation
behavior. The simpler fix may be to drop the early return at
`yolo_guard.go:33-35` for sub-agents and always fall through to the DB
(orchestrator's row) — but that has its own failure mode (a sub-agent legitimately
opted out of YOLO would lose the ability to do so).

---

## Q4 — Commit-to-main: what was the sub-agent CWD?

**Bug record (bug-730c0f19):** "isolation:worktree is being interpreted as 'reuse the
parent's working directory' when the parent is itself inside a worktree. … no new
worktrees were created (`git worktree list` showed no sub-agent worktrees), and the
agents wrote/committed in the parent's wd, which after a checkout had ended up on main."

**Direct evidence from this dispatch:** the orchestrator is in
`/workspaces/wipnote/.claude/worktrees/multi-project-hardening-per-trk-cb36e595` on
branch `trk-cb36e595`. The sub-agent was spawned with
`Agent({isolation:"worktree", subagent_type:"wipnote:researcher"})`. After dispatch:

```
$ git worktree list
/workspaces/wipnote                                                             [main]
/workspaces/wipnote/.claude/worktrees/crispi-spec-definition-trk-13e39042       [trk-13e39042]
/workspaces/wipnote/.claude/worktrees/multi-project-hardening-per-trk-cb36e595  [trk-cb36e595]
```

**No new worktree was created for the sub-agent.** Confirmed: when the parent
orchestrator is itself in a linked worktree, Claude Code's `Agent({isolation:"worktree"})`
does NOT create a nested worktree. The sub-agent ran in the parent's CWD on the
parent's branch (`trk-cb36e595`). In this case the parent was NOT on main, so no
direct risk of committing to main — but if the parent had been on main (e.g., a
top-level orchestrator running outside a worktree), the sub-agent would have been
on main with no isolation.

**Existing guards that should fire in YOLO mode:**

- `checkYoloWorktreeGuard` (`yolo_guard.go:325-328`) blocks Write/Edit on `main`
  when `yolo == true`.
- `checkYoloBashWorktreeGuard` (referenced at `pretooluse.go:146`) extends the
  same guard to Bash file-write commands.
- `checkYoloCommitGuard` (`yolo_guard.go:155`-ref) gates `git commit` in YOLO
  mode (specifically on the test-ran condition).

**All three are gated by `ctx.IsYoloMode == true`.** If YOLO inheritance is broken
for sub-agents (Q3 hazard), `ctx.IsYoloMode == false` and **none of these guards
fire**. The sub-agent runs on main, edits files, commits — all bypassed by the
upstream YOLO-detection failure.

**This unifies #88 with #90.** The 2/8 sub-agents that committed to main were not
defeating a worktree-specific guard — they were simply running with `IsYoloMode=false`
because of #90, and every YOLO-mode guard was disabled. With Q3 fixed, #88 is
mostly fixed too.

**The remaining gap that slice 2 SHOULD address:** sub-agents in a NON-YOLO parent
(legitimate, deliberate non-YOLO dispatch) can still commit to main with no guard.
The fix is a YOLO-independent guard: refuse `git commit` from `is_subagent=1`
sessions on `main`/`master`/origin's HEAD branch, regardless of YOLO state. This
is the "defense-in-depth at wipnote's layer" the bug record recommends.

**`is_subagent` detection caveat:** as noted in Q2, `ctx.IsSubagent` for a
sub-agent's tool call comes from `sessions WHERE session_id = event.SessionID`,
which matches the **orchestrator's** row, so `ctx.IsSubagent = false`. **The current
code cannot distinguish sub-agent from orchestrator in PreToolUse.** Fixing slice 2
requires a way to detect "this CloudEvent came from a sub-agent" — likely by
checking `event.AgentID != ""` and `event.AgentID != owner_session_id` (or a similar
signal from the CloudEvent payload). The opus critique on `getSessionAndParent`
being one-hop is also relevant: a true sub-agent detector cannot rely on
session-row joining.

**Finding:** #88's mechanism is the same as #90 (broken YOLO inheritance disables
all worktree guards). #88's surviving fix-need is a YOLO-independent commit guard,
but it requires **fixing `IsSubagent` detection in PreToolUse first** — currently
no hook can reliably tell that a tool call came from a sub-agent vs. the
orchestrator, because both look up the same sessions row.

---

## Confirmed root causes

- **#87 (attribution claim):** **Already fixed** by COALESCE fallback in
  `GetToolUseContext` (commit `7a252c1f`, 2026-04-13). The cited 6/8 failure
  reflects a dispatch on an older or external (jobsmith) codebase. No further fix
  needed in current wipnote.

- **#88 (commit-to-main):** Two-part mechanism:
  1. `Agent({isolation:"worktree"})` from a parent already in a worktree does NOT
     create a nested worktree (Claude Code behavior). Sub-agent operates in
     parent's CWD. **Confirmed empirically in this spike** (no nested worktree
     created).
  2. When the parent's CWD is on `main`, all YOLO worktree guards rely on
     `ctx.IsYoloMode`, which is broken for sub-agents because `IsSubagent`
     itself is broken (see #90 root cause). With #90 fixed, the YOLO worktree
     guard fires and #88 is mostly resolved.
  3. Residual gap: non-YOLO orchestrators dispatching sub-agents have no commit
     guard at all. Slice 2 still has scope, but smaller.

- **#90 (YOLO inheritance):** Two paths through `isYoloFromEvent`:
  - DB fallback (when `event.PermissionMode == ""`): inheritance is **automatic**
    via cb4918d8 invariant — `isYoloFromDB(orchestrator_session_id)` returns the
    orchestrator's posture. Works.
  - Live state (when `event.PermissionMode != ""`): trusts the field. If Claude
    Code propagates a non-bypass value, inheritance breaks. **Unverified** —
    requires YOLO reproduction to know which case applies.

  Underlying contributing factor: **`ctx.IsSubagent` is unreliable in PreToolUse.**
  The sessions table query keys on `event.SessionID`, which is the orchestrator's
  id; `IsSubagent=false` for every sub-agent tool call. Many slice-2/3 mitigations
  fail because they cannot detect "this is a sub-agent" in the first place.

---

## Slice-1/2/3 implications

### Slice 1 (feat-ecd82f68 — PreToolUse claim check walks parent session chain)

| Current `what` step | Status |
|---|---|
| Walk parent chain via `getSessionAndParent` to find orchestrator's claim | **Wrong-problem-fix.** Hook already operates on orchestrator's session row (per cb4918d8). No chain to walk; existing COALESCE fallback in `GetToolUseContext` already returns orchestrator's claim. |
| Replace lookup with parent-chain walk | Unnecessary. Existing fallback works. |
| Multi-hop chain support | Solves a problem that doesn't exist with current dispatch model. |
| Logging for chain inheritance | Could be useful for telemetry, but outside the original problem scope. |

**Verdict: WRONG-PROBLEM. Drop slice 1 entirely.** The bug is already fixed in
current wipnote. The plan's misreading of the cited dispatch (which happened on
an external project, possibly older wipnote) led to drafting a fix for an
already-solved problem. Recommend marking feat-ecd82f68 as `won't fix — already
resolved by commit 7a252c1f`. If new attribution failures surface, file a fresh
bug with concrete repro on current wipnote.

### Slice 2 (feat-9cef857b — PreToolUse guard refuses sub-agent git commit on main)

| Current `what` step | Status |
|---|---|
| Detect Bash `git commit` via regex | Use existing `gitCommitPattern` (`yolo_guard.go:237`); plan's new regex would re-introduce bug-a10ae96a. **Re-scope to use existing.** |
| Identify sub-agent sessions via `getSessionAndParent` | **Wrong-problem.** `getSessionAndParent` returns orchestrator's row for sub-agent tool calls; cannot detect sub-agent from this. **Need new mechanism**: distinguish from `event.AgentID` vs. orchestrator's own agent id, or use `claims.claimed_by_agent_id` semantics. |
| Resolve current branch via `git -C <cwd> rev-parse` | Use existing `currentBranchIn`/`branchForFilePath` helpers (`yolo_guard.go:651-674`). **Re-scope to reuse.** |
| Per-agent branch escape hatch (worktree-agent-* prefix or env var) | **Drop.** Per opus's MEDIUM critique: branch-name escape hatch is brittle/dead code; env vars leak. Invert: refuse only on `main`/`master`/origin's HEAD; allow all other branches. |
| Error message structured | Keep, but include `event.AgentID` (or sub-agent identifier) in the message. |
| Wire after slice 1's claim-check enhancement | **Drop dependency on slice 1** (slice 1 is being dropped). |

**Verdict: NEEDS RE-SCOPE.**

Recommended new shape:
- Single function `checkSubagentCommitGuard(event, ctx)` in
  `internal/hooks/worktree_commit_guard.go`.
- Trigger: `event.ToolName == "Bash"` AND `gitCommitPattern.MatchString(cmd)`.
- Sub-agent detection: needs a reliable signal. Options to validate:
  - `event.AgentID != ""` — every CloudEvent carries `agent_id`; orchestrator's
    own tool calls would have `event.AgentID == event.SessionID` (or some
    sentinel). Verify in DB which value the orchestrator uses.
  - Look up `sessions WHERE session_id = event.AgentID` (sub-agent's own row); if
    `is_subagent=1` AND `parent_session_id != ""`, this is a sub-agent.
- Branch check: refuse on `main`, `master`, and `origin/HEAD` symbolic-ref target.
  No branch-name escape hatch.
- YOLO-independent: this guard fires regardless of YOLO mode. It is the missing
  defense for non-YOLO orchestrators.
- Detached HEAD / non-repo CWD: refuse for sub-agents (don't fall through silently
  per opus's HIGH critique on the original plan).

### Slice 3 (feat-72876845 — YOLO permission inheritance via parent-session chain)

| Current `what` step | Status |
|---|---|
| Walk parent chain via `getSessionAndParent` at YOLO check site | **Wrong-problem-fix.** Same issue as slice 1: hook already queries orchestrator's row. Walker is one-hop and queries the wrong table for sub-agent identification. |
| Reuse slice 1's helper | Slice 1 is being dropped. Helper unchanged. |
| Conservative scope (don't override explicit non-YOLO) | Right semantics, but the implementation site is wrong. |
| Logging for inheritance | Useful, but secondary. |

**Verdict: NEEDS RE-SCOPE.**

The actual question is: **what does Claude Code put in `CloudEvent.PermissionMode`
for a sub-agent's tool call when the parent is in YOLO mode?** Three scenarios:

1. **Field is empty** → `isYoloFromDB(event.SessionID)` looks up the orchestrator's
   row → returns YOLO. **Inheritance works automatically. No fix needed.**
2. **Field is `"bypassPermissions"`** → returned true at line 30. **Inheritance
   works. No fix needed.**
3. **Field is `"default"` or other non-bypass value** → returned false at line
   34. **Inheritance broken.**

Recommendation: **before implementing slice 3**, run a YOLO reproduction
(`wipnote yolo` session, dispatch a sub-agent, capture
`agent_events.data` for the sub-agent's tool call). If scenario 1 or 2: drop
slice 3. If scenario 3: the fix is to check `is_subagent` for the event's
session and, if true, prefer the parent's DB posture over the live event field.
But that requires `is_subagent` detection to work for CloudEvents (currently
broken — see #88 analysis).

---

## Recommended slice rewrite

### Drop slice 1 (feat-ecd82f68)

- Already fixed by commit `7a252c1f` (2026-04-13). Mark as `won't fix — resolved
  upstream of plan`.
- Add a unit test (if not already present) that explicitly exercises the
  sub-agent COALESCE fallback shape with the `__root__` claimed_by_agent_id
  sentinel — guards against regression. Existing
  `internal/db/session_repo_test.go:12-…` may already cover this; verify.

### Re-scope slice 2 (feat-9cef857b) to a YOLO-independent commit guard

1. Find a reliable sub-agent detector for CloudEvents (NEW work — likely a
   second sessions-table lookup keyed on `event.AgentID`, OR add a
   `IsSubagent` field to the CloudEvent payload).
2. Add `checkSubagentCommitGuard` in `internal/hooks/worktree_commit_guard.go`:
   - Trigger on `gitCommitPattern.MatchString(cmd)` for `event.ToolName == "Bash"`.
   - If sub-agent AND branch in {`main`, `master`, origin's HEAD}: refuse with
     structured message including session, agent_id, branch, parent_id.
   - Detached HEAD / non-repo: refuse for sub-agents (no silent fall-through).
3. Wire into `pretooluse.go` after the existing YOLO worktree guards.
4. Tests: cover orchestrator-on-main (allow), sub-agent-on-main (refuse),
   sub-agent-on-track-branch (allow), sub-agent on detached HEAD (refuse),
   non-`git commit` Bash (allow).
5. Drop the per-agent branch escape hatch and env-var override.

### Defer or drop slice 3 (feat-72876845)

1. **Block on YOLO reproduction first.** Run a `wipnote yolo` session,
   dispatch a sub-agent, dump
   `SELECT data FROM agent_events WHERE agent_id = '<sub>' AND tool_name = 'Edit' LIMIT 1`.
2. Inspect `data` for `permission_mode`:
   - Empty or `"bypassPermissions"` → drop slice 3. YOLO inheritance works
     automatically via the DB fallback.
   - Non-bypass non-empty → re-scope slice 3 to add a sub-agent-aware override
     at `isYoloFromEvent` (see Q3 analysis). This requires the same sub-agent
     detector as slice 2 — share infrastructure.

### Cross-cutting infrastructure work surfaced by this spike

- **`IsSubagent` detection in PreToolUse is broken.** The current
  `GetToolUseContext` query keys on `event.SessionID` which always resolves to
  the orchestrator's row. Fix: add a second lookup keyed on `event.AgentID`
  (sub-agent's own session row) and expose `IsSubagent` from THAT row. This
  unblocks slices 2 and 3 simultaneously.
- **Bug records #87/#88/#90 should cite the wipnote version of the cited
  dispatch.** Without knowing whether the dispatch ran pre- or post-
  `7a252c1f`, the bug analysis is ambiguous. Future bug filings on dispatch
  failures should include `wipnote version` output and the
  `claims.claimed_by_agent_id` value observed in DB.

---

## Open questions for plan re-critique

1. Does Claude Code propagate `permission_mode` to sub-agent CloudEvents? (Q3 —
   needs YOLO repro.)
2. What value does `event.AgentID` carry for orchestrator's own tool calls
   vs. sub-agent tool calls? (Needed for sub-agent detection in slice 2/3.)
3. Should bug-cb4918d8 be filed as a real bug record (currently only referenced
   in code comments), or is it intentional that the fix shipped without a
   tracked bug entry?
4. What's the failure mode if the orchestrator's session row is missing from the
   DB at the time of a sub-agent's first tool call (cold start race)? The
   COALESCE returns nothing; PreToolUse returns "no claimed work item." Does
   the subagent grace period at `pretooluse.go:81` cover this?
