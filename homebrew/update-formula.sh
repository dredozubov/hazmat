#!/bin/bash
# Updates the Homebrew formula with new version and checksums.
# Called by the release workflow or manually after tagging a release.
#
# Usage: ./update-formula.sh v0.1.0 <arm64-sha256> <amd64-sha256>
#
# To get checksums from a release:
#   curl -sL https://github.com/dredozubov/hazmat/releases/download/v0.1.0/checksums.txt

set -euo pipefail

VERSION="${1:?usage: update-formula.sh <version> <arm64-sha256> <amd64-sha256>}"
SHA_ARM64="${2:?missing arm64 sha256}"
SHA_AMD64="${3:?missing amd64 sha256}"

# Strip leading v for the formula version field
BARE_VERSION="${VERSION#v}"

FORMULA="$(dirname "$0")/hazmat.rb"

sed -i '' \
    -e "s/version \".*\"/version \"${BARE_VERSION}\"/" \
    -e "s/sha256 \"PLACEHOLDER_ARM64\"/sha256 \"${SHA_ARM64}\"/" \
    -e "s/sha256 \"PLACEHOLDER_AMD64\"/sha256 \"${SHA_AMD64}\"/" \
    -e "s|sha256 \"[a-f0-9]\{64\}\" # updated by release workflow|sha256 \"PLACEHOLDER\"|g" \
    "$FORMULA"

# Now set the real checksums (handles both fresh and update cases)
sed -i '' \
    -e "/arm64/s/sha256 \"PLACEHOLDER\"/sha256 \"${SHA_ARM64}\"/" \
    -e "/amd64/s/sha256 \"PLACEHOLDER\"/sha256 \"${SHA_AMD64}\"/" \
    "$FORMULA"

echo "Updated ${FORMULA} to ${VERSION}"
echo "  arm64: ${SHA_ARM64}"
echo "  amd64: ${SHA_AMD64}"
