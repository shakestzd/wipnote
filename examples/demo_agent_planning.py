#!/usr/bin/env python3
"""
Demo: AI Agent Strategic Planning with wipnote

Shows how AI agents can use the strategic planning features to:
- Find bottlenecks blocking the most work
- Identify parallel work opportunities
- Get smart recommendations on what to work on next
- Assess project risks
- Analyze impact of completing tasks

This is the AGENT-FRIENDLY interface - simple dicts, no complex objects.
"""

from wipnote import SDK


def main():
    print("\n" + "🤖 " * 35)
    print("AI AGENT STRATEGIC PLANNING DEMO")
    print("🤖 " * 35)

    # Initialize SDK with agent ID
    sdk = SDK(agent="claude")

    # =========================================================================
    # 1. FIND BOTTLENECKS
    # =========================================================================
    print("\n1. 🚧 Finding Bottlenecks (what's blocking the most work?)")
    print("=" * 70)

    bottlenecks = sdk.find_bottlenecks(top_n=3)

    if bottlenecks:
        print(f"\nFound {len(bottlenecks)} bottlenecks:")
        for i, bn in enumerate(bottlenecks, 1):
            print(f"\n{i}. {bn['title']}")
            print(f"   Status: {bn['status']} | Priority: {bn['priority']}")
            print(f"   Blocking: {bn['blocks_count']} tasks")
            print(f"   Impact score: {bn['impact_score']:.1f}")
            if bn["blocked_tasks"]:
                print(f"   Blocks: {', '.join(bn['blocked_tasks'][:3])}")
    else:
        print("  ✓ No bottlenecks found!")

    # =========================================================================
    # 2. GET PARALLEL WORK OPPORTUNITIES
    # =========================================================================
    print("\n2. ⚡ Finding Parallel Work (what can multiple agents do simultaneously?)")
    print("=" * 70)

    parallel = sdk.get_parallel_work(max_agents=5)

    print(
        f"\nMax parallelism: {parallel['max_parallelism']} tasks can be worked on at once"
    )
    print(f"Total ready now: {parallel['total_ready']} tasks")
    print(f"Dependency levels: {parallel['level_count']}")

    if parallel["ready_now"]:
        print(f"\nReady to start immediately ({len(parallel['ready_now'])} tasks):")
        for task_id in parallel["ready_now"]:
            feature = sdk.features.get(task_id)
            if feature:
                print(f"  - {feature.title} ({feature.priority} priority)")

    # =========================================================================
    # 3. GET SMART RECOMMENDATIONS
    # =========================================================================
    print("\n3. 💡 Smart Recommendations (what should I prioritize?)")
    print("=" * 70)

    recs = sdk.recommend_next_work(agent_count=3)

    if recs:
        print(f"\nTop {len(recs)} recommended tasks:")
        for i, rec in enumerate(recs, 1):
            print(f"\n{i}. {rec['title']}")
            print(f"   Priority: {rec['priority']} | Score: {rec['score']:.2f}")
            if rec["estimated_hours"] > 0:
                print(f"   Estimated: {rec['estimated_hours']:.1f} hours")
            print(f"   Unlocks: {rec['unlocks_count']} tasks")
            print("   Reasons:")
            for reason in rec["reasons"][:3]:
                print(f"     - {reason}")
    else:
        print("  ✓ No pending tasks to recommend!")

    # =========================================================================
    # 4. ASSESS RISKS
    # =========================================================================
    print("\n4. ⚠️  Risk Assessment (are there dependency issues?)")
    print("=" * 70)

    risks = sdk.assess_risks()

    if risks["high_risk_count"] > 0:
        print(f"\n⚠️  {risks['high_risk_count']} high-risk tasks found:")
        for task in risks["high_risk_tasks"][:3]:
            print(f"\n  {task['title']} (risk: {task['risk_score']:.2f})")
            for factor in task["risk_factors"]:
                print(f"    - {factor}")
    else:
        print("  ✓ No high-risk tasks!")

    if risks["circular_dependencies"]:
        print(f"\n⚠️  {len(risks['circular_dependencies'])} circular dependencies:")
        for cycle in risks["circular_dependencies"][:3]:
            print(f"    - {' → '.join(cycle)}")
    else:
        print("  ✓ No circular dependencies")

    if risks["orphaned_count"] > 0:
        print(f"\n⚠️  {risks['orphaned_count']} orphaned tasks (no dependents)")
    else:
        print("  ✓ No orphaned tasks")

    if risks["recommendations"]:
        print("\nRecommendations:")
        for rec in risks["recommendations"][:3]:
            print(f"  • {rec}")

    # =========================================================================
    # 5. ANALYZE IMPACT (if there's a task in progress)
    # =========================================================================
    print("\n5. 📊 Impact Analysis (what does completing a task unlock?)")
    print("=" * 70)

    in_progress = sdk.features.where(status="in-progress")
    if in_progress:
        task = in_progress[0]
        impact = sdk.analyze_impact(task.id)

        print(f"\nAnalyzing: {task.title}")
        print(f"  Direct dependents: {impact['direct_dependents']}")
        print(f"  Total downstream impact: {impact['total_impact']} tasks")
        print(
            f"  Completion unlocks: {impact['completion_impact']:.1f}% of remaining work"
        )
        if impact["affected_tasks"]:
            print(f"  Unlocks: {', '.join(impact['affected_tasks'][:5])}")
    else:
        print("  No in-progress tasks to analyze")

    # =========================================================================
    # AGENT DECISION-MAKING EXAMPLE
    # =========================================================================
    print("\n" + "=" * 70)
    print("🤖 AGENT DECISION FLOW EXAMPLE")
    print("=" * 70)

    print("\nHow an AI agent might use these features:")
    print("\n1. Check for bottlenecks → Focus on unblocking high-impact work")
    print("2. Get recommendations → See what's most valuable to work on")
    print("3. Check parallel work → Coordinate with other agents if available")
    print("4. Assess risks → Be aware of potential issues")
    print("5. Analyze impact → Understand the value of completing tasks")

    if recs:
        top_rec = recs[0]
        print(f"\n💡 SUGGESTED ACTION: Work on '{top_rec['title']}'")
        print(
            f"   Why: Score {top_rec['score']:.1f} - {', '.join(top_rec['reasons'][:2])}"
        )

    print("\n" + "=" * 70)
    print("✅ Strategic planning features ready for AI agents!")
    print("=" * 70 + "\n")


if __name__ == "__main__":
    main()
