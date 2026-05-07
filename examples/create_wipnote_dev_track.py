#!/usr/bin/env python3
"""
Create wipnote development track from PRD.md.

This script creates a comprehensive track/spec/plan for wipnote's own development,
demonstrating vertical integration by using the complete stack.
"""

import sys
from pathlib import Path

# Add src to path for imports
sys.path.insert(0, str(Path(__file__).parent.parent / "src" / "python"))

from wipnote.planning import AcceptanceCriterion, Phase, Task
from wipnote.track_manager import TrackManager


def main():
    """Create the wipnote development track."""

    manager = TrackManager(".wipnote")

    print("Creating wipnote Development Track...")
    print("=" * 60)

    # 1. Create the track
    print("\n1. Creating track...")
    track = manager.create_track(
        title="wipnote - HTML is All You Need",
        description="Lightweight graph database framework built on web standards for AI agent coordination",
        priority="critical",
    )

    # Rename to wipnote-dev
    track_path = manager.tracks_dir / track.id
    dev_path = manager.tracks_dir / "wipnote-dev"

    if dev_path.exists():
        import shutil

        shutil.rmtree(dev_path)

    track_path.rename(dev_path)
    track.id = "wipnote-dev"
    manager._write_track_index(track, dev_path)

    print(f"   ✓ Created track: {track.id}")

    # 2. Create the spec
    print("\n2. Creating specification...")
    spec = manager.create_spec(
        track_id="wipnote-dev",
        title="wipnote Product Specification",
        overview="Build a zero-dependency graph database using HTML files as nodes, hyperlinks as edges, and CSS selectors for queries",
        context="AI agent developers need simple coordination infrastructure without Docker/databases. wipnote provides human-readable, git-friendly agent state management",
        author="Shakes",
    )

    print(f"   ✓ Created spec: {spec.id}")

    # Add requirements (from PRD Features & Requirements section)
    print("\n3. Adding requirements...")

    requirements = [
        # Core Features (MVP) - P0
        {
            "description": "Python library with core graph operations",
            "priority": "must-have",
            "notes": "wipnote class, add/update/query/get methods, file-based storage",
        },
        {
            "description": "HTML node creation and serialization",
            "priority": "must-have",
            "notes": "Node.to_html() method, Pydantic models, data-* attributes",
        },
        {
            "description": "CSS selector query engine",
            "priority": "must-have",
            "notes": "graph.query(selector) using justhtml parser",
        },
        {
            "description": "Hyperlink-based edge system",
            "priority": "must-have",
            "notes": "Native <a href> for relationships, data-relationship attributes",
        },
        {
            "description": "Pydantic data models with validation",
            "priority": "must-have",
            "notes": "Node, Edge, Step models with schema validation",
        },
        {
            "description": "Agent interface for AI coordination",
            "priority": "must-have",
            "notes": "Lightweight context, get_next_task(), complete_step()",
        },
        # P1
        {
            "description": "JavaScript library for browser-side queries",
            "priority": "should-have",
            "notes": "wipnote JS class, DOMParser integration, CSS selectors",
        },
        {
            "description": "Interactive dashboard with multiple views",
            "priority": "should-have",
            "notes": "Vanilla JS, kanban/graph/timeline views, real-time updates",
        },
        {
            "description": "Example implementations (todo, agent coordination)",
            "priority": "should-have",
            "notes": "Working demos showing different use cases",
        },
        # P2
        {
            "description": "SQLite index for complex queries",
            "priority": "nice-to-have",
            "notes": "Optional full-text search, analytics, performance optimization",
        },
        {
            "description": "File watching for real-time updates",
            "priority": "nice-to-have",
            "notes": "Auto-reload dashboard when HTML files change",
        },
    ]

    for req in requirements:
        manager.add_requirement(
            track_id="wipnote-dev",
            description=req["description"],
            priority=req["priority"],
            notes=req["notes"],
        )
        print(f"   ✓ Added requirement: {req['description']}")

    # Manually add acceptance criteria
    spec.acceptance_criteria = [
        AcceptanceCriterion(
            description="Parse 1000 HTML nodes in <1 second",
            test_case="test_performance_large_graph",
        ),
        AcceptanceCriterion(
            description="CSS selector queries return results in <100ms",
            test_case="test_query_performance",
        ),
        AcceptanceCriterion(
            description="Dashboard loads and renders in <500ms",
            test_case="test_dashboard_load_time",
        ),
        AcceptanceCriterion(
            description="Zero external dependencies except justhtml + Pydantic",
            test_case="test_dependencies",
        ),
        AcceptanceCriterion(
            description="Works offline without network access",
            test_case="test_offline_mode",
        ),
        AcceptanceCriterion(
            description="Git diff shows clear changes to graph state",
            test_case="test_version_control_friendly",
        ),
    ]

    spec_path = dev_path / "spec.html"
    spec_path.write_text(spec.to_html(), encoding="utf-8")
    print(f"   ✓ Added {len(spec.acceptance_criteria)} acceptance criteria")

    # 4. Create implementation plan
    print("\n4. Creating implementation plan...")
    from wipnote.planning import Plan

    plan = Plan(
        id="wipnote-dev-plan",
        title="wipnote Implementation Plan",
        track_id="wipnote-dev",
        status="active",
    )

    print(f"   ✓ Created plan: {plan.id}")

    # Phase 1: Core Library
    print("\n5. Adding Phase 1: Core Library...")
    phase1 = Phase(
        id="1",
        name="Core Library (Python)",
        description="Build foundational Python library with graph operations",
        status="completed",
    )

    phase1.tasks = [
        Task(
            id="1.1",
            description="Pydantic models (Node, Edge, Step)",
            priority="high",
            estimate_hours=4.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="1.2",
            description="HTML parser wrapper using justhtml",
            priority="high",
            estimate_hours=3.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="1.3",
            description="Node ↔ HTML converter",
            priority="high",
            estimate_hours=5.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="1.4",
            description="wipnote class (add, update, query, get)",
            priority="high",
            estimate_hours=6.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="1.5",
            description="CSS selector query engine",
            priority="high",
            estimate_hours=4.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="1.6",
            description="Graph algorithms (BFS, shortest path, dependency analysis)",
            priority="medium",
            estimate_hours=8.0,
            assigned="claude",
            completed=True,
        ),
    ]

    for task in phase1.tasks:
        status = "✅" if task.completed else "⏳"
        print(f"   {status} Added task: {task.description}")

    plan.phases.append(phase1)

    # Phase 2: Dashboard & JavaScript
    print("\n6. Adding Phase 2: Dashboard & JavaScript...")
    phase2 = Phase(
        id="2",
        name="Dashboard & JavaScript",
        description="Build browser-side interface and visualizations",
        status="completed",
    )

    phase2.tasks = [
        Task(
            id="2.1",
            description="JavaScript wipnote library",
            priority="high",
            estimate_hours=6.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="2.2",
            description="Dashboard HTML/CSS with dark theme",
            priority="high",
            estimate_hours=5.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="2.3",
            description="Kanban board view",
            priority="high",
            estimate_hours=4.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="2.4",
            description="Graph visualization view",
            priority="medium",
            estimate_hours=6.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="2.5",
            description="Analytics view with statistics",
            priority="medium",
            estimate_hours=4.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="2.6",
            description="Session history view",
            priority="medium",
            estimate_hours=3.0,
            assigned="claude",
            completed=True,
        ),
    ]

    for task in phase2.tasks:
        status = "✅" if task.completed else "⏳"
        print(f"   {status} Added task: {task.description}")

    plan.phases.append(phase2)

    # Phase 3: Advanced Features
    print("\n7. Adding Phase 3: Advanced Features...")
    phase3 = Phase(
        id="3",
        name="Advanced Features",
        description="Analytics, indexing, and performance optimizations",
        status="completed",
    )

    phase3.tasks = [
        Task(
            id="3.1",
            description="SQLite analytics index",
            priority="medium",
            estimate_hours=8.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="3.2",
            description="File watching for auto-reload",
            priority="medium",
            estimate_hours=4.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="3.3",
            description="Event logging (JSONL)",
            priority="medium",
            estimate_hours=3.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="3.4",
            description="REST API server",
            priority="medium",
            estimate_hours=6.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="3.5",
            description="CLI tool (wipnote command)",
            priority="low",
            estimate_hours=5.0,
            assigned="claude",
            completed=True,
        ),
    ]

    for task in phase3.tasks:
        status = "✅" if task.completed else "⏳"
        print(f"   {status} Added task: {task.description}")

    plan.phases.append(phase3)

    # Phase 4: Conductor Planning (current work!)
    print("\n8. Adding Phase 4: Conductor Planning...")
    phase4 = Phase(
        id="4",
        name="Conductor Planning System",
        description="Track/Spec/Plan workflow with vertical integration",
        status="in-progress",
    )

    phase4.tasks = [
        Task(
            id="4.1",
            description="Pydantic models for Track/Spec/Plan",
            priority="high",
            estimate_hours=4.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="4.2",
            description="TrackManager class for operations",
            priority="high",
            estimate_hours=5.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="4.3",
            description="HTML templates for spec/plan views",
            priority="high",
            estimate_hours=6.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="4.4",
            description="CLI commands (track new/list)",
            priority="medium",
            estimate_hours=3.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="4.5",
            description="Tracks view in dashboard",
            priority="medium",
            estimate_hours=4.0,
            assigned="claude",
            completed=True,
        ),
        Task(
            id="4.6",
            description="Vertical integration (Track → Plan → Features)",
            priority="high",
            estimate_hours=8.0,
            assigned="claude",
            completed=False,  # Currently working on this!
        ),
    ]

    for task in phase4.tasks:
        status = "✅" if task.completed else "⏳"
        print(f"   {status} Added task: {task.description}")

    plan.phases.append(phase4)

    # Phase 5: Documentation & Examples
    print("\n9. Adding Phase 5: Documentation & Examples...")
    phase5 = Phase(
        id="5",
        name="Documentation & Examples",
        description="Comprehensive docs, examples, and launch materials",
        status="not-started",
    )

    phase5.tasks = [
        Task(
            id="5.1",
            description="README with quickstart guide",
            priority="high",
            estimate_hours=4.0,
        ),
        Task(
            id="5.2",
            description="API reference documentation",
            priority="high",
            estimate_hours=6.0,
        ),
        Task(
            id="5.3",
            description="Todo list example",
            priority="medium",
            estimate_hours=3.0,
        ),
        Task(
            id="5.4",
            description="Agent coordination example",
            priority="medium",
            estimate_hours=5.0,
        ),
        Task(
            id="5.5",
            description="Philosophy & comparison docs",
            priority="low",
            estimate_hours=4.0,
        ),
    ]

    for task in phase5.tasks:
        status = "✅" if task.completed else "⏳"
        print(f"   {status} Added task: {task.description}")

    plan.phases.append(phase5)

    # Save plan
    plan_path = dev_path / "plan.html"
    plan_path.write_text(plan.to_html(), encoding="utf-8")

    print("\n" + "=" * 60)
    print("✓ wipnote Development Track created successfully!")
    print("\nView the results:")
    print("  - Track: .wipnote/tracks/wipnote-dev/index.html")
    print("  - Spec:  .wipnote/tracks/wipnote-dev/spec.html")
    print("  - Plan:  .wipnote/tracks/wipnote-dev/plan.html")
    print("\nStart the server to view:")
    print("  uv run wipnote serve")
    print("  Then open: http://localhost:8080")
    print("\nNext steps:")
    print("  1. Link existing features to plan tasks")
    print("  2. Generate features from remaining plan tasks")
    print("  3. Use track-grouped kanban view")


if __name__ == "__main__":
    main()
