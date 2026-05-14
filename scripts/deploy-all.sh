#!/usr/bin/env bash
set -euo pipefail

# deploy-all.sh — wipnote release pipeline (dev-only)
#
# Usage:
#   ./scripts/deploy-all.sh VERSION [FLAGS]
#
# Flags:
#   --no-confirm    Skip all confirmation prompts
#   --dry-run       Show what would happen without executing
#   --build-only    Only run quality gates (skip git/release)
#   --docs-only     Only commit and push (skip tag/release)
#
# The GitHub Actions workflow (release-go.yml) handles GoReleaser
# automatically when a v* tag is pushed — this script does NOT
# build cross-platform binaries locally.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PLUGIN_JSON="$PROJECT_ROOT/plugin/.claude-plugin/plugin.json"
MANIFEST_JSON="$PROJECT_ROOT/packages/plugin-core/manifest.json"
GO_DIR="$PROJECT_ROOT"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

# Parse arguments
VERSION=""
NO_CONFIRM=false
DRY_RUN=false
BUILD_ONLY=false
DOCS_ONLY=false

for arg in "$@"; do
    case "$arg" in
        --no-confirm) NO_CONFIRM=true ;;
        --dry-run) DRY_RUN=true ;;
        --build-only) BUILD_ONLY=true ;;
        --docs-only) DOCS_ONLY=true ;;
        --help|-h)
            echo "Usage: $0 VERSION [--no-confirm] [--dry-run] [--build-only] [--docs-only]"
            echo ""
            echo "  VERSION       Semantic version (e.g., 0.41.0)"
            echo "  --no-confirm  Skip confirmation prompts"
            echo "  --dry-run     Show what would happen"
            echo "  --build-only  Only run quality gates"
            echo "  --docs-only   Only commit and push (no tag/release)"
            exit 0
            ;;
        *)
            if [[ -z "$VERSION" && "$arg" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
                VERSION="$arg"
            elif [[ -z "$VERSION" && ! "$arg" =~ ^-- ]]; then
                echo -e "${RED}Invalid version: $arg (expected X.Y.Z)${NC}" >&2
                exit 1
            fi
            ;;
    esac
done

confirm() {
    if $NO_CONFIRM || $DRY_RUN; then return 0; fi
    read -p "  $1 [y/N] " -n 1 -r
    echo
    [[ $REPLY =~ ^[Yy]$ ]]
}

step() {
    echo -e "${CYAN}▸ $1${NC}"
}

ok() {
    echo -e "  ${GREEN}✓ $1${NC}"
}

warn() {
    echo -e "  ${YELLOW}⚠ $1${NC}"
}

fail() {
    echo -e "  ${RED}✗ $1${NC}" >&2
    exit 1
}

# ── Pre-flight checks ─────────────────────────────────────────

step "Pre-flight checks"

cd "$PROJECT_ROOT"

# Must be in project root
if [[ ! -f "$PLUGIN_JSON" ]]; then
    fail "Not in project root (missing $PLUGIN_JSON)"
fi

# Check current version
CURRENT_VERSION=$(grep '"version"' "$PLUGIN_JSON" | sed 's/.*"version": *"\([^"]*\)".*/\1/')
ok "Current version: $CURRENT_VERSION"

if [[ -z "$VERSION" && ! $BUILD_ONLY == true && ! $DOCS_ONLY == true ]]; then
    fail "VERSION required. Usage: $0 VERSION [--no-confirm]"
fi

if [[ -n "$VERSION" ]]; then
    ok "Target version: $VERSION"
fi

# Check git state
BRANCH=$(git branch --show-current)
if [[ "$BRANCH" != "main" ]]; then
    warn "Not on main branch (on: $BRANCH)"
    if ! confirm "Continue anyway?"; then exit 1; fi
fi

if [[ -n "$(git status --porcelain -- cmd/ internal/ go.mod plugin/hooks/hooks.json plugin/.claude-plugin)" ]]; then
    warn "Uncommitted changes in source files"
    git status --short -- cmd/ internal/ go.mod plugin/hooks plugin/.claude-plugin
    if ! confirm "Continue anyway?"; then exit 1; fi
fi

# ── Quality gates ──────────────────────────────────────────────

step "Quality gates"

if $DRY_RUN; then
    ok "[dry-run] Would run: go build, go vet, go test"
else
    echo "  Running go build..."
    (cd "$GO_DIR" && go build ./...) || fail "go build failed"
    ok "go build"

    echo "  Running go vet..."
    (cd "$GO_DIR" && go vet ./...) || fail "go vet failed"
    ok "go vet"

    echo "  Running go test..."
    (cd "$GO_DIR" && go test ./...) || fail "go test failed"
    ok "go test"
fi

if $BUILD_ONLY; then
    echo -e "\n${GREEN}Build-only complete. All quality gates passed.${NC}"
    exit 0
fi

# ── Version bump ───────────────────────────────────────────────

if [[ -n "$VERSION" && "$VERSION" != "$CURRENT_VERSION" ]]; then
    step "Bumping version: $CURRENT_VERSION → $VERSION"

    if $DRY_RUN; then
        ok "[dry-run] Would update $PLUGIN_JSON and $MANIFEST_JSON"
    else
        if [[ "$OSTYPE" == "darwin"* ]]; then
            sed -i '' "s/\"version\": \"$CURRENT_VERSION\"/\"version\": \"$VERSION\"/" "$PLUGIN_JSON"
            sed -i '' "s/\"version\": \"$CURRENT_VERSION\"/\"version\": \"$VERSION\"/" "$MANIFEST_JSON"
        else
            sed -i "s/\"version\": \"$CURRENT_VERSION\"/\"version\": \"$VERSION\"/" "$PLUGIN_JSON"
            sed -i "s/\"version\": \"$CURRENT_VERSION\"/\"version\": \"$VERSION\"/" "$MANIFEST_JSON"
        fi
        ok "Updated plugin.json + manifest.json"
    fi
fi

# ── Commit + push ──────────────────────────────────────────────

step "Git commit and push"

if $DRY_RUN; then
    ok "[dry-run] Would commit version bump and push to origin/main"
    if ! $DOCS_ONLY && [[ -n "$VERSION" ]]; then
        ok "[dry-run] Would tag v$VERSION"
    fi
else
    # Stage version files + any other tracked changes
    git add "$PLUGIN_JSON" "$MANIFEST_JSON"

    if git diff --cached --quiet; then
        ok "No changes to commit"
    else
        git commit -m "release: v$VERSION"
        ok "Committed"
    fi

    if ! $DOCS_ONLY && [[ -n "$VERSION" ]]; then
        git tag "v$VERSION"
        ok "Tagged v$VERSION"
    fi

    # Push main branch and only the specific new tag. Pushing --tags broadcasts
    # every local tag and fails the whole script when historical tags already
    # exist on the remote (bug-b0264f1b). Push the exact new ref by name.
    if ! $DOCS_ONLY && [[ -n "$VERSION" ]]; then
        git push origin main "v$VERSION"
    else
        git push origin main
    fi
    ok "Pushed to origin/main"
fi

if $DOCS_ONLY; then
    echo -e "\n${GREEN}Docs-only push complete.${NC}"
    exit 0
fi

# ── GitHub Release ─────────────────────────────────────────────

step "GitHub Release"

if $DRY_RUN; then
    ok "[dry-run] GitHub Actions will auto-create release from v$VERSION tag"
else
    echo "  GitHub Actions (release-go.yml) will automatically:"
    echo "    1. Build cross-platform binaries via GoReleaser"
    echo "    2. Create GitHub Release with assets"
    echo ""
    echo "  Monitor: gh run list --workflow=release-go.yml --limit 3"
    ok "Tag v$VERSION pushed — release pipeline triggered"
fi

# ── Update local install ──────────────────────────────────────

step "Update local install"

if $DRY_RUN; then
    ok "[dry-run] Would pull marketplace clone"
    ok "[dry-run] Would rebuild CLI binary via 'wipnote build'"
else
    # 1. Pull the marketplace clone so `claude plugin update` sees the new version.
    #    Claude Code's marketplace is a local git clone; without a pull the
    #    update command compares against the stale checkout and reports "already
    #    at latest."
    MARKETPLACE_DIR="$HOME/.claude/plugins/marketplaces/wipnote"
    if [[ -d "$MARKETPLACE_DIR/.git" ]]; then
        (cd "$MARKETPLACE_DIR" && git pull origin main --quiet 2>/dev/null) \
            && ok "Marketplace clone updated" \
            || warn "Marketplace pull failed (non-fatal)"
    else
        warn "Marketplace clone not found at $MARKETPLACE_DIR — skipping pull"
    fi

    # 2. Rebuild CLI binary so ~/.local/bin/wipnote matches the release.
    #    Plugin hooks use `wipnote` (PATH lookup), not the bundled binary.
    #    No plugin reinstall needed — the marketplace clone update (above)
    #    is sufficient. Reinstalling interferes with dev mode (--plugin-dir).
    #    Bootstrap from source via `go run` so we don't depend on a pre-existing
    #    wipnote binary in PATH (chicken-and-egg).
    (cd "$PROJECT_ROOT" && go run ./cmd/wipnote build 2>&1 | tail -1)
    ok "CLI binary rebuilt"
fi

# ── Post-release ───────────────────────────────────────────────

step "Post-release"

echo "  To check CI status:      gh run list --workflow=release-go.yml --limit 3"
echo "  To verify release:       gh release view v$VERSION"

echo -e "\n${GREEN}Deploy complete: v$VERSION${NC}"
