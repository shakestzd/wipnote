---
name: strategic-planning
description: Uses wipnote analytics and recommendations to prioritize work. Use when choosing what to work on next, finding bottlenecks, assessing risk or analyzing project impact.
disable-model-invocation: true
---

# Strategic Planning Skill

## Commands

| Question | Command |
|---|---|
| What should I work on? What is stale or blocking? What can run in parallel? | `wipnote recommend [--top N] [--json]` — health snapshot, WIP vs limit, bottlenecks (stale items, overloaded tracks), top-N recommendations, parallel opportunities per track |
| Project overview | `wipnote status`, `wipnote snapshot --summary` |
| Work by status | `wipnote find features --status in-progress` (also `todo`, `blocked`; `wipnote find bugs --status todo`) |
| Work type mix and maintenance burden | `wipnote analytics summary` |
| Throughput | `wipnote analytics velocity` (items completed per week, last 4 weeks) |

## Workflow

1. Run `wipnote recommend --top 5`.
2. If bottlenecks are listed, deal with them first: resume, unblock, or close the stale items.
3. For the top recommendations, check:
   - Dependencies: does any item block another?
   - File overlap: do they touch the same files or modules?
4. Propose the next action:
   - 2+ independent items: `/wipnote:execute` for parallel dispatch.
   - Dependent items: name the critical path and propose a sequential order, blocker first.
   - Work that needs design first: `/wipnote:plan`.
   - A single item: delegate by size to `patch-coder` (small, tightly scoped), `feature-coder` (ordinary feature), or `architect-coder` (cross-cutting design).
