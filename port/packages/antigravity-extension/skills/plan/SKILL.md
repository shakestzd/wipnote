---
name: plan
description: "Runs the whole wipnote plan lifecycle: a triage-gated interview that produces slice-card YAML, then validation, critique, human review, per-slice decisions and specs, and promotion. Also adds visual blocks to existing plans. Use when asked to plan, scope, or design work before building it, to write a slice's decisions or spec, or to add blocks to an existing plan."
disable-model-invocation: true
---

# wipnote Plan

One skill for the whole plan lifecycle. Plans live in `.wipnote/plans/<plan-id>.yaml`; change them only through the CLI. Start at the row that matches what you have:

| You have | Start at |
|---|---|
| Work to plan | Phase 1: Draft |
| A drafted plan | Phase 3: Critique |
| An approved slice that needs its decisions | Phase 5: [SPEC.md](SPEC.md), Part A |
| A promoted slice that needs its spec | Phase 7: [SPEC.md](SPEC.md), Part B |
| An existing plan that needs visual blocks | [ENRICH.md](ENRICH.md) |

Copy this checklist and tick it off. Start a phase only when the one before it passes.

```
Plan lifecycle (plan-id):
- [ ] Phase 1. Draft: triage, research, interview, YAML written
- [ ] Phase 2. Validate: `wipnote plan validate-yaml` clean
- [ ] Phase 3. Critique: every `danger` resolved (3+ slice or complex plans)
- [ ] Phase 4. Review: every slice approved or rejected
- [ ] Phase 5. Decisions: decisions_notes for each approved slice
- [ ] Phase 6. Promote: each approved slice promoted to a feature
- [ ] Phase 7. Spec: spec generated for each promoted feature
- [ ] Phase 8. Close: plan set to completed when all slices are done
```

# Phase 1: Draft

## Triage

Classify each slice first. The class sets the interview depth and the fields `validate-yaml` requires. Unset `complexity` counts as `standard`. Ask the user unless their request already makes the class clear.

| `complexity` | Interview | Required beyond the defaults |
|---|---|---|
| `trivial` | none | nothing: `what`, `done_when`, `tests`, `decisions_notes` and blocks are optional |
| `standard` | requirements, scope, done-when | `what`; `decisions_notes` ≥ 50 chars; ≥ 1 visual block or a `blocks_waiver:` |
| `complex` | all four stages | as standard, plus ≥ 2 `done_when` entries and ≥ 1 slice question with an answer |

New plans set `meta.schema_version: v4`, which enforces the research gate below. Older v1–v3 plans still load: missing slice-card fields are optional, and research is only advisory before v4.

## Mandatory Research

Training data goes stale on versions and APIs, so research live before drafting slices:

1. Current official docs for every technology the plan touches, including Claude Code, Codex CLI and Antigravity docs for harness work.
2. Maintained packages (pkg.go.dev, npm, PyPI, crates.io) for any non-trivial component. Record adopt-vs-build: no reinventing the wheel.
3. What already exists here: `wipnote relevant <topic>` and a code search. Record what you found, or "none found" with the queries.

For a design, ui, capability or dependency question, run it as a research spike, `wipnote research start "<question>" --for <feat-id>`; completing the spike becomes the feature's research record.

**The research gate (schema-enforced for v4):** `design.research` cites ≥ 1 source, and every standard or complex slice carries ≥ 1 `research:` entry or an explicit `research_waiver: <reason>`. Every `url` is http(s). Never leave research silently empty, and mark an assumption you cannot verify as `unverified`.

```yaml
design:
  research:
    - {url: https://example.com/docs, claim: "Confirms the v2 API shape used in slice 3.", accessed: 2026-06-18}
slices:
  - num: 4
    complexity: standard
    research_waiver: stdlib net/http only — no external dependency or standard applies.
```

## Interview

Run one stage per ask-user call (or one compact chat message where there is no ask-user tool). `wipnote plan interview-questions <plan-id> [<slice-num>]` prints the question set and the block prompts for each stage; [INTERVIEW_RENDERING.md](INTERVIEW_RENDERING.md) covers chat-only harnesses and saving answers.

| Stage | Author this block first | Then derive |
|---|---|---|
| 1. Requirements | `wireframe` or `diagram` (UI or flow work only) | `why`, `what`, the rationale in `decisions_notes` |
| 2. Scope & state | `file-tree` of the real files the slice touches | `files`, the scope part of `what` |
| 3. Contract (complex only) | `data-model`, one entity per request or response, method and path in `name` | `done_when` per route, interface choices in `decisions_notes` |
| 4. Done-when | none; anchor criteria to the blocks above | `done_when`, `tests`, `effort`, `risk` |

**Blocks.** Run `wipnote plan blocks` for the live catalog; never hardcode block types or fields. A block is grounded when every field, route and path it names exists or is a direct output of the slice. Never invent schema, routes or files. Wireframe HTML uses `var(--wf-*)` tokens only, never raw colors. When a slice has nothing to ground a block on, add `blocks_waiver: <reason>` to `decisions_notes` instead of padding with a speculative block.

**decisions_notes.** Write them as the answers come in: the chosen option and each rejected one with a one-line reason, under `**Trigger:**` / `**State:**` / `**Payload:**` / `**Rejected:**` headings. `wipnote spec generate --insert` copies them into the spec's `## Decisions` verbatim.

**Length.** `what` has a hard cap of 56 words (14–32 works well); put rationale in `decisions_notes`, which has no cap. Long `why`, `tests` and `done_when` only warn.

In native plan mode, do triage, research and the interview, present the outline with ExitPlanMode, and write YAML only afterwards. [EXAMPLES.md](EXAMPLES.md) has a complete trivial and complex slice.

## Write the YAML

```bash
wipnote plan create-yaml "<title>" --description "<description>" --track <trk-id>
wipnote plan rewrite-yaml <plan-id> --file /tmp/plan.yaml
```

# Phase 2: Validate

Run `wipnote plan validate-yaml <plan-id>`. Fix every error, write the YAML back with `rewrite-yaml`, and run it again until it is clean. Each error names the field and the fix; for a missing `decisions_notes` on an older plan, run `wipnote plan elicit-decisions` for that slice or mark it `complexity: trivial` if it is a one-shot patch.

# Phase 3: Critique

Skip it for a trivial single-slice plan. For 3+ slices or any complex slice, ask the user to run `/wipnote:plan-critique <plan-id>`; it runs the design and feasibility critics in their own context. Resolve every `danger` finding. If a finding changes a slice's design, re-author its block, re-derive the prose, and go back to Phase 2.

# Phase 4: Review

Get a decision on every slice in the conversation. Run `wipnote plan slices <plan-id>` first: where the wipnote-plan-review mod is loaded, it opens a pane that tracks each decision. Read the slice cards from `.wipnote/plans/<plan-id>.yaml` (`wipnote plan show` prints only metadata), then show each slice (title, `what`, `done_when`, its blocks and any open question) and ask approve, request changes, or reject, using the ask-user tool where there is one. Record each answer with `wipnote plan approve-slice <plan-id> <num>` or `wipnote plan reject-slice <plan-id> <num> [--changes-requested]`. Revise each `changes_requested` slice from the user's comments right away and go back to Phase 2.

# Phases 5–7: Decisions, promote, spec

For each approved slice: write its decisions ([SPEC.md](SPEC.md) Part A), run `wipnote plan promote-slice <plan-id> <num>` (fix whatever it names if it refuses; use `--waive-deps` only when the user accepts it), then generate the feature's spec ([SPEC.md](SPEC.md) Part B). Specced features are ready for `/wipnote:execute`.

# Phase 8: Close

When every slice is done, run `wipnote plan set-status <plan-id> completed`.

# CLI reference

| Command | Effect |
|---------|--------|
| `wipnote plan create-yaml "<title>" --description "<desc>" --track <trk>` | Create the plan stub |
| `wipnote plan rewrite-yaml <plan-id> --file <path>` | Replace the plan YAML (validated, atomic) |
| `wipnote plan validate-yaml <plan-id>` | Run schema validation |
| `wipnote plan approve-slice` / `reject-slice <plan-id> <num> [--changes-requested]` | Record a review decision |
| `wipnote plan answer-slice-question <plan-id> <num> <q-id> <answer-key>` | Record a slice question's answer |
| `wipnote plan set-slice-status <plan-id> <num> <status>` | Set `execution_status` |
| `wipnote plan elicit-decisions <plan-id> <num> --scope ... --decisions ... --context ...` | Write `decisions_notes` |
| `wipnote plan promote-slice <plan-id> <num> [--waive-deps]` | Create the slice's feature and wire its edges |
| `wipnote plan slices <plan-id>` | Print each slice's approval and execution state as JSON |
| `wipnote plan read-feedback-yaml <plan-id>` | Read the recorded review decisions |
| `wipnote plan set-status <plan-id> <status>` | Set the plan lifecycle status |
