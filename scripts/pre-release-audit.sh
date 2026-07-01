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
| os-linux | Ubuntu CI \`go-test-linux\`; major-distro pre-release CI via \`gh workflow run linux-pre-release.yml -f distro=all -f mode=all\` | pending | |
| cli-ux | \`scripts/test-lane.sh cli-ux\` | pending | |
| product-workflows | \`scripts/test-lane.sh product-workflows\` | pending | |
| release-artifacts | \`scripts/test-lane.sh release-artifacts\` or release workflow preflight/build | pending | |
| tla-proof-hygiene | CI \`tla-proof-hygiene\` or \`scripts/test-lane.sh tla-proof-hygiene\` | pending | |
| tla-model-check | CI \`tla-model-check\` or \`scripts/test-lane.sh tla-model-check\` | pending | |
| privileged-install-ownership | \`scripts/e2e.sh --vm --quick\` lifecycle, or direct \`scripts/check-privileged-install-ownership.sh --run ...\` and \`--after-rollback ...\` on disposable host | pending | |
| live-approved | Relevant wrapper(s), exact command approval required | pending | |
| destructive-lifecycle | \`scripts/e2e.sh --vm --quick\` or documented skip | pending | |
| linux-current-user | \`linux-pre-release.yml\` with \`mode=current-user\`, or \`docs/linux-current-user-vm-smoke-matrix.md\` / \`sandboxing-xuar.3.5\` transcripts | pending | |
| linux-agent-user | \`linux-pre-release.yml\` with \`mode=agent-user\`, or \`docs/linux-agent-user-vm-lifecycle-matrix.md\` / \`sandboxing-xuar.4.5\` transcripts | pending | |
| fake-harness-contract | CI \`fake-harness-contract\` job or \`scripts/e2e-harness-smoke.sh\` plus \`scripts/check-live-harness-matrix.sh --validate-contract\` | pending | |
| live-real-harness-matrix | \`.github/workflows/live-harness-matrix.yml\` artifacts or exact approved \`scripts/check-live-harness-matrix.sh --run ...\` | pending | |
| supported-harness-os-evidence | Per-harness \`metadata.json\` artifacts for macOS pass/fail and Linux typed skips from \`linux-pre-release.yml\` | pending | |

## Lane Registry Snapshot

\`\`\`
EOF

grep -v '^#' "$REPO_ROOT/docs/test-lanes.tsv" | sed '/^[[:space:]]*$/d' >>"$OUTPUT_PATH"

cat >>"$OUTPUT_PATH" <<'EOF'
```

## Supported Harness Live Evidence Matrix

EOF

python3 - "$REPO_ROOT/docs/live-harness-smoke-contract.json" >>"$OUTPUT_PATH" <<'PY'
import json
import sys

contract_path = sys.argv[1]
with open(contract_path, "r", encoding="utf-8") as f:
    contract = json.load(f)

lanes = ["macos-agent-user", "docker-sandbox", "macos-current-user", "linux-current-user", "linux-agent-user"]

def artifact_hint(row, lane):
    harness = row["id"]
    if lane == "macos-agent-user":
        return f"live-harness-macos-agent-user-native/{harness}/metadata.json"
    if lane == "docker-sandbox":
        return f"live-harness-docker-sandbox-<distro-or-native>/{harness}/metadata.json"
    if lane == "macos-current-user":
        return f"live-harness-macos-current-user-native/{harness}/metadata.json"
    if lane == "linux-current-user":
        return f"linux-pre-release/live-harness-<distro>-current-user/{harness}/metadata.json"
    if lane == "linux-agent-user":
        return f"linux-pre-release/live-harness-<distro>-agent-user/{harness}/metadata.json"
    return f"live-harness-{lane}/{harness}/metadata.json"

def cell(row, lane):
    artifact = artifact_hint(row, lane)
    for support in row["os_support"]:
        if support["lane"] == lane:
            if support["status"] == "supported":
                return f"pending live artifact: `{artifact}`"
            reason = support.get("skip_reason") or support.get("reason") or support["status"]
            return "skip: " + reason.replace("|", "\\|") + f"; artifact: `{artifact}`"
    return "missing contract lane"

print("| Harness | Fake contract | " + " | ".join(lanes) + " |")
print("| --- | --- | " + " | ".join(["---"] * len(lanes)) + " |")
for row in contract["harnesses"]:
    fake = "pending fake-harness-contract evidence"
    cells = [cell(row, lane) for lane in lanes]
    print(f"| {row['display_name']} | {fake} | " + " | ".join(cells) + " |")
PY

cat >>"$OUTPUT_PATH" <<'EOF'

## Notes

- Record exact commands, OS/arch, and pass/fail results.
- Every skipped lane needs a concrete reason.
- Live and destructive lanes require exact-command approval before execution.
- Release preflight protects tag artifacts, but it does not replace the full CI matrix.
- Major-distro Linux promotion evidence comes from `linux-pre-release.yml`, which runs Ubuntu on a disposable hosted runner and Debian/Fedora/Arch in disposable QEMU VMs.
- Supported-harness claims require fake contract evidence plus current live pass artifacts for executable OS lanes, or typed skip artifacts that preserve the provider status.
EOF

echo "pre-release-audit: wrote $OUTPUT_PATH"
