# Code Hygiene - Mandatory Quality Standards

**CRITICAL: Fix every gate failure in the files you changed, whoever introduced it. Report any other failure and start no unrelated refactor.**

## Rules

- Run `go build`, `go vet` and `go test` before every commit. The command is under Workflow.
- Fix failures in files you changed, even ones that were there before you started.
- Report any other failure and start no unrelated refactor.
- Never commit with an unresolved failure in a file you changed.
- `deploy-all.sh` blocks on build errors, vet warnings and test failures.

## Workflow

```bash
# Before every commit:
go build ./... && go vet ./... && go test ./...

# Commit when the checks pass for the files you changed
git commit -m "..."
```

## Module Size & Complexity Standards

### Size Review Triggers

| Unit | Review trigger |
|------|----------------|
| File (module) | >500 lines |
| Function | >50 lines |
| Struct (class) | >300 lines |

These are review triggers, not measures. Crossing one means checking for more than one reason to change; it does not by itself mean split. The file and function triggers live in one place, `core/hooks/constants.go` (`ModuleLimitLines`, `FuncLimitLines`). The post-edit hook, `wipnote health`, `wipnote check --gate` and `wipnote review` all read them. `check --gate` fails on a trigger that changed code crossed, unless a `// wipnote:size-ok: <reason>` comment records why the code stays whole.

### Principles

1. **Single Responsibility**: Each package should have one clear purpose describable in one sentence
2. **No Duplication**: Check `internal/` for shared utilities before writing new ones
3. **Prefer Existing Dependencies**: Check `go.mod` and stdlib before custom implementations
4. **Import Direction**: Dependencies flow one way (cmd -> internal, never internal -> cmd)
