# Phase 5: Slice decisions and spec

Captures Scope, Decisions, and Context for one approved slice, then generates the feature's OpenSpec-formatted spec into its HTML. The source of truth is the cross-harness CLI `wipnote plan elicit-decisions`.

Run this phase when:

- a slice was just approved and is about to be promoted;
- a promoted feature has no `<section class="spec">`, or it is empty;
- a slice's decisions changed and the spec must follow.

Inputs: `<plan-id>` (the active plan) and `<slice-num>` (the slice's integer `num`).

## Checklist

Copy this checklist and tick it off for each slice:

```
Slice spec (plan-id / slice-num):
- [ ] 1. Slice card read; existing decisions_notes noted
- [ ] 2. Re-elicitation choice made (only if decisions_notes exists)
- [ ] 3. Scope / Decisions / Context answered
- [ ] 4. decisions_notes written with elicit-decisions
- [ ] 5. Spec generated (or promote-slice named as the blocker)
- [ ] 6. Spec reads back: `wipnote compliance <feature-id>` lists its criteria
```

## Step 1: Read the slice card

Run `wipnote plan show <plan-id>` (or read `.wipnote/plans/<plan-id>.yaml`) and find the slice with the given `num`. Note `title`, `what`, `why`, `done_when`, `tests`, `feature_id`, and any `decisions_notes`.

## Step 2: Re-elicitation guard

Skip this step when `decisions_notes` is empty. Otherwise ask the user (ask-user tool, or chat) to pick one:

- **Re-elicit**: overwrite the notes with new answers.
- **Edit in place**: show the existing notes, let the user edit them, and write them back verbatim.
- **Skip**: keep the notes and go to Step 5.

## Step 3: Three-question interview

Ask all three in one ask-user call, or as one chat message where the harness has no such tool. For other rendering options, follow [INTERVIEW_RENDERING.md](INTERVIEW_RENDERING.md).

- **Scope**: what is in this slice, and what is out?
- **Decisions**: which design choices were made, and why? Reference any plan questions they answer.
- **Context**: what else does the implementer need? Constraints, related work, file ownership boundaries.

When a decision involves an external technology (library, SDK, API, harness primitive), first ground the options in current official docs with web search or fetch. If research finds a maintained package that covers the need, present it as an option with a one-line reason it may beat a custom build.

Free-form answers are fine, and empty answers are allowed. If you used the web form (`wipnote plan interview <plan-id> <slice-num>`), it writes `decisions_notes` itself: skip Step 4.

## Step 4: Write the decisions

```bash
wipnote plan elicit-decisions <plan-id> <slice-num> \
  --scope "<scope answer>" \
  --decisions "<decisions answer>" \
  --context "<context answer>"
```

This writes one Markdown blob to `slice.decisions_notes` atomically. If it fails, fix the reported problem and run it again.

## Step 5: Generate the spec

If the slice has no `feature_id`, stop here: tell the user to run `wipnote plan promote-slice <plan-id> <slice-num>`, then come back to this step.

Otherwise:

```bash
wipnote spec generate <feature-id> --insert
```

The write is non-destructive. If the feature already has spec content, the command prints a diff and refuses. Pass `--force` only when the user accepts the overwrite.

## Step 6: Verify, then report

```bash
wipnote compliance <feature-id>
```

It reads the spec section back and lists its acceptance criteria. If it finds no spec section or no criteria, correct the decisions (Step 4) and generate again (Step 5). Continue only when the criteria are listed. Then report in 2-3 lines: which fields changed, where the spec was written, and the next step (usually `wipnote plan promote-slice`, or the next slice).

## Notes

- `decisions_notes` is free text. `wipnote spec generate` weaves it verbatim into the spec's `## Decisions` section, so the `**Trigger:**` / `**State:**` / `**Payload:**` / `**Rejected:**` headings from the draft phase carry straight through.
- `--allow-spec-skip` on `promote-slice` and `feature complete` is an emergency override only; this phase is the normal path.
