---
name: plan-critique
description: Runs a design critic and a feasibility critic on a drafted wipnote plan, in parallel when subagents are available, otherwise in sequence. Revises slice prose in place and records a top-level critique. Use after drafting and before human review.
context: fork
disable-model-invocation: true
---

# wipnote Plan Critique

Phase 3 of `/wipnote:plan`: run **after** the plan is drafted and validated, and **before** human review. Skip it for a trivial single-slice plan; it is optional under 3 slices and required when any slice is `complexity: complex`.

```
Plan critique (plan-id):
- [ ] 1. Plan and every slice card loaded
- [ ] 2. Design critic and feasibility critic run
- [ ] 3. Findings recorded with severity and evidence
- [ ] 4. Slices revised; validate-yaml clean
- [ ] 5. Top-level critique written
- [ ] 6. Zero open `danger` items; handed to review
```

1. **Load the plan.** `wipnote plan critique <plan-id>` prints the title, status, complexity, `slice_count` and `critique_warranted`. It omits slice bodies, so read each slice in `.wipnote/plans/<plan-id>.yaml`.
2. **Run both critics,** in parallel when subagents are available, otherwise one after the other. Name the role, not a model, and record the runner you used (for example `codex-design`, `local-feasibility`).
   - **Design critic:** reads the plan as a design document. Missing slices, scope gaps, unclear acceptance criteria, conflicting goals, undefined states, and whether a reviewer can understand and approve it.
   - **Feasibility critic:** reads the plan as an implementation contract. Cited files exist and line ranges match, existing patterns are reused, and each `done_when` is testable from the listed files. Before scoring a slice it must:
     - **Verify external claims via web search / web fetch** against current official docs (library behaviour, SDK contracts, API shapes, harness hook and plugin semantics). Unverified is `warn`; contradicted is `danger`.
     - **Check every cited `research:` URL** resolves and supports its claim (dead or unsupported is `warn`, contradicted is `danger`), and re-check stale `accessed` dates on version-sensitive claims.
     - **Enforce the research gate:** run `wipnote plan validate-yaml <plan-id>`; a v4 standard/complex slice with neither `research:` nor `research_waiver:`, or no `design.research`, is `danger`.
     - **Flag reinvented wheels** as `warn`, citing the maintained package and its URL.
3. **Record each finding** with the slice `num`, a severity from `success | warn | danger | info`, a one-line summary, and evidence (a source URL, `runtime: <command>`, or `unverified: <reason>`). `danger` must be fixed before review; `warn` should be.
4. **Fix slices in place** with `wipnote plan critique revise <plan-id> --slice <num> --what "…" --why "…" --tests "…"` (empty flags leave a field unchanged; the old wording stays in git history). For other fields, edit a copy of the YAML and run `wipnote plan rewrite-yaml`. Then run `wipnote plan validate-yaml` until it is clean.
5. **Write the top-level critique** for 3+ slice or complex plans with `wipnote plan set-critique-yaml <plan-id> --data '<json>'`. Shape (see `plan/planyaml/schema.go`): `reviewed_at`; `reviewers`; `assumptions` (`id`, `status` verified|plausible|unverified|questionable|falsified, `text`, `evidence`); `critics` (`title`, `sections` of `heading` + `items` of `badge`, `kind`, `text`); `risks` (`risk`, `severity` High|Medium|Low, `mitigation`); and a one-paragraph `synthesis` that names open blockers by ID and tells the reviewer what to look at first.
6. **Hand off** to the Phase 4 review only when no `danger` item remains; otherwise go back to step 4.

## Runtime verification requirements for backend claims

- **Antigravity / `agy`:** check flags with `agy --help` and model labels with `agy models`; public docs may lag, so record runtime evidence.
- **Codex:** check the official Codex CLI docs and/or `codex exec --help`.
- **Ollama:** check the official docs and `ollama --help` when installed; otherwise say local verification was unavailable.

If you cannot verify a CLI flag or model capability, mark the assumption `unverified` or `plausible`, never `verified`. Do not run this skill during the interview: the interview captures requirements, and the critique evaluates the resulting design.
