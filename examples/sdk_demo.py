#!/usr/bin/env python3
"""
wipnote SDK Demo - AI-Friendly API

Demonstrates the improved SDK for AI agents compared to the old API.
"""

from wipnote import SDK


def old_api_example():
    """Example using the old verbose API."""
    print("=" * 60)
    print("OLD API (Verbose)")
    print("=" * 60)

    from wipnote import AgentInterface, Node, Step

    # Initialize with explicit path and agent_id
    agent = AgentInterface(".wipnote/features", agent_id="claude")

    # Create a feature (verbose)
    feature = Node(
        id="feature-old-001",
        title="User Authentication",
        type="feature",
        status="todo",
        priority="high",
        content="<p>Implement user authentication with OAuth</p>",
        steps=[
            Step(description="Create login endpoint"),
            Step(description="Add JWT middleware"),
            Step(description="Write tests"),
        ],
    )
    agent.graph.add(feature)
    print(f"✓ Created feature: {feature.id}")

    # Claim and work on feature (repetitive agent_id)
    agent.claim_task(feature.id, agent_id="claude")
    print(f"✓ Claimed: {feature.id}")

    agent.complete_step(feature.id, 0, agent_id="claude")
    print("✓ Completed step 0")

    # Get context
    context = agent.get_context(feature.id)
    print(f"\nContext:\n{context}")

    # Query features (manual filtering)
    high_priority = [
        n
        for n in agent.graph
        if n.type == "feature" and n.priority == "high" and n.status == "todo"
    ]
    print(f"\n✓ Found {len(high_priority)} high priority features")

    # Cleanup
    agent.graph.remove(feature.id)


def new_api_example():
    """Example using the new fluent SDK."""
    print("\n" + "=" * 60)
    print("NEW SDK (Fluent & AI-Friendly)")
    print("=" * 60)

    # Initialize - auto-discovers .wipnote directory
    sdk = SDK(agent="claude")

    # Create feature with fluent interface
    feature = (
        sdk.features.create("User Authentication")
        .set_priority("high")
        .set_description("Implement user authentication with OAuth")
        .add_steps(["Create login endpoint", "Add JWT middleware", "Write tests"])
        .save()
    )

    print(f"✓ Created feature: {feature.id}")

    # Edit feature with context manager (auto-saves!)
    with sdk.features.edit(feature.id) as f:
        f.status = "in-progress"
        f.agent_assigned = "claude"
        f.steps[0].completed = True

    print("✓ Claimed and completed step 0")

    # Get context
    updated = sdk.features.get(feature.id)
    context = updated.to_context()
    print(f"\nContext:\n{context}")

    # Query features (clean, declarative)
    high_priority = sdk.features.where(status="todo", priority="high")
    print(f"\n✓ Found {len(high_priority)} high priority features")

    # Batch operations
    # sdk.features.mark_done(["feat-001", "feat-002"])
    # sdk.features.assign(["feat-003", "feat-004"], agent="claude")

    # Cleanup
    sdk._graph.remove(feature.id)


def real_world_ai_workflow():
    """
    Realistic AI agent workflow using the SDK.

    This is what Claude (AI agent) would actually do.
    """
    print("\n" + "=" * 60)
    print("REAL-WORLD AI AGENT WORKFLOW")
    print("=" * 60)

    sdk = SDK(agent="claude")

    # Step 1: Get orientation
    print("\n1. Getting project summary...")
    summary = sdk.summary(max_items=5)
    print(summary)

    # Step 2: Check my current work
    print("\n2. Checking my workload...")
    workload = sdk.my_work()
    print(f"   In progress: {workload['in_progress']}")
    print(f"   Completed: {workload['completed']}")

    # Step 3: Get next task
    print("\n3. Getting next high-priority task...")
    task = sdk.next_task(priority="high", auto_claim=True)

    if task:
        print(f"   ✓ Claimed: {task.id} - {task.title}")

        # Step 4: Work on the task
        print("\n4. Working on task...")
        with sdk.features.edit(task.id) as feature:
            # Complete first step
            if feature.steps and not feature.steps[0].completed:
                feature.steps[0].completed = True
                feature.steps[0].agent = "claude"
                print(f"   ✓ Completed step: {feature.steps[0].description}")

            # Update status
            all_done = all(s.completed for s in feature.steps)
            if all_done:
                feature.status = "done"
                print("   ✓ All steps complete - marking as done")

        # Cleanup
        sdk._graph.remove(task.id)
    else:
        print("   No high-priority tasks available")

    print("\n✓ AI agent workflow complete")


def comparison_table():
    """Print comparison table."""
    print("\n" + "=" * 60)
    print("API COMPARISON")
    print("=" * 60)

    comparisons = [
        ("Feature", "Old API", "New SDK"),
        ("-" * 20, "-" * 20, "-" * 20),
        (
            "Initialization",
            "AgentInterface('.wipnote/features', agent_id='claude')",
            "SDK(agent='claude')",
        ),
        (
            "Create Feature",
            "Node(id=..., title=..., type=..., steps=[...])",
            "sdk.features.create('Title').add_steps([...])",
        ),
        (
            "Edit Feature",
            "node.status = 'done'; graph.update(node)",
            "with sdk.features.edit('id') as f: f.status = 'done'",
        ),
        (
            "Query",
            "[n for n in graph if n.status=='todo']",
            "sdk.features.where(status='todo')",
        ),
        (
            "Batch Ops",
            "for id in ids: graph.get(id).status='done'",
            "sdk.features.mark_done(ids)",
        ),
        ("Context Mgr", "❌ No", "✅ Auto-save"),
        ("Auto-discover", "❌ No", "✅ Yes"),
        ("Method Chain", "❌ No", "✅ Yes"),
        ("Agent ID", "Pass everywhere", "Set once"),
    ]

    for row in comparisons:
        print(f"{row[0]:20} | {row[1]:35} | {row[2]:35}")


if __name__ == "__main__":
    comparison_table()

    # Run examples
    old_api_example()
    new_api_example()
    real_world_ai_workflow()

    print("\n" + "=" * 60)
    print("✓ SDK Demo Complete")
    print("=" * 60)
