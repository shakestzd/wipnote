#!/usr/bin/env python3
"""
wipnote Multi-Agent Coordination Demo

Demonstrates how multiple AI agents can collaborate on a project using wipnote.
This example shows the "Ij

oka" use case - multiple agents working on interdependent features.
"""

import sys
from datetime import datetime
from pathlib import Path

# Add src to path for development
sys.path.insert(0, str(Path(__file__).parent.parent.parent / "src" / "python"))

from wipnote import SDK


def setup_project(sdk: SDK):
    """Create a sample project with interdependent features."""
    print("🏗️  Setting up multi-agent project...")

    # Feature 1: Database Schema (no dependencies)
    db_feature = (
        sdk.features.create("Database Schema Design")
        .set_priority("critical")
        .add_steps(
            [
                "Design user table schema",
                "Design product table schema",
                "Create migration scripts",
                "Add indexes and constraints",
            ]
        )
        .save()
    )

    # Feature 2: Authentication API (depends on database)
    auth_feature = (
        sdk.features.create("Authentication API")
        .set_priority("high")
        .blocked_by(db_feature.id)
        .add_steps(
            [
                "Implement JWT token generation",
                "Create login endpoint",
                "Create logout endpoint",
                "Add password hashing",
            ]
        )
        .save()
    )

    # Feature 3: User Management (depends on auth)
    user_mgmt_feature = (
        sdk.features.create("User Management API")
        .set_priority("high")
        .blocked_by(auth_feature.id)
        .add_steps(
            [
                "Create user CRUD endpoints",
                "Add role-based permissions",
                "Implement user search",
                "Add user profile endpoints",
            ]
        )
        .save()
    )

    # Feature 4: Product Catalog (depends on database)
    product_feature = (
        sdk.features.create("Product Catalog API")
        .set_priority("medium")
        .blocked_by(db_feature.id)
        .add_steps(
            [
                "Create product CRUD endpoints",
                "Implement category filtering",
                "Add product search",
                "Create inventory tracking",
            ]
        )
        .save()
    )

    # Feature 5: Frontend Dashboard (depends on user mgmt and products)
    dashboard_feature = (
        sdk.features.create("Admin Dashboard")
        .set_priority("medium")
        .blocked_by(user_mgmt_feature.id)
        .blocked_by(product_feature.id)
        .add_steps(
            [
                "Create dashboard layout",
                "Add user management UI",
                "Add product management UI",
                "Implement analytics charts",
            ]
        )
        .save()
    )

    print("✅ Created 5 interdependent features")
    return {
        "db": db_feature,
        "auth": auth_feature,
        "user_mgmt": user_mgmt_feature,
        "product": product_feature,
        "dashboard": dashboard_feature,
    }


def simulate_agent_work(sdk: SDK):
    """Simulate multiple agents working on features."""
    print("\n🤖 Simulating Multi-Agent Collaboration...\n")
    print("=" * 80)

    # Agent 1: Backend specialist (works on DB and APIs)
    print("\n👤 Agent 1 (Backend Specialist):")
    agent1 = "backend-agent"

    # Agent 1 looks for available work
    available = sdk.features.where(status="todo")
    backend_work = [f for f in available if "API" in f.title or "Database" in f.title]

    if backend_work:
        task = backend_work[0]
        print(f"   Found available task: {task.title}")
        print("   Claiming feature...")

        # Claim the feature
        with sdk.features.edit(task.id) as f:
            f.agent_assigned = agent1
            f.status = "in-progress"
            f.claimed_at = datetime.now()

        print(f"   ✅ Claimed: {task.id}")
        print(f"   📝 Working on: {task.steps[0].description}")

        # Complete first step
        with sdk.features.edit(task.id) as f:
            f.steps[0].completed = True

        print(f"   ✅ Completed step 0: {task.steps[0].description}")

    # Agent 2: Frontend specialist (works on UI)
    print("\n👤 Agent 2 (Frontend Specialist):")

    frontend_work = [f for f in available if "Dashboard" in f.title or "UI" in f.title]

    if frontend_work:
        task = frontend_work[0]
        print(f"   Found task: {task.title}")
        print("   ❌ Cannot start - blocked by dependencies")

        # Check what's blocking it
        if task.edges.get("blocked_by"):
            blocking_ids = [e.target_id for e in task.edges["blocked_by"]]
            print(f"   ⚠️  Waiting for: {', '.join(blocking_ids)}")
    else:
        print("   No frontend work available yet")

    # Agent 3: Full-stack (can work on anything)
    print("\n👤 Agent 3 (Full-Stack):")

    # Use recommendation system
    recs = sdk.recommend_next_work(agent_count=1)
    if recs:
        rec = recs[0]
        print(f"   Recommendation: {rec['title']} (score: {rec['score']:.1f})")
        print(f"   Reasons: {', '.join(rec['reasons'][:2])}")

        # Check if it's already claimed
        feature = sdk.features.get(rec["id"])
        if feature and feature.agent_assigned:
            print(f"   ❌ Already claimed by {feature.agent_assigned}")
            print("   Looking for alternative...")

            # Get parallel work
            parallel = sdk.get_parallel_work(max_agents=3)
            print(f"   💡 {parallel['total_ready']} tasks available in parallel")


def show_project_status(sdk: SDK):
    """Display current project status."""
    print("\n" + "=" * 80)
    print("📊 Project Status")
    print("=" * 80)

    all_features = sdk.features.all()

    # Group by status
    by_status = {}
    for f in all_features:
        status = f.status
        by_status.setdefault(status, []).append(f)

    # Show counts
    total = len(all_features)
    done_count = len(by_status.get("done", []))
    in_progress_count = len(by_status.get("in-progress", []))

    print(
        f"\n📈 Progress: {done_count}/{total} features complete ({done_count / total * 100:.0f}%)"
    )
    print(f"   In Progress: {in_progress_count}")
    print(f"   Todo: {len(by_status.get('todo', []))}")
    print(f"   Blocked: {len(by_status.get('blocked', []))}")

    # Show features by agent
    print("\n👥 Work Distribution:")
    by_agent = {}
    for f in all_features:
        if f.agent_assigned:
            by_agent.setdefault(f.agent_assigned, []).append(f)

    for agent, features in by_agent.items():
        print(f"\n   {agent}:")
        for f in features:
            completed_steps = sum(1 for s in f.steps if s.completed)
            total_steps = len(f.steps)
            print(f"      - {f.title} ({completed_steps}/{total_steps} steps)")

    # Show bottlenecks
    bottlenecks = sdk.find_bottlenecks(top_n=3)
    if bottlenecks:
        print("\n⚠️  Bottlenecks (blocking other work):")
        for bn in bottlenecks:
            print(f"   - {bn['title']}: blocks {bn['blocks_count']} features")


def show_coordination_insights(sdk: SDK):
    """Show insights for coordination."""
    print("\n" + "=" * 80)
    print("🧠 Coordination Insights")
    print("=" * 80)

    # Get parallel work capacity
    parallel = sdk.get_parallel_work(max_agents=5)
    print("\n⚡ Parallelization:")
    print(
        f"   Maximum agents that can work simultaneously: {parallel['max_parallelism']}"
    )
    print(f"   Tasks ready to start now: {parallel['ready_now']}")

    # Get recommendations
    recs = sdk.recommend_next_work(agent_count=3)
    print("\n💡 Top Recommendations for Next Work:")
    for i, rec in enumerate(recs[:3], 1):
        print(f"\n   {i}. {rec['title']} (score: {rec['score']:.1f})")
        print(f"      Priority: {rec['priority']}")
        print(f"      Why: {', '.join(rec['reasons'][:2])}")
        if rec.get("unlocks_count", 0) > 0:
            print(f"      Impact: Unlocks {rec['unlocks_count']} downstream tasks")

    # Risk assessment
    risks = sdk.assess_risks()
    if risks["high_risk_count"] > 0:
        print("\n⚠️  Risks Detected:")
        print(f"   {risks['high_risk_count']} high-risk tasks")
        for task in risks["high_risk_tasks"][:3]:
            print(f"   - {task['title']}: {', '.join(task['risk_factors'][:2])}")


def main():
    """Run the multi-agent coordination demo."""
    print("=" * 80)
    print("wipnote Multi-Agent Coordination Demo")
    print("'HTML is All You Need' - Agent Edition")
    print("=" * 80)

    # Initialize SDK
    sdk = SDK(directory=".", agent="demo-coordinator")

    # Setup project
    setup_project(sdk)

    # Simulate agent work
    simulate_agent_work(sdk)

    # Show status
    show_project_status(sdk)

    # Show coordination insights
    show_coordination_insights(sdk)

    print("\n" + "=" * 80)
    print("Demo complete!")
    print("Check the generated HTML files in the current directory.")
    print("Open them in a browser to see the agent coordination graph.")
    print("=" * 80)


if __name__ == "__main__":
    main()
