# Visual plans: target page and what it needs

`visual-plan-target.html` is a hand-built page showing what a wipnote plan for an
application feature should be able to show: a UI mock with real states, a flowchart with
branches and failure paths, and decisions with toggleable alternatives. It is a design
target, not generator output. Open it directly in a browser.

The feature it plans is the plan review surface (reviewers approve or push back on a plan
slice by slice in a Claude Artifact, and the session reads the decisions back). That feature
has real UI and a real flow, which is why it was chosen. Its estimates are illustrative.

## Where the current blocks fall short

| Page region | Current block | Gap |
|---|---|---|
| UI mock with states, device widths, working controls | `wireframe`: one static sketch, HTML run through a strict sanitizer (no scripts, no event handlers, colours only from design tokens, no `fixed`/`sticky` positioning) | No states, no interaction, no multi-screen flow |
| Flowchart with decisions, failure paths, click-for-detail | `diagram`: an ordered list of step strings joined by SVG arrows (`lr` or `tb`) | No branching, no node kinds, no labelled edges, no detail, no verified/untested marking |
| Decisions with an alternative that changes the plan | `decisions_notes` free text | The alternative and its effect on the design are not structured, so they cannot be toggled |
| Reply that writes itself | None | No way to turn a reviewer's picks into a reply |
| Schema, files touched | `data-model`, `file-tree` | Adequate |

## Proposed block types (derived from the page, not yet implemented)

Sketches only. Field names will change once the runtime question below is settled.

```yaml
# Decision with an alternative; `affects` names the block the toggle re-renders.
- type: decision
  title: Review data shape
  fields: {pick: per-slice, affects: data-model}
  rows:
    - {label: per-slice, summary: "...", cost: "..."}
    - {label: per-plan,  summary: "...", cost: "..."}

# Flowchart with real control flow. Laid out server-side with the same dagre pass the
# plan's dependency graph already uses, so the output is a static, exact SVG.
- type: flowchart
  entries:   # nodes
    - {id: write, kind: decision, label: "Write succeeds?", detail: "...", failure: "...", status: documented}
  rows:      # edges
    - {from: write, to: banner, label: "no"}
  # kind: process | decision | failure | terminal
  # status: proposed | documented | exists | untested   (untested renders dashed)

# UI mock with named states, built from the project's own design tokens.
- type: ui-states
  fields: {tokens: cmd/wipnote/dashboard/css/tokens.css, widths: "desktop,phone"}
  rows:
    - {state: conflict, html: "..."}
```

`reply` chips are derived from `decision` blocks and open questions rather than authored.

## The open design decision: JavaScript

The `wireframe` sanitizer strips scripts on purpose. `ui-states` and the flowchart's
click-for-detail need behaviour. Two options:

| Option | Trade-off |
|---|---|
| Sandboxed iframe (`sandbox="allow-scripts"`, no same-origin) | Real interactivity, isolated from the dashboard and plan page. Not yet checked against how the dashboard injects plan HTML. |
| CSS-only states (radio or `:target` patterns, as the `tabs` block already does) | Stays inside the current trust model. Limited to toggles, no real flows. |

The flowchart can stay fully static (clickable detail is the only scripted part); the
decision toggles and UI states are what need the runtime.

## What exists today (measured 2026-10-05)

- 46 plan YAML files. 33 have no `schema_version`, 10 are v3 with no blocks, 3 are v4. All
  3 v4 plans have blocks. So the blocks-first model works for plans written under it; few
  exist yet.
- Block usage in the whole corpus: 1 wireframe, 6 diagrams (3 plans), 6 data-models,
  19 file-trees, 1 tabs.
- The `api-endpoint` block was removed by feat-0fde8687 ("never used"). The plan skills
  still told authors to use it and their own example failed `plan validate-yaml`; that is
  fixed alongside this page.

## What was and was not verified

Verified: the page renders in Chromium at 1280px (light and dark) and 390px, makes no
external requests, raises no console errors, has no horizontal page scroll on a phone, and
its interactions behave (state presets, approve and finalize gating, decision toggles,
flowchart detail by mouse and keyboard, reply builder). Comment text is rendered as plain
text.

Not verified: the artifact behaviour the plan depends on (a database write waking the
session, live updates for other viewers, the clipboard inside an artifact sandbox). The page
marks those steps as untested instead of presenting them as fact.
