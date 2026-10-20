---
name: code-quality-skill
description: Runs the quality gate (build, lint, typecheck, test) and fixes failures before a commit, for Go, JavaScript/TypeScript, Python, Rust and other languages. Use before committing or when a gate fails.
---

# Code Quality Skill

## Quality Gate: BUILD → LINT → TEST

Commit only when all three phases pass. Fix every error in code you touch, including pre-existing ones.

This is the canonical gate table. Other wipnote prompts, agents and skills point here instead of copying it. Check the repository root for a manifest:

| File | Gate command |
|------|--------------|
| `go.mod` | `go build ./... && go vet ./... && go test ./...` (in a repo with `scripts/go-test-streaming.sh`, use `scripts/go-test-streaming.sh ./...` for the test phase) |
| `package.json` | `npm run build && npm run lint && npm test` (`tsc --noEmit` for type-check only) |
| `pyproject.toml` / `requirements.txt` | `uv run ruff check . && uv run pytest` |
| `Cargo.toml` | `cargo build && cargo clippy && cargo test` |
| `pom.xml` | `mvn compile && mvn checkstyle:check && mvn test` |
| `build.gradle` / `build.gradle.kts` | `./gradlew assemble && ./gradlew check` (`gradle` only when there is no wrapper) |

Multiple manifests can coexist (for example a Go backend with a `package.json` frontend). Run the gate for each.

Run the gate, fix the failures, and re-run until all phases pass.

### Go suite notes

- From cold, `go test ./...` is silent for about 5-6 minutes because output is buffered per package. Silence is not a stall. Budget 10 minutes before suspecting a hang.
- `scripts/go-test-streaming.sh` streams `go test -json` output live and names the last test to start if the test binary dies mid-package (bug-61973a05). Any `go test` flags pass through, for example `scripts/go-test-streaming.sh ./cmd/wipnote/... -run X`.

## Record the result

When the gate passes, run `wipnote check --gate` to record it on the active work item.
