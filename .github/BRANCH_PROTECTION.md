# Branch Protection Settings

Configure these settings on GitHub for proper CI/CD workflow.

## Main Branch (`main`)

**Settings → Branches → Branch protection rules → main**

### Protect matching branches
- ✅ Require a pull request before merging
  - ✅ Require approvals: 1
  - ✅ Dismiss stale pull request approvals when new commits are pushed
  - ✅ Require review from Code Owners (if CODEOWNERS file exists)

- ✅ Require status checks to pass before merging
  - ✅ Require branches to be up to date before merging
  - **Required checks:**
    - `test (3.10)` - Python 3.10 tests
    - `test (3.11)` - Python 3.11 tests
    - `test (3.12)` - Python 3.12 tests
    - `lint` - Linting and type checks
    - `build` - Package build

- ✅ Require conversation resolution before merging

- ✅ Require linear history (no merge commits)

- ✅ Include administrators (applies rules to admins too)

- ✅ Restrict who can push to matching branches
  - Only allow: Repository maintainers

- ✅ Allow force pushes: **NO**

- ✅ Allow deletions: **NO**

## Dev Branch (`dev`)

**Settings → Branches → Branch protection rules → dev**

### Protect matching branches
- ✅ Require a pull request before merging
  - ✅ Require approvals: 1 (can be bypassed by maintainers)
  - ⬜ Dismiss stale pull request approvals (optional)

- ✅ Require status checks to pass before merging
  - ⬜ Require branches to be up to date (optional for faster iteration)
  - **Required checks:**
    - `test (3.11)` - At least Python 3.11 tests
    - `build` - Package build

- ⬜ Require conversation resolution before merging (optional)

- ⬜ Require linear history (allow merge commits on dev)

- ⬜ Include administrators (admins can bypass for hotfixes)

- ✅ Restrict who can push to matching branches
  - Allow: Repository maintainers and collaborators

- ✅ Allow force pushes: **Limited** (only maintainers for rebasing)

- ✅ Allow deletions: **NO**

## GitHub Secrets

**Settings → Secrets and variables → Actions**

Required secrets for CI/CD:

### Repository Secrets
- `PYPI_API_TOKEN` - PyPI API token for package publishing
  - Get from: https://pypi.org/manage/account/token/
  - Scope: Project-specific token for wipnote

### Environments

Create environment: `pypi`
- **Environment protection rules:**
  - ✅ Required reviewers: 1 (maintainers)
  - ⬜ Wait timer: 0 minutes
  - ✅ Deployment branches: Only `main`

- **Environment secrets:**
  - `PYPI_API_TOKEN` - Same token as above

## Workflow Permissions

**Settings → Actions → General → Workflow permissions**

- ✅ Read and write permissions
- ✅ Allow GitHub Actions to create and approve pull requests

## Setup Instructions

1. **Create Branch Protection Rules:**
   - Go to: `Settings → Branches → Add rule`
   - Follow settings above for `main` and `dev`

2. **Add PyPI Token:**
   - Go to: `Settings → Secrets and variables → Actions`
   - Click: `New repository secret`
   - Name: `PYPI_API_TOKEN`
   - Value: Your PyPI API token

3. **Create Environment:**
   - Go to: `Settings → Environments → New environment`
   - Name: `pypi`
   - Add protection rules and secrets as above

4. **Configure Workflow Permissions:**
   - Go to: `Settings → Actions → General`
   - Set workflow permissions as above

## Testing the Setup

### Test CI on Feature Branch

```bash
git checkout dev
git checkout -b test/ci-setup
echo "# Test" > TEST.md
git add TEST.md
git commit -m "test: CI setup"
git push origin test/ci-setup

# Create PR to dev on GitHub
# Verify all CI checks run and pass
```

### Test Release Workflow

```bash
# On main branch with a tag
git checkout main
git tag v0.7.2-test
git push origin v0.7.2-test

# Verify release workflow runs
# Delete test tag after: git tag -d v0.7.2-test && git push origin :refs/tags/v0.7.2-test
```

## Troubleshooting

### CI Checks Not Running

1. Check workflow files are on the branch
2. Verify GitHub Actions is enabled: `Settings → Actions → General`
3. Check workflow permissions

### PyPI Publishing Fails

1. Verify `PYPI_API_TOKEN` secret exists
2. Check token has correct scope (wipnote project)
3. Verify environment `pypi` is configured
4. Check token hasn't expired

### Branch Protection Blocks Emergency Fix

```bash
# Temporarily disable protection or use admin override
# Settings → Branches → Edit rule → Temporarily disable

# After fix:
# Re-enable protection
```

## Maintenance

### Regular Tasks

- **Monthly**: Review and update required CI checks
- **Quarterly**: Rotate PyPI tokens
- **Per Release**: Verify all CI checks pass
- **As Needed**: Update Python versions in CI matrix

### Version Updates

When adding new Python versions:

1. Update `.github/workflows/ci.yml` matrix
2. Update `pyproject.toml` classifiers
3. Update branch protection required checks
4. Test on feature branch first

---

**Last Updated**: December 23, 2025
**Version**: 0.7.1
