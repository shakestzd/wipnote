# Analysis: Internal Plan Review System vs. Marimo

**Date:** 2026-04-07
**Status:** Proposal
**Scope:** Evaluate migrating the Marimo-based plan review UI to an internal Go/HTML/JS implementation

---

## 1. Current Marimo System — Inventory

### Custom code (business logic written by the team)

| File | Lines | Purpose |
|------|-------|---------|
| `plan_notebook.py` | 457 | Main app — loads YAML, wires widgets, assembles layout |
| `plan_ui.py` | 289 | Badges, stat cards, slice cards, progress bar, feedback summary |
| `plan_persistence.py` | 236 | SQLite CRUD for feedback, amendments, finalization |
| `claude_chat.py` | 423 | Claude CLI/API streaming backend, session persistence |
| `critique_renderer.py` | 102 | Assumption verification, risk table, critic columns |
| `dagre_widget.py` | 164 | D3/dagre-d3 dependency graph via anywidget |
| `amendment_parser.py` | 41 | Regex-based AMEND directive extraction |
| **Total** | **~1,712** | |

### What Marimo provides for free (framework layer)

| Marimo Feature | What it does | Usage count |
|---|---|---|
| `@app.cell` reactive cells | Auto-re-executes cells when dependencies change | 12 cells |
| `mo.ui.checkbox()` | Boolean toggle with `.value` binding | Design + N slices |
| `mo.ui.radio()` | Single-select with `.value` binding | N questions |
| `mo.ui.dropdown()` | Select from options | Plan selector + amendment status |
| `mo.ui.text_area()` | Multi-line input with `.value` binding | Design comment |
| `mo.ui.code_editor()` | Read-only YAML viewer | 1 instance |
| `mo.ui.dictionary()` | Groups widgets into reactive dict | 3 (slices, questions, amendments) |
| `mo.ui.chat()` | Streaming chat with custom model function | 1 sidebar |
| `mo.ui.anywidget()` | Custom JS widget bridge (dagre graph) | 1 instance |
| `mo.ui.run_button()` | Click-to-trigger action | Finalize button |
| `mo.accordion/vstack/hstack/sidebar` | Layout primitives | ~15 uses |
| `mo.md/Html/callout` | Content rendering | ~30 uses |
| `mo.stop()` | Conditional cell halt | 2 guards |
| `mo.cli_args()` | CLI argument passthrough | Plan path |
| `mo.output.replace()` | Dynamic element swap | 1 use |
| `marimo export html` | Static self-contained HTML export | Finalization |

### The dependency chain Marimo introduces

```
wipnote plan review <id>
  -> exec.LookPath("uv")                   # Requires uv on PATH
  -> os.MkdirTemp + notebook.WriteToDir     # Extract 7 .py files to temp dir
  -> uvx marimo run --sandbox               # Resolves marimo>=0.22.4, pyyaml, anywidget, traitlets
  -> Python interpreter                     # Marimo is Python-only
  -> CDN fetches (d3@7, dagre-d3)           # Runtime network dependency
```

---

## 2. What Already Exists Internally

**Critical insight: 70-80% of an internal replacement already exists** in `internal/plantmpl/` and `cmd/wipnote/api_plans.go`.

| Internal Component | Status | Marimo Equivalent |
|---|---|---|
| `DependencyGraph` (Go + dagre-d3 JS) | Complete | `dagre_widget.py` |
| `DesignSection` (Go template) | Complete | Design cell in notebook |
| `SliceCard` (Go template, N cards) | Complete | `render_slice_cards()` |
| `QuestionsSection` (Go template + radio JS) | Complete | `render_questions()` |
| `CritiqueZone` (Go template) | Complete | `critique_renderer.py` |
| `FinalizePreview` (Go template) | Complete | `render_feedback_summary()` |
| `ProgressBar` (Go template + JS update) | Complete | Progress bar in `plan_ui.py` |
| Checkbox approval handlers (vanilla JS) | Complete | `mo.ui.checkbox()` side-effects |
| Radio answer handlers (vanilla JS) | Complete | `mo.ui.radio()` side-effects |
| Comment textarea + debounce (vanilla JS) | Complete | `mo.ui.text_area()` |
| Dark/light theme toggle (CSS vars + JS) | Complete | Marimo's `data-color-mode` |
| REST API: GET/POST feedback | Complete | `persist_feedback()` Python |
| REST API: finalize | Complete | `finalize_plan()` Python |
| SQLite plan_feedback table | Shared | Same table, same schema |
| Static HTML rendering | Complete | `marimo export html` |
| Badge system (effort, risk, status) | Complete | `*_badge()` functions |

### What is missing internally

| Gap | Complexity | Lines estimate |
|---|---|---|
| **Streaming chat widget** (JS UI + SSE/WebSocket Go endpoint) | Medium-High | ~400 JS + ~200 Go |
| **Chat message persistence** (load/save history) | Low | ~50 Go (already in `plan_feedback` table) |
| **Amendment UI** (dropdown per amendment, accept/reject) | Low | ~100 JS + ~50 Go |
| **Amendment parser** (AMEND directive extraction) | Already done | Port 41-line regex to Go |
| **YAML code viewer** (read-only, syntax highlighted) | Low | ~30 JS (highlight.js already loaded) |
| **Chat history accordion** (prior conversation display) | Low | ~50 JS |
| **Graph node highlighting on approval** | Low | ~30 JS (graph already reactive to status) |
| **Total new code** | | **~660 JS + ~300 Go = ~960 lines** |

---

## 3. Feature-by-Feature Comparison

### Reactivity

**Marimo:** Automatic cell re-execution via dependency graph. When `design_approved.value` changes, the persist cell re-runs automatically. Elegant, zero-wiring.

**Internal equivalent:** Vanilla JS event listeners -> DOM update + API call. Already implemented for checkboxes, radios, textareas in `plan_page.gohtml` (lines 281-478). Requires explicit wiring but is fully functional.

**Verdict:** Wash. Marimo's reactivity is elegant in notebooks, but for a fixed-layout approval form, explicit event handlers are equally effective and easier to debug.

### Chat Widget

**Marimo:** `mo.ui.chat()` with a custom streaming model function. Handles message rendering, input box, streaming display, scroll behavior — all built in. `ClaudeChatBackend` provides the streaming generator, Marimo does the UI.

**Internal equivalent:** Does not exist yet. Would need:
- SSE endpoint in Go that proxies to Claude CLI/API
- JS chat bubble renderer (user/assistant differentiation)
- Input box with send button
- Streaming text append with auto-scroll
- Message history load on page open

**Verdict:** This is the single largest gap. ~600 lines of new code. But it's a well-understood pattern (every chat UI works the same way), and you gain full control over the UX.

### Amendment System

**Marimo:** `mo.ui.dictionary()` of `mo.ui.dropdown()` widgets, reactive persistence via side-effect cell.

**Internal equivalent:** Dropdown `<select>` elements with `change` event listeners -> POST to API. The amendment parser (41 lines of regex) ports trivially to Go.

**Verdict:** Trivial to implement internally. ~150 lines.

### Static HTML Export

**Marimo:** `marimo export html` creates a self-contained HTML with all widgets baked in (non-interactive). Called during finalization.

**Internal equivalent:** Go templates already render complete static HTML. The `plan_page.gohtml` template produces a self-contained document with embedded CSS/JS. For finalized plans, render with `data-status="finalized"` and checkboxes become disabled.

**Verdict:** Already solved internally. The Go template export is actually better because it doesn't require a subprocess call to `marimo export`.

### Dependency Graph

**Marimo:** `anywidget` bridge to custom ESM module with D3/dagre-d3. `traitlets` syncs `approved_ids` from Python to JS.

**Internal equivalent:** Same D3/dagre-d3 rendering in `plan_page.gohtml`. Node data in hidden `<div id="graph-data">`. JS `renderGraph()` function already handles status-based coloring.

**Verdict:** Feature parity. Both use the same JS libraries. Internal version avoids the anywidget/traitlets abstraction layer.

---

## 4. What You Gain by Going Internal

| Benefit | Impact |
|---|---|
| **Eliminate Python/uv dependency** | No more `exec.LookPath("uv")` gate; no `uvx marimo run --sandbox` with network resolution |
| **Single binary** | Plan review becomes `wipnote serve` -> open browser. No temp dir extraction, no subprocess |
| **Instant startup** | Go HTTP server starts in <50ms vs Marimo's 3-8 second cold start (Python + package resolution) |
| **Offline-first** | No CDN dependency for Marimo's own assets (only D3/dagre-d3, which can be vendored) |
| **Full UX control** | Chat widget, layout, animations, keyboard shortcuts — no framework constraints |
| **Unified dashboard** | Plan review becomes a route in `wipnote serve`, alongside existing dashboard |
| **Simpler CI/CD** | No Python test matrix, no marimo version pinning, no anywidget compatibility |
| **Smaller binary** | Drop ~1,700 lines of embedded Python from `internal/notebook/` |
| **No version drift risk** | Marimo 0.22.4+ requirement won't break silently on updates |

## 5. What You Lose

| Loss | Severity |
|---|---|
| **Marimo's chat widget** | Medium — must build ~600 lines of JS/Go replacement |
| **Reactive cell model** | Low — already using explicit event handlers in internal system |
| **Free Marimo upgrades** | Low — using a fixed subset of Marimo features, not the notebook ecosystem |
| **Python extensibility** | None — all business logic is already Go + vanilla JS |
| **Notebook prototyping** | Low — prototypes/ served its purpose; the design is stable |

---

## 6. Implementation Roadmap

### Phase 1: Chat endpoint + UI (the only substantial work)

- Go: SSE endpoint at `/api/plans/{id}/chat` that proxies to Claude CLI or Anthropic API
- Go: Port `ClaudeChatBackend` session management (session ID tracking, message history)
- JS: Chat bubble renderer, input box, streaming text display
- JS: Amendment detection display ("Amendment logged" confirmations)
- **~600 lines, 2-3 days**

### Phase 2: Amendment management UI

- JS: Dropdown per amendment (Pending/Accept/Reject)
- Go: API endpoint for amendment status updates (already exists as `POST /api/plans/{id}/feedback`)
- JS: Amendment count summary line
- **~150 lines, half day**

### Phase 3: Polish and parity

- JS: YAML code viewer with syntax highlighting (highlight.js already loaded)
- JS: Chat history accordion on page load
- JS: Graph node color update on approval change (minor enhancement to existing `renderGraph()`)
- Go: Remove `internal/notebook/` embed, `runPlanReview()` Marimo launch code
- **~200 lines, 1 day**

### Phase 4: Cleanup

- Delete `prototypes/*.py` (or archive to `prototypes/archive/`)
- Remove `notebook.WriteToDir()` and embed directive
- Drop uv/marimo documentation references
- Update `plugin/skills/plan/SKILL.md` step 5 from `wipnote plan review` (Marimo) to `wipnote serve` route
- **Half day**

**Total: ~960 lines of new code, 4-5 days of work, removing ~1,712 lines of Python + ~200 lines of Go embedding infrastructure.**

---

## 7. Recommendation

**Yes, build it internally.** Rationale:

1. **The hard work is already done.** `internal/plantmpl/` has 8 components, full CSS theming, vanilla JS interactivity, REST API endpoints, and SQLite persistence. This is not starting from scratch — it is closing a ~960-line gap.

2. **The Marimo dependency is disproportionately expensive.** Marimo is used for one screen (plan review) but introduces an entire Python toolchain dependency (uv, marimo, anywidget, traitlets, pyyaml). The complexity budget doesn't justify it for what amounts to a form with checkboxes, radios, and a chat sidebar.

3. **The only real gap is the chat widget.** Everything else — approvals, questions, graph, critique, progress bar, finalization — already works in the internal system. The chat widget is ~600 lines of well-understood code (SSE + bubble UI).

4. **Architectural alignment.** wipnote's philosophy is local-first, single binary, Go-native. The Marimo detour was a smart prototyping decision that validated the UX, but the production path should be consistent with the rest of the system.

5. **The risk is low.** The Marimo notebooks can stay in `prototypes/` as reference implementations. No knowledge is lost — a prototype is being graduated to production infrastructure.

**Caveat:** If the chat widget isn't a priority, plan review without AI chat can ship first (Phase 1 becomes optional), reducing the migration to a 1-2 day effort.
