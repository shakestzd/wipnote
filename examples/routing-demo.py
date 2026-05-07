#!/usr/bin/env python3
"""
Example: Capability-Based Agent Routing for Multi-Agent Coordination.

This example demonstrates how to use wipnote's routing system to
intelligently assign tasks to agents based on their capabilities and
current workload.

Usage:
    python examples/routing-demo.py

This creates a .wipnote/ directory with:
- features/ - Tasks with required capabilities
- Features are routed to the best agents based on their capabilities
"""

from wipnote.models import Node
from wipnote.routing import (
    AgentCapabilityRegistry,
    CapabilityMatcher,
    route_tasks_to_agents,
)


def print_header(title: str) -> None:
    """Print a formatted section header."""
    print(f"\n{'=' * 60}")
    print(f"  {title}")
    print(f"{'=' * 60}\n")


def main():
    """Run the capability-based routing demo."""

    print_header("wipnote Capability-Based Routing Demo")

    # Step 1: Create agent registry and register agents with capabilities
    print("Step 1: Registering agents with capabilities...")
    registry = AgentCapabilityRegistry()

    agents_config = [
        ("claude-backend", ["python", "databases", "api-design"], 5),
        ("claude-frontend", ["javascript", "react", "ui-design"], 5),
        ("claude-devops", ["docker", "kubernetes", "ci-cd"], 4),
        ("claude-ml", ["python", "pytorch", "data-science"], 5),
        ("claude-generalist", ["python", "javascript", "documentation"], 6),
    ]

    for agent_id, capabilities, wip_limit in agents_config:
        registry.register_agent(agent_id, capabilities, wip_limit)
        print(f"  ✓ {agent_id:20} - {', '.join(capabilities)}")

    # Step 2: Create diverse tasks with different capability requirements
    print("\n\nStep 2: Creating tasks with capability requirements...")

    tasks = [
        Node(
            id="backend-auth",
            title="Implement OAuth2 Backend",
            type="feature",
            status="todo",
            priority="high",
            required_capabilities=["python", "databases", "api-design"],
        ),
        Node(
            id="frontend-dashboard",
            title="Build Analytics Dashboard",
            type="feature",
            status="todo",
            priority="high",
            required_capabilities=["javascript", "react", "ui-design"],
        ),
        Node(
            id="ml-pipeline",
            title="Train ML Classification Model",
            type="feature",
            status="todo",
            priority="medium",
            required_capabilities=["python", "pytorch", "data-science"],
        ),
        Node(
            id="devops-deploy",
            title="Setup Kubernetes Deployment",
            type="feature",
            status="todo",
            priority="critical",
            required_capabilities=["docker", "kubernetes", "ci-cd"],
        ),
        Node(
            id="docs-api",
            title="Document API Endpoints",
            type="feature",
            status="todo",
            priority="medium",
            required_capabilities=["documentation"],
        ),
        Node(
            id="refactor-utils",
            title="Refactor Utility Library",
            type="feature",
            status="todo",
            priority="low",
            required_capabilities=["python"],
        ),
    ]

    for task in tasks:
        print(f"  ✓ {task.id:20} - {task.title}")
        print(f"    Required: {', '.join(task.required_capabilities)}")

    # Step 3: Route tasks to agents
    print("\n\nStep 3: Routing tasks to best-fit agents...")

    routing = route_tasks_to_agents(tasks, registry)

    print("\nRouting Results:")
    print("-" * 80)
    print(f"{'Task ID':<20} {'Task Title':<30} {'Assigned Agent':<25} {'Score':<8}")
    print("-" * 80)

    for task in tasks:
        agent, score = routing[task.id]
        agent_name = agent.agent_id if agent else "UNASSIGNED"
        score_str = f"{score:>6.1f}" if agent else "   N/A"

        print(f"{task.id:<20} {task.title:<30} {agent_name:<25} {score_str}")

    # Step 4: Demonstrate workload-aware routing
    print("\n\nStep 4: Simulating workload impact on routing...")

    registry.set_wip("claude-backend", 4)  # Nearly at capacity
    registry.set_wip("claude-generalist", 1)  # Available

    overloaded_task = Node(
        id="overload-test",
        title="Python Utility Task",
        type="feature",
        status="todo",
        required_capabilities=["python"],
    )

    agent, score = route_tasks_to_agents([overloaded_task], registry)[
        overloaded_task.id
    ]

    print(f"\n  Task: {overloaded_task.title}")
    print(f"  Requirements: {', '.join(overloaded_task.required_capabilities)}")
    print("\n  Backend agent WIP: 4/5 (nearly full)")
    print("  Generalist agent WIP: 1/6 (available)")
    print(f"\n  Result: Task routed to {agent.agent_id}")
    print(f"  Score: {score:.1f}")
    print("\n  Explanation: Even though backend is more specialized,")
    print("  generalist was chosen due to lower workload (better availability)")

    # Step 5: Show capability matching details
    print("\n\nStep 5: Detailed capability matching example...")

    ml_task = Node(
        id="ml-example",
        title="Example ML Task",
        type="feature",
        status="todo",
        required_capabilities=["pytorch", "data-science"],
    )

    print(f"\n  Task: {ml_task.title}")
    print(f"  Requirements: {', '.join(ml_task.required_capabilities)}")

    print("\n  Agent Scores:")
    print("  " + "-" * 60)

    for agent_profile in registry.get_all_agents():
        score = CapabilityMatcher.score_agent_task_fit(agent_profile, ml_task)
        match = "✓" if score >= 0 else "✗"
        print(f"    {match} {agent_profile.agent_id:20} - Score: {score:7.1f}")

        # Show capability matching details
        agent_caps = set(agent_profile.capabilities)
        required = set(ml_task.required_capabilities)
        matches = required & agent_caps
        missing = required - agent_caps

        if matches:
            print(f"      Matches: {', '.join(matches)}")
        if missing:
            print(f"      Missing: {', '.join(missing)}")

    # Step 6: Summary
    print_header("Summary")

    successful = sum(1 for _, (agent, _) in routing.items() if agent)
    print(f"Tasks routed:     {successful}/{len(tasks)}")
    print(
        f"Agents used:      {len(set(agent.agent_id for _, (agent, _) in routing.items() if agent))}"
    )
    print("\nCapability-based routing successfully demonstrated!")
    print("The system intelligently assigned tasks based on:")
    print("  • Agent capabilities (exact and partial matches)")
    print("  • Agent workload (WIP count)")
    print("  • Task priority")
    print("\nUse in production to:")
    print("  1. Register multi-agent teams with diverse capabilities")
    print("  2. Assign complex tasks automatically")
    print("  3. Balance workload across agents")
    print("  4. Prevent overloading specialized agents")


if __name__ == "__main__":
    main()
