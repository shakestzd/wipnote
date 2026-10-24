---
paths:
  - "cmd/**"
  - "internal/**"
  - "plugin/**"
---

# Debugging Workflow - Research First

**Core principle: research first, implement second. NEVER guess; always verify.**

## Debug Commands

```bash
claude --debug <command>   # Verbose output
/hooks                     # List all active hooks
/doctor                    # System diagnostics
```

## Agents

- **researcher** — documentation research and root cause analysis (no `debugger` or `test-runner` agent exists; coders run the quality gate themselves)

## Workflow

1. Research (docs, agents) — form a hypothesis from evidence
2. Implement the minimal fix
3. Validate with `go build ./... && go vet ./... && go test ./...`

For investigation, dispatch the `wipnote:researcher` agent with the symptom and ask for file:line root cause.
