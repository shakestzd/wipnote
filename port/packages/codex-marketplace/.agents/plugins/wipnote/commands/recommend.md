# /wipnote:recommend

Get smart recommendations on what to work on next, including project health, bottleneck analysis, and parallel opportunities.

## Usage

```
/wipnote:recommend [--top N]
```

## Parameters

- `--top` (optional, default: 5): Number of recommendations to show

## Examples

```bash
/wipnote:recommend
```
Get top 5 recommendations with full analysis

```bash
/wipnote:recommend --top 10
```
Get top 10 recommendations

## Output

The command displays:
- **Project Health** — Counts of features, bugs, spikes, tracks by status
- **WIP Status** — Current in-progress work against your limit
- **Bottlenecks** — Stale or overloaded items blocking progress
- **Recommended Work** — Top N items scored by priority and impact
- **Parallel Opportunities** — Features grouped by track that can run in parallel

## Instructions

Copy this checklist and tick it off:

```
Recommend:
- [ ] 1. Recommendations fetched
- [ ] 2. Bottlenecks handled or named first
- [ ] 3. Dependencies and file overlap checked for the top items
- [ ] 4. One next action proposed
```

1. **Fetch.** Run `wipnote recommend --top N` (N is the user's choice, default 5). Present the output with light markdown formatting.
2. **Bottlenecks first.** If it lists bottlenecks (stale items, overloaded tracks), propose resuming, unblocking or closing them before new work.
3. **Check the top items.** For each pair, ask: does one block the other? Do they touch the same files or modules (`wipnote trace <id>` lists an item's files)? If you cannot tell, say so rather than guessing.
4. **Propose one next action:**
   - 2+ independent items with no file overlap: `/wipnote:execute` for parallel dispatch.
   - Dependent items: name the critical path and a sequential order, blocker first.
   - Work that needs design first: `/wipnote:plan`.
   - A single item: delegate by size to `patch-coder` (small, tightly scoped), `feature-coder` (ordinary feature) or `architect-coder` (cross-cutting design).

## Deeper analysis

| Question | Command |
|---|---|
| Project overview | `wipnote status`, `wipnote snapshot --summary` |
| Work by status | `wipnote find features --status in-progress` (also `todo`, `blocked`; `wipnote find bugs --status todo`) |
| Work type mix and maintenance burden | `wipnote analytics summary` |
| Throughput | `wipnote analytics velocity` (items completed per week, last 4 weeks) |
