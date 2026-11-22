# Worker rules block

`cmd/wipnote/prompts/worker-rules.md` is one block of at most 3 KB. It replaces the
`agent-context` and `code-quality-skill` skills for harnesses that cannot preload skills.
The delegate brief preamble carries it, and the Codex and Antigravity agent builds append
it in place of the two skills (feat-0d9b57bc, plan-2622b3f7 slice 3).

## Disposition of each skill section

Each row has one disposition: in the block, printed by a named command, or dropped with a reason.

| skill | section | disposition |
|-------|---------|-------------|
| agent-context | Shared Agent Context | dropped: the title only |
| agent-context | Work attribution and completion | in the block |
| agent-context | Implementation workflow | in the block |
| agent-context | Safety | in the block |
| agent-context | Research routing | dropped: stays in plugin/agents/researcher.md, and the system prompt for sessions |
| agent-context | Persist, then summarize | in the block |
| agent-context | Writing style | printed by wipnote prose check, with one line in the block |
| agent-context | Principles | dropped: the AGENTS.md rule is in the block, and the CLI-first line is in the system prompt |
| code-quality-skill | Code Quality Skill | dropped: the title only |
| code-quality-skill | Quality Gate: BUILD → LINT → TEST | printed by wipnote check --gate, which detects the language and runs the gate |
| code-quality-skill | Go suite notes | dropped: repo-specific, stays in CLAUDE.md |
| code-quality-skill | Size, DRY and design review | printed by wipnote check --gate and wipnote review |
| code-quality-skill | Record the result | in the block |
