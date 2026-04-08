#!/bin/bash
# Tag and push a new hazmat release.
#
# Usage:
#   ./scripts/release.sh              # auto-determine version via AI changelog review
#   ./scripts/release.sh 0.5.0        # explicit version
#   ./scripts/release.sh 0.5.0 --dry  # show what would happen without doing it
#   ./scripts/release.sh --dry        # auto-determine version, dry run
#
# What happens:
#   1. hazmat claude -p reviews changes since the last tag and updates CHANGELOG.md
#   2. You review and confirm the changelog + version
#   3. The changelog commit is created, tagged, and pushed
#   4. GitHub Actions builds darwin/arm64 + darwin/amd64 binaries
#   5. Creates a GitHub release with tarballs and checksums
#   6. Updates the Homebrew tap formula at dredozubov/homebrew-tap
#
# Prerequisites:
#   - hazmat init has been run (for hazmat claude -p)
#   - TAP_TOKEN secret set in dredozubov/hazmat repo settings
#   - dredozubov/homebrew-tap repo exists

set -euo pipefail

# Parse arguments: [version] [--dry]
VERSION=""
DRY=""
for arg in "$@"; do
    if [ "$arg" = "--dry" ]; then
        DRY="--dry"
    elif [ -z "$VERSION" ]; then
        VERSION="$arg"
    fi
done

# Find the latest tag for diffing
PREV_TAG="$(git tag --sort=-v:refname | head -1)"
if [ -z "${PREV_TAG}" ]; then
    echo "error: no previous tags found" >&2
    exit 1
fi

# Check tracked files are clean (untracked files are fine)
if [ -n "$(git status --porcelain -uno)" ]; then
    echo "error: working tree has uncommitted changes — commit or stash first" >&2
    git status --porcelain -uno >&2
    exit 1
fi

# Verify builds and tests pass
echo "Running tests..."
(cd hazmat && go test ./...)
echo ""

# Gather changes since last tag
CHANGES="$(git log --format='- %s' "${PREV_TAG}..HEAD")"
if [ -z "${CHANGES}" ]; then
    echo "error: no changes since ${PREV_TAG}" >&2
    exit 1
fi

echo "Changes since ${PREV_TAG}:"
echo "${CHANGES}"
echo ""

# Use hazmat claude to update CHANGELOG.md and determine version
PROMPT="$(cat <<PROMPT_EOF
You are updating CHANGELOG.md for a new release of Hazmat.

Previous version tag: ${PREV_TAG}
Explicit version requested: ${VERSION:-"(none — determine from changes)"}

Commits since last tag:
${CHANGES}

Rules:
1. Read the current CHANGELOG.md
2. If no explicit version was given, determine the next semver version:
   - PATCH (0.x.Y+1) for bug fixes and test-only changes
   - MINOR (0.X+1.0) for new features or breaking changes
3. Move the [Unreleased] section contents into a new version section with today's date
4. Categorize commits under Added/Changed/Fixed/Tests as appropriate
5. Update the comparison links at the bottom
6. Keep [Unreleased] as an empty section at the top
7. Write the updated CHANGELOG.md

Only edit CHANGELOG.md. Do not create or modify any other files.
Print the chosen version number as the LAST line of your response, formatted exactly as: VERSION=X.Y.Z
PROMPT_EOF
)"

echo "Asking hazmat claude to update CHANGELOG.md..."
echo ""
CLAUDE_OUTPUT="$(hazmat claude --no-backup -p "${PROMPT}" 2>&1)" || {
    echo "error: hazmat claude failed" >&2
    echo "${CLAUDE_OUTPUT}" >&2
    exit 1
}

echo "${CLAUDE_OUTPUT}"
echo ""

# Extract version from claude output if not explicitly given
if [ -z "${VERSION}" ]; then
    VERSION="$(echo "${CLAUDE_OUTPUT}" | grep -oE 'VERSION=[0-9]+\.[0-9]+\.[0-9]+' | tail -1 | cut -d= -f2)"
    if [ -z "${VERSION}" ]; then
        echo "error: could not determine version from claude output" >&2
        exit 1
    fi
fi

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

echo "Release: ${TAG}"
echo "  Previous tag:    ${PREV_TAG}"
echo "  Binary version:  ${TAG} (embedded via ldflags)"
echo "  GitHub release:  https://github.com/dredozubov/hazmat/releases/tag/${TAG}"
echo "  Homebrew:        brew install dredozubov/tap/hazmat"
echo ""

# Show the changelog diff for review
echo "CHANGELOG.md diff:"
git diff CHANGELOG.md
echo ""

if [ "${DRY}" = "--dry" ]; then
    echo "[dry run] Would run:"
    echo "  git add CHANGELOG.md"
    echo "  git commit -m \"docs: update CHANGELOG for ${TAG}\""
    echo "  git tag -a ${TAG} -m \"Release ${TAG}\""
    echo "  git push origin master ${TAG}"
    git checkout CHANGELOG.md
    exit 0
fi

read -rp "Commit changelog, tag ${TAG}, and push? [y/N] " confirm
if [[ ! "${confirm}" =~ ^[Yy]$ ]]; then
    echo "Restoring CHANGELOG.md..."
    git checkout CHANGELOG.md
    echo "Aborted."
    exit 0
fi

git add CHANGELOG.md
git commit -m "docs: update CHANGELOG for ${TAG}"
git tag -a "${TAG}" -m "Release ${TAG}"
git push origin master "${TAG}"

echo ""
echo "Done. Watch the release workflow:"
echo "  https://github.com/dredozubov/hazmat/actions"
