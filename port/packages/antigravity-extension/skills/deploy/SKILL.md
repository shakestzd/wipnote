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
2. **Quality gates** — `go build ./...`, `go vet ./...`, `go test ./...`
3. **Version bump** — Update `plugin/.claude-plugin/plugin.json`
4. **Commit + tag** — `release: vX.Y.Z` commit, `vX.Y.Z` git tag
5. **Push** — Push commits and tags to origin/main
6. **GitHub Release** — Triggered automatically by `release-go.yml` workflow on `v*` tag

## Instructions

When the user asks to deploy or release:

1. **Check for a version argument.** If not provided, read the current version from
   `plugin/.claude-plugin/plugin.json` and suggest the next patch/minor/major bump.

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

4. **If the script doesn't exist** (user is not in the wipnote dev repo), guide them
   through manual steps:
   ```bash
   # 1. Run quality gates (see /wipnote:code-quality-skill)

   # 2. Bump version in plugin.json

   # 3. Commit, tag, push
   git add plugin/.claude-plugin/plugin.json
   git commit -m "release: vVERSION"
   git tag vVERSION
   git push origin main --tags
   ```

5. **After deployment**, check CI status:
   ```bash
   gh run list --workflow=ci.yml --limit 1
   gh run list --workflow=release-go.yml --limit 3
   ```

6. **Verify the release and that installed versions match:**
   ```bash
   gh release view vVERSION
   grep '"version"' plugin/.claude-plugin/plugin.json
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
- Patch (X.Y.Z+1) for bug fixes, Minor (X.Y+1.0) for features, Major (X+1.0.0) for breaking changes

## Other Modes

```bash
# Build-only (quality gates, no release)
./scripts/deploy-all.sh --build-only

# Docs-only (commit + push, no tag/release)
./scripts/deploy-all.sh --docs-only
```

## Version Numbering

wipnote follows semantic versioning:
- **MAJOR** — Breaking changes
- **MINOR** — New features (backward compatible)
- **PATCH** — Bug fixes
