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
scripts/devcontainer.sh up                 # Create or reuse the devcontainer
scripts/devcontainer.sh rebuild            # Rebuild after devcontainer config changes
scripts/devcontainer.sh shell              # Open zsh inside the running container
scripts/devcontainer-verify.sh          # Full verification suite for the devcontainer
                                         # (go build + go vet + go test + binary + dashboard smoke test)
```

## Delegate Phase B

```bash
scripts/delegate-phase-b.sh             # on the Mac, inside a herdr pane, from the repo root
scripts/delegate-phase-b.sh --self-test # container smoke run (no herdr, no model)
```

See `docs/design/delegate-phase-b.md` for the checks and flags.

## Claude cloud environments

```bash
scripts/cloud-env-setup.sh             # install the pinned wipnote release (idempotent)
```

Point a Claude cloud environment's setup script at it. See
`docs/guides/claude-projects.md`.

## Other

```bash
scripts/git-commit-push.sh              # Stage, commit, push
```
