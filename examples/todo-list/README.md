# Todo List Example

A simple todo list application demonstrating wipnote's core features.

## Overview

This example shows how to build a basic task management system using wipnote, where:
- Each task is an HTML file (a graph node)
- Dependencies are hyperlinks (graph edges)
- The browser is the UI (no build step needed)
- Python provides the backend (queries, updates)

## Files

- `index.html` - Dashboard showing all tasks (open in browser)
- `styles.css` - Styling for the task cards
- `task-001.html` through `task-005.html` - Individual task nodes
- `demo.py` - Python script demonstrating wipnote operations

## Quick Start

### View in Browser

Simply open `index.html` in your browser to see the dashboard with all tasks.

### Run the Demo

```bash
# From the examples/todo-list directory
python demo.py
```

The demo script demonstrates:
- ✅ Loading tasks from HTML files
- ✅ Querying with CSS selectors (`[data-status='blocked']`)
- ✅ Graph traversal (dependencies, bottlenecks)
- ✅ Agent interface for AI agents
- ✅ Creating new tasks programmatically
- ✅ Generating AI-friendly context

## What It Demonstrates

### 1. Graph Structure

Tasks are connected via dependencies:
```
task-001 (Setup environment)
    ↓ blocks
task-002 (Implement core models)
    ↓ blocks
task-003 (Add API endpoints)
```

### 2. CSS Selector Queries

```python
# Find all blocked tasks
blocked = graph.query("[data-status='blocked']")

# Find high priority tasks
high_priority = graph.query("[data-priority='high']")

# Combine filters
urgent = graph.query("[data-status='todo'][data-priority='high']")
```

### 3. Graph Algorithms

```python
# Find all dependencies (transitive closure)
deps = graph.transitive_deps("task-003", relationship="blocked_by")

# Find bottlenecks (tasks blocking the most others)
bottlenecks = graph.find_bottlenecks(top_n=3)
```

### 4. AI Agent Interface

```python
from wipnote.agents import AgentInterface

agent = AgentInterface(".", agent_id="my-agent")

# Get next available task
task = agent.get_next_task()

# Get lightweight context (for LLMs)
context = agent.get_context(task.id)
```

### 5. Programmatic Task Creation

```python
from wipnote import wipnote, Node, Step, Edge

graph = wipnote(".")

new_task = Node(
    id="task-006",
    title="Deploy to production",
    type="task",
    status="todo",
    priority="high",
    steps=[
        Step(description="Run tests"),
        Step(description="Build artifacts"),
        Step(description="Deploy")
    ],
    edges={
        "blocked_by": [Edge(target_id="task-005")]
    }
)

graph.add(new_task)  # Creates task-006.html
```

## Key Concepts

### HTML as Graph Nodes

Each task is a standalone HTML file that:
- Can be opened directly in a browser
- Is human-readable (view source)
- Works with git (text diff, blame, history)
- Contains both data and presentation

### Hyperlinks as Graph Edges

Dependencies are regular HTML links:
```html
<nav data-graph-edges>
    <section data-edge-type="blocked_by">
        <h3>⚠️ Blocked By:</h3>
        <ul>
            <li><a href="task-001.html">Setup environment</a></li>
        </ul>
    </section>
</nav>
```

### No Build Step Required

The entire system works with:
- No webpack, no bundler
- No database, no server (static files)
- No templating engine (raw HTML)
- No JavaScript framework (vanilla JS)

Just HTML, CSS, and optionally Python for queries.

## Philosophy

> "HTML is All You Need"

This example proves that complex graph operations (queries, traversal, algorithms) can be performed on simple HTML files. No Neo4j, no MongoDB, no custom formats - just web standards.

## Next Steps

- Modify existing tasks by editing the HTML files
- Run the demo script to see graph operations
- Try the agent interface for AI-powered task management
- Open the dashboard in multiple browsers simultaneously (it's just files!)

## Learn More

- See the main [wipnote documentation](../../docs/)
- Explore the [Python API](../../docs/api/graph.md)
- Check out the [SDK guide](../../docs/api/sdk.md)
