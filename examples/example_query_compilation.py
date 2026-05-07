#!/usr/bin/env python3
"""
Example demonstrating query compilation for CSS selector reuse.

This shows how to pre-compile frequently used selectors for better performance.
"""

import tempfile

from wipnote import wipnote
from wipnote.models import Node


def main():
    """Demonstrate query compilation usage."""
    print("🔍 Query Compilation Example\n")
    print("=" * 60)

    with tempfile.TemporaryDirectory() as tmpdir:
        # Create graph
        graph = wipnote(tmpdir, auto_load=False)

        # Add sample nodes
        print("\n📝 Adding sample nodes...")
        graph.add(
            Node(id="feat-001", title="Auth System", status="blocked", priority="high")
        )
        graph.add(
            Node(id="feat-002", title="Database", status="blocked", priority="critical")
        )
        graph.add(Node(id="feat-003", title="API", status="todo", priority="high"))
        graph.add(Node(id="feat-004", title="UI", status="done", priority="medium"))
        graph.add(Node(id="feat-005", title="Tests", status="blocked", priority="low"))

        # Reset metrics for clean measurement
        graph.reset_metrics()

        # Example 1: Regular query
        print("\n📊 Example 1: Regular Query")
        print("-" * 60)
        results1 = graph.query("[data-status='blocked']")
        print(f"Found {len(results1)} blocked items:")
        for node in results1:
            print(f"  - {node.id}: {node.title} ({node.priority} priority)")

        # Example 2: Pre-compile for reuse
        print("\n📊 Example 2: Pre-compiled Query")
        print("-" * 60)
        compiled = graph.compile_query("[data-status='blocked']")
        print(f"Compiled query for selector: {compiled.selector}")

        # Use compiled query multiple times
        results2 = graph.query_compiled(compiled)
        results3 = graph.query_compiled(compiled)
        print(f"First execution: {len(results2)} results")
        print(f"Second execution: {len(results3)} results (uses cache)")

        # Example 3: Multiple compiled queries
        print("\n📊 Example 3: Multiple Compiled Queries")
        print("-" * 60)
        high_priority = graph.compile_query("[data-priority='high']")
        critical = graph.compile_query("[data-priority='critical']")

        high_results = graph.query_compiled(high_priority)
        critical_results = graph.query_compiled(critical)

        print(f"High priority items: {len(high_results)}")
        print(f"Critical priority items: {len(critical_results)}")

        # Show metrics
        print("\n📈 Performance Metrics")
        print("-" * 60)
        metrics = graph.metrics
        print(f"Total queries: {metrics['query_count']}")
        print(f"Cache hits: {metrics['cache_hits']}")
        print(f"Cache misses: {metrics['cache_misses']}")
        print(f"Cache hit rate: {metrics['cache_hit_rate']}")
        print(f"\nCompiled queries created: {metrics['compiled_queries']}")
        print(f"Compilation cache hits: {metrics['compiled_query_hits']}")
        print(f"Compilation hit rate: {metrics['compilation_hit_rate']}")
        print(f"Compiled queries cached: {metrics['compiled_queries_cached']}")

        # Example 4: Compilation cache reuse
        print("\n📊 Example 4: Compilation Cache Reuse")
        print("-" * 60)
        # Compiling the same selector again returns cached instance
        blocked_query1 = graph.compile_query("[data-status='blocked']")
        blocked_query2 = graph.compile_query("[data-status='blocked']")

        if blocked_query1 is blocked_query2:
            print("✅ Compilation cache works - same instance returned")
        else:
            print("❌ Unexpected - different instances")

        print("\n" + "=" * 60)
        print("✅ Query compilation example completed successfully!")


if __name__ == "__main__":
    main()
