---
name: skills
description: "Lists every wipnote skill, including those hidden from the skill list (UI checks, guard profiles, issue reports), and how to load each one. Use to find the right wipnote workflow before starting one."
---

# wipnote skills

Most wipnote skills are hidden from the model's skill list to save context. They still exist. To use one:

- **The person** types its slash command.
- **You** run `wipnote skill show <dir>` and follow what it prints. It works on every harness. A skill's linked files open the same way: `wipnote skill show <dir> <file>`. For a skill marked *slash only*, ask the person to run the command instead.

Always in the skill list: `skills`. Coder agents get the worker rules from the SubagentStart hook, not from a skill.

| Need | Slash command | `<dir>` |
|---|---|---|
| Verify UI changes with screenshots | `/wipnote:ui-stills-verification` | `ui-stills-verification` |
| Write a guard rule | `/wipnote:guard-authoring` | `guard-authoring` |
| Report a wipnote issue upstream — *slash only* | `/wipnote:report-issue` | `report-issue` |
| Write, convert or check text in ASD-STE100 Simplified Technical English (linter + dictionary lookup) | `/wipnote:simplified-technical-english` | `simplified-technical-english` |

To pick what to work on next, run the `/wipnote:recommend` command. For a recap of finished work, run `wipnote recap <id>`; to seed architecture cards, run `wipnote arch bootstrap` and follow its printed instructions.

Planning does not need plan mode: ask for a plan in plain words ("plan this, don't build yet"), or run `wipnote plan next`. Plans are saved in `.wipnote/plans/`.
