#!/usr/bin/env python3
"""
Demo script: Create track-001-auth example.

This demonstrates the Conductor-style planning workflow with wipnote:
1. Create a track for User Authentication
2. Create a spec with requirements and acceptance criteria
3. Create an implementation plan with phases and tasks
4. Show how to view and interact with the results
"""

import sys
from pathlib import Path

# Add src to path for imports
sys.path.insert(0, str(Path(__file__).parent.parent / "src" / "python"))

from wipnote.planning import AcceptanceCriterion, Phase, Task
from wipnote.track_manager import TrackManager


def main():
    """Create the demo track."""

    # Initialize manager
    manager = TrackManager(".wipnote")

    print("Creating track-001-auth demo...")
    print("=" * 60)

    # 1. Create the track
    print("\n1. Creating track...")
    track = manager.create_track(
        title="User Authentication",
        description="Implement user authentication system with OAuth 2.0 support for Google and GitHub providers.",
        priority="high",
    )

    # Rename to track-001-auth for consistency
    track_path = manager.tracks_dir / track.id
    demo_path = manager.tracks_dir / "track-001-auth"

    if demo_path.exists():
        import shutil

        shutil.rmtree(demo_path)

    track_path.rename(demo_path)
    track.id = "track-001-auth"

    # Regenerate index.html with the updated ID
    manager._write_track_index(track, demo_path)

    print(f"   ✓ Created track: {track.id}")
    print(f"   ✓ Path: {demo_path}")

    # 2. Create the spec
    print("\n2. Creating specification...")
    spec = manager.create_spec(
        track_id="track-001-auth",
        title="User Authentication Specification",
        overview="Build a secure authentication system supporting OAuth 2.0 providers (Google, GitHub) with JWT-based session management.",
        context="Users need to authenticate to access protected features. OAuth reduces password management burden and improves security.",
        author="claude-code",
    )

    print(f"   ✓ Created spec: {spec.id}")

    # Add requirements
    print("\n3. Adding requirements...")

    requirements = [
        {
            "description": "Support Google OAuth 2.0",
            "priority": "must-have",
            "notes": "Use Google Identity Platform for authentication",
        },
        {
            "description": "Support GitHub OAuth 2.0",
            "priority": "must-have",
            "notes": "Use GitHub OAuth Apps for authentication",
        },
        {
            "description": "JWT-based session management",
            "priority": "must-have",
            "notes": "Store sessions in secure HTTP-only cookies",
        },
        {
            "description": "Refresh token rotation",
            "priority": "should-have",
            "notes": "Implement automatic token refresh for better security",
        },
        {
            "description": "Two-factor authentication",
            "priority": "nice-to-have",
            "notes": "Support TOTP-based 2FA for enhanced security",
        },
    ]

    for req in requirements:
        manager.add_requirement(
            track_id="track-001-auth",
            description=req["description"],
            priority=req["priority"],
            notes=req["notes"],
        )
        print(f"   ✓ Added requirement: {req['description']}")

    # Manually add acceptance criteria to spec
    spec.acceptance_criteria = [
        AcceptanceCriterion(
            description="User can log in with Google account",
            test_case="test_google_oauth_login",
        ),
        AcceptanceCriterion(
            description="User can log in with GitHub account",
            test_case="test_github_oauth_login",
        ),
        AcceptanceCriterion(
            description="JWT tokens expire after 24 hours",
            test_case="test_jwt_expiration",
        ),
        AcceptanceCriterion(
            description="Refresh token rotation works correctly",
            test_case="test_refresh_token_rotation",
        ),
        AcceptanceCriterion(
            description="User session persists across page reloads",
            test_case="test_session_persistence",
        ),
    ]

    # Save updated spec
    spec_path = demo_path / "spec.html"
    spec_path.write_text(spec.to_html(), encoding="utf-8")

    print(f"   ✓ Added {len(spec.acceptance_criteria)} acceptance criteria")

    # 4. Create the plan with all phases and tasks
    print("\n4. Creating implementation plan...")
    from wipnote.planning import Plan

    plan = Plan(
        id="track-001-auth-plan",
        title="User Authentication Implementation Plan",
        track_id="track-001-auth",
        status="active",
    )

    print(f"   ✓ Created plan: {plan.id}")

    # Add Phase 1: Setup
    print("\n5. Adding Phase 1: Setup...")
    phase1 = Phase(
        id="1",
        name="Setup",
        description="Set up authentication infrastructure and dependencies",
        status="not-started",
    )

    # Add tasks to Phase 1
    phase1.tasks = [
        Task(
            id="1.1",
            description="Create database schema for users and sessions",
            priority="high",
            estimate_hours=3.0,
            assigned="claude",
        ),
        Task(
            id="1.2",
            description="Install and configure OAuth libraries (passport.js or similar)",
            priority="high",
            estimate_hours=2.0,
            assigned="claude",
        ),
        Task(
            id="1.3",
            description="Set up JWT token generation and validation",
            priority="high",
            estimate_hours=2.0,
            assigned="claude",
        ),
        Task(
            id="1.4",
            description="Configure environment variables for OAuth credentials",
            priority="medium",
            estimate_hours=1.0,
        ),
    ]

    for task in phase1.tasks:
        print(f"   ✓ Added task: {task.description}")

    plan.phases.append(phase1)

    # Add Phase 2: OAuth Implementation
    print("\n6. Adding Phase 2: OAuth Implementation...")
    phase2 = Phase(
        id="2",
        name="OAuth Implementation",
        description="Implement OAuth flows for Google and GitHub",
        status="not-started",
    )

    phase2.tasks = [
        Task(
            id="2.1",
            description="Implement Google OAuth callback handler",
            priority="high",
            estimate_hours=4.0,
            assigned="claude",
        ),
        Task(
            id="2.2",
            description="Implement GitHub OAuth callback handler",
            priority="high",
            estimate_hours=4.0,
            assigned="claude",
        ),
        Task(
            id="2.3",
            description="Create user profile fetching from OAuth providers",
            priority="medium",
            estimate_hours=3.0,
        ),
        Task(
            id="2.4",
            description="Handle OAuth errors and edge cases",
            priority="high",
            estimate_hours=3.0,
        ),
    ]

    for task in phase2.tasks:
        print(f"   ✓ Added task: {task.description}")

    plan.phases.append(phase2)

    # Add Phase 3: Session Management
    print("\n7. Adding Phase 3: Session Management...")
    phase3 = Phase(
        id="3",
        name="Session Management",
        description="Implement JWT-based session management with refresh tokens",
        status="not-started",
    )

    phase3.tasks = [
        Task(
            id="3.1",
            description="Create session creation and validation middleware",
            priority="high",
            estimate_hours=4.0,
        ),
        Task(
            id="3.2",
            description="Implement refresh token rotation logic",
            priority="medium",
            estimate_hours=5.0,
        ),
        Task(
            id="3.3",
            description="Add logout functionality (token invalidation)",
            priority="high",
            estimate_hours=2.0,
        ),
        Task(
            id="3.4",
            description="Create user profile endpoint",
            priority="medium",
            estimate_hours=2.0,
        ),
    ]

    for task in phase3.tasks:
        print(f"   ✓ Added task: {task.description}")

    plan.phases.append(phase3)

    # Add Phase 4: Testing
    print("\n8. Adding Phase 4: Testing...")
    phase4 = Phase(
        id="4",
        name="Testing & Documentation",
        description="Write tests and documentation for the authentication system",
        status="not-started",
    )

    phase4.tasks = [
        Task(
            id="4.1",
            description="Write unit tests for OAuth handlers",
            priority="high",
            estimate_hours=6.0,
        ),
        Task(
            id="4.2",
            description="Write integration tests for full auth flow",
            priority="high",
            estimate_hours=4.0,
        ),
        Task(
            id="4.3",
            description="Add API documentation for auth endpoints",
            priority="medium",
            estimate_hours=3.0,
        ),
        Task(
            id="4.4",
            description="Create user guide for OAuth setup",
            priority="low",
            estimate_hours=2.0,
        ),
    ]

    for task in phase4.tasks:
        print(f"   ✓ Added task: {task.description}")

    plan.phases.append(phase4)

    # Save final plan
    plan_path = demo_path / "plan.html"
    plan_path.write_text(plan.to_html(), encoding="utf-8")

    print("\n" + "=" * 60)
    print("✓ Demo track created successfully!")
    print("\nView the results:")
    print("  - Track index: .wipnote/tracks/track-001-auth/index.html")
    print("  - Spec:        .wipnote/tracks/track-001-auth/spec.html")
    print("  - Plan:        .wipnote/tracks/track-001-auth/plan.html")
    print("\nOpen the plan.html in a browser to see:")
    print("  - List view (default)")
    print("  - Kanban board")
    print("  - Timeline view")
    print("  - Dependency graph")
    print("\nStart the server to view the dashboard:")
    print("  uv run wipnote serve")
    print("  Then open: http://localhost:8080")


if __name__ == "__main__":
    main()
