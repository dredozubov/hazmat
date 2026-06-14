#!/bin/sh

set -eu

SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)"
REPO_ROOT="$(CDPATH='' cd -- "$SCRIPT_DIR/.." && pwd)"

usage() {
	cat <<'EOF'
Usage: scripts/test-lane.sh <lane>
       scripts/test-lane.sh --list

Runs named non-live pre-release test lanes.

Non-live lanes:
  source-safety
  package-boundaries
  package-contracts
  os-linux
  os-macos
  cli-ux
  product-workflows
  release-artifacts
  tla-proof-hygiene
  tla-model-check

Approval-gated lanes are intentionally not run through this aggregator:
  privileged-install-ownership
  live-approved
  destructive-lifecycle
  drift
EOF
}

require_os() {
	want="$1"
	got="$(uname -s | tr '[:upper:]' '[:lower:]')"
	if [ "$got" != "$want" ]; then
		echo "test-lane: lane requires $want, current host is $got" >&2
		exit 2
	fi
}

run_go_test() {
	(cd "$REPO_ROOT/hazmat" && go test ./...)
}

run_go_vet() {
	(cd "$REPO_ROOT/hazmat" && go vet ./...)
}

list_lanes() {
	sed -n 's/^[^#][^	]*	\([^	]*\).*/\1/p' "$REPO_ROOT/docs/test-lanes.tsv" | sort -u
}

if [ "$#" -ne 1 ]; then
	usage >&2
	exit 2
fi

case "$1" in
	--list)
		list_lanes
		;;
	-h|--help)
		usage
		;;
	source-safety)
		bash "$REPO_ROOT/scripts/check-secret-patterns.sh"
		bash "$REPO_ROOT/scripts/test-secret-patterns.sh"
		bash "$REPO_ROOT/scripts/check-credential-regressions.sh"
		bash "$REPO_ROOT/scripts/test-credential-regressions.sh"
		;;
	package-boundaries)
		bash "$REPO_ROOT/scripts/check-import-boundaries.sh"
		;;
	package-contracts)
		run_go_test
		;;
	os-linux)
		require_os linux
		run_go_test
		bash "$REPO_ROOT/scripts/check-linux-compile.sh"
		;;
	os-macos)
		require_os darwin
		run_go_vet
		run_go_test
		(cd "$REPO_ROOT/hazmat" && bash ../scripts/check-cli-smoke.sh)
		;;
	cli-ux)
		bash "$REPO_ROOT/scripts/test-entrypoint-guards.sh"
		(cd "$REPO_ROOT/hazmat" && bash ../scripts/check-cli-smoke.sh)
		;;
	product-workflows)
		bash "$REPO_ROOT/scripts/e2e-harness-smoke.sh"
		bash "$REPO_ROOT/scripts/e2e-service-harness-smoke.sh"
		bash "$REPO_ROOT/scripts/e2e-stack-matrix.sh" --contract
		;;
	release-artifacts)
		require_os darwin
		make -C "$REPO_ROOT" all
		bash "$REPO_ROOT/scripts/check-linux-compile.sh"
		;;
	tla-proof-hygiene)
		(
			cd "$REPO_ROOT/tla"
			bash proof_ownership_check.sh
			bash trace_artifact_check.sh
			bash proof_audit.sh --fail-on-drift >/dev/null
		)
		;;
	tla-model-check)
		(
			cd "$REPO_ROOT/tla"
			bash check_suite.sh
		)
		;;
	privileged-install-ownership)
		echo "test-lane: privileged-install-ownership is approval-gated." >&2
		echo "Use: scripts/check-privileged-install-ownership.sh --run --i-understand-this-checks-privileged-install-ownership" >&2
		exit 2
		;;
	live-approved)
		echo "test-lane: live-approved lanes must use their exact wrapper and acknowledgement flag." >&2
		exit 2
		;;
	destructive-lifecycle)
		echo "test-lane: destructive-lifecycle must run through scripts/e2e-vm.sh or scripts/e2e.sh with explicit approval." >&2
		exit 2
		;;
	drift)
		echo "test-lane: drift lanes are scheduled CI checks, not local release blockers." >&2
		exit 2
		;;
	*)
		echo "test-lane: unknown lane: $1" >&2
		usage >&2
		exit 2
		;;
esac
