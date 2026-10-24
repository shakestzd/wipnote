---
name: skills
description: "Lists every wipnote skill, including those hidden from the skill list (planning, plan critique, UI checks, guard profiles, issue reports), and how to load each one. Use to find the right wipnote workflow before starting one."
---

# wipnote skills

Most wipnote skills are hidden from the model's skill list to save context. They still exist. To use one:

- **The person** types its slash command.
- **You** run `wipnote skill show <dir>` and follow what it prints. It works on every harness. A skill's linked files open the same way: `wipnote skill show plan SPEC.md`. For a skill marked *slash only*, ask the person to run the command instead.

Always in the skill list: `execute`, `code-quality-skill`, and `agent-context` (preloaded into coder agents).

| Need | Slash command | `<dir>` |
|---|---|---|
| Plan lifecycle: draft, validate, review, slice specs, promote; add blocks to an existing plan | `/wipnote:plan` | `plan` |
| Critique a drafted plan (Phase 3 of plan) — *slash only* | `/wipnote:plan-critique <plan-id>` | `plan-critique` |
| Run a plan or track: parallel dispatch to agents | `/wipnote:execute <id>` | `execute` |
| Quality gate before a commit | `/wipnote:code-quality-skill` | `code-quality-skill` |
| Verify UI changes with screenshots | `/wipnote:ui-stills-verification` | `ui-stills-verification` |
| Write a guard rule | `/wipnote:guard-authoring` | `guard-authoring` |
| Rules shared by coder agents, including the test-first implementation workflow (preloaded into them) | — | `agent-context` |
| Report a wipnote issue upstream — *slash only* | `/wipnote:report-issue` | `report-issue` |

To pick what to work on next, run the `/wipnote:recommend` command. For a recap of finished work, run `wipnote recap <id>`; to seed architecture cards, run `wipnote arch bootstrap` and follow its printed instructions.

Planning does not need plan mode: ask for a plan in plain words ("plan this, don't build yet"), or run `/wipnote:plan`. Plans are saved in `.wipnote/plans/`.
