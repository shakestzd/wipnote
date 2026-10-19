---
disable-model-invocation: true
description: "Deploy and release wipnote — run quality gates, bump version, tag, push, and trigger GitHub release pipeline. Use when asked to deploy, release, publish, ship, or push a new version."
argument-hint: "[VERSION]"
allowed-tools: ["Bash", "Read", "Grep", "Glob"]
---

# Deploy wipnote

Run the full deployment pipeline for wipnote.

## Quick Deploy

```bash
# Non-interactive (recommended for CI/automation)
./scripts/deploy-all.sh VERSION --no-confirm

# Interactive (with confirmations)
./scripts/deploy-all.sh VERSION

# Dry run (preview what would happen)
./scripts/deploy-all.sh VERSION --dry-run
```

## What the Pipeline Does

1. **Pre-flight** — Verify clean git state, correct branch, current version
2. **Quality gates** — `go build ./...`, `go vet ./...`, `go test -short ./...`, the SQLite contention stress test, and a plugin port-drift check
3. **Version bump** — Update `plugin/.claude-plugin/plugin.json`, `packages/plugin-core/manifest.json` and `.claude-plugin/marketplace.json`, then run `wipnote plugin build-ports`
4. **Commit + tag** — `release: vX.Y.Z` commit (version files plus regenerated `port/packages/` trees), `vX.Y.Z` git tag
5. **Push** — Push commits and tags to origin/main
6. **GitHub Release** — Triggered automatically by `release-go.yml` workflow on `v*` tag

## Instructions

When the user asks to deploy or release:

1. **Check for a version argument.** If not provided, read the current version from
   `plugin/.claude-plugin/plugin.json` and suggest the next bump: patch for fixes,
   minor for features, major for breaking changes.

2. **Ensure all source changes are committed** before deploying:
   ```bash
   git status --porcelain -- cmd/ internal/ go.mod plugin/
   ```
   If there are uncommitted changes, commit them first (ask the user for a commit
   message if unclear).

3. **Run the deploy script:**
   ```bash
   ./scripts/deploy-all.sh VERSION --no-confirm
   ```

4. **If `scripts/deploy-all.sh` is not found**, stop. You are not at the wipnote repo
   root. Run it from there; do not bump versions by hand.

5. **After deployment**, check CI status:
   ```bash
   gh run list --workflow=ci.yml --limit 1
   gh run list --workflow=release-go.yml --limit 3
   ```

6. **Verify the release and that installed versions match:**
   ```bash
   gh release view vVERSION
   grep '"version"' plugin/.claude-plugin/plugin.json packages/plugin-core/manifest.json .claude-plugin/marketplace.json
   wipnote version
   cat ~/.local/share/wipnote/.binary-version
   ```

## If Deployment Fails

- **Quality gate failure** — fix the issue, re-run the script
- **Git push failure** — check the branch is main and there are no upstream conflicts
- **Build failure** — run `wipnote build` directly to see the error

## Rules

- NEVER deploy without passing quality gates
- NEVER drop `--no-confirm` in automation (interactive prompts block the run)
- ALWAYS verify post-deployment that the CLI and plugin versions match

## Other Modes

```bash
# Build-only (quality gates, no release)
./scripts/deploy-all.sh --build-only

# Docs-only (commit + push, no tag/release)
./scripts/deploy-all.sh --docs-only
```
