# wipnote Scripts

Development, testing, installation, and deployment scripts for the wipnote project.

## Deployment & Release

```bash
# Full release (non-interactive)
./scripts/deploy-all.sh 0.95.0 --no-confirm

# Preview (dry-run)
./scripts/deploy-all.sh 0.95.0 --dry-run

# Build-only (quality gates)
./scripts/deploy-all.sh --build-only

# Docs-only (commit + push)
./scripts/deploy-all.sh --docs-only

# Verify version alignment across manifest.json, plugin.json, and latest GitHub Release
./scripts/verify-versions.sh
```

See `./scripts/deploy-all.sh --help` for all options.

## Testing & Verification

```bash
scripts/go-test-streaming.sh ./...         # Live-streaming Go test runner with event log
scripts/git-test-selective.sh              # Run tests scoped to changed packages
scripts/check-host-paths.sh                # Detect leaked host-local absolute paths
scripts/stress-sqlite-busy.sh              # SQLite contention stress harness
scripts/prompt-ab.sh                       # A/B prompt evaluation runner
```

## Installers & Cloud Setup

```bash
scripts/install.sh                         # Workstation bash installer (binary + bundled trees)
scripts/cloud-env-setup.sh                 # Pinned, idempotent installer for Claude cloud environments
```

See `docs/INSTALL.md` and `docs/guides/claude-projects.md`.

## Worktree Helpers

Worktree creation is handled by the Go CLI (`wipnote yolo --track <trk-id>`, `wipnote worktree list/clean`). These thin shell helpers manage worktrees directly:

```bash
scripts/worktree-status.sh                 # Show all worktrees
scripts/worktree-cleanup.sh <name>         # Remove a worktree
```

## Devcontainer

```bash
scripts/devcontainer.sh up                 # Create or reuse the devcontainer
scripts/devcontainer.sh rebuild            # Rebuild after devcontainer config changes
scripts/devcontainer.sh shell              # Open zsh inside the running container
scripts/devcontainer-verify.sh             # Full verification suite for the devcontainer
```

## Delegate Phase B

```bash
scripts/delegate-phase-b.sh                # On macOS, inside a herdr pane, from repo root
scripts/delegate-phase-b.sh --self-test    # Container smoke run (no herdr, no model)
```

See `docs/design/delegate-phase-b.md` for the checks and flags.

## Other

```bash
scripts/git-commit-push.sh                 # Stage, commit, push
```
