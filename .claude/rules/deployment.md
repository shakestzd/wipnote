---
paths:
  - "scripts/**"
  - "plugin/.claude-plugin/**"
  - ".goreleaser.yml"
---

# Deployment & Release Rules

**CRITICAL: Use `./scripts/deploy-all.sh` or `/deploy` for all deployment operations.**

## Bundled Distribution Model

Every release ships the CLI binary and bundled harness trees together in the GitHub Release tarball (and Homebrew formula):

| Component | Local install path | Source |
|-----------|--------------------|--------|
| **CLI binary** | `~/.local/bin/wipnote` (copy) | `wipnote build` |
| **Bundled trees** | `~/.local/share/wipnote/{plugin,codex-marketplace,antigravity-extension,mods}/` | `wipnote build` / release tarball |

`wipnote build` copies the compiled binary to `~/.local/bin/wipnote` and refreshes the bundled trees under `~/.local/share/wipnote/`. Keeping a copy (rather than a symlink to the working tree) isolates the installed `wipnote` binary from broken mid-refactor builds.

## Using the Deployment Script

```bash
# Recommended: one command, fully automated
./scripts/deploy-all.sh X.Y.Z --no-confirm

# Or via repo-local slash command:
/deploy X.Y.Z
```

**Pre-requisite:** Clean git state on `main` and all quality gates passing (`go build ./...`, `go vet ./...`, `go test -short ./...`, plus sub-modules `core/`, `plan/`, `port/`, `observe/`).

**What `scripts/deploy-all.sh` Does:**
1. **Pre-flight** — verifies clean git state, branch, and version format
2. **Quality gates** — `go build`, `go vet`, `go test`, contention stress test, and plugin port-drift check
3. **Version bump** — updates `packages/plugin-core/manifest.json`, `plugin/.claude-plugin/plugin.json`, and `.claude-plugin/marketplace.json`, then runs `wipnote plugin build-ports`
4. **Commit, tag, and push** — commits `release: vX.Y.Z`, creates tag `vX.Y.Z`, pushes to `origin/main`
5. **GitHub Release** — tag triggers GoReleaser via `.github/workflows/release-go.yml`
6. **Local rebuild** — runs `go run ./cmd/wipnote build` to update `~/.local/bin/wipnote` and `~/.local/share/wipnote/`

**Available Flags:**
- `--no-confirm` — Skip confirmation prompts (recommended for AI)
- `--docs-only` — Only commit and push (skip tag/release)
- `--build-only` — Only run quality gates
- `--dry-run` — Show what would happen without executing

## Version Numbering

Semantic Versioning: **MAJOR.MINOR.PATCH**
- Patch (`X.Y.Z+1`): Bug fixes
- Minor (`X.Y+1.0`): New features (backward compatible)
- Major (`X+1.0.0`): Breaking changes

**Version source of truth:** `packages/plugin-core/manifest.json` (generated into `plugin/.claude-plugin/plugin.json` and mirrored into `.claude-plugin/marketplace.json`).

## Post-Deployment Verification

```bash
# CI and release pipelines
gh run list --workflow=ci.yml --limit 1
gh run list --workflow=release-go.yml --limit 3

# All version surfaces agree
./scripts/verify-versions.sh
grep '"version"' plugin/.claude-plugin/plugin.json packages/plugin-core/manifest.json .claude-plugin/marketplace.json
wipnote version
```

## Rollback

1. Bump to the next patch version with the fix
2. Or delete the release/tag if caught before distribution: `gh release delete vX.Y.Z`
3. Re-deploy the corrected version
