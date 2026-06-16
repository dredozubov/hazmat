#!/bin/sh
# Local pre-release gate for checks that are too stateful for normal pre-push.

set -eu

usage() {
	cat <<EOF
Usage:
  bash scripts/pre-release-local.sh [--vm] [--vm-full]

Runs the local pre-release gate.

Options:
  --vm       Also run the isolated VM lifecycle gate with --quick.
  --vm-full  Also run the isolated VM lifecycle gate without --quick.
  -h, --help Show this help.

The VM gate requires a reusable prebuilt macOS base VM. Pull the base image once
before the first VM run with:
  bash scripts/e2e-vm.sh --step download --quick
  HAZMAT_E2E_VM_PROVIDER=tart bash scripts/e2e-vm.sh --step download --quick
The default provider is Tart/Cirrus because those images are SSH-ready without
Setup Assistant automation. Set HAZMAT_E2E_VM_PROVIDER=lume only when you
provide an SSH-enabled Lume base image or existing base VM.
EOF
}

RUN_VM="${HAZMAT_PRE_RELEASE_VM:-}"
VM_ARGS="--quick"

while [ "$#" -gt 0 ]; do
	case "$1" in
		--vm)
			RUN_VM="1"
			VM_ARGS="--quick"
			;;
		--vm-full)
			RUN_VM="1"
			VM_ARGS=""
			;;
		--help|-h)
			usage
			exit 0
			;;
		*)
			echo "error: unknown argument: $1" >&2
			usage >&2
			exit 1
			;;
	esac
	shift
done

REPO_ROOT="$(git rev-parse --show-toplevel)"
cd "$REPO_ROOT"

echo "pre-release-local: fast repository gate..."
bash "$REPO_ROOT/scripts/pre-push"

echo "pre-release-local: package boundary guard..."
bash "$REPO_ROOT/scripts/check-import-boundaries.sh"

echo "pre-release-local: hermetic all-harness e2e smoke..."
bash "$REPO_ROOT/scripts/e2e-harness-smoke.sh"

echo "pre-release-local: fake service-harness lifecycle smoke..."
bash "$REPO_ROOT/scripts/e2e-service-harness-smoke.sh"

if [ -n "$RUN_VM" ] && [ "$RUN_VM" != "0" ]; then
	echo "pre-release-local: VM destructive lifecycle..."
	# shellcheck disable=SC2086
	bash "$REPO_ROOT/scripts/e2e-vm.sh" $VM_ARGS
else
	echo "pre-release-local: VM destructive lifecycle skipped (pass --vm for isolated full lifecycle)"
fi

echo "pre-release-local: all checks passed"
