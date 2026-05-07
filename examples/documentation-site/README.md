# Documentation Site Example

A static documentation site using wipnote for page linking and navigation.

## Overview

This example demonstrates using wipnote as a documentation system where:
- Each doc page is an HTML file
- Cross-references are hyperlinks
- Table of contents is auto-generated
- No build step required

## Structure

```
docs/
├── index.html          # Homepage/TOC
├── getting-started.html
├── installation.html
├── quickstart.html
├── api/
│   ├── overview.html
│   ├── graph.html
│   └── sdk.html
└── examples/
    ├── basic.html
    └── advanced.html
```

## Key Features

### 1. No Build Step
Just HTML files - open in browser immediately.

### 2. Cross-Linking
```html
<p>See also: <a href="api/sdk.html">SDK Reference</a></p>
```

### 3. Navigation
- Breadcrumbs from file hierarchy
- Previous/Next from graph structure
- Related pages from graph edges

### 4. Search
Use wipnote queries:
```python
# Find pages about "authentication"
results = graph.query("[data-keywords*='authentication']")
```

## Advantages

### vs. Docusaurus/VuePress
- ✅ No Node.js required
- ✅ No build step
- ✅ Instant preview
- ✅ Pure HTML/CSS

### vs. MkDocs/Sphinx
- ✅ No Python required
- ✅ No theme configuration
- ✅ Direct editing
- ✅ Graph queries

### vs. GitBook
- ✅ Fully offline
- ✅ No hosting needed
- ✅ No lock-in
- ✅ Self-contained

## Use Cases

1. **API Documentation** - Function references with links
2. **User Guides** - Step-by-step tutorials
3. **Internal Docs** - Team wikis
4. **Project Docs** - Architecture decisions

## Implementation

The wipnote docs site (docs/) is itself built this way!

Check `../../docs/` for a real-world example.

## Learn More

See the main [wipnote documentation](../../docs/)
