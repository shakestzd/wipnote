# wipnote Scripts

Development and deployment scripts for the wipnote project.

## Deployment

```bash
# Full release (non-interactive)
./scripts/deploy-all.sh 0.41.0 --no-confirm

# Preview (dry-run)
./scripts/deploy-all.sh 0.41.0 --dry-run

# Build-only (quality gates)
./scripts/deploy-all.sh --build-only

# Docs-only (commit + push)
./scripts/deploy-all.sh --docs-only
```

See `deploy-all.sh --help` for all options.

## Worktree Helpers

Worktree creation is handled by the Go CLI:

```bash
wipnote yolo --track <trk-id>         # Create worktree + branch for a track
```

These thin shell helpers manage worktrees after they exist:

```bash
scripts/worktree-status.sh              # Show all worktrees
scripts/worktree-cleanup.sh <name>      # Remove a worktree
```

## Devcontainer

```bash
scripts/devcontainer-verify.sh          # Full verification suite for the devcontainer
                                         # (go build + go vet + go test + binary + dashboard smoke test)
```

## Other

```bash
scripts/git-commit-push.sh              # Stage, commit, push
```
