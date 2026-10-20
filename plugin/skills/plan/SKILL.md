---
name: wipnote:plan
description: Plan development work using a triage-gated, blocks-first interview. Classify scope as trivial/standard/complex, then run 0/3/4 staged interview rounds through the current harness native ask-user tool when available, or plain conversation when it is not. Each stage elicits the VISUAL block first (file-tree, data-model, wireframe/diagram) and derives the prose slice fields (what/why/done_when) from it — blocks are authored inline as the YAML is built, not in a post-pass. Produces slice-card YAML with grounded visual blocks and visible research provenance for external claims; pauses for human review; promotes approved slices to features. Use when asked to plan, create a development plan, or build a feature with design clarity first.
disable-model-invocation: true
---

# wipnote Plan

Treat plan creation as a **blocks-first** system design interview: you are the candidate, the user is the interviewer with requirements. Extract requirements through staged questions before producing slice YAML, using the harness's native ask-user tool when available and plain chat otherwise. Earn each field through the interview, or mark it inferred/skipped when the user supplied a complete spec and the harness has no interactive tools.

**Blocks-first is the defining discipline.** In each stage, author the VISUAL artifact first (the file-tree the slice touches, the data-model of its contract with the route in the block name, the wireframe/diagram of its UI/flow), then DERIVE `what`/`why`/`done_when` from it. Blocks go into the slice YAML inline as it is built; there is no separate visual pass (`wipnote:visual-plan` is only for enriching existing or legacy plans).

**Native plan mode:** in plan mode, do triage, research and the interview, then present the outline with ExitPlanMode, and write YAML only afterwards.

**Trigger keywords:** create plan, development plan, parallel plan, plan tasks, plan this feature, review before building, generate plan, scaffold plan, slice plan, crispi

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

| Stage | Author this block FIRST | Then derive these prose fields | Typical AUQ shape |
|---|---|---|---|
| 1. Requirements | `wireframe`/`diagram` (UI/flow work only; skip for non-visual slices) | `why`, `what` (from the sketched surface/flow), `decisions_notes` (rationale half) | "Sketch the user-visible surface/flow. What problem does it solve, for whom?" |
| 2. Scope & state | `file-tree` of the real files this slice touches (new + edited) | `files`, `what` (scope half) — read straight off the tree | "List the files. Where does the state live? Any cross-slice ordering?" |
| 3. API / contract | `data-model` (typed columns; model each request/response body as an entity and put the method+path in the block's `name` field, which is the heading the renderer shows; a data-model's `title` is not rendered) | `what` (contract half), `done_when` (per route/entity), `decisions_notes` (interface picks) | "Author the entity. What's the firing rule? What does the response carry?" |
| 4. Done-when | (no new block — anchor acceptance criteria to the blocks above) | `done_when`, `tests`, `effort`, `risk` | "How will you tell each block's behavior works? Which existing tests must still pass?" |

**Derivation, not duplication:** prose restates the block and must stay consistent with it. If a stage has no natural visual artifact, say so and go straight to prose; never invent a block. Keep `what` under 56 words (a hard cap, see Prose-Length Caps). Each stage is the block-authoring step plus 1-3 questions in one ask-user call or one compact chat question set.

### Block-authoring prep (read the live catalog once, up front)

Run `wipnote plan blocks` for the supported block types and required fields; never hardcode them. `wipnote plan interview-questions <plan-id> <slice-num>` emits, per stage, a `blocks` array (`type`, catalog `description`, and a `prompt`); render those prompts alongside the stage questions.

### Grounding rule (enforced discipline)

A block is grounded when every field, route, and file path it names already exists or will exist as a direct output of this slice. Never invent schema, routes, or files; skip an ungroundable block type and note why in `decisions_notes`.

- `data-model` rows: real or will-exist field names/types.
- `data-model` named with a route (e.g. `name: POST /api/ingest — 429 response`): the method/path must be a route the slice will actually implement.
- `file-tree` entries: real files the slice touches.
- `wireframe` HTML: `var(--wf-*)` design tokens only — never raw hex/rgb.

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

### Rendering the interview (cross-harness)

The questions have one canonical source, the binary; render them with the lowest-friction mechanism the current harness supports:

    wipnote plan interview-questions <plan-id>            # upfront: triage + problem/goals/constraints
    wipnote plan interview-questions <plan-id> <slice-num>  # per-slice: template + the slice's open questions

Render it by capability, in this preference order:

1. **Native ask-user tool, inline** (Claude `AskUserQuestion`, Antigravity `ask_user` when exposed): one tool call per stage. Preferred.
2. **Plain chat** where there is no ask-user tool (e.g. Codex): present each question and its options; the user answers in their reply. Triage is always asked this way or via the tool, never a form.
3. **Web form (optional)**, only for a long complex interview or on request: `wipnote plan interview-questions … | wipnote plan interview … --questions -`. It blocks, persists on submit, and embeds the plan-review chat panel.

Then **persist non-interactively** (the form does this itself; for the inline/conversational paths you write the answers):
- Upfront intake → `wipnote plan set-design-yaml <plan> --problem … --goals … --constraints …`, then draft slice cards using the assessed complexity.
- Per-slice → `wipnote plan elicit-decisions <plan> <slice> --scope … --decisions … --context …`.

**Adaptive follow-ups** use the same channel: after persisting, ask gap questions for any unmet mandatory field or ambiguity. Nothing keeps state between rounds; you own the adaptivity.

### Codex-compatible fallback

Codex may not expose `AskUserQuestion`, Task tools, or Claude-style agent dispatch. In Codex:

1. Ask triage and interview questions in plain chat, or infer answers from the supplied spec if the user explicitly asks you to proceed without more questions.
2. Persist inferred/skipped answers in `decisions_notes` with labels such as `**Inferred:**` and `**Skipped:**` so reviewers can see where the interview was compressed.
3. Run mandatory live web research before drafting slices, and record each finding in the structured `research:` fields (`design.research` + per-slice `research:`/`research_waiver:`).
4. Validate locally with `wipnote plan validate-yaml <plan-id>` before requesting human review — for v4 plans this fails on any standard/complex slice missing `research:`/`research_waiver:` or a `design.research` basis.

Generated plans must not depend on Claude-only model aliases or Task/AUQ tools, must be `v4` with cited `research:` or `research_waiver:` on every standard/complex slice, must record adopt-vs-build for every non-trivial component, and must name and explain any skipped interview stage in `decisions_notes`.

**`--questions` JSON schema:**

    {"stages": [
      {"key": "scope", "title": "Scope & state", "bucket": "Scope",
       "blocks": [
         {"type": "file-tree", "description": "An ordered list of file paths the slice touches.",
          "prompt": "FIRST author a file-tree block of the real files this slice touches; derive `files` and the scope half of `what` from it."}
       ],
       "questions": [
         {"id": "scope.0", "header": "State", "type": "choice",
          "prompt": "Where does the state live?",
          "options": [{"label": "In .wipnote/ canonical files", "description": "HTML / plan YAML / ledgers — committed"}]}
       ]}
    ]}

- `blocks` (optional, per stage): `type` (a key from `wipnote plan blocks`), catalog `description`, and a `prompt`; populated from the live catalog.
- `bucket` ∈ `Scope` | `Decisions` | `Context`: routes answers into that `decisions_notes` subsection.
- `type` ∈ `choice` (needs `options`, optional `multiSelect`) | `text` | `yesno`.
- Question `id`s must be unique; answers compose into the same `### Scope` / `### Decisions` / `### Context` markdown as the AUQ path, so downstream commands behave identically regardless of renderer.

### Example AUQs

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

## Trivial Slice Example

```yaml
meta:
  id: plan-<generated>
  title: "Fix typo in serve.go log message"
  status: draft
  schema_version: v4
slices:
  - id: slice-1
    num: 1
    title: "Fix typo in serve.go log message"
    why: "User-facing log message reads 'lisening' instead of 'listening'."
    files:
      - cmd/wipnote/serve.go
    effort: S
    risk: Low
    complexity: trivial
    deps: []
```

That's it. No `what`, `done_when`, `tests`, or `decisions_notes` required.

## Complex Slice Example (abbreviated)

```yaml
meta:
  id: plan-<generated>
  title: "Rate-limit /api/ingest"
  status: draft
  schema_version: v4
slices:
  - id: slice-1
    num: 1
    title: "Rate-limit /api/ingest per client IP"
    what: |
      Add token-bucket limiter in internal/ratelimit/limiter.go keyed by
      client IP. Middleware in cmd/wipnote/serve.go returns HTTP 429 with
      JSON body when bucket empty.
    why: |
      Protects the canonical event writer from burst overload (Goal 1 — throughput cap).
    files:
      - internal/ratelimit/limiter.go
      - cmd/wipnote/serve.go
    deps: []
    blocks:
      # Authored FIRST during the Scope stage — `files` was read off this tree.
      - type: file-tree
        title: Files touched
        entries:
          - internal/ratelimit/limiter.go
          - cmd/wipnote/serve.go
      # Authored FIRST during the API/contract stage — `done_when` derived from it.
      - type: data-model
        fields:
          name: "POST /api/ingest — 429 response"
        rows:
          - name: error
            type: 'string ("rate_limited")'
          - name: retry_after
            type: int (seconds)
    done_when:
      - "Requests above the configured limit receive HTTP 429"
      - "Requests within the limit pass through unchanged"
      - "Counter `wipnote_throttled_total` increments on each 429"
    effort: M
    risk: Med
    tests: |
      Unit: TestLimiter_Allow — 10 req/s burst, first 5 allow, next reject
      Integration: TestServeRateLimit — spin up server, assert 429s
    complexity: complex
    decisions_notes: |
      **Trigger:** middleware on /api/ingest, keyed by RemoteAddr.
      **State:** in-memory map[ip]bucket, reset on process restart.
      **Payload:** 429 + {"error":"rate_limited","retry_after":1}.
      **Rejected:** file-backed counters (write amplification under load);
        Redis (new runtime dep — violates constraint).
    questions:
      - id: q-key
        text: "Key by RemoteAddr or X-Forwarded-For?"
        answer: "RemoteAddr — wipnote runs behind localhost only; XFF would need explicit trust list."
```

---

## Step 1: Create the Plan YAML

```bash
wipnote plan create-yaml "<title>" --description "<description>" --track <trk-id>
```

Note the returned plan ID. Write the YAML to `.wipnote/plans/<plan-id>.yaml` via `wipnote plan rewrite-yaml <plan-id> --file /tmp/plan.yaml`.

## Step 2: Validate, Critique, Review, Promote

```bash
wipnote plan validate-yaml <plan-id>
```

Fix schema errors before continuing. A blocks-gate failure means revisiting that slice's interview (or `wipnote:visual-plan` for a legacy plan, or a genuine `blocks_waiver:`).

Then ask the user to run `/wipnote:plan-critique <plan-id>` for the design/feasibility review (it is a slash command only; it runs as a forked subagent). If critique changes a slice's design, re-author its block and re-derive the prose in the same edit.

Then:

```bash
wipnote serve              # human reviews per-slice in dashboard
wipnote plan read-feedback-yaml <plan-id>   # ingest feedback
wipnote plan elicit-decisions <plan-id> <num> --scope ... --decisions ... --context ...
wipnote plan promote-slice <plan-id> <num>  # creates feat-XXX
wipnote spec generate <feat-id> --insert    # materialises spec section
wipnote plan set-status <plan-id> completed # when all slices done
```

---

## Section-Naming Contract (load-bearing)

State stored in `plan_feedback` uses these section keys — mirrored in
`plan/planyaml/schema.go:43-55` and enforced by `validSectionRe` in
`cmd/wipnote/api_plans.go`:

| Key pattern | What it stores |
|-------------|----------------|
| `slice-<num>` | Slice-level approval (`action=approve`) and execution status (`action=set_execution_status`) |
| `slice-<num>-question-<id>` | Answer to a slice-local question (`action=answer`) |
| `design` | Design section approval |
| `questions` | Plan-level question answers |

The `answer-slice-question` command maps `<slice-num>` and `<question-id>` to the section key automatically.

---

## CLI Reference

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

## Related Skills

- `plugin/skills/plan-critique/SKILL.md` — dual-critic review pass (run after the plan is drafted)
- `plugin/skills/visual-plan/SKILL.md` — enrichment only: add or revise blocks on an existing or legacy plan.
- `plugin/skills/plan/LEGACY.md` — v1 plan compatibility notes (referenced by validator errors when an old plan is loaded)
