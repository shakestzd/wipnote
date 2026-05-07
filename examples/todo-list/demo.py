#!/usr/bin/env python3
"""
wipnote Todo List Demo

Demonstrates basic wipnote usage with a simple todo list.
Run this script from the examples/todo-list directory.
"""

import sys
from pathlib import Path

# Add src to path for development
sys.path.insert(0, str(Path(__file__).parent.parent.parent / "src" / "python"))

from wipnote import Edge, wipnote, Node, Step
from wipnote.agents import AgentInterface


def main():
    """Demonstrate wipnote todo list operations."""
    print("=" * 60)
    print("wipnote Todo List Demo")
    print("'HTML is All You Need'")
    print("=" * 60)

    # Initialize graph from current directory
    graph = wipnote(".")
    print(f"\n📂 Loaded {len(graph)} tasks from HTML files")

    # Show stats
    stats = graph.stats()
    print("\n📊 Statistics:")
    print(f"   Total tasks: {stats['total']}")
    print(f"   Completion: {stats['completion_rate']}%")
    print(f"   By status: {stats['by_status']}")

    # Query examples
    print("\n🔍 Query Examples:")

    # Find blocked tasks
    blocked = graph.query("[data-status='blocked']")
    print(f"\n   Blocked tasks ({len(blocked)}):")
    for task in blocked:
        print(f"   - {task.id}: {task.title}")

    # Find high priority tasks
    high_priority = graph.query("[data-priority='high']")
    print(f"\n   High priority tasks ({len(high_priority)}):")
    for task in high_priority:
        print(f"   - {task.id}: {task.title} [{task.status}]")

    # Graph traversal
    print("\n🔗 Graph Traversal:")

    # Find dependencies
    deps = graph.transitive_deps("task-003", relationship="blocked_by")
    print(f"\n   task-003 depends on: {deps}")

    # Find bottlenecks
    bottlenecks = graph.find_bottlenecks(top_n=3)
    print("\n   Bottleneck tasks (blocking most others):")
    for task_id, count in bottlenecks:
        task = graph.get(task_id)
        name = task.title if task else task_id
        print(f"   - {task_id}: {name} (blocks {count} others)")

    # Agent interface demo
    print("\n🤖 Agent Interface Demo:")

    agent = AgentInterface(".", agent_id="demo-agent")

    # Get summary
    print(f"\n   {agent.get_summary()}")

    # Get next available task
    next_task = agent.get_next_task()
    if next_task:
        print("\n   Next available task:")
        print(f"   {agent.get_context(next_task.id)}")

    # Create a new task programmatically
    print("\n✨ Creating new task programmatically...")

    new_task = Node(
        id="task-006",
        title="Optimize performance",
        type="task",
        status="todo",
        priority="medium",
        content="<p>Improve graph query performance for large datasets.</p>",
        steps=[
            Step(description="Profile current performance"),
            Step(description="Implement caching layer"),
            Step(description="Add benchmark tests"),
        ],
        edges={
            "blocked_by": [Edge(target_id="task-002", title="Implement core models")]
        },
    )

    # Save to HTML file (use overwrite=True to allow re-running the demo)
    filepath = graph.add(new_task, overwrite=True)
    print(f"   Created: {filepath}")

    # Show the generated HTML
    print("\n   Generated HTML preview (first 500 chars):")
    html_content = filepath.read_text()[:500]
    print(f"   {html_content}...")

    # Generate context for AI agent
    print("\n📝 AI Agent Context (lightweight ~50 tokens):")
    print(new_task.to_context())

    # Export as Mermaid diagram
    print("\n📈 Mermaid Diagram:")
    print(graph.to_mermaid(relationship="blocked_by"))

    print("\n" + "=" * 60)
    print("Demo complete! Check the generated HTML files.")
    print("=" * 60)


if __name__ == "__main__":
    main()
