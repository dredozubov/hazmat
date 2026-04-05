#!/bin/bash
# Install hazmat from GitHub releases.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/dredozubov/hazmat/master/scripts/install.sh | bash
#
#   # Or with a specific version:
#   curl -fsSL https://raw.githubusercontent.com/dredozubov/hazmat/master/scripts/install.sh | bash -s -- --version 0.4.2
#
# What this does:
#   1. Detects your architecture (arm64 or amd64)
#   2. Downloads the latest release from GitHub
#   3. Verifies the SHA-256 checksum
#   4. Installs hazmat to /usr/local/bin
#   5. Stages hazmat-launch for `hazmat init` to install later
#
# After install, run:
#   hazmat init

set -euo pipefail

REPO="dredozubov/hazmat"
INSTALL_DIR="/usr/local/bin"
HELPER_STAGING_DIR="/usr/local/libexec"

# ── Parse flags ─────────────────────────────────────────────────────────────

VERSION=""
for arg in "$@"; do
    case "$arg" in
        --version) shift; VERSION="${1:-}"; shift ;;
        --version=*) VERSION="${arg#--version=}" ;;
        --help|-h)
            echo "Usage: install.sh [--version X.Y.Z]"
            echo "  Downloads and installs hazmat from GitHub releases."
            echo "  Defaults to the latest release."
            exit 0
            ;;
    esac
done

# ── Preflight ───────────────────────────────────────────────────────────────

if [ "$(uname -s)" != "Darwin" ]; then
    echo "Error: hazmat is macOS-only." >&2
    exit 1
fi

if ! command -v curl &>/dev/null; then
    echo "Error: curl is required." >&2
    exit 1
fi

if ! command -v shasum &>/dev/null; then
    echo "Error: shasum is required." >&2
    exit 1
fi

# ── Detect architecture ────────────────────────────────────────────────────

ARCH="$(uname -m)"
case "$ARCH" in
    arm64|aarch64) ARCH="arm64" ;;
    x86_64)        ARCH="amd64" ;;
    *)
        echo "Error: unsupported architecture: $ARCH" >&2
        exit 1
        ;;
esac

# ── Resolve version ────────────────────────────────────────────────────────

if [ -z "$VERSION" ]; then
    VERSION=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
        | grep '"tag_name"' | head -1 | sed 's/.*"v\([^"]*\)".*/\1/')
    if [ -z "$VERSION" ]; then
        echo "Error: could not determine latest version." >&2
        exit 1
    fi
fi
VERSION="${VERSION#v}"  # strip leading v if present
TAG="v${VERSION}"

echo "Installing hazmat ${TAG} (darwin/${ARCH})..."

# ── Download and verify ─────────────────────────────────────────────────────

TARBALL="hazmat-${TAG}-darwin-${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${TAG}"
TMPDIR_INSTALL="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_INSTALL"' EXIT

echo "  Downloading ${TARBALL}..."
curl -fsSL "${BASE_URL}/${TARBALL}" -o "${TMPDIR_INSTALL}/${TARBALL}"

echo "  Downloading checksums..."
curl -fsSL "${BASE_URL}/checksums.txt" -o "${TMPDIR_INSTALL}/checksums.txt"

echo "  Verifying checksum..."
EXPECTED=$(grep "${TARBALL}" "${TMPDIR_INSTALL}/checksums.txt" | awk '{print $1}')
if [ -z "$EXPECTED" ]; then
    echo "Error: checksum not found for ${TARBALL}" >&2
    exit 1
fi
ACTUAL=$(shasum -a 256 "${TMPDIR_INSTALL}/${TARBALL}" | awk '{print $1}')
if [ "$EXPECTED" != "$ACTUAL" ]; then
    echo "Error: checksum mismatch" >&2
    echo "  expected: ${EXPECTED}" >&2
    echo "  got:      ${ACTUAL}" >&2
    exit 1
fi
echo "  Checksum OK."

# ── Extract ─────────────────────────────────────────────────────────────────

tar -xzf "${TMPDIR_INSTALL}/${TARBALL}" -C "${TMPDIR_INSTALL}"

# ── Install ─────────────────────────────────────────────────────────────────

echo "  Installing hazmat to ${INSTALL_DIR}/hazmat..."
sudo install -d -m 0755 "${INSTALL_DIR}"
sudo install -m 0755 "${TMPDIR_INSTALL}/hazmat" "${INSTALL_DIR}/hazmat"

echo "  Staging hazmat-launch at ${HELPER_STAGING_DIR}/hazmat-launch..."
sudo install -d -m 0755 "${HELPER_STAGING_DIR}"
sudo install -m 0755 "${TMPDIR_INSTALL}/hazmat-launch" "${HELPER_STAGING_DIR}/hazmat-launch"

# ── Verify ──────────────────────────────────────────────────────────────────

INSTALLED_VERSION=$("${INSTALL_DIR}/hazmat" --version 2>&1 | awk '{print $NF}')
echo ""
echo "  hazmat ${INSTALLED_VERSION} installed successfully."
echo ""
echo "  Next steps:"
echo "    hazmat init          Set up containment"
echo "    hazmat --help        See all commands"
