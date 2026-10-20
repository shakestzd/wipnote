---
name: diagnose
description: "Diagnose bugs, errors, and issues with root cause analysis. Use when asked to diagnose, debug, investigate, or find root cause of any problem — whether a wipnote bug ID, error message, unexpected behavior, or delegation audit."
user-invocable: true
disable-model-invocation: true
---

# /wipnote:diagnose

General-purpose diagnostic skill for investigating bugs, errors, and unexpected behavior.

## Usage

```
/wipnote:diagnose <bug-id>              # Investigate a specific bug
/wipnote:diagnose <error or symptom>    # Investigate an error or behavior
/wipnote:diagnose --delegation          # Audit delegation compliance
```

## Instructions

### Route by Input

**If given a bug ID** (matches `bug-*`):
1. Start attribution: `wipnote bug start <bug-id>`
2. Fetch bug details: `wipnote bug show <bug-id>`
3. Dispatch `wipnote-researcher` with the bug context (see Bug Investigation)
4. Present findings and suggested fix. Leave the bug open; it is done only when a fix lands.

**If given an error message or symptom**:
1. Create and start a bug: `wipnote bug create "<summary>" --track <trk-id>` then `wipnote bug start <id>`
2. Dispatch `wipnote-researcher` with the error context (see Bug Investigation)
3. Present findings and suggested fix. Leave the bug open; it is done only when a fix lands.

**If `--delegation` flag**:
1. Run the delegation audit (see Delegation Audit Mode below)

**If no arguments**:
1. Check project health: `wipnote recommend --top 3`
2. Identify bottlenecks, stale items, or anomalies
3. Suggest what to investigate

### Bug Investigation (Primary Mode)

Dispatch `call spawn_agent with agent_type "wipnote-researcher"` with a structured prompt:

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

After the agent reports back, present findings to the user in this format:

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

1. **Collect data**:
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
