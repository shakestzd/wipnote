#!/usr/bin/env bash
set -euo pipefail

# verify-versions.sh — Detect version drift across wipnote's release surfaces.
#
# Cross-checks three version sources and reports a table of source -> version:
#   1. plugin/.claude-plugin/plugin.json       (generated Claude target manifest)
#   2. packages/plugin-core/manifest.json      (single source of truth)
#   3. Latest GitHub Release tag               (what users actually receive)
#
# Exit 0 if all three agree, 1 if any disagree.
#
# Usage:
#   ./scripts/verify-versions.sh
#   ./scripts/verify-versions.sh --quiet     # suppress success output (CI)
#   ./scripts/verify-versions.sh --help
#
# Auth fallback:
#   Uses `gh release view` when available + authenticated; otherwise falls back
#   to the public GitHub API via curl. Never hard-fails on missing tools — if
#   no release lookup is possible the entry is reported as "unknown" and the
#   script exits 1 only if the local sources also disagree.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PLUGIN_JSON="$PROJECT_ROOT/plugin/.claude-plugin/plugin.json"
MANIFEST_JSON="$PROJECT_ROOT/packages/plugin-core/manifest.json"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

QUIET=false

for arg in "$@"; do
    case "$arg" in
        --quiet|-q) QUIET=true ;;
        --help|-h)
            echo "Usage: $0 [--quiet] [--help]"
            echo ""
            echo "Compare version fields across wipnote's release surfaces and"
            echo "report drift. Exits 0 if all versions match, 1 otherwise."
            echo ""
            echo "  --quiet, -q   Suppress success output (for CI usage)."
            echo "                Mismatches are always printed."
            echo "  --help, -h    Show this help."
            exit 0
            ;;
        *)
            echo "Unknown argument: $arg" >&2
            echo "Try: $0 --help" >&2
            exit 2
            ;;
    esac
done

ok() {
    $QUIET && return 0
    printf "  ${GREEN}OK${NC} %s\n" "$1"
}

info() {
    $QUIET && return 0
    printf "  %s\n" "$1"
}

step() {
    $QUIET && return 0
    printf "${CYAN}==>${NC} %s\n" "$1"
}

warn() {
    printf "  ${YELLOW}WARN${NC} %s\n" "$1" >&2
}

err() {
    printf "  ${RED}ERR${NC} %s\n" "$1" >&2
}

# --- Read plugin/.claude-plugin/plugin.json --------------------------------
read_plugin_json() {
    if [ ! -f "$PLUGIN_JSON" ]; then
        echo "unknown"
        return
    fi
    if command -v jq >/dev/null 2>&1; then
        jq -r '.version // "unknown"' "$PLUGIN_JSON" 2>/dev/null || echo "unknown"
    else
        sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$PLUGIN_JSON" | head -1
    fi
}

# --- Read packages/plugin-core/manifest.json -------------------------------
read_manifest_json() {
    if [ ! -f "$MANIFEST_JSON" ]; then
        echo "unknown"
        return
    fi
    if command -v jq >/dev/null 2>&1; then
        jq -r '.version // "unknown"' "$MANIFEST_JSON" 2>/dev/null || echo "unknown"
    else
        # The manifest has top-level "version": "X.Y.Z" — but also nested fields
        # (e.g. "schema_version" in some sub-objects). Match only the first
        # occurrence of a top-level "version" key.
        sed -n 's/^[[:space:]]*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$MANIFEST_JSON" | head -1
    fi
}

# --- Read latest GitHub Release tag ----------------------------------------
read_github_release() {
    # Prefer gh when installed AND authenticated. `gh release view` succeeds
    # quietly on auth; we treat any non-zero exit as "fall through".
    if command -v gh >/dev/null 2>&1; then
        if _tag="$(gh release view --json tagName -q '.tagName' 2>/dev/null)"; then
            if [ -n "$_tag" ]; then
                printf '%s\n' "${_tag#v}"
                return
            fi
        fi
    fi

    # Fallback: public API via curl. No auth needed for release metadata.
    if command -v curl >/dev/null 2>&1; then
        _api="https://api.github.com/repos/shakestzd/wipnote/releases/latest"
        _tag="$(curl -fsSL --max-time 8 "$_api" 2>/dev/null \
            | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' \
            | head -1 || true)"
        if [ -n "$_tag" ]; then
            printf '%s\n' "${_tag#v}"
            return
        fi
    fi

    echo "unknown"
}

# --- Compare ---------------------------------------------------------------
step "Reading version fields"

PLUGIN_VER="$(read_plugin_json)"
MANIFEST_VER="$(read_manifest_json)"
RELEASE_VER="$(read_github_release)"

# Buffer the table so we can print it on mismatch even in --quiet mode.
TABLE="$(printf "  %-44s %s\n" "SOURCE" "VERSION"
         printf "  %-44s %s\n" "------" "-------"
         printf "  %-44s %s\n" "plugin/.claude-plugin/plugin.json" "$PLUGIN_VER"
         printf "  %-44s %s\n" "packages/plugin-core/manifest.json" "$MANIFEST_VER"
         printf "  %-44s %s\n" "github: latest release tag" "$RELEASE_VER")"

# Drift detection. We require plugin.json and manifest.json to match
# unconditionally (manifest is the source of truth; plugin.json is generated
# from it and they should never drift). The release tag is allowed to lag
# behind unreleased local changes, but a mismatch is still surfaced as drift
# because that is exactly the bug class this script exists to catch.
local_mismatch=false
release_mismatch=false

if [ "$PLUGIN_VER" != "$MANIFEST_VER" ]; then
    local_mismatch=true
fi

if [ "$RELEASE_VER" = "unknown" ]; then
    : # release lookup unavailable; do not block on it alone
elif [ "$RELEASE_VER" != "$PLUGIN_VER" ] || [ "$RELEASE_VER" != "$MANIFEST_VER" ]; then
    release_mismatch=true
fi

if ! $local_mismatch && ! $release_mismatch; then
    if ! $QUIET; then
        printf '%s\n' "$TABLE"
        ok "All version sources agree on $PLUGIN_VER"
    fi
    exit 0
fi

# Mismatch path: always print regardless of --quiet.
printf "%s\n" "$TABLE" >&2
if $local_mismatch; then
    err "plugin.json ($PLUGIN_VER) does not match manifest.json ($MANIFEST_VER)"
    err "fix: edit packages/plugin-core/manifest.json then run 'wipnote plugin build-ports'"
fi
if $release_mismatch; then
    err "local version ($PLUGIN_VER / $MANIFEST_VER) does not match latest GitHub release ($RELEASE_VER)"
fi
exit 1
