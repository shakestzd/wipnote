# Knowledge Base Example

Personal knowledge management using wipnote - like Obsidian or Roam Research, but with HTML.

## Overview

This example shows how to build a personal knowledge base where:
- Each note is an HTML file
- [[Wiki-style links]] connect concepts
- The browser is your knowledge viewer
- Git tracks the evolution of your knowledge

## Quick Start

```bash
# From the examples/knowledge-base directory
python demo.py
```

## What It Demonstrates

### 1. Concept Notes

Create interconnected notes about different topics:
- wipnote concept
- Web Standards
- Graph Databases
- AI Agents

Each note links to related concepts, forming a knowledge graph.

### 2. Bidirectional Links

Notes reference each other:
```
wipnote → Web Standards
wipnote → Graph Databases
wipnote → AI Agents
```

### 3. Knowledge Queries

Find notes by various criteria:
```python
# All high-priority notes
important = sdk.features.where(type="note", priority="high")

# Find related notes
related = graph.find_related("note-wipnote", relationship="related")

# Find connection paths
path = graph.shortest_path("note-a", "note-b", relationship="related")
```

### 4. Graph Visualization

Generate diagrams showing connections:
```python
mermaid = graph.to_mermaid(relationship="related")
# Paste into https://mermaid.live/
```

## Use Cases

### 1. Zettelkasten Method
- Atomic notes (one concept per file)
- Links between notes (graph edges)
- Emergent structure (no rigid hierarchy)

### 2. Research Notes
- Papers and articles
- Citations as links
- Topic clustering

### 3. Learning
- Course notes
- Concept maps
- Spaced repetition

### 4. Project Documentation
- Design decisions
- Architecture diagrams
- Meeting notes

## Benefits Over Traditional Tools

### vs. Obsidian/Roam
- ✅ No proprietary format
- ✅ Works without the app
- ✅ Open in any browser
- ✅ Full git integration

### vs. Notion
- ✅ Fully offline
- ✅ No lock-in
- ✅ Text-based (greppable)
- ✅ No subscription needed

### vs. Markdown + Foam
- ✅ Native rendering (HTML/CSS)
- ✅ Rich styling out of box
- ✅ Graph queries built-in
- ✅ No build step

## Knowledge Graph Patterns

### Pattern 1: Daily Notes
```python
daily = Node(
    id=f"daily-{date.today()}",
    title=f"Daily Note: {date.today()}",
    type="daily-note",
    content="<p>Today I learned...</p>",
    edges={
        "references": [
            Edge(target_id="note-concept-x"),
            Edge(target_id="note-concept-y")
        ]
    }
)
```

### Pattern 2: Topic Hierarchy
```python
parent_topic = Node(id="topic-programming", title="Programming")
subtopic = Node(
    id="topic-python",
    title="Python Programming",
    edges={"parent": [Edge(target_id="topic-programming")]}
)
```

### Pattern 3: Citations
```python
paper = Node(
    id="paper-attention",
    title="Attention Is All You Need",
    edges={
        "cites": [
            Edge(target_id="paper-transformer"),
            Edge(target_id="paper-lstm")
        ],
        "cited_by": [
            Edge(target_id="paper-bert"),
            Edge(target_id="paper-gpt")
        ]
    }
)
```

## Next Steps

1. Run the demo to create sample notes
2. Open HTML files in browser to explore
3. Create your own notes by editing HTML
4. Build your personal knowledge graph!

## Learn More

- [wipnote Documentation](../../docs/)
- [Graph Queries](../../docs/guide/queries.md)
- [SDK Reference](../../docs/api/sdk.md)
