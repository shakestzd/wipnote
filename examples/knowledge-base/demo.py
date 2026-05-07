#!/usr/bin/env python3
"""
wipnote Knowledge Base Demo

Demonstrates using wipnote for personal knowledge management.
Think Obsidian or Roam Research, but with HTML files.
"""

import sys
from datetime import date
from pathlib import Path

# Add src to path for development
sys.path.insert(0, str(Path(__file__).parent.parent.parent / "src" / "python"))

from wipnote import SDK, Edge, Node


def create_notes(sdk: SDK):
    """Create a sample knowledge base with interconnected notes."""
    print("📝 Creating knowledge base notes...")

    # Daily note
    (sdk.features.create(f"Daily Note: {date.today()}").set_priority("low").save())

    # Concept notes
    wipnote_note = Node(
        id="note-wipnote",
        title="wipnote",
        type="note",
        status="active",
        priority="high",
        content="""
        <h2>wipnote - HTML is All You Need</h2>
        <p>A graph database framework built entirely on web standards.</p>

        <h3>Core Concept</h3>
        <p>Instead of using Neo4j or other graph databases, wipnote uses:</p>
        <ul>
            <li>HTML files as nodes</li>
            <li>Hyperlinks as edges</li>
            <li>CSS selectors as query language</li>
        </ul>

        <h3>Key Benefits</h3>
        <ul>
            <li>Zero dependencies - no Docker, no JVM</li>
            <li>Git-friendly - text diffs work perfectly</li>
            <li>Human-readable - view source in browser</li>
            <li>Offline-first - just files</li>
        </ul>

        <h3>Related Concepts</h3>
        <p>See: [[Web Standards]], [[Graph Databases]], [[AI Agents]]</p>
        """,
        edges={
            "related": [
                Edge(target_id="note-web-standards", relationship="related"),
                Edge(target_id="note-graph-db", relationship="related"),
                Edge(target_id="note-ai-agents", relationship="related"),
            ]
        },
    )

    web_standards_note = Node(
        id="note-web-standards",
        title="Web Standards",
        type="note",
        status="active",
        priority="medium",
        content="""
        <h2>Web Standards</h2>
        <p>The foundation of the web: HTML, CSS, and JavaScript.</p>

        <h3>Why They Matter</h3>
        <ul>
            <li>Universal - work everywhere</li>
            <li>Stable - decades of backwards compatibility</li>
            <li>Open - W3C standards, not proprietary</li>
            <li>Well-documented - millions of developers know them</li>
        </ul>

        <h3>Applications</h3>
        <ul>
            <li>wipnote - graph database in HTML</li>
            <li>Progressive Web Apps</li>
            <li>Static site generators</li>
        </ul>
        """,
        edges={"related": [Edge(target_id="note-wipnote", relationship="related")]},
    )

    graph_db_note = Node(
        id="note-graph-db",
        title="Graph Databases",
        type="note",
        status="active",
        priority="medium",
        content="""
        <h2>Graph Databases</h2>
        <p>Databases optimized for storing and querying graph structures.</p>

        <h3>Traditional Options</h3>
        <ul>
            <li>Neo4j - Most popular, Cypher query language</li>
            <li>Memgraph - High-performance, Cypher compatible</li>
            <li>ArangoDB - Multi-model database</li>
        </ul>

        <h3>The HTML Alternative</h3>
        <p>wipnote proves you don't need a traditional graph database.</p>
        <p>HTML files + hyperlinks = graph database!</p>
        """,
        edges={"related": [Edge(target_id="note-wipnote", relationship="related")]},
    )

    ai_agents_note = Node(
        id="note-ai-agents",
        title="AI Agents",
        type="note",
        status="active",
        priority="high",
        content="""
        <h2>AI Agents</h2>
        <p>Autonomous software that can reason and take actions.</p>

        <h3>Coordination Challenges</h3>
        <ul>
            <li>Shared state management</li>
            <li>Preventing conflicts</li>
            <li>Dependency tracking</li>
            <li>Human observability</li>
        </ul>

        <h3>wipnote Solution</h3>
        <p>HTML files provide a simple coordination mechanism:</p>
        <ul>
            <li>Git handles merging and conflicts</li>
            <li>Hyperlinks show dependencies</li>
            <li>Browser shows current state</li>
            <li>No custom protocol needed</li>
        </ul>
        """,
        edges={"related": [Edge(target_id="note-wipnote", relationship="related")]},
    )

    # Add all notes to graph
    for note in [wipnote_note, web_standards_note, graph_db_note, ai_agents_note]:
        sdk.features._ensure_graph().add(note, overwrite=True)
        print(f"   ✅ Created: {note.title}")

    print("\n📚 Created 4 interconnected concept notes")


def demonstrate_queries(sdk: SDK):
    """Show different ways to query the knowledge base."""
    print("\n🔍 Knowledge Base Queries")
    print("=" * 80)

    # Get all notes
    all_notes = sdk.features.where(type="note")
    print(f"\n📝 Total notes: {len(all_notes)}")

    # Find high-priority notes
    important = sdk.features.where(type="note", priority="high")
    print("\n⭐ High-priority notes:")
    for note in important:
        print(f"   - {note.title}")

    # Find related notes
    print("\n🔗 Notes related to 'wipnote':")
    wipnote_note = sdk.features.get("note-wipnote")
    if wipnote_note:
        related_ids = [e.target_id for e in wipnote_note.edges.get("related", [])]
        for note_id in related_ids:
            note = sdk.features.get(note_id)
            if note:
                print(f"   - {note.title}")

    # Find connection paths
    print("\n🛤️  Connection path (Web Standards → AI Agents):")
    path = sdk._graph.shortest_path(
        "note-web-standards", "note-ai-agents", relationship="related"
    )
    if path:
        for i, node_id in enumerate(path):
            node = sdk.features.get(node_id)
            if node:
                indent = "   " + "  " * i
                arrow = "→ " if i > 0 else ""
                print(f"{indent}{arrow}{node.title}")


def demonstrate_graph_visualization(sdk: SDK):
    """Show graph visualization options."""
    print("\n" + "=" * 80)
    print("📊 Graph Visualization")
    print("=" * 80)

    # Generate Mermaid diagram
    mermaid = sdk._graph.to_mermaid(relationship="related")
    print("\n🔶 Mermaid Diagram (paste into https://mermaid.live/):")
    print(mermaid)

    # Show stats
    stats = sdk._graph.stats()
    print("\n📈 Statistics:")
    print(f"   Total nodes: {stats['total']}")
    print(f"   Total edges: {stats['edge_count']}")
    print(f"   By type: {stats['by_type']}")


def main():
    """Run the knowledge base demo."""
    print("=" * 80)
    print("wipnote Knowledge Base Demo")
    print("'HTML is All You Need' - Knowledge Management Edition")
    print("=" * 80)

    # Initialize SDK
    sdk = SDK(directory=".", agent="knowledge-curator")

    # Create notes
    create_notes(sdk)

    # Demonstrate queries
    demonstrate_queries(sdk)

    # Show visualization
    demonstrate_graph_visualization(sdk)

    print("\n" + "=" * 80)
    print("Demo complete!")
    print("")
    print("💡 Tips:")
    print("   - Open the generated HTML files in a browser")
    print("   - Click links to navigate between notes")
    print("   - Edit the HTML files to add content")
    print("   - Commit to git to track knowledge evolution")
    print("=" * 80)


if __name__ == "__main__":
    main()
