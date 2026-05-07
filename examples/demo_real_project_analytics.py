#!/usr/bin/env python3
"""
Demo of dependency analytics on the real wipnote project.

Shows how to use SDK.dep_analytics to analyze the actual project dependencies.
"""

from wipnote import SDK


def main():
    print("\n" + "📊 " * 35)
    print("WIPNOTE PROJECT - DEPENDENCY ANALYTICS")
    print("📊 " * 35)

    # Initialize SDK
    sdk = SDK(agent="claude")

    print("\n1. Finding Bottlenecks (features blocking the most work)...")
    print("=" * 70)

    bottlenecks = sdk.dep_analytics.find_bottlenecks(
        status_filter=["in-progress", "todo", "blocked"], top_n=5
    )

    if bottlenecks:
        for i, bn in enumerate(bottlenecks, 1):
            print(f"\n{i}. {bn.title}")
            print(f"   Status: {bn.status} | Priority: {bn.priority}")
            print(
                f"   Blocking: {bn.transitive_blocking} features (impact: {bn.weighted_impact:.1f})"
            )
            if bn.blocked_nodes:
                print(f"   Directly blocks: {', '.join(bn.blocked_nodes[:3])}")
    else:
        print("  ✓ No bottlenecks found!")

    print("\n2. Parallelization Opportunities (what can we work on simultaneously?)")
    print("=" * 70)

    parallel = sdk.dep_analytics.find_parallelizable_work(status="todo")

    print(
        f"\nMax parallelism: {parallel.max_parallelism} features can be worked on at once"
    )

    if parallel.dependency_levels:
        print(
            f"\nCurrent level (ready to start): {len(parallel.dependency_levels[0].nodes)} features"
        )
        for node_id in parallel.dependency_levels[0].nodes[:5]:
            feature = sdk.features.get(node_id)
            if feature:
                print(f"  - {feature.title} ({feature.priority} priority)")

    print("\n3. Work Recommendations (what should we prioritize?)")
    print("=" * 70)

    recs = sdk.dep_analytics.recommend_next_tasks(agent_count=3, lookahead=5)

    if recs.recommendations:
        print(f"\nTop {len(recs.recommendations)} recommended tasks:")
        for i, rec in enumerate(recs.recommendations[:5], 1):
            print(f"\n{i}. {rec.title}")
            print(f"   Priority: {rec.priority} | Score: {rec.score:.2f}")
            print(f"   Reasons: {', '.join(rec.reasons[:2])}")
            if rec.unlocks:
                print(f"   Unlocks: {len(rec.unlocks)} features")
    else:
        print("  ✓ No pending tasks to recommend!")

    print("\n4. Risk Assessment (are there dependency issues?)")
    print("=" * 70)

    risk = sdk.dep_analytics.assess_dependency_risk(spof_threshold=2)

    if risk.high_risk:
        print(f"\n⚠️  High-risk nodes: {len(risk.high_risk)}")
        for node in risk.high_risk[:3]:
            print(f"\n  {node.title} (risk: {node.risk_score:.2f})")
            for factor in node.risk_factors:
                print(f"    - [{factor.severity}] {factor.description}")
    else:
        print("  ✓ No high-risk dependencies!")

    if risk.circular_dependencies:
        print(f"\n⚠️  Circular dependencies: {len(risk.circular_dependencies)}")
        for cycle in risk.circular_dependencies[:3]:
            print(f"    - {' -> '.join(cycle)}")
    else:
        print("  ✓ No circular dependencies")

    if risk.orphaned_nodes:
        print(f"\n⚠️  Orphaned nodes (no dependents): {len(risk.orphaned_nodes)}")
    else:
        print("  ✓ No orphaned nodes")

    if risk.recommendations:
        print("\nRecommendations:")
        for rec in risk.recommendations[:3]:
            print(f"  • {rec}")

    print("\n5. Impact Analysis (what does completing a feature unlock?)")
    print("=" * 70)

    # Find a feature to analyze
    in_progress = sdk.features.where(status="in-progress")
    if in_progress:
        feature = in_progress[0]
        impact = sdk.dep_analytics.impact_analysis(feature.id)

        print(f"\nAnalyzing: {feature.title}")
        print(f"  Direct dependents: {impact.direct_dependents}")
        print(f"  Transitive impact: {impact.transitive_dependents} features")
        print(
            f"  Completion unlocks: {impact.completion_impact:.1f}% of remaining work"
        )

        if impact.affected_nodes:
            print(f"  Affects: {', '.join(impact.affected_nodes[:5])}")
    else:
        print("  No in-progress features to analyze")

    print("\n" + "=" * 70)
    print("✅ Dependency analytics complete!")
    print("=" * 70 + "\n")


if __name__ == "__main__":
    main()
