#!/bin/bash
# Tag and push a new hazmat release.
#
# Usage:
#   ./scripts/release.sh 0.1.0        # tags v0.1.0, pushes to trigger release workflow
#   ./scripts/release.sh 0.1.0 --dry  # show what would happen without doing it
#
# What happens after push:
#   1. GitHub Actions builds darwin/arm64 + darwin/amd64 binaries
#   2. Creates a GitHub release with tarballs and checksums
#   3. Updates the Homebrew tap formula at dredozubov/homebrew-tap
#
# Prerequisites:
#   - TAP_TOKEN secret set in dredozubov/hazmat repo settings
#   - dredozubov/homebrew-tap repo exists

set -euo pipefail

VERSION="${1:?usage: release.sh <version> [--dry]}"
DRY="${2:-}"

# Normalize: strip leading v if user passes it
VERSION="${VERSION#v}"
TAG="v${VERSION}"

# Sanity checks
if ! [[ "${VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$ ]]; then
    echo "error: version '${VERSION}' doesn't look like semver (expected X.Y.Z)" >&2
    exit 1
fi

if git rev-parse "${TAG}" >/dev/null 2>&1; then
    echo "error: tag ${TAG} already exists" >&2
    exit 1
fi

# Check working tree is clean
if [ -n "$(git status --porcelain)" ]; then
    echo "error: working tree is not clean — commit or stash changes first" >&2
    exit 1
fi

# Verify builds and tests pass
echo "Running tests..."
(cd hazmat && go test ./...)
echo ""

echo "Release: ${TAG}"
echo "  Binary version:  ${TAG} (embedded via ldflags)"
echo "  GitHub release:  https://github.com/dredozubov/hazmat/releases/tag/${TAG}"
echo "  Homebrew:        brew install dredozubov/tap/hazmat"
echo ""

if [ "${DRY}" = "--dry" ]; then
    echo "[dry run] Would run:"
    echo "  git tag -a ${TAG} -m \"Release ${TAG}\""
    echo "  git push origin ${TAG}"
    exit 0
fi

read -rp "Tag and push ${TAG}? [y/N] " confirm
if [[ ! "${confirm}" =~ ^[Yy]$ ]]; then
    echo "Aborted."
    exit 0
fi

git tag -a "${TAG}" -m "Release ${TAG}"
git push origin "${TAG}"

echo ""
echo "Done. Watch the release workflow:"
echo "  https://github.com/dredozubov/hazmat/actions"
