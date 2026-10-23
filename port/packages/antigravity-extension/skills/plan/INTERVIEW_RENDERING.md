# Rendering the interview

Used by the draft interview (Phase 1 of [SKILL.md](SKILL.md)) and the slice decisions phase ([SPEC.md](SPEC.md), Part A).

## Question source

The binary holds the questions:

    wipnote plan interview-questions <plan-id>              # upfront: triage, problem, goals, constraints
    wipnote plan interview-questions <plan-id> <slice-num>  # one slice: its template and open questions

Each stage carries a `blocks` array (`type`, `description`, `prompt`); show those prompts with the stage's questions.

## How to ask

1. **Native ask-user tool** (Claude `AskUserQuestion`, Antigravity `ask_user`), one call per stage. Preferred.
2. **Plain chat** where there is none (for example Codex): list each question and its options; the user answers in their reply. Infer answers from a supplied spec only if the user asks you to proceed without questions, and label those in `decisions_notes` with `**Inferred:**` or `**Skipped:**` so reviewers see where the interview was compressed.

Plans must not depend on Claude-only model aliases or tools.

## Saving answers

- Upfront intake: `wipnote plan set-design-yaml <plan> --problem … --goals … --constraints …`, then draft the slices at the assessed complexity.
- Per slice: `wipnote plan elicit-decisions <plan> <slice> --scope … --decisions … --context …`.

Nothing keeps state between rounds, so ask follow-ups yourself for any unmet mandatory field.
