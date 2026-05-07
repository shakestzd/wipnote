#!/usr/bin/env python3
"""
wipnote Documentation Site Demo

Demonstrates using wipnote for building a static documentation site.
No build step, no framework - just HTML files with smart linking.
"""

import sys
from pathlib import Path

# Add src to path for development
sys.path.insert(0, str(Path(__file__).parent.parent.parent / "src" / "python"))

from wipnote import SDK, Edge, Node


def create_documentation_pages(sdk: SDK):
    """Create a sample documentation site structure."""
    print("📄 Creating documentation pages...")

    # Homepage / Table of Contents
    homepage = Node(
        id="doc-index",
        title="wipnote Documentation",
        type="doc",
        status="active",
        priority="high",
        content="""
        <h1>wipnote Documentation</h1>
        <p>Welcome to wipnote - "HTML is All You Need"</p>

        <h2>Getting Started</h2>
        <ul>
            <li><a href="#doc-installation">Installation</a></li>
            <li><a href="#doc-quickstart">Quickstart Guide</a></li>
        </ul>

        <h2>API Reference</h2>
        <ul>
            <li><a href="#doc-api-overview">API Overview</a></li>
            <li><a href="#doc-api-graph">Graph API</a></li>
            <li><a href="#doc-api-sdk">SDK Reference</a></li>
        </ul>

        <h2>Examples</h2>
        <ul>
            <li><a href="#doc-example-basic">Basic Usage</a></li>
            <li><a href="#doc-example-advanced">Advanced Patterns</a></li>
        </ul>
        """,
        edges={
            "navigates_to": [
                Edge(target_id="doc-installation", relationship="navigates_to"),
                Edge(target_id="doc-quickstart", relationship="navigates_to"),
                Edge(target_id="doc-api-overview", relationship="navigates_to"),
            ]
        },
    )

    # Getting Started - Installation
    installation = Node(
        id="doc-installation",
        title="Installation",
        type="doc",
        status="active",
        priority="high",
        content="""
        <h1>Installation</h1>

        <h2>Prerequisites</h2>
        <ul>
            <li>Python 3.10 or higher</li>
            <li>pip or uv package manager</li>
        </ul>

        <h2>Install via pip</h2>
        <pre><code>pip install wipnote</code></pre>

        <h2>Install via uv</h2>
        <pre><code>uv pip install wipnote</code></pre>

        <h2>Verify Installation</h2>
        <pre><code>python -c "import wipnote; print(wipnote.__version__)"</code></pre>

        <h2>Next Steps</h2>
        <p>Ready to start? Check out the <a href="#doc-quickstart">Quickstart Guide</a>.</p>
        """,
        edges={
            "previous": [Edge(target_id="doc-index", relationship="previous")],
            "next": [Edge(target_id="doc-quickstart", relationship="next")],
            "related": [Edge(target_id="doc-quickstart", relationship="related")],
        },
    )

    # Getting Started - Quickstart
    quickstart = Node(
        id="doc-quickstart",
        title="Quickstart Guide",
        type="doc",
        status="active",
        priority="high",
        content="""
        <h1>Quickstart Guide</h1>

        <h2>Your First Graph</h2>
        <pre><code>from wipnote import wipnote, Node

# Create a graph
graph = wipnote("my-graph")

# Add a node
node = Node(
    id="task-001",
    title="My First Task",
    type="task",
    status="todo"
)

# Save to HTML file
graph.add(node)</code></pre>

        <h2>Query Your Graph</h2>
        <pre><code># Find all tasks
tasks = graph.query("[data-type='task']")

# Find completed tasks
done = graph.query("[data-status='done']")</code></pre>

        <h2>Using the SDK</h2>
        <pre><code>from wipnote import SDK

sdk = SDK(directory=".", agent="my-agent")

# Create a feature
feature = sdk.features.create("Add authentication")</code></pre>

        <h2>Next Steps</h2>
        <ul>
            <li>Explore the <a href="#doc-api-overview">API Reference</a></li>
            <li>Check out <a href="#doc-example-basic">Basic Examples</a></li>
        </ul>
        """,
        edges={
            "previous": [Edge(target_id="doc-installation", relationship="previous")],
            "next": [Edge(target_id="doc-api-overview", relationship="next")],
            "related": [
                Edge(target_id="doc-api-overview", relationship="related"),
                Edge(target_id="doc-example-basic", relationship="related"),
            ],
        },
    )

    # API Reference - Overview
    api_overview = Node(
        id="doc-api-overview",
        title="API Overview",
        type="doc",
        status="active",
        priority="medium",
        content="""
        <h1>API Overview</h1>

        <h2>Core Concepts</h2>
        <p>wipnote provides three main APIs:</p>

        <h3>1. Graph API</h3>
        <p>Low-level graph operations for creating and querying HTML nodes.</p>
        <p>See: <a href="#doc-api-graph">Graph API Reference</a></p>

        <h3>2. SDK API</h3>
        <p>High-level interface for feature tracking and agent coordination.</p>
        <p>See: <a href="#doc-api-sdk">SDK Reference</a></p>

        <h3>3. Agent Interface</h3>
        <p>Simplified API for AI agents to interact with the graph.</p>

        <h2>Philosophy</h2>
        <p>wipnote uses web standards:</p>
        <ul>
            <li>HTML files = graph nodes</li>
            <li>Hyperlinks = graph edges</li>
            <li>CSS selectors = query language</li>
        </ul>
        """,
        edges={
            "previous": [Edge(target_id="doc-quickstart", relationship="previous")],
            "next": [Edge(target_id="doc-api-graph", relationship="next")],
            "related": [
                Edge(target_id="doc-api-graph", relationship="related"),
                Edge(target_id="doc-api-sdk", relationship="related"),
            ],
        },
    )

    # API Reference - Graph API
    graph_api = Node(
        id="doc-api-graph",
        title="Graph API Reference",
        type="doc",
        status="active",
        priority="medium",
        content="""
        <h1>Graph API Reference</h1>

        <h2>wipnote Class</h2>

        <h3>Constructor</h3>
        <pre><code>graph = wipnote(directory="path/to/graph")</code></pre>

        <h3>Methods</h3>

        <h4>add(node, overwrite=False)</h4>
        <p>Add a node to the graph and save as HTML file.</p>
        <pre><code>filepath = graph.add(node)</code></pre>

        <h4>get(node_id)</h4>
        <p>Retrieve a node by ID.</p>
        <pre><code>node = graph.get("task-001")</code></pre>

        <h4>query(selector)</h4>
        <p>Query nodes using CSS selectors.</p>
        <pre><code>results = graph.query("[data-status='done']")</code></pre>

        <h4>shortest_path(start, end, relationship=None)</h4>
        <p>Find shortest path between two nodes.</p>
        <pre><code>path = graph.shortest_path("a", "b", relationship="blocks")</code></pre>

        <h2>Related</h2>
        <ul>
            <li><a href="#doc-api-sdk">SDK Reference</a></li>
            <li><a href="#doc-example-basic">Basic Examples</a></li>
        </ul>
        """,
        edges={
            "previous": [Edge(target_id="doc-api-overview", relationship="previous")],
            "next": [Edge(target_id="doc-api-sdk", relationship="next")],
            "related": [
                Edge(target_id="doc-api-sdk", relationship="related"),
                Edge(target_id="doc-example-basic", relationship="related"),
            ],
        },
    )

    # API Reference - SDK
    sdk_api = Node(
        id="doc-api-sdk",
        title="SDK Reference",
        type="doc",
        status="active",
        priority="medium",
        content="""
        <h1>SDK Reference</h1>

        <h2>SDK Class</h2>

        <h3>Constructor</h3>
        <pre><code>sdk = SDK(directory=".", agent="my-agent")</code></pre>

        <h3>Features API</h3>

        <h4>create(title)</h4>
        <p>Create a new feature with fluent API.</p>
        <pre><code>feature = sdk.features.create("Add login") \\
    .set_priority("high") \\
    .add_step("Create auth route") \\
    .save()</code></pre>

        <h4>where(**filters)</h4>
        <p>Query features by attributes.</p>
        <pre><code>high_priority = sdk.features.where(priority="high")</code></pre>

        <h4>get(feature_id)</h4>
        <p>Get a specific feature.</p>
        <pre><code>feature = sdk.features.get("feature-001")</code></pre>

        <h3>Sessions API</h3>

        <h4>start(title)</h4>
        <p>Start a new work session.</p>
        <pre><code>session = sdk.sessions.start("Implement auth")</code></pre>

        <h2>Related</h2>
        <ul>
            <li><a href="#doc-api-graph">Graph API Reference</a></li>
            <li><a href="#doc-example-advanced">Advanced Examples</a></li>
        </ul>
        """,
        edges={
            "previous": [Edge(target_id="doc-api-graph", relationship="previous")],
            "next": [Edge(target_id="doc-example-basic", relationship="next")],
            "related": [
                Edge(target_id="doc-api-graph", relationship="related"),
                Edge(target_id="doc-example-advanced", relationship="related"),
            ],
        },
    )

    # Examples - Basic
    basic_example = Node(
        id="doc-example-basic",
        title="Basic Usage Examples",
        type="doc",
        status="active",
        priority="low",
        content="""
        <h1>Basic Usage Examples</h1>

        <h2>Example 1: Todo List</h2>
        <p>Create a simple todo list using wipnote.</p>
        <pre><code>from wipnote import wipnote, Node

graph = wipnote("todos")

# Create tasks
task1 = Node(id="task-1", title="Write docs", type="task")
task2 = Node(id="task-2", title="Add tests", type="task")

# Add to graph
graph.add(task1)
graph.add(task2)</code></pre>

        <h2>Example 2: Query Tasks</h2>
        <pre><code># Find all tasks
all_tasks = graph.query("[data-type='task']")

# Find high priority
urgent = graph.query("[data-priority='high']")</code></pre>

        <h2>Example 3: Track Dependencies</h2>
        <pre><code>from wipnote import Edge

task = Node(
    id="task-3",
    title="Deploy",
    edges={
        "blocked_by": [
            Edge(target_id="task-1"),
            Edge(target_id="task-2")
        ]
    }
)

graph.add(task)</code></pre>

        <h2>See Also</h2>
        <ul>
            <li><a href="#doc-example-advanced">Advanced Examples</a></li>
            <li><a href="#doc-api-graph">Graph API Reference</a></li>
        </ul>
        """,
        edges={
            "previous": [Edge(target_id="doc-api-sdk", relationship="previous")],
            "next": [Edge(target_id="doc-example-advanced", relationship="next")],
            "related": [
                Edge(target_id="doc-example-advanced", relationship="related"),
                Edge(target_id="doc-api-graph", relationship="related"),
            ],
        },
    )

    # Examples - Advanced
    advanced_example = Node(
        id="doc-example-advanced",
        title="Advanced Patterns",
        type="doc",
        status="active",
        priority="low",
        content="""
        <h1>Advanced Patterns</h1>

        <h2>Pattern 1: Graph Traversal</h2>
        <p>Find all transitive dependencies of a task.</p>
        <pre><code>deps = graph.transitive_deps("task-001", relationship="blocked_by")
print(f"Task depends on: {deps}")</code></pre>

        <h2>Pattern 2: Bottleneck Detection</h2>
        <p>Find tasks that block the most other tasks.</p>
        <pre><code>bottlenecks = graph.find_bottlenecks(top_n=5)
for task_id, blocked_count in bottlenecks:
    print(f"{task_id} blocks {blocked_count} tasks")</code></pre>

        <h2>Pattern 3: Agent Coordination</h2>
        <p>Multiple agents working on the same graph.</p>
        <pre><code>from wipnote.agents import AgentInterface

agent = AgentInterface(".", agent_id="agent-1")

# Get next available task
task = agent.get_next_task(priority="high")

# Claim it
agent.claim_task(task.id, agent_id="agent-1")

# Work on it...
agent.complete_step(task.id, step_index=0)

# Complete it
agent.complete_task(task.id)</code></pre>

        <h2>Pattern 4: Visualization</h2>
        <p>Generate Mermaid diagrams of your graph.</p>
        <pre><code>mermaid = graph.to_mermaid(relationship="blocked_by")
print(mermaid)
# Paste into https://mermaid.live/</code></pre>

        <h2>See Also</h2>
        <ul>
            <li><a href="#doc-example-basic">Basic Examples</a></li>
            <li><a href="#doc-api-sdk">SDK Reference</a></li>
        </ul>
        """,
        edges={
            "previous": [Edge(target_id="doc-example-basic", relationship="previous")],
            "related": [
                Edge(target_id="doc-example-basic", relationship="related"),
                Edge(target_id="doc-api-sdk", relationship="related"),
            ],
        },
    )

    # Add all pages to graph
    pages = [
        homepage,
        installation,
        quickstart,
        api_overview,
        graph_api,
        sdk_api,
        basic_example,
        advanced_example,
    ]

    for page in pages:
        sdk.features._ensure_graph().add(page, overwrite=True)
        print(f"   ✅ Created: {page.title}")

    print(f"\n📚 Created {len(pages)} documentation pages")
    return pages


def demonstrate_navigation(sdk: SDK, pages: list):
    """Show how navigation works in the documentation site."""
    print("\n🧭 Navigation Patterns")
    print("=" * 80)

    # Show breadcrumb trail
    print("\n📍 Breadcrumb Example (Installation → Quickstart → API Overview):")
    breadcrumb = ["doc-installation", "doc-quickstart", "doc-api-overview"]
    for i, doc_id in enumerate(breadcrumb):
        doc = sdk.features.get(doc_id)
        if doc:
            indent = "   " + "  " * i
            arrow = "→ " if i > 0 else ""
            print(f"{indent}{arrow}{doc.title}")

    # Show previous/next navigation
    print("\n⬅️ ➡️ Previous/Next Navigation from 'Quickstart':")
    quickstart = sdk.features.get("doc-quickstart")
    if quickstart:
        prev_edges = quickstart.edges.get("previous", [])
        next_edges = quickstart.edges.get("next", [])

        if prev_edges:
            prev_doc = sdk.features.get(prev_edges[0].target_id)
            if prev_doc:
                print(f"   Previous: {prev_doc.title}")

        print(f"   Current:  {quickstart.title}")

        if next_edges:
            next_doc = sdk.features.get(next_edges[0].target_id)
            if next_doc:
                print(f"   Next:     {next_doc.title}")

    # Show related pages
    print("\n🔗 Related Pages from 'API Overview':")
    api_overview = sdk.features.get("doc-api-overview")
    if api_overview:
        related_edges = api_overview.edges.get("related", [])
        for edge in related_edges:
            related_doc = sdk.features.get(edge.target_id)
            if related_doc:
                print(f"   - {related_doc.title}")


def demonstrate_search(sdk: SDK):
    """Show how to search documentation."""
    print("\n" + "=" * 80)
    print("🔍 Documentation Search")
    print("=" * 80)

    # Search by type
    all_docs = sdk.features.where(type="doc")
    print(f"\n📄 Total documentation pages: {len(all_docs)}")

    # Search by status
    active = sdk.features.where(type="doc", status="active")
    print(f"   Active: {len(active)}")

    # Search by priority
    important = sdk.features.where(type="doc", priority="high")
    print("\n⭐ High-priority pages:")
    for doc in important:
        print(f"   - {doc.title}")


def generate_table_of_contents(sdk: SDK):
    """Generate a table of contents from the graph."""
    print("\n" + "=" * 80)
    print("📑 Auto-Generated Table of Contents")
    print("=" * 80)

    # Start from homepage
    homepage = sdk.features.get("doc-index")
    if not homepage:
        print("   ⚠️  Homepage not found")
        return

    print(f"\n{homepage.title}")

    # Group by category (based on ID prefix)
    categories = {"Getting Started": [], "API Reference": [], "Examples": []}

    all_docs = sdk.features.where(type="doc")
    for doc in all_docs:
        if doc.id == "doc-index":
            continue
        elif doc.id.startswith("doc-installation") or doc.id.startswith(
            "doc-quickstart"
        ):
            categories["Getting Started"].append(doc)
        elif doc.id.startswith("doc-api"):
            categories["API Reference"].append(doc)
        elif doc.id.startswith("doc-example"):
            categories["Examples"].append(doc)

    # Print TOC
    for category, docs in categories.items():
        if docs:
            print(f"\n{category}")
            for doc in docs:
                print(f"   → {doc.title}")


def main():
    """Run the documentation site demo."""
    print("=" * 80)
    print("wipnote Documentation Site Demo")
    print("'HTML is All You Need' - Documentation Edition")
    print("=" * 80)

    # Initialize SDK
    sdk = SDK(directory=".", agent="doc-generator")

    # Create documentation pages
    pages = create_documentation_pages(sdk)

    # Demonstrate navigation
    demonstrate_navigation(sdk, pages)

    # Demonstrate search
    demonstrate_search(sdk)

    # Generate table of contents
    generate_table_of_contents(sdk)

    # Show visualization
    print("\n" + "=" * 80)
    print("📊 Documentation Graph")
    print("=" * 80)

    mermaid = sdk._graph.to_mermaid(relationship="next")
    print("\n🔶 Navigation Flow (paste into https://mermaid.live/):")
    print(mermaid)

    print("\n" + "=" * 80)
    print("Demo complete!")
    print("")
    print("💡 Tips:")
    print("   - Open the generated HTML files in a browser")
    print("   - Follow Previous/Next links to navigate")
    print("   - Use Related links to explore topics")
    print("   - No build step needed - just HTML files!")
    print("=" * 80)


if __name__ == "__main__":
    main()
