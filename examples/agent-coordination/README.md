# Agent Coordination Example

Multi-agent collaboration using wipnote for coordinated software development.

## Overview

This example demonstrates how multiple AI agents can work together on interdependent features, showing:
- **Agent claiming and releasing** - Exclusive access to features
- **Dependency management** - Automatic blocking until prerequisites complete
- **Strategic recommendations** - SDK suggests highest-impact work
- **Parallel work discovery** - Find tasks that can run simultaneously
- **Bottleneck detection** - Identify features blocking others

## The "Ijoka" Use Case

This replicates the original Ijoka vision: multiple AI agents collaborating on a software project through a shared graph database, but using HTML files instead of Neo4j.

## Quick Start

```bash
# From the examples/agent-coordination directory
python demo.py
```

## What It Demonstrates

### 1. Feature Dependencies

The demo creates a realistic dependency graph:

```
Database Schema (Agent 1)
    ├─> Authentication API (Agent 1)
    │       └─> User Management (blocked - needs auth)
    └─> Product Catalog (Agent 3)
            └─> Admin Dashboard (blocked - needs products + users)
```

### 2. Agent Claiming

Agents claim features to prevent conflicts:

```python
# Agent claims a feature
sdk.features.claim(feature_id, agent="backend-agent")

# Feature is now exclusively assigned
with sdk.features.edit(feature_id) as f:
    f.agent_assigned  # "backend-agent"
    f.claimed_at      # timestamp
    f.status          # "in-progress"
```

### 3. Strategic Recommendations

The SDK recommends what to work on next:

```python
recs = sdk.recommend_next_work(agent_count=3)

for rec in recs:
    print(f"{rec['title']} (score: {rec['score']})")
    print(f"Reasons: {rec['reasons']}")
    print(f"Unlocks: {rec['unlocks_count']} tasks")
```

### 4. Parallel Work Discovery

Find tasks that can run simultaneously:

```python
parallel = sdk.get_parallel_work(max_agents=5)

print(f"Max parallelism: {parallel['max_parallelism']}")
print(f"Ready now: {parallel['ready_now']}")
# Output: Can run 3 agents simultaneously
```

### 5. Bottleneck Detection

Identify tasks blocking others:

```python
bottlenecks = sdk.find_bottlenecks(top_n=3)

for bn in bottlenecks:
    print(f"{bn['title']} blocks {bn['blocks_count']} features")
```

## Coordination Patterns

### Pattern 1: Specialist Agents

Different agents specialize in different work:

```python
# Backend agent
backend_work = sdk.features.where(
    status="todo",
    # Filter for backend tasks
).filter(lambda f: "API" in f.title)

# Frontend agent
frontend_work = sdk.features.where(
    status="todo"
).filter(lambda f: "UI" in f.title or "Dashboard" in f.title)
```

### Pattern 2: Priority-Based Assignment

Agents pick highest-priority available work:

```python
recs = sdk.recommend_next_work(agent_count=1)
if recs:
    best_task = recs[0]  # Highest-scoring task
    sdk.features.claim(best_task['id'], agent="my-agent")
```

### Pattern 3: Dependency Awareness

Agents check dependencies before starting:

```python
feature = sdk.features.get("feat-123")

if feature.edges.get("blocked_by"):
    blockers = [e.target_id for e in feature.edges["blocked_by"]]
    # Check if blockers are done
    ready = all(
        sdk.features.get(bid).status == "done"
        for bid in blockers
    )
```

### Pattern 4: Work Handoff

Agents release features for others:

```python
# Agent 1 completes setup work
with sdk.features.edit("feat-db-schema") as f:
    f.status = "done"
    # Automatically unblocks dependent features

# Agent 2 can now start on unblocked work
next_task = sdk.next_task(agent="agent-2", auto_claim=True)
```

## Multi-Agent Workflow

### 1. Coordinator Agent
```python
# Sets up project structure
track = sdk.tracks.builder() \
    .title("E-commerce Platform") \
    .with_plan_phases([...]) \
    .create()

# Creates features from plan
for phase in track.plan.phases:
    for task in phase.tasks:
        sdk.features.create(task.description) \
            .set_track(track.id) \
            .save()
```

### 2. Worker Agents
```python
# Each agent polls for work
while True:
    task = sdk.next_task(agent="worker-1", auto_claim=True)
    if task:
        work_on(task)
        mark_complete(task)
    else:
        # No work available, check again later
        time.sleep(60)
```

### 3. Reviewer Agent
```python
# Reviews completed work
completed = sdk.features.where(status="done", reviewed=False)

for feature in completed:
    if review_passes(feature):
        mark_as_reviewed(feature)
    else:
        reopen_with_feedback(feature)
```

## Benefits of HTML-Based Coordination

### 1. Human Observability
- Open any feature.html in a browser
- See current status, assigned agent, progress
- View dependency graph visually
- No special tools needed

### 2. Git-Friendly
- Text-based diffs show exactly what changed
- Blame shows which agent made changes
- Branch per agent for isolated work
- Merge conflicts are readable

### 3. Offline-First
- No database server required
- Works with just files on disk
- Agents can work offline
- Sync via git push/pull

### 4. Zero Lock-In
- Standard HTML/CSS/JS
- Switch tools anytime
- Export to any format
- No vendor dependency

## Real-World Usage

This pattern scales to:
- **Multiple teams** - Each team has their own feature set
- **Cross-project dependencies** - Features link across repos
- **Long-running projects** - Git history tracks evolution
- **Distributed teams** - Everyone works on same files via git

## Next Steps

1. Run the demo to see agents coordinate
2. Modify agent behaviors in demo.py
3. Try different dependency structures
4. Implement your own coordination patterns
5. Scale to real multi-agent systems

## Learn More

- [Strategic Planning Guide](../../docs/AGENT_STRATEGIC_PLANNING.md)
- [SDK Documentation](../../docs/api/sdk.md)
- [Dependency Analytics](../../docs/SDK_ANALYTICS.md)
