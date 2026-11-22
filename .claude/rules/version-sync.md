---
paths:
  - "scripts/**"
  - "plugin/.claude-plugin/**"
---

# Version Synchronization Rules

**CRITICAL: Plugin manifest version and GitHub Release version MUST always match.**

## Why This Matters

The Go binary and bundled plugin trees are distributed together via GitHub Releases, and bootstrap/upgrade scripts resolve release assets from the manifest version. If versions diverge, installs and upgrades fail or pull mismatched assets.

## Version Files (Must All Match)

```
packages/plugin-core/manifest.json:      "version": "X.Y.Z"  (source of truth)
plugin/.claude-plugin/plugin.json:       "version": "X.Y.Z"  (generated via wipnote plugin build-ports)
.claude-plugin/marketplace.json:         "version": "X.Y.Z"  (updated by scripts/deploy-all.sh)
```

## Checking Versions

```bash
# Cross-check plugin.json, manifest.json, and latest GitHub Release
./scripts/verify-versions.sh

# Quiet mode (prints only on mismatch)
./scripts/verify-versions.sh --quiet
```

## Publishing Requirements

Use `./scripts/deploy-all.sh X.Y.Z --no-confirm` (or `/deploy X.Y.Z`) rather than bumping version files by hand. The deploy script updates all three files, runs `wipnote plugin build-ports`, and tags the release.

## For AI Agents

**DIRECTIVE**: When discussing or bumping version numbers:
1. Check `packages/plugin-core/manifest.json` and `plugin/.claude-plugin/plugin.json` for the current version
2. Verify all surfaces agree with `./scripts/verify-versions.sh`
3. Use `./scripts/deploy-all.sh X.Y.Z --no-confirm` for releases
