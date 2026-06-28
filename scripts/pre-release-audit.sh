#!/bin/sh

set -eu

SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)"
REPO_ROOT="$(CDPATH='' cd -- "$SCRIPT_DIR/.." && pwd)"
OUTPUT_DIR="${HAZMAT_PRE_RELEASE_AUDIT_DIR:-$REPO_ROOT/pre-release-audits}"
OUTPUT_PATH=""

usage() {
	cat <<'EOF'
Usage: scripts/pre-release-audit.sh [--output PATH]

Create a markdown audit record for a local release candidate. The script does
not run tests; it records the commit, host facts, lane registry, required
commands, and explicit skipped-lane fields so release evidence is reviewable.

Options:
  --output PATH  Write the audit record to PATH instead of pre-release-audits/.
  -h, --help     Show this help.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--output)
			shift
			if [ "$#" -eq 0 ]; then
				echo "pre-release-audit: --output requires a path" >&2
				exit 2
			fi
			OUTPUT_PATH="$1"
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "pre-release-audit: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

commit="$(git -C "$REPO_ROOT" rev-parse HEAD)"
branch="$(git -C "$REPO_ROOT" branch --show-current || true)"
timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
os_name="$(uname -s)"
arch="$(uname -m)"

if [ -z "$OUTPUT_PATH" ]; then
	mkdir -p "$OUTPUT_DIR"
	OUTPUT_PATH="$OUTPUT_DIR/pre-release-audit-$timestamp.md"
else
	mkdir -p "$(dirname "$OUTPUT_PATH")"
fi

cat >"$OUTPUT_PATH" <<EOF
# Hazmat Pre-Release Audit

- Timestamp: $timestamp
- Commit: $commit
- Branch: ${branch:-detached}
- Host: $os_name/$arch

## Required Lane Evidence

| Lane | Command or source | Status | Evidence / skip reason |
| --- | --- | --- | --- |
| source-safety | \`scripts/test-lane.sh source-safety\` | pending | |
| package-boundaries | \`scripts/test-lane.sh package-boundaries\` | pending | |
| package-contracts | \`scripts/test-lane.sh package-contracts\` | pending | |
| os-macos | \`scripts/test-lane.sh os-macos\` on macOS | pending | |
| os-linux | Ubuntu CI \`go-test-linux\` or \`make linux-apple-container-test APPLE_CONTAINER_ACK=1\` | pending | |
| cli-ux | \`scripts/test-lane.sh cli-ux\` | pending | |
| product-workflows | \`scripts/test-lane.sh product-workflows\` | pending | |
| release-artifacts | \`scripts/test-lane.sh release-artifacts\` or release workflow preflight/build | pending | |
| tla-proof-hygiene | CI \`tla-proof-hygiene\` or \`scripts/test-lane.sh tla-proof-hygiene\` | pending | |
| tla-model-check | CI \`tla-model-check\` or \`scripts/test-lane.sh tla-model-check\` | pending | |
| privileged-install-ownership | \`scripts/e2e.sh --vm --quick\` lifecycle, or direct \`scripts/check-privileged-install-ownership.sh --run ...\` and \`--after-rollback ...\` on disposable host | pending | |
| live-approved | Relevant wrapper(s), exact command approval required | pending | |
| destructive-lifecycle | \`scripts/e2e.sh --vm --quick\` or documented skip | pending | |
| linux-current-user | \`docs/linux-current-user-vm-smoke-matrix.md\` / \`sandboxing-xuar.3.5\` transcripts | pending | |
| linux-agent-user | \`docs/linux-agent-user-vm-lifecycle-matrix.md\` / \`sandboxing-xuar.4.5\` transcripts | pending | |

## Lane Registry Snapshot

\`\`\`
EOF

grep -v '^#' "$REPO_ROOT/docs/test-lanes.tsv" | sed '/^[[:space:]]*$/d' >>"$OUTPUT_PATH"

cat >>"$OUTPUT_PATH" <<'EOF'
```

## Notes

- Record exact commands, OS/arch, and pass/fail results.
- Every skipped lane needs a concrete reason.
- Live and destructive lanes require exact-command approval before execution.
- Release preflight protects tag artifacts, but it does not replace the full CI matrix.
EOF

echo "pre-release-audit: wrote $OUTPUT_PATH"
