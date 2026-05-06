---
name: strategic-planning
description: Use Erinn AI analytics to make smart work prioritization decisions. Activate when recommending work, finding bottlenecks, assessing risks, or analyzing project impact.
---

# Strategic Planning Skill

## When to Activate This Skill

**Trigger keywords:**
- "what should I work on", "recommend", "prioritize"
- "bottleneck", "blocking", "stuck"
- "risk", "impact", "dependencies"
- "strategic", "roadmap", "plan"

**Trigger situations:**
- Starting a new session (what to work on?)
- Multiple tasks available (which is most important?)
- Progress seems slow (what's blocking us?)
- Planning major changes (what's the impact?)

**CLI reference:** Run `erinn help` for available commands. Key commands:
- `erinn status` — project overview
- `erinn find features --status in-progress` — active work
- `erinn recommend` — AI-recommended next work

---

## Core Principle: Data-Driven Decisions

Erinn AI provides analytics that consider:
- **Dependencies** - What blocks/enables other work
- **Priority** - Business importance
- **Impact** - How many tasks are unlocked
- **Risk** - Circular deps, complexity
- **Parallelism** - What can run concurrently

---

## Quick Decision Framework

```bash
# 1. What should I work on? (recommendations)
erinn analytics summary

# 2. What's blocking progress?
erinn analytics summary

# 3. Project snapshot (status + WIP)
erinn snapshot --summary

# 4. Find in-progress work
erinn find features --status in-progress
```

---

## CLI Reference

### `erinn analytics summary`

Find tasks that block the most downstream work.

```bash
erinn analytics summary
```

**Use when:**
- Progress feels slow
- Many tasks are "blocked"
- Planning sprint priorities

---

### `erinn analytics summary`

Get scored recommendations considering all factors.

```bash
erinn analytics summary
```

**Scoring factors:**
- Priority weight (critical=100, high=75, medium=50, low=25)
- Blocks count (×10 per blocked task)
- No dependencies bonus (+20)
- Bottleneck bonus (+30)

---

### `erinn find features --status todo`

Find tasks that can run concurrently (no dependencies).

```bash
# All todo features
erinn find features --status todo

# All in-progress
erinn find features --status in-progress
```

**Use when:**
- Multiple agents available
- Want to speed up delivery
- Planning parallel sprints

---

### `erinn snapshot --summary`

Project health and status overview.

```bash
erinn snapshot --summary
```

**Use when:**
- Before major releases
- Sprint planning
- Health checks

---

## Decision Patterns

### Pattern 1: Start of Session

```bash
# Get project status overview
erinn status
erinn snapshot --summary
erinn analytics summary
```

---

### Pattern 2: Something Is Blocked

```bash
# Find what's causing the block
erinn analytics summary
erinn find features --status blocked
```

---

### Pattern 3: Planning Parallel Work

```bash
# Check what's ready (no dependencies)
erinn analytics summary
erinn find features --status todo
```

---

### Pattern 4: Review All Work

```bash
# See everything by status
erinn find features --status in-progress
erinn find features --status todo
erinn find bugs --status open
```

---

## Integration with Planning

Use CLI analytics to inform planning decisions:

```bash
# Get full picture before planning
erinn analytics summary
erinn snapshot --summary

# Use erinn plan generate to create formal plans
erinn plan generate <track-id>
```

---

## Best Practices

### DO

1. **Check bottlenecks first** - High-leverage work
2. **Use recommendations** - Considers all factors
3. **Assess risks before big changes** - Avoid surprises
4. **Analyze impact** - Understand consequences
5. **Check parallel capacity** - Optimize throughput

### DON'T

1. **Ignore blocked tasks** - They signal bottlenecks
2. **Skip risk assessment** - Before major releases
3. **Parallelize without analysis** - May cause conflicts
4. **Work on low-impact tasks** - When bottlenecks exist

---

## Quick Reference

```bash
# What's blocking us?
erinn analytics summary

# What should I do?
erinn analytics summary

# Project snapshot
erinn snapshot --summary

# Check status
erinn status

# Find in-progress work
erinn find features --status in-progress

# Find todo work (parallelizable candidates)
erinn find features --status todo
```

---

## Recommend Workflow

Run `erinn recommend [--top N]` (default N=5) for scored recommendations:

```bash
erinn recommend --top 5
```

After presenting output, analyze whether the top recommendations can run in parallel:

1. Check dependencies — do any recommended items block each other?
2. Check file overlap — do they modify the same files or modules?
3. If independent → propose `/wipnote:execute` for parallel dispatch
4. If dependent → identify critical path, propose sequential order

**Propose next action:**
- 2+ independent items → `/wipnote:execute` (parallel)
- Blocked items → start with the blocker using `/wipnote:plan <id>`
- Single item → delegate to appropriate agent tier (haiku/sonnet/opus)
