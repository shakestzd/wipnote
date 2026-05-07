# Product Requirements Document (PRD)
# wipnote: "HTML is All You Need"

**Version**: 1.0  
**Date**: December 16, 2024  
**Author**: Shakes  
**Status**: Draft

---

## Executive Summary

**wipnote** is a lightweight graph database framework built entirely on web standards (HTML, CSS, JavaScript) that eliminates the need for external graph databases like Neo4j or Memgraph. It enables AI agents to coordinate work using HTML files as nodes, hyperlinks as edges, and CSS selectors as the query language.

**Market Opportunity**: With the explosion of AI coding agents (Claude Code, Copilot, Cursor, Aider), developers need simple infrastructure for agent coordination and observability. Current solutions require Docker, complex databases, and custom protocols. wipnote provides a zero-dependency alternative that's human-readable and version-control friendly.

**Target Launch**: Q1 2025

---

## Problem Statement

### Current Challenges

**For AI Agent Developers:**
1. **Infrastructure Complexity**
   - Need Docker for Neo4j/Memgraph
   - Learn Cypher query language
   - Manage database migrations
   - Handle connection pooling
   - Deploy and monitor database servers

2. **Poor Human Observability**
   - Database queries needed to see agent state
   - No visual representation without custom UI
   - Hard to debug agent behavior
   - Can't "view source" on agent decisions

3. **Version Control Issues**
   - Binary database dumps don't diff well
   - Can't see what changed in git
   - Hard to code review agent state
   - Difficult to rollback changes

4. **Integration Friction**
   - Each agent needs database drivers
   - Serialization/deserialization overhead
   - Network latency to database
   - Credential management

**For Human Operators:**
1. Can't easily see what agents are doing
2. Need specialized tools to query state
3. Hard to manually intervene or correct
4. No "open in browser and understand" option

### Market Validation

- **LangGraph** (3.5k GitHub stars) - Custom state management
- **AutoGPT** (167k stars) - JSON files for memory
- **Claude Code** - Proprietary coordination
- **Cursor/Windsurf** - Opaque agent state

All of these reinvent coordination mechanisms. None use web standards.

---

## Solution Overview

### Core Concept

**Use HTML as the graph database format:**
- HTML files = Graph nodes
- `<a href>` = Graph edges  
- `data-*` attributes = Node properties
- CSS selectors = Query language
- Web browsers = Human interface
- justhtml/DOMParser = Machine interface

### Key Innovation

**Dual Interface Design:**
```
                ┌─────────────────┐
                │   HTML Files    │
                │  (Source of     │
                │   Truth)        │
                └────────┬────────┘
                         │
           ┌─────────────┴──────────────┐
           │                            │
    ┌──────▼───────┐           ┌───────▼────────┐
    │   Browsers   │           │   AI Agents    │
    │   (Human     │           │   (Machine     │
    │    View)     │           │    View)       │
    └──────────────┘           └────────────────┘
         CSS                    Pydantic
       Rendering                Serialization
```

### Value Proposition

**For Developers:**
- ✅ Zero dependencies (no Docker, no database servers)
- ✅ Zero learning curve (everyone knows HTML/CSS)
- ✅ Zero deployment complexity (just files)
- ✅ Version control native (git diff works perfectly)

**For AI Agents:**
- ✅ Lightweight context (Pydantic serialization)
- ✅ Simple queries (CSS selectors)
- ✅ Fast reads (no network latency)
- ✅ Safe writes (file system guarantees)

**For Human Operators:**
- ✅ Visual observability (open in browser)
- ✅ Easy debugging (view source)
- ✅ Simple intervention (edit HTML)
- ✅ Searchable history (grep works)

---

## Target Users

### Primary Personas

**1. AI Agent Developer ("Alex")**
- **Profile**: Software engineer building AI coding assistants
- **Pain**: Struggling with Neo4j complexity for agent coordination
- **Goals**: Simple, reliable agent state management
- **Success Metric**: Agents coordinate without database infrastructure

**2. Solo Developer with ADHD ("Shakes")**
- **Profile**: Needs visual observability of complex systems
- **Pain**: Can't see what agents are doing without queries
- **Goals**: "Open browser and understand" simplicity
- **Success Metric**: Can debug agent behavior visually

**3. Open Source Maintainer ("Morgan")**
- **Profile**: Building public AI agent tools/frameworks
- **Pain**: Users struggle with database setup in README
- **Goals**: Zero-dependency tool that "just works"
- **Success Metric**: Contributors can run project without Docker

### Secondary Personas

**4. Technical Writer ("Jordan")**
- **Use Case**: Documentation site with graph structure
- **Value**: Hyperlinked docs, no static site generator needed

**5. Knowledge Worker ("Taylor")**
- **Use Case**: Personal knowledge base with connections
- **Value**: Visual note-taking, version controlled

---

## User Stories

### MVP (Phase 1)

**Story 1: Create Graph Nodes**
```
As an AI agent developer
I want to create graph nodes as HTML files
So that I can store agent state in version control

Acceptance Criteria:
- [ ] Can create Node with Python API
- [ ] Node.to_html() generates valid HTML
- [ ] HTML file opens in browser
- [ ] All properties visible in browser
```

**Story 2: Query with CSS Selectors**
```
As an AI agent developer
I want to query nodes using CSS selectors
So that I don't need to learn Cypher or SQL

Acceptance Criteria:
- [ ] graph.query("[data-status='blocked']") works
- [ ] Can combine multiple selectors
- [ ] Returns Python objects for processing
- [ ] Query performance <100ms for 100 nodes
```

**Story 3: Visual Dashboard**
```
As a human operator
I want to open a dashboard in my browser
So that I can see what agents are doing

Acceptance Criteria:
- [ ] Dashboard shows all nodes
- [ ] Can filter by status/priority
- [ ] Shows dependency graph
- [ ] Pure vanilla JS, no build step
```

**Story 4: Agent Coordination**
```
As an AI coding agent
I want to get lightweight task context
So that I don't waste tokens on HTML

Acceptance Criteria:
- [ ] node.to_context() returns <100 tokens
- [ ] Contains next action to take
- [ ] Shows blocking dependencies
- [ ] Can update and write back
```

### Phase 2 (Post-Launch)

**Story 5: Full-Text Search**
```
As a user with large graphs
I want to search across all nodes
So that I can find relevant information quickly

Solution: Optional SQLite FTS index
```

**Story 6: Real-Time Updates**
```
As a developer running multiple agents
I want the dashboard to update in real-time
So that I can see live progress

Solution: File watching + WebSocket/SSE
```

**Story 7: Graph Visualization**
```
As a visual thinker
I want to see the dependency graph
So that I can understand relationships

Solution: D3.js or Cytoscape.js integration
```

---

## Features & Requirements

### Core Features (MVP)

| Feature | Priority | Status | Notes |
|---------|----------|--------|-------|
| Python library | P0 | Planned | Core functionality |
| HTML node creation | P0 | Planned | Node.to_html() |
| CSS selector queries | P0 | Planned | graph.query() |
| Hyperlink edges | P0 | Planned | Native <a href> |
| Pydantic models | P0 | Planned | Schema validation |
| Agent interface | P0 | Planned | Lightweight context |
| JavaScript library | P1 | Planned | Browser-side queries |
| Dashboard | P1 | Planned | Vanilla JS + CSS |
| Todo example | P1 | Planned | Simple demo |
| Agent coordination example | P1 | Planned | Ijoka migration |

### Future Features (Post-MVP)

| Feature | Priority | Status | Notes |
|---------|----------|--------|-------|
| SQLite indexer | P2 | Backlog | For large graphs |
| File watching | P2 | Backlog | Real-time updates |
| Graph visualization | P2 | Backlog | D3.js integration |
| TypeScript definitions | P2 | Backlog | Better DX |
| VSCode extension | P3 | Idea | Graph explorer |
| CLI tool | P3 | Idea | Command-line queries |

### Non-Functional Requirements

**Performance:**
- Parse 1000 nodes in <1 second (Python)
- Query with CSS in <100ms (JavaScript)
- Dashboard loads in <500ms
- Memory usage <100MB for 1000 nodes

**Reliability:**
- 100% test coverage for core library
- Graceful handling of malformed HTML
- Data validation with Pydantic
- File corruption detection

**Usability:**
- Zero dependencies except justhtml + Pydantic
- No build step required
- Works offline
- Copy-paste examples that work

**Compatibility:**
- Python 3.10+
- Modern browsers (Chrome, Firefox, Safari)
- Windows, macOS, Linux
- Works in Pyodide (WASM)

---

## Technical Architecture

### System Components

```
┌─────────────────────────────────────────────────────────────┐
│                    File System Layer                         │
│  features/feature-001.html  sessions/session-abc.html       │
└────────────────┬────────────────────────────────────────────┘
                 │
┌────────────────┴────────────────────────────────────────────┐
│                    Python Library                            │
│  - Parser (justhtml wrapper)                                │
│  - Models (Pydantic schemas)                                │
│  - Graph (operations + algorithms)                          │
│  - Converter (HTML ↔ Pydantic)                              │
│  - Agents (lightweight interface)                           │
└────────────────┬────────────────────────────────────────────┘
                 │
┌────────────────┴────────────────────────────────────────────┐
│                  JavaScript Library                          │
│  - wipnote class                                          │
│  - DOMParser integration                                    │
│  - CSS selector queries                                     │
│  - Graph algorithms (BFS, DFS)                              │
│  - Dashboard rendering                                      │
└────────────────┬────────────────────────────────────────────┘
                 │
┌────────────────┴────────────────────────────────────────────┐
│                 Optional: SQLite Index                       │
│  - Full-text search                                         │
│  - Complex queries                                          │
│  - Performance optimization                                 │
└─────────────────────────────────────────────────────────────┘
```

### Data Model

**Node (HTML File):**
```python
Node(
    id: str,              # Unique identifier
    title: str,           # Human-readable title
    type: str,            # "feature", "task", "note", etc.
    status: str,          # "todo", "in-progress", "done", etc.
    priority: str,        # "low", "medium", "high", "critical"
    properties: dict,     # Arbitrary key-value pairs
    edges: dict,          # {relationship_type: [node_ids]}
    steps: list[Step],    # Implementation steps
    created: datetime,    # Creation timestamp
    updated: datetime,    # Last update timestamp
)
```

**Edge (Hyperlink):**
```html
<a href="target-node.html" 
   data-relationship="blocks"
   data-since="2024-12-16">
    Target Node Title
</a>
```

### API Surface

**Python:**
```python
# Graph operations
graph = wipnote('directory/')
graph.add(node)
graph.update(node)
graph.query(css_selector)
graph.get(node_id)
graph.shortest_path(from_id, to_id)

# Agent interface
agent = AgentInterface('directory/')
task = agent.get_next_task()
context = agent.get_context(task_id)
agent.complete_step(task_id, step_index)
```

**JavaScript:**
```javascript
// Graph operations
const graph = new wipnote();
await graph.loadFrom('directory/');
const nodes = graph.query(css_selector);
const path = graph.findPath(from, to);

// Dashboard
graph.renderDashboard('#app', options);
```

---

## Success Metrics

### Launch Goals (Month 1)

**Adoption:**
- [ ] 100 GitHub stars
- [ ] 10 PyPI downloads/day
- [ ] 5 real-world implementations
- [ ] 3 blog posts from others

**Community:**
- [ ] Front page of Hacker News
- [ ] r/programming discussion (100+ upvotes)
- [ ] Twitter thread (1000+ likes)
- [ ] 5+ contributors

**Technical:**
- [ ] 100% test coverage
- [ ] Zero critical bugs
- [ ] Documentation complete
- [ ] 3+ working examples

### Long-term Goals (6 Months)

**Adoption:**
- [ ] 1000 GitHub stars
- [ ] 100 PyPI downloads/day
- [ ] Used in 3+ production systems
- [ ] 10+ blog posts/tutorials

**Ecosystem:**
- [ ] VSCode extension
- [ ] Integration with LangChain
- [ ] Integration with AutoGPT
- [ ] Featured in awesome-lists

**Impact:**
- [ ] "HTML is All You Need" becomes a meme
- [ ] Cited in academic papers
- [ ] Adopted by major AI agent projects
- [ ] Conference talk acceptances

---

## Competitive Analysis

### Direct Competitors

**Neo4j**
- **Strengths**: Mature, powerful Cypher queries, enterprise support
- **Weaknesses**: Complex setup, expensive, binary format
- **Differentiation**: wipnote has zero dependencies, human-readable

**Memgraph**
- **Strengths**: Fast, Cypher compatible, good for streaming
- **Weaknesses**: Requires Docker, learning curve
- **Differentiation**: wipnote needs no server

**ArangoDB**
- **Strengths**: Multi-model (graph, document, key-value)
- **Weaknesses**: Heavy, complex, overkill for simple use cases
- **Differentiation**: wipnote is lightweight and focused

### Indirect Competitors

**JSON Files**
- **Strengths**: Simple, ubiquitous
- **Weaknesses**: No native graph structure, no visual rendering
- **Differentiation**: wipnote has hyperlinks + browser rendering

**SQLite**
- **Strengths**: Zero setup, fast queries
- **Weaknesses**: Not designed for graphs, no visual layer
- **Differentiation**: wipnote is graph-native + human-readable

**Markdown + Obsidian**
- **Strengths**: Note-taking focused, backlinks, visual
- **Weaknesses**: Desktop app required, not agent-native
- **Differentiation**: wipnote is agent-first + programmable

### Positioning

**wipnote** positions itself as:
- **Simpler than** Neo4j/Memgraph (zero dependencies)
- **More visual than** JSON/SQLite (browsers render it)
- **More programmable than** Obsidian/Roam (API-first)
- **More standard than** custom agent protocols (web standards)

**Tagline Options:**
1. "HTML is All You Need" (primary)
2. "Graph Database on Web Standards"
3. "Zero-Dependency AI Agent Coordination"
4. "The Web is Already a Graph Database"

---

## Go-to-Market Strategy

### Phase 1: Build in Public (Weeks 1-2)

**Activities:**
- [ ] Daily Twitter threads on progress
- [ ] Stream coding sessions on Twitch
- [ ] Post to r/programming weekly
- [ ] Engage with AI agent communities

**Content:**
- "Why I'm building a graph DB in HTML" blog post
- "Zero to Dashboard in 10 minutes" video
- "HTML vs Neo4j: A Comparison" article

### Phase 2: Launch (Week 3)

**Primary Channels:**
- [ ] Hacker News submission (aim for front page)
- [ ] r/programming (detailed post)
- [ ] r/MachineLearning (agent focus)
- [ ] Twitter thread with demos
- [ ] Dev.to article

**Launch Assets:**
- [ ] GitHub repo with README
- [ ] Live demo site
- [ ] 3 working examples
- [ ] Quickstart guide
- [ ] Comparison chart
- [ ] "HTML is All You Need" manifesto

### Phase 3: Community Growth (Months 1-3)

**Activities:**
- [ ] Weekly office hours (Discord/GitHub Discussions)
- [ ] Guest blog posts on popular dev blogs
- [ ] Conference talk submissions
- [ ] Integration with popular AI tools

**Content Calendar:**
- Week 1: "Getting Started with wipnote"
- Week 2: "Migrating from Neo4j to wipnote"
- Week 3: "Building AI Agent Systems"
- Week 4: "wipnote Performance Deep-Dive"

### Phase 4: Ecosystem (Months 3-6)

**Integrations:**
- [ ] LangChain plugin
- [ ] AutoGPT adapter
- [ ] Claude Code integration
- [ ] Cursor/Windsurf support

**Tooling:**
- [ ] VSCode extension
- [ ] CLI tool
- [ ] GitHub Action
- [ ] Vercel/Netlify deployment

---

## Risks & Mitigations

### Technical Risks

**Risk 1: Performance at Scale**
- **Issue**: HTML parsing may be slow for 10k+ nodes
- **Likelihood**: Medium
- **Impact**: High
- **Mitigation**: Add optional SQLite indexer for large graphs

**Risk 2: Concurrent Writes**
- **Issue**: Multiple agents writing to same file
- **Likelihood**: High
- **Impact**: Medium
- **Mitigation**: Document as append-mostly pattern, add file locking

**Risk 3: Browser Memory**
- **Issue**: Dashboard loading 1000+ nodes may crash browser
- **Likelihood**: Low
- **Impact**: Medium
- **Mitigation**: Implement pagination, virtual scrolling

### Market Risks

**Risk 4: "Not Serious Enough"**
- **Issue**: Developers may see HTML as toy solution
- **Likelihood**: Medium
- **Impact**: High
- **Mitigation**: Focus on simplicity as feature, not limitation

**Risk 5: Limited Adoption**
- **Issue**: Network effect of existing graph DBs
- **Likelihood**: Medium
- **Impact**: High
- **Mitigation**: Target niche (AI agents) where complexity is pain point

**Risk 6: Competition**
- **Issue**: Neo4j or others build similar solution
- **Likelihood**: Low
- **Impact**: Medium
- **Mitigation**: Open source + community-driven development

### Execution Risks

**Risk 7: Scope Creep**
- **Issue**: Adding too many features before launch
- **Likelihood**: High
- **Impact**: Medium
- **Mitigation**: Strict MVP scope, post-launch roadmap

**Risk 8: Documentation Debt**
- **Issue**: Code without good docs won't get adopted
- **Likelihood**: Medium
- **Impact**: High
- **Mitigation**: Write docs alongside code, not after

---

## Timeline

### Week 1: Foundation
- [ ] Project setup (structure, config)
- [ ] Python core (parser, models, graph)
- [ ] Unit tests (>90% coverage)
- [ ] Simple todo example

### Week 2: Interfaces
- [ ] JavaScript library
- [ ] Dashboard (vanilla JS + CSS)
- [ ] Agent interface
- [ ] Agent coordination example

### Week 3: Polish & Launch
- [ ] Documentation (README, quickstart, cookbook)
- [ ] Performance optimization
- [ ] Launch materials (blog post, demos)
- [ ] Submit to HN/Reddit

### Month 2-3: Community
- [ ] Office hours
- [ ] Guest blog posts
- [ ] Conference submissions
- [ ] Integration work

### Month 4-6: Ecosystem
- [ ] VSCode extension
- [ ] LangChain integration
- [ ] Performance improvements
- [ ] Version 1.0 release

---

## Open Questions

1. **Naming**: Is "wipnote" the right name? Alternatives:
   - WebGraph
   - HtmlDB
   - GraphML (might conflict with existing GraphML format)
   - HyperGraph (too generic)

2. **Licensing**: MIT vs Apache 2.0?
   - MIT is simpler, more permissive
   - Apache has patent protection

3. **Monetization**: Is there a business model here?
   - Consulting/training?
   - Enterprise features (SQLite indexer, hosting)?
   - SaaS dashboard?

4. **Scope**: Should we include graph visualization in MVP?
   - Pro: More impressive demos
   - Con: Adds complexity, dependencies

5. **Standards**: Should we propose this as a W3C standard?
   - Pro: Legitimacy, wider adoption
   - Con: Slow process, may constrain evolution

---

## Approval & Sign-off

**Product Owner**: Shakes  
**Technical Lead**: Shakes  
**Target Launch**: Q1 2025  

**Approved by**:
- [ ] Technical Review
- [ ] Design Review
- [ ] Marketing Review
- [ ] Legal Review (open source license)

---

## Appendices

### Appendix A: HTML Node Example
See CLAUDE.md section "HTML File Format Specification"

### Appendix B: API Reference
See CLAUDE.md section "Python API Design" and "JavaScript API Design"

### Appendix C: Comparison Matrix
See CLAUDE.md section "Comparison to Alternatives"

### Appendix D: Use Case Examples
See CLAUDE.md section "Use Cases"

---

**Document Version History:**
- v1.0 (2024-12-16): Initial PRD draft

**Next Review Date**: 2024-12-30

**Contact**: github.com/shakestzd/wipnote
