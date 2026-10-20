---
name: plan-critique
description: Runs a design critic and a feasibility critic on a drafted wipnote plan, in parallel when subagents are available, otherwise in sequence. Revises slice prose in place and records a top-level critique. Use after drafting and before human review.
context: fork
disable-model-invocation: true
---

# wipnote Plan Critique

This is Phase 3 of `/wipnote:plan`. Run it **after** the plan is drafted (slices laid out, decisions_notes captured) and **before** human review. It runs a design critic and a feasibility critic against the plan, fixes the affected slices, and records the critique.

---

## When to run

| Plan shape | Critique? |
|---|---|
| Trivial single-slice plan | Skip — overhead outweighs value |
| Fewer than 3 slices (`critique_warranted: false`) | Optional |
| 3+ slices | Run |
| Any slice with `complexity: complex` | **Required** before human review |

---

## Steps

Copy this checklist and tick it off:

```
Plan critique (plan-id):
- [ ] 1. Plan and every slice card loaded
- [ ] 2. Design critic and feasibility critic run
- [ ] 3. Findings recorded with severity and evidence
- [ ] 4. Severity triaged
- [ ] 5. Slices revised; validate-yaml clean
- [ ] 6. Top-level critique written
- [ ] 7. Zero open `danger` items; handed to human review
```

1. **Load the plan.** Run `wipnote plan critique <plan-id>`. It prints JSON: title, status, complexity, `slice_count`, `critique_warranted` (false under 3 slices), and slice numbers and titles. It does not print slice bodies, so then read `.wipnote/plans/<plan-id>.yaml` for each slice card.
2. **Run both critics yourself** (no separate CLI dispatch). The two critics are independent. Run them in parallel when the harness can dispatch subagents; otherwise run them one after the other.
   - **DESIGN CRITIC** reads the plan as a design document. It looks for missing slices, scope gaps, unclear acceptance criteria, conflicting goals, undefined lifecycle states, and reviewer ergonomics (will a human understand and approve this?).
   - **FEASIBILITY CRITIC** reads the plan as an implementation contract. It checks that cited files exist, line ranges match, existing patterns are reused, gates and regex/SQL constraints accept the proposed sections, and each done_when entry is testable from the listed files alone. Before scoring any slice it MUST:
     - **Verify external claims via web search / web fetch.** Check claims about library behaviour, SDK contracts, API response shapes, or harness (Claude Code / Codex CLI / Antigravity) plugin/hook semantics against current official docs. An unverified claim is `warn`. A claim that contradicts the docs is `danger`.
     - **Verify cited `research:` sources.** Fetch each `research[].url` and `design.research` url. It must resolve and support its `claim`. A dead or non-supporting link is `warn`; a contradicting source is `danger`. Re-check stale `accessed` dates on version-sensitive claims.
     - **Enforce the research gate (v4).** A standard/complex slice with neither `research:` nor `research_waiver:`, or a plan with no `design.research`, is `danger`. Run `wipnote plan validate-yaml <plan-id>` and fold its research errors into the critique.
     - **Flag reinvented wheels.** If a slice builds something a maintained OSS package already solves, flag it as `warn` and cite the package name and source URL.
3. **Record each finding** with the slice `num`, a severity, a one-line summary, and evidence for any external or runtime claim. Evidence is a source URL, `runtime: <command>`, or `unverified: <reason>`.
4. **Act on severity.** Use one vocabulary everywhere: `success | warn | danger | info`.
   - `success`, `info`: no action.
   - `warn`: consider fixing before human review.
   - `danger`: must fix before human review.
5. **Fix slices in place:** `wipnote plan critique revise <plan-id> --slice <num> --what "…" --why "…" --tests "…"` (empty flags leave a field unchanged). The old wording stays in git history (`wipnote history <plan-id>`). Do not append `critic_revisions`; that field is deprecated. For other fields, edit the YAML and run `wipnote plan rewrite-yaml <plan-id> --file <path>`. Then run `wipnote plan validate-yaml <plan-id>`. If it fails, fix the slice and run it again; go on only when it is clean.
6. **Write the top-level critique** for 3+ slice or complex plans (see below).
7. **Hand off** to human review (`wipnote serve` → dashboard) only after every `danger` item is resolved. If any `danger` remains, go back to step 5.

---

## Harness-neutral runner rules

Name the role (`design critic`, `feasibility critic`), not a model. Do not require Claude model aliases (Haiku, Sonnet, Opus) as runners. Record the runner you actually used as metadata, e.g. `antigravity-design` / `antigravity-feasibility`, `codex-design` / `codex-feasibility`, or `local-design` / `local-feasibility`. Put concrete model names, if known, in `critique.reviewers` text.

Runtime verification requirements for backend claims:

- **Antigravity / `agy`**: check flags with `agy --help` and model labels with `agy models`. Public docs may lag, so record runtime evidence.
- **Codex**: check official OpenAI Codex CLI docs and/or `codex exec --help`.
- **Ollama**: check official Ollama CLI docs and `ollama --help` when installed. If it is not installed, say local verification was unavailable.

If you cannot verify an external CLI flag or model capability, mark the assumption `unverified` or `plausible`, not `verified`.

---

## Top-level critique section

Write it with `wipnote plan set-critique-yaml <plan-id> --data '<json>'` (or JSON on stdin). The JSON keys match this YAML shape (`plan/planyaml/schema.go`):

```yaml
critique:
  reviewed_at: "YYYY-MM-DD"
  reviewers:
    - "antigravity-design (role: design critic; runner/model verified locally or by docs)"
    - "antigravity-feasibility (role: feasibility critic; runner/model verified locally or by docs)"
  assumptions:
    - id: A1
      status: verified|plausible|unverified|questionable|falsified
      text: "<assumption>"
      evidence: "<file:line citation, source URL, or runtime command>"
  critics:
    - title: DESIGN CRITIC
      sections:
        - heading: "<theme>"
          items:
            - badge: "<short label>"
              kind: success|warn|danger|info
              text: "<finding>"
    - title: FEASIBILITY CRITIC
      sections:
        # ... same shape ...
  risks:
    - risk: "<one-line risk>"
      severity: High|Medium|Low
      mitigation: "<concrete action>"
  synthesis: |
    One paragraph: which blockers are resolved, which remain, and what
    the human reviewer should look at first.
```

The orchestrator reads `synthesis` to decide if the plan is ready for review. Name blockers by ID (A1, B2) so readers can map findings back to code.

---

## Relationship to other skills

- **`/wipnote:plan`** owns the lifecycle. This skill is its Phase 3; the plan is drafted (Phase 1) and validated (Phase 2) first.
- **Phase 5 of `/wipnote:plan`** (slice decisions and spec) runs after human approval. It reads `decisions_notes`, so merge critique findings into that prose before promotion.

Do not run this skill during the interview. The interview captures requirements; the critique evaluates the resulting design.
