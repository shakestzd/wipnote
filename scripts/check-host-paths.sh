#!/usr/bin/env bash
# check-host-paths.sh — scan committed artifacts for host-local absolute paths.
#
# PATTERNS DETECTED:
#   /Users/<anything>/          — macOS home directories
#   /home/<user>/               — Linux home directories (except /home/runner/ for CI)
#   /workspaces/<username>/     — GitHub Codespaces workspace paths
#   /private/var/folders/       — macOS temp directories
#
# ALLOWLISTS:
#   scripts/host-paths-allowlist.txt (literal paths, repo-relative)
#     Files listed here are skipped entirely. Not git-tracked (*.txt gitignore).
#   scripts/host-paths-allowlist-patterns.conf (glob patterns, git-tracked)
#     Shell glob patterns (fnmatch) matched against repo-relative paths.
#     Supports wildcards: * (within one path component), ** (any depth).
#     Lines beginning with '#' and blank lines are ignored.
#
# EXIT CODES:
#   0  — no violations found (prints "OK — N files scanned")
#   1  — one or more violations found (prints "file:line: <matched-path>")
#
# USAGE:
#   scripts/check-host-paths.sh                    # scan .wipnote/ and .claude/ (default)
#   scripts/check-host-paths.sh --staged           # scan only git-staged files (pre-commit)
#   scripts/check-host-paths.sh --full             # scan entire git-tracked repo (CI)
#   scripts/check-host-paths.sh path/to/file       # scan specific file(s)
#
set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
ALLOWLIST_FILE="$REPO_ROOT/scripts/host-paths-allowlist.txt"
PATTERNS_FILE="$REPO_ROOT/scripts/host-paths-allowlist-patterns.conf"
STAGED_ONLY=0
FULL_SCAN=0
EXPLICIT_FILES=()

# Parse arguments
for arg in "$@"; do
    if [[ "$arg" == "--staged" ]]; then
        STAGED_ONLY=1
    elif [[ "$arg" == "--full" ]]; then
        FULL_SCAN=1
    else
        EXPLICIT_FILES+=("$arg")
    fi
done

# Build literal allowlist set (relative paths from repo root)
declare -A ALLOWLIST
if [[ -f "$ALLOWLIST_FILE" ]]; then
    while IFS= read -r line; do
        # Skip comments and blank lines
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        [[ -z "${line// }" ]] && continue
        ALLOWLIST["$line"]=1
    done < "$ALLOWLIST_FILE"
fi

# Load glob patterns from the patterns conf file
declare -a ALLOW_PATTERNS
if [[ -f "$PATTERNS_FILE" ]]; then
    while IFS= read -r line; do
        # Skip comments and blank lines
        [[ "$line" =~ ^[[:space:]]*# ]] && continue
        [[ -z "${line// }" ]] && continue
        ALLOW_PATTERNS+=("$line")
    done < "$PATTERNS_FILE"
fi

# match_glob <pattern> <path>
# Returns 0 if <path> matches the shell glob <pattern>, 1 otherwise.
# Supports ** to match across path separators.
match_glob() {
    local pattern="$1"
    local path="$2"

    # Use bash's [[ == ]] glob matching for simple patterns
    # For ** patterns, we use a recursive approach via extglob
    shopt -s extglob globstar 2>/dev/null || true

    # Convert ** glob to a pattern bash can match against slashes
    # bash [[ == ]] with globstar handles ** when globstar is on.
    # SC2053: unquoted RHS is intentional — we want glob expansion.
    # shellcheck disable=SC2053
    if [[ "$path" == $pattern ]]; then
        return 0
    fi
    return 1
}

# is_allowlisted <rel_path>
# Returns 0 if the relative path is in the literal allowlist or matches a glob pattern.
is_allowlisted() {
    local rel="$1"

    # Check literal allowlist
    if [[ -n "${ALLOWLIST[$rel]+x}" ]]; then
        return 0
    fi

    # Check glob patterns
    local pat
    for pat in "${ALLOW_PATTERNS[@]}"; do
        if match_glob "$pat" "$rel"; then
            return 0
        fi
    done
    return 1
}

# Collect files to scan
declare -a FILES_TO_SCAN

if [[ ${#EXPLICIT_FILES[@]} -gt 0 ]]; then
    FILES_TO_SCAN=("${EXPLICIT_FILES[@]}")
elif [[ "$STAGED_ONLY" -eq 1 ]]; then
    while IFS= read -r f; do
        # Only include files matching our scope
        if [[ "$f" == .wipnote/* || "$f" == .claude/* ]]; then
            # Skip files that are intentionally machine-specific (matches full-scan exclusions)
            [[ "$(basename "$f")" == "wipnote.db" ]] && continue
            [[ "$(basename "$f")" == "settings.local.json" ]] && continue
            FILES_TO_SCAN+=("$REPO_ROOT/$f")
        fi
    done < <(git -C "$REPO_ROOT" diff --cached --name-only --diff-filter=ACMR 2>/dev/null || true)
elif [[ "$FULL_SCAN" -eq 1 ]]; then
    # Full scan: entire git-tracked tree
    while IFS= read -r f; do
        # Skip binary/build artifacts that can't contain text paths
        [[ "$(basename "$f")" == "wipnote.db" ]] && continue
        [[ "$(basename "$f")" == "wipnote.db-wal" ]] && continue
        [[ "$(basename "$f")" == "wipnote.db-shm" ]] && continue
        FILES_TO_SCAN+=("$REPO_ROOT/$f")
    done < <(git -C "$REPO_ROOT" ls-files 2>/dev/null || true)
else
    # Default scan: .wipnote/** and .claude/**
    while IFS= read -r f; do
        FILES_TO_SCAN+=("$f")
    done < <(find "$REPO_ROOT/.wipnote" "$REPO_ROOT/.claude" \
        -type f \
        ! -name "wipnote.db" \
        ! -name "settings.local.json" \
        2>/dev/null || true)
fi

# Filter out allowlisted files and scan
VIOLATIONS=0
SCANNED=0

for abs_file in "${FILES_TO_SCAN[@]}"; do
    # Skip if file doesn't exist (e.g. deleted staged file)
    [[ -f "$abs_file" ]] || continue

    # Compute relative path for allowlist lookup
    rel_file="${abs_file#"$REPO_ROOT"/}"

    # Skip allowlisted files (literal or glob pattern)
    if is_allowlisted "$rel_file"; then
        continue
    fi

    SCANNED=$((SCANNED + 1))

    # Scan for host-local patterns using perl (supports lookahead for /home/runner/ exclusion)
    while IFS= read -r hit; do
        echo "$hit"
        VIOLATIONS=$((VIOLATIONS + 1))
    done < <(perl -ne '
        while (m{(/Users/[^/\s]+/|/home/(?!runner/)[^/\s]+/|/workspaces/[^/\s]+/|/private/var/folders/)}g) {
            print $ARGV . ":" . $. . ": " . $1 . "\n";
        }
    ' "$abs_file" 2>/dev/null || true)
done

if [[ "$VIOLATIONS" -gt 0 ]]; then
    echo ""
    echo "FAIL: $VIOLATIONS host-local path violation(s) found in $SCANNED file(s) scanned."
    echo "      These paths must not be committed — they are machine-specific."
    echo "      To allowlist a file, add its repo-relative path to scripts/host-paths-allowlist.txt"
    echo "      or add a glob pattern to scripts/host-paths-allowlist-patterns.conf"
    exit 1
fi

echo "OK — $SCANNED file(s) scanned, no host-local path violations found."
