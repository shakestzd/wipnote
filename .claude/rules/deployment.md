---
paths:
  - "scripts/**"
  - "plugin/.claude-plugin/**"
  - ".goreleaser.yml"
---

# Deployment & Release Rules

**CRITICAL: Use `./scripts/deploy-all.sh` or `/deploy` for all deployment operations.**

## Two Independent Install Paths

Wipnote has **two components** that must stay in sync after every release:

| Component | Location | Mechanism |
|-----------|----------|-----------|
| **CLI binary** | `~/.local/bin/wipnote` (copy) | `plugin/build.sh` |
| **Plugin** | `~/.claude/plugins/cache/wipnote/wipnote/<ver>/` | `claude plugin install wipnote@wipnote` |

The deploy script updates both automatically. **Never update one without the other.**

### Why Reinstall (Not Update)

`claude plugin update` preserves the old `gitCommitSha` in `installed_plugins.json`. Claude Code uses that SHA to resolve the plugin subdirectory within the marketplace clone. If the project structure ever changes (e.g., `packages/go-plugin/` → `plugin/`), the stale SHA points to a nonexistent path and **breaks hooks in ALL projects on the machine**. The deploy script does `uninstall + install` to get a clean SHA.

### Why Copy (Not Symlink) for CLI

`build.sh` copies the binary to `~/.local/bin/wipnote`. A symlink would make every project use the live dev binary — dangerous if the dev tree has a broken build mid-refactor. The copy provides isolation between "wipnote the thing I'm building" and "wipnote the thing I rely on."

## Using the Deployment Script

```bash
# Recommended: one command, fully automated
./scripts/deploy-all.sh 1.0.0 --no-confirm

# Or via slash command:
/deploy 1.0.0
```

**Pre-requisite:** `go test ./...` must pass.

**What the Script Does:**
1. **Quality gates** — `go build`, `go vet`, `go test`
2. **Version bump** — Updates `plugin/.claude-plugin/plugin.json`
3. **Git push** — Commits version change, tags, pushes to origin/main
4. **GitHub Release** — Tag triggers GoReleaser via GitHub Actions
5. **Marketplace pull** — `git pull` on `~/.claude/plugins/marketplaces/wipnote/`
6. **Plugin reinstall** — `claude plugin uninstall` + `install` (clean `gitCommitSha`)
7. **CLI rebuild** — `plugin/build.sh` copies binary to `~/.local/bin/`

**Available Flags:**
- `--no-confirm` — Skip confirmation prompts (recommended for AI)
- `--docs-only` — Only commit and push (skip tag/release)
- `--build-only` — Only run quality gates
- `--dry-run` — Show what would happen without executing

## Version Numbering

Semantic Versioning: **MAJOR.MINOR.PATCH**
- Patch (X.Y.Z+1): Bug fixes
- Minor (X.Y+1.0): New features (backward compatible)
- Major (X+1.0.0): Breaking changes

**Version file:** `plugin/.claude-plugin/plugin.json`

## Post-Deployment Verification

```bash
# CI pipelines
gh run list --workflow=ci.yml --limit 1
gh run list --workflow=release-go.yml --limit 1

# Installed versions match
grep '"version"' plugin/.claude-plugin/plugin.json
cat ~/.local/share/wipnote/.binary-version
```

## Rollback

1. Bump to next patch version with the fix
2. Or delete the release: `gh release delete v1.0.0` (if caught quickly)
3. Re-deploy the corrected version
