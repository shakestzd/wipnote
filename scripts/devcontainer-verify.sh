#!/usr/bin/env bash
# devcontainer-verify.sh — Full verification suite for the wipnote devcontainer.
#
# Runs the complete quality gate: build, vet, and the full Go test suite.
# Also verifies the tools installed by the Dockerfile and post-create hook,
# then exercises a minimal smoke test of the wipnote CLI.
#
# Usage:
#   bash scripts/devcontainer-verify.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
cd "${REPO_ROOT}"

export PATH="${HOME}/.local/bin:${PATH}"
export WIPNOTE_CACHE_DIR="${WIPNOTE_CACHE_DIR:-/tmp/wipnote-devcontainer-cache}"
mkdir -p "${WIPNOTE_CACHE_DIR}"

section() {
    printf '\n==> %s\n' "$1"
}

section "go build ./..."
go build ./...

section "go vet ./..."
go vet ./...

section "go test ./... -count=1"
go test ./... -count=1

section "required devcontainer tools"
for tool in wipnote claude codex gemini copilot roborev uv mkdocs oh-my-posh ttyd bwrap tmux rg fd jq sqlite3 shellcheck zsh direnv; do
    if ! command -v "$tool" >/dev/null 2>&1; then
        echo "$tool is not on PATH. Rebuild the devcontainer or rerun .devcontainer/post-create.sh." >&2
        exit 1
    fi
done

section "wipnote binary smoke test"
wipnote version
wipnote help --compact | head -20 || true
roborev version
roborev status || roborev daemon start

section "wipnote serve smoke test"
wipnote serve --port 8081 >/tmp/wipnote-verify-serve.log 2>&1 &
SERVE_PID=$!
trap 'kill "${SERVE_PID}" 2>/dev/null || true' EXIT
sleep 1
if curl --silent --fail --max-time 3 http://127.0.0.1:8081/ >/dev/null; then
    echo "dashboard reachable on port 8081"
else
    echo "dashboard smoke test FAILED — see /tmp/wipnote-verify-serve.log" >&2
    cat /tmp/wipnote-verify-serve.log >&2 || true
    exit 1
fi
kill "${SERVE_PID}" 2>/dev/null || true
trap - EXIT

echo
echo "Devcontainer verification complete — all checks passed."
