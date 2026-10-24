---
name: diagnose
description: "Finds the root cause of a bug, error, or unexpected behavior by dispatching a researcher, and audits delegation compliance. Use when asked to diagnose, debug, or investigate a wipnote bug ID, an error message, or a symptom."
user-invocable: true
disable-model-invocation: true
---

# /wipnote:diagnose

## Usage

```
/wipnote:diagnose <bug-id>              # Investigate a specific bug
/wipnote:diagnose <error or symptom>    # Investigate an error or behavior
/wipnote:diagnose --delegation          # Audit delegation compliance
```

## Instructions

Copy this checklist and tick it off (bug or symptom input):

```
Diagnose (<bug-id or symptom>):
- [ ] 1. Input routed; bug started (created first for a symptom)
- [ ] 2. Researcher dispatched with the structured prompt
- [ ] 3. Root cause verified: cited file:line read and it explains the symptom
- [ ] 4. Root cause recorded with set-description
- [ ] 5. Findings presented; bug left open
```

### Route by Input

**If given a bug ID** (matches `bug-*`):
1. Start attribution: `wipnote bug start <bug-id>`
2. Fetch bug details: `wipnote bug show <bug-id>`
3. Dispatch `wipnote:researcher` with the bug context (see Bug Investigation)
4. Present findings and suggested fix. Leave the bug open; it is done only when a fix lands.

**If given an error message or symptom**:
1. Create and start a bug: `wipnote bug create "<summary>" --track <trk-id>` then `wipnote bug start <id>`
2. Dispatch `wipnote:researcher` with the error context (see Bug Investigation)
3. Present findings and suggested fix. Leave the bug open; it is done only when a fix lands.

**If `--delegation` flag**:
1. Run the delegation audit (see Delegation Audit Mode below)

**If no arguments**:
1. Check project health: `wipnote recommend --top 3`
2. Identify bottlenecks, stale items, or anomalies
3. Suggest what to investigate

### Bug Investigation (Primary Mode)

Dispatch `Agent(subagent_type="wipnote:researcher")` with a structured prompt:

```text
## Bug: <bug-id> — <title>

### Symptom
<description from bug or user>

### Investigation Steps
1. Search codebase for relevant code paths
2. Read the source files involved
3. Identify the root cause with file paths and line numbers
4. Check if the issue affects other cases beyond the reported one
5. Verify current state from canonical files (`.wipnote/` HTML, plan YAML, ledgers, per-session NDJSON) and git, or via `wipnote` query commands — there is no project database to query
6. Propose a fix (describe what to change, don't implement)

### Report Format
- **Root cause**: What's wrong and where (file:line)
- **Blast radius**: Does this affect other cases?
- **Suggested fix**: What code change resolves it
- **Verification**: How to confirm the fix works

### Lifecycle
Diagnosis only. Record the root cause with `wipnote bug set-description <bug-id> "..."`.
Do not run `wipnote bug complete`: the bug stays open until a fix lands.
```

After the agent reports back, verify before presenting. Read the cited `file:line` yourself and check that it explains the symptom. If the report has no location, or the code does not explain the symptom, send the researcher back with the specific gap. Repeat until the root cause holds up.

Then present findings to the user in this format:

```markdown
## Diagnosis: <bug-id>

**Root cause:** <one-line summary>
**Location:** `<file>:<line>`
**Blast radius:** <scope of impact>

### Details
<agent's detailed findings>

### Suggested Fix
<what to change>

### Next Steps
- [ ] Implement fix
- [ ] Run the gate from `/wipnote:code-quality-skill`
- [ ] Verify: <specific verification command>
```

### Delegation Audit Mode (--delegation)

When `--delegation` is specified, audit the current session's delegation compliance:

1. **Collect data** (needs `jq`; if it is missing, read `events.ndjson` with the Read tool and count `tool_decision` events by `attrs.tool_name`):
```bash
wipnote status
# Tool counts for the most recent session, read from its canonical event log.
# The per-session NDJSON is the record — there is no agent_events table and no
# project database; the SQLite projection is in-memory and dies with the command.
SESSION_DIR=$(ls -td .wipnote/sessions/*/ | head -1)
jq -r 'select(.canonical=="tool_decision") | .attrs.tool_name // empty' \
  "$SESSION_DIR/events.ndjson" | sort | uniq -c | sort -rn
```

2. **Compute score**: `delegations / (delegations + direct_impl) * 100`
   - Delegations = Task + Agent calls
   - Direct impl = Edit + Write calls

3. **Present report**:
```markdown
## Delegation Diagnostic

### Score: X% (N/M actions delegated)

### Gaps Found
| Time | Tool | Action | Should Use |
|------|------|--------|------------|
| ... | ... | ... | ... |

### Recommendations
1. ...
```
