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

PROMPT_FILE=""
RELEASE_PLAN_FILE=""
RESTORE_CHANGELOG_ON_EXIT=0

restore_changelog() {
    git restore --staged --worktree --source=HEAD -- CHANGELOG.md
}

discard_changelog_draft() {
    if [ "${RESTORE_CHANGELOG_ON_EXIT}" = "1" ]; then
        restore_changelog
        RESTORE_CHANGELOG_ON_EXIT=0
    fi
}

cleanup_release_run() {
    local exit_code=$?

    if [ "${RESTORE_CHANGELOG_ON_EXIT}" = "1" ] && [ "${exit_code}" -ne 0 ]; then
        restore_changelog >/dev/null 2>&1 || true
    fi
    rm -f "${PROMPT_FILE}" "${RELEASE_PLAN_FILE}"
}

trap cleanup_release_run EXIT

resolve_editor() {
    local editor=""
    if editor="$(git var GIT_EDITOR 2>/dev/null)"; then
        :
    elif [ -n "${VISUAL:-}" ]; then
        editor="${VISUAL}"
    elif [ -n "${EDITOR:-}" ]; then
        editor="${EDITOR}"
    else
        editor="vi"
    fi

    printf '%s' "${editor}"
}

run_editor() {
    local editor_cmd="$1"
    shift

    EDITOR_CMD="${editor_cmd}" /bin/sh -c '
        eval "set -- ${EDITOR_CMD} \"\$@\""
        exec "$@"
    ' sh "$@"
}

extract_version_from_plan() {
    local plan_file="$1"

    sed -nE 's/^[[:space:]]*VERSION[[:space:]]*=[[:space:]]*([^[:space:]#]+).*$/\1/p' "${plan_file}" | tail -1
}

validate_release_plan() {
    local version="$1"
    local prev_tag="$2"
    local errors=()
    local tag=""
    local version_header_pattern=""
    local unreleased_link_pattern=""
    local release_link_pattern=""

    version="${version#v}"
    if ! [[ "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$ ]]; then
        errors+=("VERSION must look like semver (expected X.Y.Z or X.Y.Z-suffix).")
    fi

    tag="v${version}"
    if git rev-parse "${tag}" >/dev/null 2>&1; then
        errors+=("Tag ${tag} already exists.")
    fi

    version_header_pattern="^## \\[${version//./\\.}\\] - [0-9]{4}-[0-9]{2}-[0-9]{2}$"
    if ! grep -Eq "${version_header_pattern}" CHANGELOG.md; then
        errors+=("CHANGELOG.md must contain a section header like '## [${version}] - YYYY-MM-DD'.")
    fi

    unreleased_link_pattern="^\\[Unreleased\\]: .*compare/${tag//./\\.}\\.\\.\\.HEAD$"
    if ! grep -Eq "${unreleased_link_pattern}" CHANGELOG.md; then
        errors+=("CHANGELOG.md must update the [Unreleased] link to compare ${tag}...HEAD.")
    fi

    release_link_pattern="^\\[${version//./\\.}\\]: .*compare/${prev_tag//./\\.}\\.\\.\\.${tag//./\\.}$"
    if ! grep -Eq "${release_link_pattern}" CHANGELOG.md; then
        errors+=("CHANGELOG.md must include a [${version}] link comparing ${prev_tag}...${tag}.")
    fi

    if [ "${#errors[@]}" -gt 0 ]; then
        printf '%s\n' "${errors[@]}"
        return 1
    fi

    return 0
}

write_release_plan() {
    local plan_file="$1"
    local version="$2"
    local prev_tag="$3"
    local requested_version="$4"
    local changes="$5"

    cat > "${plan_file}" <<EOF
# Review this release plan, then save and exit.
# Edit VERSION as needed. Edit CHANGELOG.md to match before continuing.
# Only include shipped, release-relevant changes in the changelog.
# Exclude docs-only, planning, CI-only, and internal refactor entries unless they
# materially changed the release itself.
#
# Validation rules:
# - VERSION must be semver: X.Y.Z or X.Y.Z-suffix
# - CHANGELOG.md must contain: ## [VERSION] - YYYY-MM-DD
# - [Unreleased] must compare VERSION...HEAD
# - [VERSION] must compare ${prev_tag}...vVERSION
#
# Context:
# Previous tag: ${prev_tag}
# Requested on CLI: ${requested_version:-(none)}
#
# Commits since ${prev_tag}:
$(printf '%s\n' "${changes}" | sed 's/^/# /')

VERSION=${version}
EOF
}

# Parse arguments: [version] [--dry]
REQUESTED_VERSION=""
DRY=""
for arg in "$@"; do
    if [ "$arg" = "--dry" ]; then
        DRY="--dry"
    elif [ -z "$REQUESTED_VERSION" ]; then
        REQUESTED_VERSION="$arg"
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

# Use the installed Hazmat pair for the changelog session. Native sessions
# depend on the helper path registered with sudoers, so a checkout-built CLI
# can drift from the installed helper and fail during session prep.
CHANGELOG_HAZMAT_BIN="$(command -v hazmat || true)"
if [ -z "${CHANGELOG_HAZMAT_BIN}" ]; then
    echo "error: installed hazmat binary not found on PATH" >&2
    echo "Run 'make install' and 'hazmat init' before releasing." >&2
    exit 1
fi
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

# Build prompt in a temp file to avoid nested quoting issues
PROMPT_FILE="$(mktemp)"
RELEASE_PLAN_FILE="$(mktemp)"

cat > "${PROMPT_FILE}" <<PROMPT_EOF
You are drafting CHANGELOG.md for a new release of Hazmat.

Previous version tag: ${PREV_TAG}
Explicit version requested: ${REQUESTED_VERSION:-(none — determine from changes)}

Commits since last tag:
${CHANGES}

Rules:
1. Read the current CHANGELOG.md
2. Draft release notes for shipped, user-relevant changes only.
3. Ignore docs-only, planning, CI-only, and internal refactor commits unless they materially changed shipped release behavior.
4. If no explicit version was given, determine the next semver version conservatively:
   - PATCH (0.x.Y+1) for bug fixes, tooling fixes, and internal changes
   - MINOR (0.X+1.0) only for clearly shipped new user-facing behavior
5. Move the [Unreleased] section contents into a new version section with today's date
6. Categorize entries under Added/Changed/Fixed/Tests as appropriate
7. Update the comparison links at the bottom
8. Keep [Unreleased] as an empty section at the top
9. Write the updated CHANGELOG.md
10. This is a draft only. The human reviewer will edit both VERSION and CHANGELOG.md before release.

Only edit CHANGELOG.md. Do not create or modify any other files.
Print the chosen version number as the LAST line of your response, formatted exactly as: VERSION=X.Y.Z
PROMPT_EOF

echo "Asking hazmat claude to update CHANGELOG.md..."
echo ""
CLAUDE_OUTPUT="$("${CHANGELOG_HAZMAT_BIN}" claude --no-backup -p "$(cat "${PROMPT_FILE}")" 2>&1)" || {
    echo "error: hazmat claude failed" >&2
    echo "${CLAUDE_OUTPUT}" >&2
    exit 1
}

echo "${CLAUDE_OUTPUT}"
echo ""
RESTORE_CHANGELOG_ON_EXIT=1

# Extract draft version from claude output if not explicitly given
DRAFT_VERSION="${REQUESTED_VERSION}"
if [ -z "${DRAFT_VERSION}" ]; then
    DRAFT_VERSION="$(printf '%s\n' "${CLAUDE_OUTPUT}" | sed -nE 's/.*VERSION=([0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.]+)?).*/\1/p' | tail -1)"
    if [ -z "${DRAFT_VERSION}" ]; then
        echo "error: could not determine draft version from claude output" >&2
        exit 1
    fi
fi
DRAFT_VERSION="${DRAFT_VERSION#v}"

write_release_plan "${RELEASE_PLAN_FILE}" "${DRAFT_VERSION}" "${PREV_TAG}" "${REQUESTED_VERSION}" "${CHANGES}"

EDITOR_CMD="$(resolve_editor)"
echo "Opening release review in ${EDITOR_CMD}..."
echo "Edit VERSION in ${RELEASE_PLAN_FILE} and revise CHANGELOG.md as needed."
echo ""

while true; do
    if ! run_editor "${EDITOR_CMD}" "${RELEASE_PLAN_FILE}" CHANGELOG.md; then
        echo "Editor exited non-zero. Restoring CHANGELOG.md..."
        discard_changelog_draft
        echo "Aborted."
        exit 1
    fi

    VERSION="$(extract_version_from_plan "${RELEASE_PLAN_FILE}")"
    if [ -z "${VERSION}" ]; then
        echo "Release plan is invalid:"
        echo "  - Add a line like VERSION=0.6.1 to ${RELEASE_PLAN_FILE}"
        echo ""
    else
        if validation_errors="$(validate_release_plan "${VERSION}" "${PREV_TAG}")"; then
            break
        fi
        echo "Release plan validation failed:"
        while IFS= read -r validation_error; do
            echo "  - ${validation_error}"
        done <<< "${validation_errors}"
        echo ""
    fi

    read -rp "Re-open editor to fix release metadata? [Y/n] " retry
    if [[ "${retry}" =~ ^[Nn]$ ]]; then
        echo "Restoring CHANGELOG.md..."
        discard_changelog_draft
        echo "Aborted."
        exit 1
    fi
done

VERSION="${VERSION#v}"
TAG="v${VERSION}"

echo "Building release binaries for ${TAG}..."
make VERSION="${TAG}" all >/dev/null
LOCAL_BUILD_VERSION="$("$(pwd)/hazmat/hazmat" --version 2>/dev/null || true)"
if [ "${LOCAL_BUILD_VERSION}" != "hazmat version ${TAG}" ]; then
    echo "error: local hazmat build reports '${LOCAL_BUILD_VERSION}', expected 'hazmat version ${TAG}'" >&2
    exit 1
fi
echo ""

echo "Release: ${TAG}"
echo "  Previous tag:    ${PREV_TAG}"
echo "  Local build:     ${LOCAL_BUILD_VERSION}"
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
    discard_changelog_draft
    exit 0
fi

read -rp "Commit changelog, tag ${TAG}, and push? [y/N] " confirm
if [[ ! "${confirm}" =~ ^[Yy]$ ]]; then
    echo "Restoring CHANGELOG.md..."
    discard_changelog_draft
    echo "Aborted."
    exit 0
fi

git add CHANGELOG.md
git commit -m "docs: update CHANGELOG for ${TAG}"
git tag -a "${TAG}" -m "Release ${TAG}"
git push origin master "${TAG}"
RESTORE_CHANGELOG_ON_EXIT=0

echo ""
echo "Done. Watch the release workflow:"
echo "  https://github.com/dredozubov/hazmat/actions"
