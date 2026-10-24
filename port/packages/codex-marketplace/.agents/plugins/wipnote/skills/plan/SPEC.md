# Phases 5 and 7: Slice decisions and feature spec

Part A writes one approved slice's Scope, Decisions and Context into `decisions_notes`; promotion needs them. Part B generates the promoted feature's spec, which needs the feature that promotion creates. Inputs: `<plan-id>` and the slice's integer `<slice-num>`.

```
Slice decisions and spec (plan-id / slice-num):
Part A — before promotion
- [ ] 1. Slice card read; existing decisions_notes noted
- [ ] 2. Scope / Decisions / Context answered
- [ ] 3. decisions_notes written with elicit-decisions
(Phase 6: wipnote plan promote-slice <plan-id> <slice-num>)
Part B — after promotion
- [ ] 4. Spec generated for the slice's feature_id
- [ ] 5. `wipnote compliance <feature-id>` lists its criteria
```

## Part A: Decisions

1. **Read the slice** with `wipnote plan show <plan-id>`: note `title`, `what`, `why`, `done_when`, `tests`, `feature_id` and any `decisions_notes`. If `decisions_notes` already exists, ask whether to re-elicit, edit in place, or keep it.
2. **Ask three questions** in one ask-user call or one chat message ([INTERVIEW_RENDERING.md](INTERVIEW_RENDERING.md)):
   - **Scope:** what is in this slice and what is out?
   - **Decisions:** which design choices were made, and why? Name any plan questions they answer.
   - **Context:** what else does the implementer need: constraints, related work, file ownership?

   When a decision involves an external library, SDK, API or harness primitive, ground the options in current official docs first, and offer a maintained package when one covers the need.
3. **Write them:** `wipnote plan elicit-decisions <plan-id> <slice-num> --scope "…" --decisions "…" --context "…"`. Fix any reported problem and run it again.

## Part B: Spec

4. Read the slice's `feature_id`. If it is empty, promote the slice first. Then run `wipnote spec generate <feature-id> --insert`. It refuses to overwrite existing spec content and prints a diff; pass `--force` only when the user accepts the overwrite.
5. Run `wipnote compliance <feature-id>`. If it lists no criteria, fix the decisions (step 3) and generate again. Report in 2–3 lines: what changed, where the spec was written, and the next step (usually the next slice, or `/wipnote:execute`).

`decisions_notes` is woven verbatim into the spec's `## Decisions`, so the `**Trigger:**` / `**State:**` / `**Payload:**` / `**Rejected:**` headings carry through. `--allow-spec-skip` on `promote-slice` and `feature complete` is an emergency override only.
