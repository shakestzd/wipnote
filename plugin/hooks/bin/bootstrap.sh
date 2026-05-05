#!/bin/sh
# bootstrap.sh - Lightweight bootstrap for htmlgraph Go binary.
#
# In the distributed plugin, this script IS named "htmlgraph".
# On first run it downloads the correct platform binary from GitHub Releases,
# then exec's into it.  Subsequent runs simply exec the cached binary after
# a fast (~1 ms) version check.
#
# Install location: ~/.local/bin/htmlgraph — shared by plugin bootstrap,
# curl install script, Homebrew, and setup-cli.  Metadata (version tracking)
# lives at ~/.local/share/htmlgraph/.
#
# Design constraints:
#   - POSIX sh (no bash-isms)
#   - No dependencies beyond curl/tar (standard on macOS + Linux)
#   - Never blocks Claude Code: on error, prints {} to stdout and exits 0
#   - Stdin passthrough via exec (CloudEvent JSON piped by hooks)

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# Install the binary to ~/.local/bin so it's on PATH for both plugin and
# standalone users.  Metadata (version file) lives in ~/.local/share/htmlgraph.
INSTALL_DIR="${HOME}/.local/bin"
BINARY="${INSTALL_DIR}/htmlgraph"
META_DIR="${HOME}/.local/share/htmlgraph"
VERSION_FILE="${META_DIR}/.binary-version"

# ---------------------------------------------------------------------------
# Resolve expected version from plugin.json
# ---------------------------------------------------------------------------
resolve_version() {
    plugin_json=""

    # CLAUDE_PLUGIN_ROOT is set by Claude Code at hook invocation time.
    if [ -n "${CLAUDE_PLUGIN_ROOT:-}" ]; then
        plugin_json="${CLAUDE_PLUGIN_ROOT}/.claude-plugin/plugin.json"
    fi

    # Fallback: walk up from script dir (hooks/bin -> hooks -> plugin root)
    if [ -z "${plugin_json}" ] || [ ! -f "${plugin_json}" ]; then
        plugin_json="${SCRIPT_DIR}/../../.claude-plugin/plugin.json"
    fi

    if [ ! -f "${plugin_json}" ]; then
        # Third fallback: explicit env var (for CI / pinning).
        if [ -n "${ERINN_VERSION:-}" ]; then
            echo "${ERINN_VERSION}"
            return
        fi

        # Fourth fallback: query GitHub releases API for the latest tag.
        # Wrapped in a 5-second timeout; silently ignored if unavailable.
        _api_url="https://api.github.com/repos/shakestzd/htmlgraph/releases/latest"
        _tag=""
        if command -v curl >/dev/null 2>&1; then
            _tag="$(curl -fsSL --max-time 5 "${_api_url}" 2>/dev/null \
                | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"v\{0,1\}\([^"]*\)".*/\1/p' \
                | head -1 || true)"
        elif command -v wget >/dev/null 2>&1; then
            _tag="$(wget -q -T 5 -O - "${_api_url}" 2>/dev/null \
                | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"v\{0,1\}\([^"]*\)".*/\1/p' \
                | head -1 || true)"
        fi
        echo "${_tag:-}"
        return
    fi

    # Extract "version": "X.Y.Z" without jq — portable sed.
    sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${plugin_json}" | head -1
}

# ---------------------------------------------------------------------------
# Detect OS and architecture, map to goreleaser archive names
# ---------------------------------------------------------------------------
detect_platform() {
    _os="$(uname -s | tr '[:upper:]' '[:lower:]')"
    _arch="$(uname -m)"

    case "${_os}" in
        darwin) PLATFORM_OS="darwin" ;;
        linux)  PLATFORM_OS="linux"  ;;
        *)
            log_err "Unsupported OS: ${_os}"
            bail
            ;;
    esac

    case "${_arch}" in
        x86_64|amd64)   PLATFORM_ARCH="amd64" ;;
        arm64|aarch64)  PLATFORM_ARCH="arm64" ;;
        *)
            log_err "Unsupported architecture: ${_arch}"
            bail
            ;;
    esac
}

# ---------------------------------------------------------------------------
# Download binary from GitHub Releases
# ---------------------------------------------------------------------------
download_binary() {
    _version="$1"
    _archive="htmlgraph-${PLATFORM_OS}-${PLATFORM_ARCH}.tar.gz"
    _url="https://github.com/shakestzd/htmlgraph/releases/download/v${_version}/${_archive}"

    log_err "Downloading binary v${_version} for ${PLATFORM_OS}/${PLATFORM_ARCH}..."

    mkdir -p "${INSTALL_DIR}"
    mkdir -p "${META_DIR}"
    _tmpdir="$(mktemp -d)"
    _tarball="${_tmpdir}/htmlgraph.tar.gz"

    # Try curl first (available on macOS + most Linux), fall back to wget.
    if command -v curl >/dev/null 2>&1; then
        if ! curl -fsSL -o "${_tarball}" "${_url}" 2>/dev/null; then
            rm -rf "${_tmpdir}"
            log_err "Download failed (curl): ${_url}"
            bail
        fi
    elif command -v wget >/dev/null 2>&1; then
        if ! wget -q -O "${_tarball}" "${_url}" 2>/dev/null; then
            rm -rf "${_tmpdir}"
            log_err "Download failed (wget): ${_url}"
            bail
        fi
    else
        rm -rf "${_tmpdir}"
        log_err "Neither curl nor wget found. Cannot download binary."
        bail
    fi

    # Extract — archive contains binary named "htmlgraph-${os}-${arch}"
    if ! tar xzf "${_tarball}" -C "${_tmpdir}" 2>/dev/null; then
        rm -rf "${_tmpdir}"
        log_err "Failed to extract archive."
        bail
    fi

    # Move extracted binary into place (archive names it htmlgraph-${os}-${arch})
    _extracted="${_tmpdir}/htmlgraph-${PLATFORM_OS}-${PLATFORM_ARCH}"
    if [ -f "${_extracted}" ]; then
        mv "${_extracted}" "${BINARY}"
    elif [ -f "${_tmpdir}/htmlgraph" ]; then
        mv "${_tmpdir}/htmlgraph" "${BINARY}"
    else
        rm -rf "${_tmpdir}"
        log_err "Binary not found in archive."
        bail
    fi

    chmod +x "${BINARY}"
    echo "${_version}" > "${VERSION_FILE}"

    rm -rf "${_tmpdir}"
    log_err "Installed htmlgraph v${_version} to ${BINARY}."
}

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
log_err() {
    echo "[htmlgraph] $*" >&2
}

# bail outputs {} to stdout (so Claude Code sees valid JSON) and exits 0.
bail() {
    echo "{}"
    exit 0
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------
EXPECTED_VERSION="$(resolve_version)"

if [ -z "${EXPECTED_VERSION}" ]; then
    log_err "Could not determine expected version from plugin.json."
    bail
fi

# ---------------------------------------------------------------------------
# Prefer PATH-installed binary if version matches (Homebrew, go install, curl)
# ---------------------------------------------------------------------------
PATH_BINARY="$(command -v htmlgraph 2>/dev/null || true)"
if [ -n "${PATH_BINARY}" ]; then
    # Guard: don't exec ourselves (bootstrap is also named "htmlgraph")
    # Resolve real path of found binary
    _real_path="$(cd "$(dirname "${PATH_BINARY}")" && pwd)/$(basename "${PATH_BINARY}")"
    _self_path="${SCRIPT_DIR}/$(basename "$0")"

    if [ "${_real_path}" != "${_self_path}" ]; then
        # Check version matches expected
        _path_ver="$("${PATH_BINARY}" version 2>/dev/null | grep -o '[0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*' | head -1 || true)"
        if [ "${_path_ver}" = "${EXPECTED_VERSION}" ]; then
            exec "${PATH_BINARY}" "$@"
        fi
    fi
fi

# Fast path: binary exists and version matches.
if [ -x "${BINARY}" ] && [ -f "${VERSION_FILE}" ]; then
    CACHED_VERSION="$(cat "${VERSION_FILE}" 2>/dev/null || echo "")"
    if [ "${CACHED_VERSION}" = "${EXPECTED_VERSION}" ]; then
        exec "${BINARY}" "$@"
    fi
fi

# Slow path: download or update.
detect_platform
download_binary "${EXPECTED_VERSION}"

# Now exec the freshly downloaded binary.
if [ -x "${BINARY}" ]; then
    exec "${BINARY}" "$@"
fi

# Should not reach here, but handle gracefully.
log_err "Binary not executable after download."
bail
