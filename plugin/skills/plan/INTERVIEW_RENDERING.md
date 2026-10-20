# Rendering the Interview (cross-harness)

Used by the draft interview (Phase 1) and the slice decisions phase ([SPEC.md](SPEC.md), Part A).

## Contents

- Question source
- Rendering ladder
- Persisting answers
- Codex and other harnesses without an ask-user tool
- `--questions` JSON schema

## Question source

The binary is the one source of the questions:

    wipnote plan interview-questions <plan-id>              # upfront: triage + problem/goals/constraints
    wipnote plan interview-questions <plan-id> <slice-num>  # per slice: template + the slice's open questions

Each stage carries a `blocks` array (`type`, catalog `description`, `prompt`). Show those prompts with the stage questions.

## Rendering ladder

Use the first option the harness supports:

1. **Native ask-user tool, inline** (Claude `AskUserQuestion`, Antigravity `ask_user` when exposed). One call per stage. Preferred.
2. **Plain chat** when there is no ask-user tool (e.g. Codex). Show each question and its options. The user answers in their reply. Triage is always asked this way or via the tool, never a form.
3. **Web form (optional).** Only for a long complex interview or on request: `wipnote plan interview-questions … | wipnote plan interview … --questions -`. It blocks, saves on submit, and embeds the plan-review chat panel.

## Persisting answers

The web form saves its own answers. For the inline and chat paths, write them yourself:

- Upfront intake: `wipnote plan set-design-yaml <plan> --problem … --goals … --constraints …`. Then draft slice cards at the assessed complexity.
- Per slice: `wipnote plan elicit-decisions <plan> <slice> --scope … --decisions … --context …`.

Ask follow-ups on the same channel for any unmet mandatory field or ambiguity. Nothing keeps state between rounds, so you own the follow-ups.

## Codex and other harnesses without an ask-user tool

Codex may lack an ask-user tool, Task tools, and Claude-style agent dispatch. There:

1. Ask triage and interview questions in plain chat. Infer answers from the supplied spec only if the user asks you to proceed without more questions.
2. Label inferred or skipped answers in `decisions_notes` with `**Inferred:**` and `**Skipped:**`, so reviewers see where the interview was compressed.
3. Run the mandatory live web research before drafting slices. Record it in `design.research` and per-slice `research:` / `research_waiver:`.
4. Run `wipnote plan validate-yaml <plan-id>` before asking for human review.

Generated plans must not depend on Claude-only model aliases or tools. They must name and explain any skipped interview stage in `decisions_notes`.

## `--questions` JSON schema

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

- `blocks` (optional, per stage): `type` (a key from `wipnote plan blocks`), catalog `description`, and a `prompt`.
- `bucket` is `Scope`, `Decisions`, or `Context`. It routes answers into that `decisions_notes` subsection.
- `type` is `choice` (needs `options`, optional `multiSelect`), `text`, or `yesno`.
- Question `id`s must be unique. Answers compose into the same `### Scope` / `### Decisions` / `### Context` markdown as the inline path, so later commands behave the same whichever renderer you used.
