#!/usr/bin/env sh
# update-formula.sh — Update wipnote.rb with correct version and SHA256 checksums.
#
# Usage: ./update-formula.sh VERSION
# Example: ./update-formula.sh 0.35.0
#
# Requires: curl, awk, sed

set -eu

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
FORMULA="$SCRIPT_DIR/wipnote.rb"

if [ $# -ne 1 ]; then
  echo "Usage: $0 VERSION" >&2
  echo "Example: $0 0.35.0" >&2
  exit 1
fi

VERSION="$1"
CHECKSUMS_URL="https://github.com/shakestzd/wipnote/releases/download/v${VERSION}/wipnote_${VERSION}_checksums.txt"

echo "Fetching checksums for v${VERSION}..."
CHECKSUMS=$(curl -fsSL "$CHECKSUMS_URL") || {
  echo "Error: failed to download checksums from $CHECKSUMS_URL" >&2
  exit 1
}

get_sha256() {
  echo "$CHECKSUMS" | awk -v file="$1" '$2 == file { print $1 }'
}

SHA256_DARWIN_ARM64=$(get_sha256 "wipnote_${VERSION}_darwin_arm64.tar.gz")
SHA256_DARWIN_AMD64=$(get_sha256 "wipnote_${VERSION}_darwin_amd64.tar.gz")
SHA256_LINUX_ARM64=$(get_sha256  "wipnote_${VERSION}_linux_arm64.tar.gz")
SHA256_LINUX_AMD64=$(get_sha256  "wipnote_${VERSION}_linux_amd64.tar.gz")

for var in SHA256_DARWIN_ARM64 SHA256_DARWIN_AMD64 SHA256_LINUX_ARM64 SHA256_LINUX_AMD64; do
  eval val=\$$var
  if [ -z "$val" ]; then
    echo "Error: could not find checksum for $var in checksums file" >&2
    echo "Checksums file contents:" >&2
    echo "$CHECKSUMS" >&2
    exit 1
  fi
done

echo "Updating $FORMULA..."

# Detect sed in-place flag (BSD vs GNU)
if sed --version >/dev/null 2>&1; then
  SED_INPLACE="sed -i"
else
  SED_INPLACE="sed -i ''"
fi

# Detect current version in formula
CURRENT_VERSION=$(awk '/^  version / { gsub(/"/, "", $2); print $2 }' "$FORMULA")

$SED_INPLACE "s|version \"${CURRENT_VERSION}\"|version \"${VERSION}\"|" "$FORMULA"
$SED_INPLACE "s|SHA256_DARWIN_ARM64|${SHA256_DARWIN_ARM64}|g" "$FORMULA"
$SED_INPLACE "s|SHA256_DARWIN_AMD64|${SHA256_DARWIN_AMD64}|g" "$FORMULA"
$SED_INPLACE "s|SHA256_LINUX_ARM64|${SHA256_LINUX_ARM64}|g"   "$FORMULA"
$SED_INPLACE "s|SHA256_LINUX_AMD64|${SHA256_LINUX_AMD64}|g"   "$FORMULA"

echo "Done. Updated $FORMULA to v${VERSION}."
echo ""
echo "SHA256 values written:"
echo "  darwin_arm64: ${SHA256_DARWIN_ARM64}"
echo "  darwin_amd64: ${SHA256_DARWIN_AMD64}"
echo "  linux_arm64:  ${SHA256_LINUX_ARM64}"
echo "  linux_amd64:  ${SHA256_LINUX_AMD64}"
echo ""
echo "Next: commit and push wipnote.rb to shakestzd/homebrew-wipnote"
