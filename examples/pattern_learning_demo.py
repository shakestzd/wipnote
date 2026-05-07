"""
Demo: Pattern Learning from Agent Behavior

Shows how to use the Pattern Learning feature to identify workflow patterns,
anti-patterns, and optimization opportunities from tool call history.
"""

from wipnote import SDK


def main():
    """Demo pattern learning capabilities."""
    # Initialize SDK
    sdk = SDK(agent="demo")

    print("=== Pattern Learning Demo ===\n")

    # 1. Detect patterns from tool call history
    print("1. Detecting patterns from tool call history...")
    patterns = sdk.pattern_learning.detect_patterns(
        window_size=3,  # 3-tool sequences
        min_frequency=5,  # Must occur at least 5 times
    )
    print(f"   Found {len(patterns)} patterns\n")

    # Show top 5 patterns
    print("   Top 5 Most Frequent Patterns:")
    for i, pattern in enumerate(patterns[:5], 1):
        print(f"   {i}. {' → '.join(pattern.sequence)}")
        print(f"      Frequency: {pattern.frequency} times")
        print(f"      Success Rate: {pattern.success_rate:.1f}%")
        print(f"      Avg Duration: {pattern.avg_duration_seconds:.1f}s\n")

    # 2. Get recommendations
    print("\n2. Getting recommendations based on patterns...")
    recommendations = sdk.pattern_learning.get_recommendations(limit=3)
    print(f"   Generated {len(recommendations)} recommendations:\n")

    for i, rec in enumerate(recommendations, 1):
        print(f"   {i}. {rec.title}")
        print(f"      {rec.description}")
        print(f"      Impact Score: {rec.impact_score:.1f}\n")

    # 3. Identify anti-patterns
    print("\n3. Identifying anti-patterns...")
    anti_patterns = sdk.pattern_learning.get_anti_patterns()
    print(f"   Found {len(anti_patterns)} anti-patterns:\n")

    for i, anti in enumerate(anti_patterns[:3], 1):
        print(f"   {i}. {anti.title}")
        print(f"      {anti.description}")
        print(f"      Impact Score: {anti.impact_score:.1f}\n")

    # 4. Export learnings to markdown
    print("\n4. Exporting learnings to markdown...")
    output_path = ".wipnote/pattern_learnings.md"
    sdk.pattern_learning.export_learnings(output_path)
    print(f"   Exported to: {output_path}\n")

    # 5. Provide feedback on patterns
    print("5. Pattern feedback loop:")
    print("   You can provide feedback on insights to improve recommendations:")
    print(
        "   - sdk.pattern_learning.learning_loop.update_feedback(pattern_id, 1)  # Helpful"
    )
    print(
        "   - sdk.pattern_learning.learning_loop.update_feedback(pattern_id, 0)  # Neutral"
    )
    print(
        "   - sdk.pattern_learning.learning_loop.update_feedback(pattern_id, -1) # Not helpful"
    )

    print("\n=== Demo Complete ===")


if __name__ == "__main__":
    main()
