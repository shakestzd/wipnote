---
name: erinn-tracker
description: ARCHIVED — Use erinn skill instead. Erinn AI workflow combining session tracking, orchestration, and parallel coordination.
---

<!-- ARCHIVED: This skill has been superseded by the erinn skill -->
<!-- Python SDK references removed — use Go CLI commands instead -->

# Erinn AI Tracker Skill (ARCHIVED)

> **This skill is archived.** Use `/erinn:erinn` for current workflow patterns.

---

## Core Workflow

```bash
# Session start
erinn status
erinn analytics summary

# Create and track work
erinn feature create "Title"
erinn feature start <feat-id>

# Mark complete
erinn feature complete <feat-id>
```

---

## Work Item Commands

```bash
# Features
erinn feature create "Title"
erinn feature start <feat-id>
erinn feature complete <feat-id>
erinn find features --status todo
erinn find features --status in-progress

# Bugs
erinn bug create "Title"
erinn bug start <bug-id>
erinn bug complete <bug-id>

# Spikes (investigation)
erinn spike create "Title"
erinn spike start <spike-id>
erinn spike complete <spike-id>

# Tracks (multi-feature initiatives)
erinn track new "Title"
```

---

## Analytics

```bash
erinn analytics summary
erinn analytics summary
erinn snapshot --summary
erinn find features --status todo
```

---

## Parallel Orchestration

Dispatch independent tasks in a single message:

```python
# All in one message = parallel execution
Task(subagent_type="erinn:gemini-operator", prompt="Research...")
Task(subagent_type="erinn:sonnet-coder", prompt="Implement feat-123...")
Task(subagent_type="erinn:sonnet-coder", prompt="Implement feat-456...")
```

See `/erinn:orchestrator-directives-skill` for complete patterns.

---

## Work Type Classification

Work type is inferred from work item ID prefix:
- `feat-*` → feature-implementation
- `spike-*` → spike-investigation
- `bug-*` → bug-fix
- `chore-*` → maintenance
