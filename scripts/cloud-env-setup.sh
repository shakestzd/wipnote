#!/usr/bin/env bash
# cloud-env-setup.sh: install a pinned wipnote for a Claude cloud environment.
#
# A Claude project thread (or any Claude Code cloud session) runs in a cloud
# environment whose setup script installs tools before Claude starts. Point
# that setup script at this file, for example:
#
#   bash scripts/cloud-env-setup.sh
#
# It installs the wipnote binary and its bundled plugin trees from the
# project's GitHub release assets (the same assets scripts/install.sh uses),
# verifies the tarball against the release's checksums file, and records the
# installed version. A second run with the same pin downloads nothing.
#
# It never blocks the environment from starting: on any failure it prints
# what went wrong and exits 0, and `wipnote doctor --project-thread` then
# reports the missing binary. Set WIPNOTE_SETUP_STRICT=1 to exit non-zero.
# No secrets are read or needed; the release assets are public.
#
# Environment variables:
#   WIPNOTE_VERSION            version to install (default: WIPNOTE_PIN below;
#                              "latest" is refused, the install is pinned)
#   WIPNOTE_BIN_DIR            binary directory (default: $HOME/.local/bin)
#   WIPNOTE_SHARE_DIR          plugin trees (default: $HOME/.local/share/wipnote)
#   WIPNOTE_SETUP_STRICT       1: exit non-zero when the install fails
#   WIPNOTE_RELEASE_BASE_URL   release download root (tests point it at a
#                              local file:// release)
#   WIPNOTE_CURL_CONNECT_TIMEOUT, WIPNOTE_CURL_MAX_TIME
#                              curl --connect-timeout and --max-time in seconds
#                              (default 15 and 300) so a stalled host cannot
#                              hang the environment start

set -uo pipefail

# The pinned release. Move it deliberately, to a version whose assets exist.
WIPNOTE_PIN="0.85.0"

tmp="" # the download folder, removed on exit
RELEASE_BASE_DEFAULT="https://github.com/shakestzd/wipnote/releases/download"

info() { printf '==> %s\n' "$*"; }

# fail reports why nothing was installed and exits: 0 by default so the
# cloud environment still starts, 1 with WIPNOTE_SETUP_STRICT=1.
fail() {
  printf 'wipnote cloud setup: %s\n' "$*" >&2
  printf "wipnote was not installed; run 'wipnote doctor --project-thread' in the thread to see what is missing.\n" >&2
  if [[ "${WIPNOTE_SETUP_STRICT:-0}" == "1" ]]; then
    exit 1
  fi
  exit 0
}

# platform prints <os>_<arch> for the release asset name.
platform() {
  local os arch
  case "$(uname -s)" in
    Darwin) os="darwin" ;;
    Linux) os="linux" ;;
    *) return 1 ;;
  esac
  case "$(uname -m)" in
    x86_64 | amd64) arch="amd64" ;;
    arm64 | aarch64) arch="arm64" ;;
    *) return 1 ;;
  esac
  printf '%s_%s\n' "$os" "$arch"
}

# sha256_of prints the SHA-256 of a file.
sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    return 1
  fi
}

# verify_checksum checks tarball $1 against checksums file $2.
verify_checksum() {
  local tarball="$1" sums="$2" name expected actual
  name="$(basename "$tarball")"
  expected="$(awk -v n="$name" '$2 == n {print $1}' "$sums")"
  [[ -n "$expected" ]] || fail "no checksum for ${name} in $(basename "$sums")"
  actual="$(sha256_of "$tarball")" || fail "neither sha256sum nor shasum is installed; cannot verify ${name}"
  [[ "$expected" == "$actual" ]] || fail "checksum mismatch for ${name} (expected ${expected}, got ${actual})"
}

# download fetches $1 to $2.
download() {
  curl -fsSL --retry 2 --connect-timeout "${WIPNOTE_CURL_CONNECT_TIMEOUT:-15}" \
    --max-time "${WIPNOTE_CURL_MAX_TIME:-300}" "$1" -o "$2" || fail "download failed: $1"
}

# install_trees moves the binary and plugin trees from $1 into place; the
# binary is renamed over the old one so a reader never sees half a file.
install_trees() {
  local src="$1" tree
  [[ -f "${src}/wipnote" ]] || fail "the release tarball has no wipnote binary"
  mkdir -p "$BIN_DIR" "$SHARE_DIR" || fail "cannot create ${BIN_DIR} or ${SHARE_DIR}"
  if ! { cp "${src}/wipnote" "${BIN_DIR}/.wipnote.new" && chmod +x "${BIN_DIR}/.wipnote.new" &&
    mv -f "${BIN_DIR}/.wipnote.new" "${BIN_DIR}/wipnote"; }; then
    rm -f "${BIN_DIR}/.wipnote.new"
    fail "cannot install into ${BIN_DIR}"
  fi
  for tree in plugin codex-marketplace antigravity-extension mods; do
    if [[ -d "${src}/${tree}" ]]; then
      if ! { rm -rf "${SHARE_DIR:?}/${tree}" && mv "${src}/${tree}" "${SHARE_DIR}/${tree}"; }; then
        fail "cannot install ${tree} into ${SHARE_DIR}"
      fi
    fi
  done
}

# already_installed reports whether the pinned version is fully in place:
# the stamp names it, the binary reports it, and the plugin tree exists.
# The version call is bounded: it may try an update check.
already_installed() {
  local out
  [[ -x "${BIN_DIR}/wipnote" && -d "${SHARE_DIR}/plugin" ]] || return 1
  [[ "$(cat "$1" 2>/dev/null)" == "$VERSION" ]] || return 1
  if command -v timeout >/dev/null 2>&1; then
    out="$(timeout 20 "${BIN_DIR}/wipnote" version 2>/dev/null | head -n 1)"
  else
    out="$("${BIN_DIR}/wipnote" version 2>/dev/null | head -n 1)"
  fi
  [[ "$out" == "wipnote ${VERSION} "* || "$out" == "wipnote v${VERSION} "* ]]
}

main() {
  VERSION="${WIPNOTE_VERSION:-$WIPNOTE_PIN}"
  VERSION="${VERSION#v}"
  [[ "$VERSION" != "latest" ]] || fail "WIPNOTE_VERSION=latest is refused: the cloud environment installs a pinned version"
  BIN_DIR="${WIPNOTE_BIN_DIR:-$HOME/.local/bin}"
  SHARE_DIR="${WIPNOTE_SHARE_DIR:-$HOME/.local/share/wipnote}"
  local stamp="${SHARE_DIR}/.cloud-env-version"
  if already_installed "$stamp"; then
    info "wipnote v${VERSION} already installed in ${BIN_DIR}"
    exit 0
  fi
  local plat base tarball sums
  plat="$(platform)" || fail "no release asset for $(uname -s) $(uname -m)"
  tmp="$(mktemp -d)" || fail "mktemp failed"
  trap '[[ -n "$tmp" ]] && rm -rf "$tmp"' EXIT
  base="${WIPNOTE_RELEASE_BASE_URL:-$RELEASE_BASE_DEFAULT}/v${VERSION}"
  tarball="${tmp}/wipnote_${VERSION}_${plat}.tar.gz"
  sums="${tmp}/wipnote_${VERSION}_checksums.txt"
  info "Installing wipnote v${VERSION} (${plat})"
  download "${base}/$(basename "$tarball")" "$tarball"
  download "${base}/$(basename "$sums")" "$sums"
  verify_checksum "$tarball" "$sums"
  if ! { mkdir -p "${tmp}/x" && tar -xzf "$tarball" -C "${tmp}/x"; }; then
    fail "cannot extract $(basename "$tarball")"
  fi
  install_trees "${tmp}/x"
  printf '%s\n' "$VERSION" >"$stamp" || fail "cannot write ${stamp}"
  info "Installed wipnote v${VERSION} into ${BIN_DIR}"
  case ":${PATH}:" in
    *":${BIN_DIR}:"*) ;;
    *) info "${BIN_DIR} is not on PATH: add it, or set WIPNOTE_BIN_DIR to a directory that is" ;;
  esac
}

main "$@"
