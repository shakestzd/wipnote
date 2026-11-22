# Branch Protection Settings

Recommended GitHub repository settings for the `wipnote` Go monorepo.

## Main Branch (`main`)

wipnote uses a **`main`-only** branch strategy (see `CONTRIBUTING.md`). All changes land on `main` via pull request.

**Settings → Branches → Branch protection rules → `main`**

### Protect matching branches
- Require a pull request before merging
  - Require approvals: 1 (for multi-maintainer setups)
  - Dismiss stale pull request approvals when new commits are pushed
- Require status checks to pass before merging
  - Require branches to be up to date before merging
  - **Required checks** (from `.github/workflows/ci.yml`):
    - `quality-gates` — builds, vets, and runs `go test -short` across the root module plus `core/`, `plan/`, `port/`, and `observe/`, along with the CI-safe parallel-launcher acceptance harness
- Require conversation resolution before merging
- Allow force pushes: **NO**
- Allow deletions: **NO**

## GitHub Secrets & Release Pipeline

**Settings → Secrets and variables → Actions**

Releases are triggered by pushing a `vX.Y.Z` tag via `./scripts/deploy-all.sh X.Y.Z --no-confirm`, which runs `.github/workflows/release-go.yml` (GoReleaser + keyless `cosign` signing via GitHub Actions OIDC).

### Repository Secrets
- `HOMEBREW_TAP_GITHUB_TOKEN` — GitHub token with write access to `shakestzd/homebrew-wipnote` so GoReleaser can publish the updated Homebrew formula.
- `GITHUB_TOKEN` — provided automatically by GitHub Actions (`contents: write` and `id-token: write` permissions are declared in `.github/workflows/release-go.yml`).

## Workflow Permissions

**Settings → Actions → General → Workflow permissions**

- Read and write permissions (or per-workflow `permissions:` blocks as configured in `.github/workflows/release-go.yml`)
