---
name: skills
description: "Lists every wipnote skill, including those hidden from the skill list (planning, diagnosis, recaps, UI checks, deploy), and how to load each one. Use to find the right wipnote workflow before starting one."
---

# wipnote skills

Most wipnote skills are hidden from the model's skill list to save context. They still exist. To use one:

- **The person** types its slash command.
- **You** run `wipnote skill show <dir>` and follow what it prints. It works on every harness. For a skill marked *slash only*, ask the person to run the command instead.

Always in the skill list: `execute`, `code-quality-skill`, `orchestrator-directives-skill`, and `agent-context` (preloaded into coder agents).

| Need | Slash command | `<dir>` |
|---|---|---|
| Plan work: triage, interview, slice YAML, review | `/wipnote:plan` | `plan` |
| Critique a drafted plan (design and feasibility critics) — *slash only* | `/wipnote:plan-critique <plan-id>` | `plan-critique` |
| Add visual blocks to an existing plan | `/wipnote:visual-plan` | `visual-plan` |
| Turn an approved slice into a feature spec | `/wipnote:spec-from-slice` | `spec-from-slice` |
| Run a plan or track: parallel dispatch to agents | `/wipnote:execute <id>` | `execute` |
| Pick what to work on next, bottlenecks, risk | `/wipnote:strategic-planning` | `strategic-planning` |
| Diagnose a bug or error to its root cause | `/wipnote:diagnose <bug-id or symptom>` | `diagnose` |
| TDD steps for an implementation task | `/wipnote:tdd-protocol` | `tdd-protocol` |
| Quality gate before a commit | `/wipnote:code-quality-skill` | `code-quality-skill` |
| Verify UI changes with screenshots | `/wipnote:ui-stills-verification` | `ui-stills-verification` |
| Visual recap of finished work | `/wipnote:visual-recap <id>` | `visual-recap` |
| Orchestrator rules: delegation, briefs, stalled agents | `/wipnote:orchestrator-directives-skill` | `orchestrator-directives-skill` |
| Write a guard rule | `/wipnote:guard-authoring` | `guard-authoring` |
| Bootstrap architecture memory cards | `/wipnote:arch-bootstrap` | `arch-bootstrap` |
| Rules shared by coder agents (preloaded into them) | — | `agent-context` |
| Release wipnote — *slash only* | `/wipnote:deploy <version>` | `deploy` |
| Report a wipnote issue upstream — *slash only* | `/wipnote:report-issue` | `report-issue` |

Planning does not need plan mode: ask for a plan in plain words ("plan this, don't build yet"), or run `/wipnote:plan`. Plans are saved in `.wipnote/plans/`.
