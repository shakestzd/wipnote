# wipnote Project Memory

## Project Positioning (Strategic)

**Canonical description:** "Local-first observability and coordination platform for AI-assisted development"

**Core constraint:** "No external infrastructure required" (no Postgres, no Redis, no cloud sync)
NOT: "zero dependencies" (that was old/wrong framing)

**"Local-first" unlocks:**
- SQLite for fast local indexing and analytics
- FastAPI for a real local server with WebSockets
- HTMX for live UI without a frontend build step
- HTML artifacts that are git-diffable and browser-readable
- Future: local LLM inference, local vector search, local sync

**Architecture layers:**
- HTML files = canonical work item CRUD (features, bugs, spikes, tracks)
- JSONL = append-only event history
- SQLite = canonical for operational querying, dashboard, analytics, sync state
- FastAPI + HTMX + SSE/WebSocket = live dashboard protocol surface

**"HTML is All You Need"** = origin philosophy / design influence, NOT a literal architecture claim.
Demote to historical context in all docs.

## WIP Limit Debugging (Anti-Pattern to Avoid)

**Lesson:** WIP limit blocks are multi-layered — delegate investigation instead of iterating with Bash.

When `sdk.features.start()` raises `WIP limit (3) reached`:
- `get_active_features()` counts ALL in-progress nodes including **spikes** (not just features)
- Spikes live in `.wipnote/features/spk-*.html` — same directory as features
- `sdk.spikes.edit()` fails if spike not found via spikes collection — edit HTML directly
- The right fix: find all in-progress HTML files, reset stale ones to `done` via direct HTML edit

**Correct approach:**
```
Agent(researcher) → understand WIP system first
Agent(haiku-coder) → reset stale nodes + start feature in one delegation
```
Never: 10 iterative Bash commands chasing the problem directly.

## Parallel Worktree Pattern

For parallel feature development:
1. `sdk.features.start(feat_id)` for each feature (check WIP limit first)
2. `git worktree add ../wipnote-{feat_id} -b {feat_id}-work` for each
3. Launch background `Agent(sonnet-coder)` per worktree
4. Merge back when agents complete

Worktree dirs: `../wipnote-{feature-id}/` (sibling to main repo)

## Auto Work Item Attribution (v0.33.41)

**Implemented:** Claude-via-SDK attribution pattern in `UserPromptSubmit` hook.

Files: `src/python/wipnote/hooks/prompt_analyzer.py`, `packages/claude-plugin/hooks/scripts/user-prompt-submit.py`

**How it works:** Every prompt receives a "Work Item Attribution" block in CIGS guidance listing all open work items. Claude is instructed to call `sdk.features.start("correct-id")` if needed.

**Key design:** Use Claude's natural language understanding as the attribution engine — no heuristics.

**Feature requires track linkage:** `sdk.features.create(...).set_track('track_id').save()` — no track = ValueError.

## Dashboard Architecture (v0.33.43)

**Active dashboard:** `src/python/wipnote/api/templates/dashboard-redesign.html`
(NOT `src/python/wipnote/dashboard.html` — old WebSocket version)

**Live updates:** SSE via `/activity-feed/stream` → `refreshActivityFeed()` → HTMX morphs DOM

**No-flicker pattern:** Uses idiomorph (`https://unpkg.com/idiomorph@0.3.0/dist/idiomorph-ext.min.js`) with `hx-ext="morph"` on `<main id="content-area">`. Morphs DOM in-place so expanded rows survive live updates.

**Template files:**
- `api/templates/dashboard-redesign.html` — main dashboard, SSE, JS
- `api/templates/partials/activity-feed-hierarchical.html` — table structure (Event | Work Item | Time)
- `api/routes/dashboard.py` — `/views/activity-feed` route

**Column layout:** thead = Event | Work Item | Time (3 cols, NO status col).

## Deploy Test Speed

Full `uv run pytest` takes ~8 minutes (2,194 tests). For pre-deploy template changes:
`uv run pytest tests/python/ -q -x --ignore=tests/benchmarks` (~2-3 minutes)

Use full suite only for SDK/core changes.

## Post-Deploy CI Check

**After every deployment (`./scripts/deploy-all.sh`), verify CI passes:**
```bash
gh run list --workflow=ci.yml --limit 3
gh run list --workflow=release.yml --limit 3
```

Common failures: `ModuleNotFoundError` (file not committed), broken symlinks, PyPI trusted publisher config, testpaths in pytest.ini.
