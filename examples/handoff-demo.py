#!/usr/bin/env python
"""
Handoff Context System Demo

Demonstrates agent-to-agent task transitions with preserved context.
Shows how agents can hand off work with lightweight hyperlink references
instead of embedding full context.

Usage:
    uv run examples/handoff-demo.py
"""

import tempfile
from datetime import datetime

from wipnote import SDK
from wipnote.models import Node
from wipnote.session_manager import SessionManager


def demo_sdk_handoff():
    """Demonstrate handoff using SDK fluent API."""
    print("=" * 70)
    print("SDK Fluent API - Feature Creation with Handoff")
    print("=" * 70)

    with tempfile.TemporaryDirectory() as tmpdir:
        sdk = SDK(directory=tmpdir, agent="alice")

        # Create feature with built-in handoff
        feature = (
            sdk.features.create("Implement User Authentication")
            .set_priority("high")
            .add_steps(
                [
                    "Create JWT-based authentication",
                    "Implement OAuth 2.0 provider",
                    "Add refresh token rotation",
                    "Write comprehensive tests",
                ]
            )
            .set_description(
                "Multi-provider authentication system with secure token handling"
            )
            .complete_and_handoff(
                reason="requires cryptography expertise for OAuth flow",
                notes="Basic JWT implementation done. Need expert to implement OAuth providers and refresh token rotation. All tests passing.",
            )
            .save()
        )

        print(f"\n✅ Feature created: {feature.id}")
        print(f"   Title: {feature.title}")
        print(f"   Status: {feature.status}")
        print(f"   Handoff required: {feature.handoff_required}")
        print(f"   Previous agent: {feature.previous_agent}")
        print(f"   Reason: {feature.handoff_reason}")

        # Show lightweight context that would be sent to next agent
        print("\n📄 Lightweight context for next agent:")
        print("-" * 70)
        print(feature.to_context())
        print("-" * 70)

        # Show HTML generation
        html = feature.to_html()
        print(f"\n📄 HTML size: {len(html)} bytes")
        print(f"   Contains handoff section: {'data-handoff' in html}")


def demo_session_manager_handoff():
    """Demonstrate handoff using SessionManager."""
    print("\n" + "=" * 70)
    print("SessionManager - Agent-to-Agent Handoff")
    print("=" * 70)

    with tempfile.TemporaryDirectory() as tmpdir:
        manager = SessionManager(tmpdir)

        # Scenario: Agent Alice is working on a feature
        print("\n[Stage 1] Agent Alice claims and works on feature")
        print("-" * 70)

        # Create feature
        feature = Node(
            id="feat-auth-001",
            title="User Authentication System",
            type="feature",
            status="in-progress",
            priority="high",
            agent_assigned="alice",
            claimed_at=datetime.now(),
            claimed_by_session="session-alice-20251223",
            content="<p>Multi-provider authentication system with secure token handling</p>",
        )
        manager.features_graph.add(feature)

        print("✅ Feature created and claimed by alice")
        print(f"   ID: {feature.id}")
        print(f"   Title: {feature.title}")
        print(f"   Assigned to: {feature.agent_assigned}")

        # Alice does some work...
        print("\n[Stage 2] Alice works on feature, hits blockers")
        print("-" * 70)
        print("Alice completed:")
        print("  - Basic JWT implementation")
        print("  - Session management middleware")
        print("  - Unit tests")
        print("\nAlice needs help with:")
        print("  - OAuth 2.0 provider integration (needs cryptography expert)")
        print("  - Refresh token rotation (complex security)")

        # Alice hands off to Bob
        print("\n[Stage 3] Alice hands off to Bob")
        print("-" * 70)

        handed_off = manager.create_handoff(
            feature_id="feat-auth-001",
            reason="requires OAuth expertise and cryptography knowledge",
            notes=(
                "JWT implementation complete with working middleware. "
                "Session tests pass. Need expert to implement OAuth providers and "
                "secure token refresh rotation. See session-alice-20251223 for work context."
            ),
            agent="alice",
            next_agent="bob",
        )

        print("✅ Feature handed off by alice to bob")
        print(f"   Reason: {handed_off.handoff_reason}")
        print(f"   Notes: {handed_off.handoff_notes}")
        print(f"   Feature released: {handed_off.agent_assigned is None}")

        # Bob claims the feature
        print("\n[Stage 4] Bob claims the handed-off feature")
        print("-" * 70)

        bob_claims = manager.claim_feature(
            feature_id="feat-auth-001",
            agent="bob",
        )

        print("✅ Feature claimed by bob")
        print(f"   Assigned to: {bob_claims.agent_assigned}")
        print(f"   Handoff context preserved: {bob_claims.handoff_required}")
        print(f"   Previous agent: {bob_claims.previous_agent}")

        # Show Bob what he inherits
        print("\n📄 Context Bob receives:")
        print("-" * 70)
        print(bob_claims.to_context())
        print("-" * 70)


def demo_multiple_handoffs():
    """Demonstrate multiple handoffs in a sequence."""
    print("\n" + "=" * 70)
    print("Multi-Agent Relay - Multiple Handoffs")
    print("=" * 70)

    with tempfile.TemporaryDirectory() as tmpdir:
        manager = SessionManager(tmpdir)
        agents = ["alice", "bob", "charlie"]
        handoff_reasons = [
            "needs database expertise",
            "requires frontend knowledge",
            "needs performance optimization",
        ]

        # Create feature
        feature = Node(
            id="feat-dashboard-001",
            title="Analytics Dashboard",
            type="feature",
            status="todo",
            priority="high",
        )
        manager.features_graph.add(feature)
        print(f"\n✅ Feature created: {feature.id}")

        # Simulate passing through multiple agents
        for i, (agent, reason) in enumerate(zip(agents[:-1], handoff_reasons[:-1])):
            # Claim
            print(f"\n[Agent {i + 1}] {agent.capitalize()} claims feature")
            feature = manager.claim_feature(
                feature_id="feat-dashboard-001",
                agent=agent,
            )
            print(f"   Status: {feature.status}")

            # Do work
            print(f"   {agent.capitalize()} works on feature...")

            # Hand off
            next_agent = agents[i + 1]
            feature = manager.create_handoff(
                feature_id="feat-dashboard-001",
                reason=reason,
                notes=f"Work completed by {agent}, passed to {next_agent}",
                agent=agent,
                next_agent=next_agent,
            )
            print(f"   Handed off to: {next_agent.capitalize()}")
            print(f"   Reason: {reason}")

        # Final agent completes
        print(f"\n[Final] {agents[-1].capitalize()} completes feature")
        feature = manager.claim_feature(
            feature_id="feat-dashboard-001",
            agent=agents[-1],
        )
        print(f"   Status: {feature.status}")
        print(f"   Previous agent: {feature.previous_agent}")
        print(f"   Handoff audit trail preserved: {feature.handoff_required}")


def demo_handoff_efficiency():
    """Demonstrate the efficiency gains of handoff approach."""
    print("\n" + "=" * 70)
    print("Efficiency Demonstration")
    print("=" * 70)

    feature = Node(
        id="feat-perf-001",
        title="Performance Optimization",
        handoff_required=True,
        previous_agent="alice",
        handoff_reason="requires profiling expertise",
        handoff_notes=(
            "Identified bottleneck in database query layer. "
            "Query optimizer already attempted. Need profiling expert to identify "
            "whether issue is in application code or database. "
            "See session-alice-001 for query analysis."
        ),
    )

    # Show lightweight context vs full context
    context = feature.to_context()
    html = feature.to_html()

    print("\nHandoff Context Size:")
    print(
        f"  - Lightweight context: {len(context)} chars (~{len(context) / 4:.0f} tokens)"
    )
    print(f"  - Full HTML document: {len(html)} chars (~{len(html) / 4:.0f} tokens)")
    print(f"  - Ratio: {len(html) / len(context):.1f}x")

    print("\nToken Usage Comparison:")
    print("  - Using hyperlink reference: ~50 tokens")
    print(f"  - Embedding full context: ~{len(html) / 4:.0f} tokens")
    print(f"  - Savings: {(1 - 50 / (len(html) / 4)) * 100:.0f}%")

    print("\nBenefit: Agents can pass lightweight context with hyperlink to full HTML")
    print("         while maintaining git-friendly, queryable audit trail")


if __name__ == "__main__":
    demo_sdk_handoff()
    demo_session_manager_handoff()
    demo_multiple_handoffs()
    demo_handoff_efficiency()

    print("\n" + "=" * 70)
    print("Demo Complete!")
    print("=" * 70)
