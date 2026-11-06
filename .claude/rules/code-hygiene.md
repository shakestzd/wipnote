# Code Hygiene - Mandatory Quality Standards

**CRITICAL: Always fix ALL errors with every commit, regardless of when they were introduced.**

## Philosophy

Maintaining clean, error-free code is non-negotiable. Every commit should reduce technical debt, not accumulate it.

## Rules

1. **Fix All Errors Before Committing**
   - Run `go build`, `go vet`, and `go test` before every commit
   - Fix ALL errors, even pre-existing ones from previous sessions
   - Never commit with unresolved build errors, vet warnings, or test failures

2. **No "I'll Fix It Later" Mentality**
   - Errors compound over time
   - Pre-existing errors are YOUR responsibility when you touch related code
   - Clean as you go - leave code better than you found it

3. **Deployment Blockers**
   - The `deploy-all.sh` script blocks on:
     - Go build errors
     - Go vet warnings
     - Test failures
   - This is intentional - maintain quality gates

4. **Why This Matters**
   - **Prevents Error Accumulation** - Small issues don't become large problems
   - **Better Code Hygiene** - Clean code is easier to maintain
   - **Faster Development** - No time wasted debugging old errors
   - **Professional Standards** - Production-grade code quality

## Workflow

```bash
# Before every commit:
go build ./... && go vet ./... && go test ./...

# Only commit when ALL checks pass
git commit -m "..."
```

**Remember: Fixing errors immediately is faster than letting them accumulate.**

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
