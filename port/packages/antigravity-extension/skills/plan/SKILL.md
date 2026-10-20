---
name: plan
description: "Runs the whole wipnote plan lifecycle: a triage-gated, blocks-first interview that produces slice-card YAML, then validation, critique, human review, per-slice decisions and specs, and promotion. Also adds visual blocks to existing plans. Use when asked to plan, scope, or design work before building it, to write a slice's decisions or spec, or to add blocks to an existing plan."
disable-model-invocation: true
---

# wipnote Plan

One skill for the whole plan lifecycle. Start at the row that matches what you have:

| You have | Start at |
|---|---|
| Work to plan | Phase 1: Draft (below) |
| A drafted plan | Phase 3: Critique |
| An approved slice that needs its decisions | Phase 5: [SPEC.md](SPEC.md), Part A |
| A promoted slice that needs its spec | Phase 7: [SPEC.md](SPEC.md), Part B |
| An existing or legacy plan that needs visual blocks | [ENRICH.md](ENRICH.md) |

## Lifecycle checklist

Copy this checklist and tick it off. Do not start a phase until the one before it passes its check.

```
Plan lifecycle (plan-id):
- [ ] Phase 1. Draft: triage, research, interview, YAML written
- [ ] Phase 2. Validate: `wipnote plan validate-yaml` clean
- [ ] Phase 3. Critique: every `danger` resolved (3+ slice or complex plans)
- [ ] Phase 4. Review: human feedback ingested; slices approved
- [ ] Phase 5. Decisions: decisions_notes for each approved slice (SPEC.md Part A)
- [ ] Phase 6. Promote: each approved slice promoted to a feature
- [ ] Phase 7. Spec: spec generated for each promoted feature (SPEC.md Part B)
- [ ] Phase 8. Close: plan set to completed when all slices are done
```

# Phase 1: Draft

Treat plan creation as a **blocks-first** system design interview: you are the candidate, the user is the interviewer with requirements. Extract requirements through staged questions before producing slice YAML, using the harness's native ask-user tool when available and plain chat otherwise. Earn each field through the interview, or mark it inferred/skipped when the user supplied a complete spec and the harness has no interactive tools.

**Blocks-first is the defining discipline.** In each stage, author the VISUAL artifact first (the file-tree the slice touches, the data-model of its contract with the route in the block name, the wireframe/diagram of its UI/flow), then DERIVE `what`/`why`/`done_when` from it. Blocks go into the slice YAML inline as it is built; there is no separate visual pass ([ENRICH.md](ENRICH.md) is only for existing or legacy plans).

**Native plan mode:** in plan mode, do triage, research and the interview, then present the outline with ExitPlanMode, and write YAML only afterwards.

### Draft checklist

```
Phase 1 draft:
- [ ] Step 0: Triage — complexity set for each slice
- [ ] Research — design.research and per-slice research (or research_waiver) recorded
- [ ] Interview — stages done for each slice's complexity, blocks authored first
- [ ] Step 1: Plan YAML created and written with rewrite-yaml
```

---

## Step 0: Triage

Before any slice content, classify the work. The classification drives both the interview depth and the validator's mandatory-field set on the resulting slice cards.

| Complexity | Interview stages | Mandatory deltas vs default |
|---|---|---|
| `trivial` | 0 stages | `what`/`done_when`/`tests`/`decisions_notes`/`blocks` all optional |
| `standard` | 3 stages (Requirements, Scope & state, Done-when) | `what`, `decisions_notes` >=50 chars, >=1 visual block **or** an explicit `blocks_waiver:` |
| `complex`  | 4 stages (all) | `decisions_notes` >=50 chars; >=2 `done_when` entries; >=1 slice-local question with an answer; >=1 visual block **or** an explicit `blocks_waiver:` |

Set `complexity: trivial|standard|complex` on each slice card (read by `effectiveComplexity` in `plan/planyaml/validate.go`; unset defaults to `standard`).

Every new plan sets `meta.schema_version: v4`, which enforces the decisions_notes requirement, **the blocks gate** and **the research gate** (both below) for standard/complex slices. `v3` plans are still accepted with the same decisions_notes/blocks enforcement, but research is only advisory there.

### Triage question template

Ask this before emitting any slice (native ask-user tool, or plain chat where there is none), unless the user already supplied enough detail to classify the work:

```json
{
  "questions": [
    {
      "question": "How would you classify this work?",
      "header": "Triage",
      "multiSelect": false,
      "options": [
        {"label": "Trivial — one-shot patch, no design risk", "description": "Skip interview. Produce a minimal slice card."},
        {"label": "Standard — needs design clarity but scope is bounded", "description": "Run 3 interview stages (Requirements, Scope & state, Done-when)."},
        {"label": "Complex — non-trivial system design, multiple unknowns", "description": "Run all 4 interview stages (Requirements, Scope, Contract, Done-when)."},
        {"label": "Skip interview — I'll paste the spec", "description": "User supplies the full spec directly; agent drafts the YAML from it."}
      ]
    }
  ]
}
```

---

## Mandatory Research (ENFORCED for v4 plans)

Before drafting slices, run live web research (web search / web fetch); training data goes stale on version-sensitive details:

1. **Current official docs and standards** for every technology the plan touches.
2. **Existing OSS packages** (pkg.go.dev, npm, PyPI, crates.io) for any non-trivial component. Record adopt-vs-build: cite the adopted package, or what you evaluated and why it was rejected. The goal is **no reinventing the wheel**.
3. **Provider docs** for Claude Code, Codex CLI, or Antigravity integration work, to find existing primitives.

### The research gate (schema-enforced)

For `v4` plans the validator enforces it: `design.research` cites ≥1 source; every standard/complex slice carries ≥1 `research:` source or an explicit `research_waiver:` (trivial slices are exempt); every `url` is `http(s)`. Violations fail `wipnote plan validate-yaml` and finalize. Record research in the structured field, not in prose:

```yaml
design:
  research:
    - url: https://example.com/docs
      claim: Confirms the v2 API shape used in slice 3.
      accessed: 2026-06-18
slices:
  - num: 3
    complexity: standard
    research:
      - url: https://pkg.go.dev/github.com/x/y
        claim: Battle-tested parser — adopted instead of hand-rolling (slice avoids reinventing).
        accessed: 2026-06-18
  - num: 4
    complexity: standard
    research_waiver: stdlib net/http only — no external dependency or standard applies.
```

Never leave research silently empty; use `research_waiver:` with the reason. Mark any unverifiable external assumption `unverified`.

---

## The Interview — Blocks-First (standard = stages 1, 2, 4; complex = all 4)

Each stage elicits the VISUAL block FIRST, then derives prose from it. The block is authored INLINE into the slice's `blocks:` field as the YAML is built — never as a later pass.

| Stage | Author this block FIRST | Then derive these prose fields | Typical ask-user question |
|---|---|---|---|
| 1. Requirements | `wireframe`/`diagram` (UI/flow work only; skip for non-visual slices) | `why`, `what` (from the sketched surface/flow), `decisions_notes` (rationale half) | "Sketch the user-visible surface/flow. What problem does it solve, for whom?" |
| 2. Scope & state | `file-tree` of the real files this slice touches (new + edited) | `files`, `what` (scope half) — read straight off the tree | "List the files. Where does the state live? Any cross-slice ordering?" |
| 3. API / contract | `data-model` (typed columns; one entity per request/response body, method+path in `name`) | `what` (contract half), `done_when` (per route/entity), `decisions_notes` (interface picks) | "Author the entity. What's the firing rule? What does the response carry?" |
| 4. Done-when | (no new block — anchor acceptance criteria to the blocks above) | `done_when`, `tests`, `effort`, `risk` | "How will you tell each block's behavior works? Which existing tests must still pass?" |

**Derivation, not duplication:** prose restates the block and must stay consistent with it. If a stage has no natural visual artifact, say so and go straight to prose; never invent a block. Keep `what` under 56 words (a hard cap, see Prose-Length Caps). Each stage is the block-authoring step plus 1-3 questions in one ask-user call or one compact chat question set.

### Block authoring

Before authoring any block, follow [BLOCK_GROUNDING.md](BLOCK_GROUNDING.md). It covers the live catalog (`wipnote plan blocks`) and the grounding rules. `wipnote plan interview-questions <plan-id> <slice-num>` emits a `blocks` array per stage; show its prompts with the stage questions.

### Triage gating for blocks (the blocks gate)

Trivial slices skip blocks. Standard slices carry blocks when concrete enough to ground them (a real file-tree at minimum; a data-model where a contract exists). Complex slices carry every block type the design justifies.

This is schema-enforced: `wipnote plan validate-yaml` FAILS any standard/complex slice with zero blocks unless `decisions_notes` carries a `blocks_waiver: <reason>` line, for plans with `schema_version` v3/v4 or an explicit `complexity:` (pre-triage legacy plans are exempt; see the blocks-gate comment in `plan/planyaml/validate.go`).

```yaml
slices:
  - num: 4
    complexity: standard
    decisions_notes: |
      Pure refactor of the retry loop's backoff constant; no new file, route,
      or schema surface to visualise.
      blocks_waiver: refactor-only slice, nothing to ground a block against.
```

### Rendering the interview

Use the native ask-user tool, one call per stage. If the harness has no such tool (e.g. Codex), or you need the web form or a custom `--questions` set, read [INTERVIEW_RENDERING.md](INTERVIEW_RENDERING.md). It also covers how to persist answers.

### Example ask-user questions

Lead each stage with its block-authoring step, then ask its questions. Stage 1 example:

```json
{
  "questions": [
    {"question": "What's the user-visible behavior we're after?", "header": "Goal", "multiSelect": false, "options": [
      {"label": "New capability — feature add", "description": "User can do X they couldn't before."},
      {"label": "Behavior change — modify existing", "description": "X already works but in a way that's wrong/slow/incomplete."},
      {"label": "Bug fix — restore intended behavior", "description": "X should already work; root-cause and fix."}
    ]},
    {"question": "What's the hard constraint?", "header": "Constraint", "multiSelect": false, "options": [
      {"label": "Performance budget", "description": "p99, throughput, or memory ceiling."},
      {"label": "Backward compatibility", "description": "Existing on-disk data must keep working."},
      {"label": "No new runtime dependency", "description": "Stdlib + existing go.mod only."}
    ]}
  ]
}
```

Later stages follow the same shape:
- **Stage 2 — State:** where does the state live? `.wipnote/<kind>/*.html` (canonical, committed) / a `.wipnote` ledger or NDJSON (append-only) / derived from git / in-memory only (where the SQLite projection lives) / filesystem outside `.wipnote/`.
- **Stage 3 — Trigger:** every tool call (PreToolUse/PostToolUse) / user prompt (UserPromptSubmit) / session lifecycle (SessionStart/SessionEnd) / explicit CLI only.
- **Stage 4 — Acceptance:** unit test on the smallest function / integration test through the CLI / manual dashboard smoke test.

---

## decisions_notes Discipline

Write `decisions_notes` inline as the Q&A unfolds, capturing the chosen option and each rejected one with a one-line reason. Use `**Trigger:**` / `**State:**` / `**Payload:**` / `**Rejected:**` headings so `wipnote spec generate --insert` can weave them into the spec's `## Decisions` verbatim. Standard/complex slices need ≥50 characters unless the plan is finalized.

---

## Prose-Length Caps

`wipnote plan validate-yaml` enforces a **56-word hard cap on `what`** (`plan/planyaml/validate.go`; finalized plans are exempt). 14-32 words is a proven working range: state the change and where it lands, and move rationale and rejected alternatives into `decisions_notes`, which has no cap. `why` (>48 words), `tests` (>53) and `done_when` (>77 summed) only warn; treat the warning as a prompt to trim.

---

## Slice examples

See [EXAMPLES.md](EXAMPLES.md) for a complete trivial slice (no `what`, `done_when`, `tests` or `decisions_notes` needed) and a complete complex slice (blocks, `done_when`, `decisions_notes`, a slice-local question).

---

## Step 1: Create the Plan YAML

```bash
wipnote plan create-yaml "<title>" --description "<description>" --track <trk-id>
```

Note the returned plan ID. Write the YAML to `.wipnote/plans/<plan-id>.yaml` via `wipnote plan rewrite-yaml <plan-id> --file /tmp/plan.yaml`.

# Phase 2: Validate

```bash
wipnote plan validate-yaml <plan-id>
```

If it reports errors, fix the YAML, write it back with `rewrite-yaml`, and run it again. Continue only when it is clean. A blocks-gate failure means revisiting that slice's interview, or [ENRICH.md](ENRICH.md) for a legacy plan, or a genuine `blocks_waiver:`. [LEGACY.md](LEGACY.md) maps common validator errors to fixes.

# Phase 3: Critique

Skip for trivial single-slice plans. For 3+ slices, or any `complexity: complex` slice, ask the user to run `/wipnote:plan-critique <plan-id>`. It is a slash command only, because it runs the design and feasibility critics in their own subagent. When it returns, every `danger` finding must be resolved. If a finding changes a slice's design, re-author its block, re-derive the prose in the same edit, and go back to Phase 2.

# Phase 4: Review

Run `wipnote serve` and ask the user to review the slices in the dashboard. Then run `wipnote plan read-feedback-yaml <plan-id>`. For each slice with `changes_requested`, revise it and go back to Phase 2. Continue when every slice is approved or rejected.

# Phase 5: Decisions

For each approved slice, follow Part A of [SPEC.md](SPEC.md) to capture Scope, Decisions and Context in `decisions_notes`. Promotion needs them, so this comes first.

# Phase 6: Promote

```bash
wipnote plan promote-slice <plan-id> <num>
```

This creates the slice's `feat-…` feature. If it refuses (for example, missing decisions or unmet dependencies), fix the cause it names and run it again; use `--waive-deps` only when the user accepts it.

# Phase 7: Spec

For each promoted feature, follow Part B of [SPEC.md](SPEC.md) to generate its spec and check that it reads back. Specced features are ready for `/wipnote:execute`.

# Phase 8: Close

When every slice is done, run `wipnote plan set-status <plan-id> completed`.

---

# CLI reference

| Command | Effect |
|---------|--------|
| `wipnote plan create-yaml "<title>" --description "<desc>" --track <trk>` | Creates plan stub |
| `wipnote plan rewrite-yaml <plan-id> --file <path>` | Replaces plan YAML body |
| `wipnote plan validate-yaml <plan-id>` | Runs schema validation |
| `wipnote plan approve-slice <plan-id> <num>` | Sets `approval_status=approved` |
| `wipnote plan reject-slice <plan-id> <num> [--changes-requested]` | Sets `approval_status=rejected` / `changes_requested` |
| `wipnote plan answer-slice-question <plan-id> <num> <q-id> <answer-key>` | Records slice-local answer |
| `wipnote plan set-slice-status <plan-id> <num> <status>` | Sets `execution_status` |
| `wipnote plan elicit-decisions <plan-id> <num> --scope ... --decisions ... --context ...` | Writes `decisions_notes` |
| `wipnote plan promote-slice <plan-id> <num> [--waive-deps]` | Creates `feat-XXX`, wires edges |
| `wipnote plan read-feedback-yaml <plan-id>` | Reads human feedback from `plan_feedback` |
| `wipnote plan set-status <plan-id> <status>` | Plan lifecycle (`active`/`completed`) |

---

# Reference files and related skills

- [EXAMPLES.md](EXAMPLES.md): a complete trivial and a complete complex slice card.
- [BLOCK_GROUNDING.md](BLOCK_GROUNDING.md): block grounding rules.
- [INTERVIEW_RENDERING.md](INTERVIEW_RENDERING.md): interview rendering without an ask-user tool, the web form, and the `--questions` schema.
- [LEGACY.md](LEGACY.md): v1 and v2 (pre-triage) plan compatibility and a validator-error cheatsheet.
- [SPEC.md](SPEC.md): Phase 5 (slice decisions, Part A) and Phase 7 (feature spec, Part B).
- [ENRICH.md](ENRICH.md): add or revise blocks on an existing or legacy plan.
- `/wipnote:plan-critique`: Phase 3, the dual-critic review pass.
- `/wipnote:execute`: runs promoted slices.
