---
name: htmlgraph:plan
description: Plan development work using v2 slice-card YAML. Generates a plan with slice cards as executable specs, runs critique, pauses for human review, then promotes approved slices to features. Use when asked to plan, create a development plan, or build a feature with design clarity first.
---

# HtmlGraph Plan

Use this skill when asked to plan development work, organize tasks for multi-agent execution, or design a feature with human review before implementation.

**Trigger keywords:** create plan, development plan, parallel plan, plan tasks, plan this feature, review before building, generate plan, scaffold plan, slice plan, crispi

---

## Overview

A v2 plan is a YAML document containing slice cards. Each slice card is an executable spec — it defines what to build, why it matters, acceptance criteria, and tests. The human reviews and approves slices individually. Approved slices are promoted to feature work items incrementally via `htmlgraph plan promote-slice`.

**Spec-driven mapping:**

| Artifact | Role |
|----------|------|
| `.htmlgraph/plans/<plan-id>.yaml` | Plan = context document (problem, goals, constraints, slices) |
| Each slice card (`what/why/done_when/tests`) | Executable spec — what an agent implements |
| `feat-XXX` work item | Promoted feature = implementation tracker |
| Sessions and commits | Evidence linked via `implemented_in` edges |

---

## Step 0: Work Item Attribution (MANDATORY)

Before anything else:

1. `htmlgraph status` — is there an active feature/track?
2. If yes: `htmlgraph feature start <id>`
3. If no: `htmlgraph feature create "<title>" --track <trk-id>` then `htmlgraph feature start <id>`

---

## Step 1: Research

Research the area before writing any YAML. Answer:
- Current state of the codebase
- Desired end state
- Open design questions (architecture choices)
- Candidate vertical slices (end-to-end, not horizontal layers)
- Real dependencies between slices
- Prior art and existing patterns

**Skip only for:** trivial changes, bug fixes with known root cause, documentation-only work.

---

## Step 2: Create the Plan YAML

```bash
htmlgraph plan create-yaml "<title>" --description "<description>" --track <trk-id>
```

Note the returned plan ID. Then write the YAML to `.htmlgraph/plans/<plan-id>.yaml` using `htmlgraph plan rewrite-yaml <plan-id> --file /tmp/plan.yaml`.

### v2 YAML Schema

```yaml
meta:
  id: plan-<hex8>
  track_id: <trk-id>
  title: "<plan title>"
  description: >
    One paragraph: what this plan designs and why.
  created_at: "YYYY-MM-DD"
  status: active              # active | completed
  created_by: claude-opus

design:
  problem: >
    Current state, what is broken, and why it matters.
  goals:
    - "**Goal 1** — measurable outcome"
    - "**Goal 2** — measurable outcome"
  constraints:
    - "Constraint — why it exists"
  approved: false
  comment: ""

slices:
  - id: slice-1
    num: 1
    title: "<slice title>"
    what: |
      What exactly will be implemented. Name functions, files, APIs.
      An agent reading this should know what to build without asking.
    why: |
      Why this slice exists. What problem does it solve? Which goal does
      it serve? What breaks without it?
    files:
      - path/to/file1.go
      - path/to/file2.go
    deps: []                  # slice numbers this depends on, e.g. [1, 3]
    done_when:
      - "Acceptance criterion 1 — testable and concrete"
      - "Acceptance criterion 2 — testable and concrete"
    effort: S                 # S | M | L
    risk: Low                 # Low | Med | High
    tests: |
      Unit: specific test with expected input/output
      Integration: specific integration scenario
      Regression: which existing tests must still pass
    approved: false
    comment: ""

    # V2 slice-local lifecycle (omit from legacy plans — they remain valid)
    approval_status: pending  # pending | approved | rejected | changes_requested
    execution_status: not_started  # not_started | promoted | in_progress | done | blocked | superseded
    questions:                # slice-local open questions (optional)
      - id: q-approach
        text: "Should we use X or Y approach here?"
        answer: ""            # empty = unanswered
    critic_revisions:         # critic feedback scoped to this slice (optional)
      - source: sonnet
        severity: HIGH
        summary: "Missing error handling for network timeout path"

  - id: slice-2
    num: 2
    # ... (same fields as slice-1)

# Plan-level design questions (optional — prefer slice-local questions for v2)
questions:
  - id: q-<kebab-name>
    text: "Design question?"
    description: >
      Context: why this question matters and what the tradeoffs are.
    recommended: option-a
    options:
      - key: option-a
        label: "A: Short name — full description"
      - key: option-b
        label: "B: Short name — full description"
    answer: null              # null = unanswered

critique: null                # null = not yet run; populated in Step 4
```

### Mandatory slice fields

All of these must be present and non-empty on every slice:

| Field | Requirement |
|-------|-------------|
| `what` | Specific enough for an agent to implement without further questions |
| `why` | Links to a design goal or explains the business need |
| `files` | At least one file path |
| `done_when` | At least two testable acceptance criteria |
| `tests` | At least one unit test and one integration/regression test |
| `effort` | `S` (<50 lines), `M` (50–200 lines), `L` (>200 lines) |
| `risk` | `Low` (pure addition), `Med` (modifies existing), `High` (changes hot path or shared interface) |

Use `what: |`, `why: |`, and `tests: |` (YAML literal blocks) so Markdown renders correctly in the dashboard.

---

## Step 3: Validate

```bash
htmlgraph plan validate-yaml <plan-id>
```

Fix any schema errors before proceeding.

---

## Step 4: Critique (for plans with 3+ slices)

Run two critique agents in parallel. Each reads the plan YAML and produces findings.

After critique, integrate HIGH/DANGER findings directly into the affected slice cards as `critic_revisions` entries. Rewrite the YAML via `htmlgraph plan rewrite-yaml`.

---

## Step 5: Open for Human Review (PAUSE)

```bash
htmlgraph serve
```

Tell the human:

```
Plan ready for review in the dashboard at http://localhost:8088/#plans

Per-slice review:
  1. Read each slice card — what/why/done_when/tests
  2. Approve or reject each slice individually
  3. Answer any slice-local questions
  4. Read the critique revisions embedded in each card

CLI shortcuts (if reviewing outside the dashboard):
  htmlgraph plan approve-slice <plan-id> <num>
  htmlgraph plan reject-slice <plan-id> <num> [--changes-requested]
  htmlgraph plan answer-slice-question <plan-id> <num> <question-id> <answer-key>

I will wait until you signal readiness before promoting any slices.
```

**STOP. Do not promote slices until the human has reviewed.**

---

## Step 6: Read Decisions and Integrate Feedback

```bash
htmlgraph plan read-feedback-yaml <plan-id>
```

For rejected slices: update the YAML to address the rejection, then re-run Step 5.
For slices with `changes_requested`: revise the slice card and re-present.
For answered questions: bake the decision into the affected slice's `what` field.

---

## Step 7: Promote Approved Slices

Promote slices incrementally as they are approved — no need to wait for full-plan finalization.

```bash
# Promote a single approved slice (creates feat-XXX, wires edges):
htmlgraph plan promote-slice <plan-id> <slice-num>

# If a slice's deps are already done externally (e.g. already in-flight):
htmlgraph plan promote-slice <plan-id> <slice-num> --waive-deps
```

Rules:
- The slice must have `approval_status=approved` (set via `approve-slice`).
- All dependency slices must have `execution_status=done` or `superseded`, unless `--waive-deps`.
- `promote-slice` is idempotent: if `feature_id` is already set, it reuses the existing feature.
- After promotion, `execution_status` is set to `promoted` in both YAML and `plan_feedback`.

The command prints the promoted `feat-XXX` ID. That feature is now part of the track and participates in the dependency-driven dispatch loop (see `/htmlgraph:execute`).

### Promotion workflow for a multi-slice plan

```
for each slice (in dependency order):
  1. Review → htmlgraph plan approve-slice <plan-id> <num>
  2. Promote → htmlgraph plan promote-slice <plan-id> <num>
  3. Track execution → htmlgraph plan set-slice-status <plan-id> <num> in_progress
  4. When done → htmlgraph plan set-slice-status <plan-id> <num> done
```

---

## Step 8: Close the Plan

When all slices are done, rejected, or superseded:

```bash
htmlgraph plan set-status <plan-id> completed
```

---

## CLI Reference for Slice Lifecycle

| Command | Usage | Effect |
|---------|-------|--------|
| `approve-slice` | `htmlgraph plan approve-slice <plan-id> <num>` | Sets `approval_status=approved` in `plan_feedback` |
| `reject-slice` | `htmlgraph plan reject-slice <plan-id> <num> [--changes-requested]` | Sets `approval_status=rejected` or `changes_requested` |
| `answer-slice-question` | `htmlgraph plan answer-slice-question <plan-id> <num> <question-id> <answer-key>` | Records answer to a slice-local question |
| `set-slice-status` | `htmlgraph plan set-slice-status <plan-id> <num> <status>` | Sets `execution_status` (`not_started\|promoted\|in_progress\|done\|blocked\|superseded`) |
| `promote-slice` | `htmlgraph plan promote-slice <plan-id> <num> [--waive-deps]` | Creates `feat-XXX`, wires edges, sets `execution_status=promoted` |

---

## Section-Naming Contract

State stored in `plan_feedback` uses these section keys:

| Key pattern | What it stores |
|-------------|----------------|
| `slice-<num>` | Slice-level approval (`action=approve`) and execution status (`action=set_execution_status`) |
| `slice-<num>-question-<id>` | Answer to a slice-local question (`action=answer`) |
| `design` | Design section approval |
| `questions` | Plan-level question answers |

The `answer-slice-question` command maps `<slice-num>` and `<question-id>` to the section key automatically.

---

## Minimal Example (2-slice plan)

```yaml
meta:
  id: plan-a1b2c3d4
  track_id: trk-xyz
  title: "Add rate limiting to API"
  status: active
  created_at: "2026-04-28"

design:
  problem: >
    The /api/ingest endpoint accepts unbounded request volume. Under load,
    the SQLite writer becomes a bottleneck and drops events silently.
  goals:
    - "**Throughput cap** — reject requests above 100 req/s per client with 429"
    - "**Visibility** — expose a counter metric for throttled requests"
  constraints:
    - "No new runtime dependencies — use stdlib only"
  approved: false
  comment: ""

slices:
  - id: slice-1
    num: 1
    title: "In-memory rate limiter middleware"
    what: |
      Add `internal/ratelimit/limiter.go`: a token-bucket limiter keyed by
      client IP. Wire it as HTTP middleware in `cmd/htmlgraph/serve.go`.
      Return HTTP 429 with a JSON error body when the bucket is empty.
    why: |
      Protects the SQLite writer from burst overload. Addresses the throughput
      cap goal.
    files:
      - internal/ratelimit/limiter.go
      - cmd/htmlgraph/serve.go
    deps: []
    done_when:
      - "Requests above the configured limit receive HTTP 429"
      - "Requests within the limit pass through unchanged"
    effort: S
    risk: Low
    tests: |
      Unit: TestLimiter_Allow — burst of 10 req/s, expect first 5 allowed, next rejected
      Integration: TestServeRateLimit — spin up server, hammer /api/ingest, assert 429s
    approval_status: pending
    execution_status: not_started

  - id: slice-2
    num: 2
    title: "Throttle counter metric"
    what: |
      Increment a `htmlgraph_throttled_total` counter in the limiter middleware.
      Expose it on the existing `/metrics` endpoint via the Prometheus-compatible
      text format already used by `cmd/htmlgraph/metrics.go`.
    why: |
      Addresses the visibility goal. Without this, operators cannot tell whether
      rate limiting is firing or calibrated correctly.
    files:
      - internal/ratelimit/limiter.go
      - cmd/htmlgraph/metrics.go
    deps: [1]
    done_when:
      - "`htmlgraph_throttled_total` increments on each 429 response"
      - "Counter is present in /metrics output"
    effort: S
    risk: Low
    tests: |
      Unit: TestLimiterMetric — after N rejections, counter equals N
      Regression: existing /metrics tests still pass
    approval_status: pending
    execution_status: not_started

questions: []
critique: null
```

---

## Legacy Compatibility

Plans created before v2 (without `approval_status`, `execution_status`, `questions`, or `critic_revisions` on slices) remain valid. The schema treats these fields as optional (`omitempty`). The dashboard renders them via a global Questions/Critique fallback when the v2 slice-card fields are absent.

Legacy plans can be migrated incrementally: add v2 fields to individual slices as they come up for review, without touching the rest of the YAML.

---

## Key Rules

- **v2 slices are slice cards** — each card contains its own questions, critic revisions, approval status, and execution status
- **Use literal blocks** (`|`) for `what`, `why`, `tests` so Markdown renders in the dashboard
- **Promote incrementally** — `promote-slice` does not require full-plan finalization
- **Critique is embedded per-slice** — use `critic_revisions` instead of a global critique section for v2 plans
- **All slice fields are mandatory** — missing fields will fail `validate-yaml`
- **Finalize is for legacy plans** — v2 plans use `promote-slice` per slice; `htmlgraph plan finalize` is the legacy all-at-once path
