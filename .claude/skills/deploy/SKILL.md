---
name: deploy
disable-model-invocation: true
description: "Releases wipnote: runs the quality gates, bumps the version, tags, pushes, and triggers the GitHub release pipeline. Use when asked to deploy, release, publish, or ship a new wipnote version."
argument-hint: "[VERSION]"
allowed-tools: ["Bash", "Read", "Grep", "Glob"]
---

# Deploy wipnote

`scripts/deploy-all.sh` does the whole release. Do not bump versions, tag, or push by hand.

## What the script does

1. **Pre-flight**: clean git state, correct branch, current version.
2. **Quality gates**: `go build ./...`, `go vet ./...`, `go test -short ./...`, the SQLite contention stress test, and a plugin port-drift check.
3. **Version bump**: `plugin/.claude-plugin/plugin.json`, `packages/plugin-core/manifest.json` and `.claude-plugin/marketplace.json`, then `wipnote plugin build-ports`.
4. **Commit and tag**: a `release: vX.Y.Z` commit (version files plus regenerated `port/packages/` trees) and a `vX.Y.Z` tag.
5. **Push**: commits and tags to origin/main.
6. **GitHub release**: the `release-go.yml` workflow runs on the `v*` tag.

## Release workflow

Copy this checklist and tick it off:

```
Release vVERSION:
- [ ] 1. Version chosen
- [ ] 2. Source changes committed
- [ ] 3. Dry run reviewed
- [ ] 4. Release run
- [ ] 5. CI and release verified
```

**1. Choose the version.** If no version argument was given, read the current version from `plugin/.claude-plugin/plugin.json` and suggest the next bump: patch for fixes, minor for features, major for breaking changes.

**2. Commit source changes.**

```bash
git status --porcelain -- cmd/ internal/ go.mod plugin/
```

Commit anything listed first. Ask the user for a commit message if the intent is unclear.

**3. Dry run.** Run from the repo root. If `scripts/deploy-all.sh` is not found, you are not at the wipnote repo root: stop and move there.

```bash
./scripts/deploy-all.sh VERSION --dry-run
```

Check the version, branch, and file list it prints. Fix any problem it reports and run the dry run again. Continue only when it is clean.

**4. Release.**

```bash
./scripts/deploy-all.sh VERSION --no-confirm
```

Keep `--no-confirm`: without it the script waits on interactive prompts. If a quality gate fails, fix the cause and go back to step 3. If the push fails, check that the branch is main and has no upstream conflicts. If the build fails, run `wipnote build` to see the error.

**5. Verify.** These commands need an authenticated `gh` (`gh auth status`). Without it, check the Actions and Releases pages on GitHub instead.

```bash
gh run list --workflow=ci.yml --limit 1
gh run list --workflow=release-go.yml --limit 3
gh release view vVERSION
grep '"version"' plugin/.claude-plugin/plugin.json packages/plugin-core/manifest.json .claude-plugin/marketplace.json
wipnote version
cat ~/.local/share/wipnote/.binary-version
```

The release is done only when CI is green, the release exists, and every version printed above matches.

## Other modes

```bash
./scripts/deploy-all.sh --build-only   # quality gates only, no release
./scripts/deploy-all.sh --docs-only    # commit and push, no tag or release
```
