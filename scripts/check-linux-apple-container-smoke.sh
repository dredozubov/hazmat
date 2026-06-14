#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
APP_DIR="$REPO_ROOT/hazmat"
CONTAINER_BIN="${HAZMAT_APPLE_CONTAINER_BIN:-container}"
IMAGE="${HAZMAT_LINUX_APPLE_CONTAINER_IMAGE:-golang:1.25}"
PACKAGES="${HAZMAT_LINUX_APPLE_CONTAINER_PACKAGES:-./platform/linux ./containment/linux ./internal/runtime ./internal/runtime/linux ./internal/runtime/applecontainer}"
TEST_ARGS="${HAZMAT_LINUX_APPLE_CONTAINER_TEST_ARGS:--test.v}"
GO_TEST_ARGS="${HAZMAT_LINUX_APPLE_CONTAINER_GO_TEST_ARGS:-$PACKAGES}"
NETWORK="${HAZMAT_LINUX_APPLE_CONTAINER_NETWORK:-none}"
GOARCH_VALUE="${HAZMAT_LINUX_APPLE_CONTAINER_GOARCH:-}"
MODE="disclosure"
ACK_RUN=0
SKIP_IF_MISSING=0
MISSING_PREREQS=""
TMPDIR_LINUX_APPLE_CONTAINER=""

usage() {
	cat <<'EOF'
Usage:
  scripts/check-linux-apple-container-smoke.sh
  scripts/check-linux-apple-container-smoke.sh --check-packages
  scripts/check-linux-apple-container-smoke.sh --compile-tests
  scripts/check-linux-apple-container-smoke.sh --check-prereqs
  scripts/check-linux-apple-container-smoke.sh --run --i-understand-this-runs-apple-container-linux-tests [--skip-if-missing-prereqs]
  scripts/check-linux-apple-container-smoke.sh --go-test --i-understand-this-runs-apple-container-linux-tests [--skip-if-missing-prereqs]

Default mode is disclosure-only. The focused live smoke cross-compiles selected
Go test binaries for linux/<arch>, then runs them in Apple Container. The
container-native Linux suite runs `go test` inside Apple Container against a
writable copy of the repository.

Options:
  --check-packages               Check that the selected package set lists for linux/<arch>.
  --compile-tests                Compile selected linux/<arch> test binaries without running containers.
  --check-prereqs                Check host-side Apple Container prerequisites.
  --skip-if-missing-prereqs      Exit 0 when prerequisites are absent.
  --run                          Run the live Linux test smoke.
  --go-test                      Run container-native `go test`.
  --i-understand-this-runs-apple-container-linux-tests
                                 Required acknowledgement for --run and --go-test.
  -h, --help                     Show this help.

Environment:
  HAZMAT_APPLE_CONTAINER_BIN                 Apple Container executable name or absolute path.
  HAZMAT_LINUX_APPLE_CONTAINER_IMAGE         Guest image. Default: golang:1.25
  HAZMAT_LINUX_APPLE_CONTAINER_GOARCH        Guest arch. Default: arm64 on Apple silicon.
  HAZMAT_LINUX_APPLE_CONTAINER_NETWORK       container run network. Default: none
  HAZMAT_LINUX_APPLE_CONTAINER_PACKAGES      Space-separated Go package patterns.
  HAZMAT_LINUX_APPLE_CONTAINER_TEST_ARGS     Args passed to each compiled test binary.
  HAZMAT_LINUX_APPLE_CONTAINER_GO_TEST_ARGS  Args passed to container-native go test.
                                             Default: focused Linux package set.

Prereq checks inspect local Apple Container setup with `container system status`.
The live run creates short-lived exact-named containers and may pull the
configured image. Agents must ask for explicit approval before running
--check-prereqs, --skip-if-missing-prereqs, --run, or --go-test.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--check-prereqs)
			MODE="check"
			;;
		--check-packages)
			MODE="packages"
			;;
		--compile-tests)
			MODE="compile"
			;;
		--skip-if-missing-prereqs)
			SKIP_IF_MISSING=1
			if [ "$MODE" = "disclosure" ]; then
				MODE="skip"
			fi
			;;
		--run)
			MODE="run"
			;;
		--go-test)
			MODE="go-test"
			;;
		--i-understand-this-runs-apple-container-linux-tests)
			ACK_RUN=1
			;;
		--help|-h)
			usage
			exit 0
			;;
		*)
			echo "linux-apple-container-smoke: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

add_missing_prereq() {
	if [ -z "$MISSING_PREREQS" ]; then
		MISSING_PREREQS="- $*"
	else
		MISSING_PREREQS="$MISSING_PREREQS
- $*"
	fi
}

require_command() {
	case "$1" in
		*/*)
			case "$1" in
				/*)
					;;
				*)
					add_missing_prereq "$1 must be an absolute path or command name"
					return
					;;
			esac
			if [ ! -x "$1" ]; then
				add_missing_prereq "$1 is missing or not executable"
			fi
			;;
		*)
			if ! command -v "$1" >/dev/null 2>&1; then
				add_missing_prereq "$1 is not on PATH"
			fi
			;;
	esac
}

host_arch_default() {
	case "$(uname -m)" in
		arm64|aarch64)
			printf '%s\n' arm64
			;;
		x86_64|amd64)
			printf '%s\n' amd64
			;;
		*)
			uname -m
			;;
	esac
}

container_cmd() {
	case "$CONTAINER_BIN" in
		/*)
			printf '%s\n' "$CONTAINER_BIN"
			;;
		*/*)
			printf '%s\n' "$CONTAINER_BIN"
			;;
		*)
			command -v "$CONTAINER_BIN" 2>/dev/null || printf '%s\n' "$CONTAINER_BIN"
			;;
	esac
}

check_prereqs() {
	MISSING_PREREQS=""

	require_command go
	require_command mktemp
	require_command "$CONTAINER_BIN"

	if [ "$(uname -s)" != "Darwin" ]; then
		add_missing_prereq "Apple Container requires macOS; current host is $(uname -s)"
	fi
	if [ "$(uname -m)" != "arm64" ]; then
		add_missing_prereq "Apple Container requires Apple silicon; current host arch is $(uname -m)"
	fi

	if command -v "$CONTAINER_BIN" >/dev/null 2>&1 || { [ -x "$CONTAINER_BIN" ] 2>/dev/null; }; then
		if ! "$CONTAINER_BIN" system status --format json >/tmp/hazmat-apple-container-status.$$ 2>/tmp/hazmat-apple-container-status.err.$$; then
			add_missing_prereq "container system status failed; start Apple Container for the invoking user"
		elif ! grep -q '"status"[[:space:]]*:[[:space:]]*"running"' /tmp/hazmat-apple-container-status.$$; then
			add_missing_prereq "container system status did not report running"
		fi
		rm -f /tmp/hazmat-apple-container-status.$$ /tmp/hazmat-apple-container-status.err.$$
	fi

	if [ -n "$MISSING_PREREQS" ]; then
		return 1
	fi
	return 0
}

check_packages() {
	if [ -z "$GOARCH_VALUE" ]; then
		GOARCH_VALUE="$(host_arch_default)"
	fi
	(
		cd "$APP_DIR"
		# shellcheck disable=SC2086
		GOOS=linux GOARCH="$GOARCH_VALUE" CGO_ENABLED=0 go list $PACKAGES >/dev/null
	)
}

print_missing_prereqs() {
	echo "linux-apple-container-smoke: missing prerequisites:" >&2
	printf '%s\n' "$MISSING_PREREQS" >&2
}

print_disclosure() {
	cat <<EOF
linux-apple-container-smoke: disclosure-only

This live smoke cross-compiles selected Hazmat Go test binaries for Linux and
runs them inside Apple Container. It can also run a container-native Linux
go test suite against a writable copy of the repository. It bind-mounts the
repository read-only, uses exact-named short-lived containers, and does not run
hazmat init, hazmat doctor, helper-backed native containment, Docker, or sudo.

Default package set:
  $PACKAGES

Live mode may run Apple Container system probes, create containers, and pull the
configured image if it is not already present. Ask for explicit approval for
this exact command before running it:

  scripts/check-linux-apple-container-smoke.sh --run --i-understand-this-runs-apple-container-linux-tests

Container-native local Linux test-suite:
  scripts/check-linux-apple-container-smoke.sh --go-test --i-understand-this-runs-apple-container-linux-tests

Prereq check:
  scripts/check-linux-apple-container-smoke.sh --check-prereqs

Package-set check:
  scripts/check-linux-apple-container-smoke.sh --check-packages

Compile-only check:
  scripts/check-linux-apple-container-smoke.sh --compile-tests

Common override:
  HAZMAT_LINUX_APPLE_CONTAINER_PACKAGES='./...' scripts/check-linux-apple-container-smoke.sh --run --i-understand-this-runs-apple-container-linux-tests
  HAZMAT_LINUX_APPLE_CONTAINER_GO_TEST_ARGS='./...' scripts/check-linux-apple-container-smoke.sh --go-test --i-understand-this-runs-apple-container-linux-tests
EOF
}

cleanup() {
	if [ -n "$TMPDIR_LINUX_APPLE_CONTAINER" ]; then
		rm -rf "$TMPDIR_LINUX_APPLE_CONTAINER"
	fi
}
trap cleanup EXIT INT TERM HUP

safe_pkg_name() {
	printf '%s' "$1" | tr '/.' '__'
}

safe_container_fragment() {
	printf '%s' "$1" | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9]/-/g; s/--*/-/g; s/^-//; s/-$//'
}

run_smoke() {
	run_mode="${1:-run}"
	if [ -z "$GOARCH_VALUE" ]; then
		GOARCH_VALUE="$(host_arch_default)"
	fi
	TMPDIR_LINUX_APPLE_CONTAINER="$(mktemp -d)"
	BIN_DIR="$TMPDIR_LINUX_APPLE_CONTAINER/bin"
	mkdir -p "$BIN_DIR"

	echo "linux-apple-container-smoke: compiling linux/$GOARCH_VALUE test binaries..."
	(
		cd "$APP_DIR"
		# shellcheck disable=SC2086
		GOOS=linux GOARCH="$GOARCH_VALUE" CGO_ENABLED=0 go list -f '{{.ImportPath}} {{.Dir}}' $PACKAGES
	) | while IFS=' ' read -r import_path package_dir; do
		[ -n "$import_path" ] || continue
		binary="$BIN_DIR/$(safe_pkg_name "$import_path").test"
		echo "linux-apple-container-smoke: compile $import_path"
		(
			cd "$APP_DIR"
			GOOS=linux GOARCH="$GOARCH_VALUE" CGO_ENABLED=0 go test -c "$import_path" -o "$binary"
		)
		if [ ! -x "$binary" ]; then
			echo "linux-apple-container-smoke: skip $import_path; no compiled test binary"
			continue
		fi
		if [ "$run_mode" = "compile-only" ]; then
			continue
		fi

		container_name="hazmat-linux-test-$(safe_container_fragment "$import_path")-$$"
		echo "linux-apple-container-smoke: run $import_path in $IMAGE"
		# shellcheck disable=SC2086
		"$(container_cmd)" run --rm \
			--name "$container_name" \
			--network "$NETWORK" \
			--mount "type=bind,source=$REPO_ROOT,target=$REPO_ROOT,readonly" \
			--mount "type=bind,source=$TMPDIR_LINUX_APPLE_CONTAINER,target=$TMPDIR_LINUX_APPLE_CONTAINER,readonly" \
			--workdir "$package_dir" \
			"$IMAGE" \
			"$binary" $TEST_ARGS
	done
}

run_go_test_suite() {
	if [ -z "$GOARCH_VALUE" ]; then
		GOARCH_VALUE="$(host_arch_default)"
	fi
	TMPDIR_LINUX_APPLE_CONTAINER="$(mktemp -d)"
	mkdir -p "$TMPDIR_LINUX_APPLE_CONTAINER/work"

	gomodcache="$(cd "$APP_DIR" && go env GOMODCACHE)"
	container_gomodcache="/work/gomodcache"
	mount_module_cache=0
	if [ -n "$gomodcache" ] && [ -d "$gomodcache" ]; then
		mount_module_cache=1
		container_gomodcache="/go/pkg/mod"
	fi

	container_name="hazmat-linux-go-test-$$"
	echo "linux-apple-container-smoke: run go test $GO_TEST_ARGS in $IMAGE"
	if [ "$mount_module_cache" -eq 1 ]; then
		# shellcheck disable=SC2086
		"$(container_cmd)" run --rm \
			--name "$container_name" \
			--network "$NETWORK" \
			--mount "type=bind,source=$REPO_ROOT,target=/hazmat-src,readonly" \
			--mount "type=bind,source=$TMPDIR_LINUX_APPLE_CONTAINER/work,target=/work" \
			--mount "type=bind,source=$gomodcache,target=/go/pkg/mod,readonly" \
			--workdir /work \
			"$IMAGE" \
			sh -eu -c "$container_go_test_script" sh "$container_gomodcache" $GO_TEST_ARGS
	else
		# shellcheck disable=SC2086
		"$(container_cmd)" run --rm \
			--name "$container_name" \
			--network "$NETWORK" \
			--mount "type=bind,source=$REPO_ROOT,target=/hazmat-src,readonly" \
			--mount "type=bind,source=$TMPDIR_LINUX_APPLE_CONTAINER/work,target=/work" \
			--workdir /work \
			"$IMAGE" \
			sh -eu -c "$container_go_test_script" sh "$container_gomodcache" $GO_TEST_ARGS
	fi
}

container_go_test_script='
gomodcache="$1"
shift
mkdir -p /work/src /work/gocache /work/tmp /work/home "$gomodcache"
tar -C /hazmat-src \
	--exclude ./.git \
	--exclude ./.beads \
	--exclude ./spike-apple-container-results \
	--exclude ./tla/states \
	-cf - . | tar -C /work/src -xf -
cd /work/src/hazmat
GOOS=linux \
GOARCH="$(go env GOHOSTARCH)" \
CGO_ENABLED=0 \
GOWORK=off \
HOME=/work/home \
GOCACHE=/work/gocache \
GOMODCACHE="$gomodcache" \
GOTMPDIR=/work/tmp \
GOFLAGS="-mod=readonly ${GOFLAGS:-}" \
go test "$@"
'

case "$MODE" in
	disclosure)
		print_disclosure
		exit 0
		;;
	check|skip)
		if check_prereqs; then
			echo "linux-apple-container-smoke: prerequisites ok"
			exit 0
		fi
		if [ "$MODE" = "skip" ] || [ "$SKIP_IF_MISSING" -eq 1 ]; then
			echo "linux-apple-container-smoke: skipped because prerequisites are missing" >&2
			print_missing_prereqs
			exit 0
		fi
		print_missing_prereqs
		exit 2
		;;
	packages)
		if check_packages; then
			echo "linux-apple-container-smoke: packages ok"
			exit 0
		fi
		exit 1
		;;
	compile)
		run_smoke compile-only
		echo "linux-apple-container-smoke: compile ok"
		exit 0
		;;
	run)
		if [ "$ACK_RUN" -ne 1 ]; then
			echo "linux-apple-container-smoke: refusing live run without --i-understand-this-runs-apple-container-linux-tests" >&2
			exit 2
		fi
		if ! check_prereqs; then
			if [ "$SKIP_IF_MISSING" -eq 1 ]; then
				echo "linux-apple-container-smoke: skipped because prerequisites are missing" >&2
				print_missing_prereqs
				exit 0
			fi
			print_missing_prereqs
			exit 2
		fi
		run_smoke
		;;
	go-test)
		if [ "$ACK_RUN" -ne 1 ]; then
			echo "linux-apple-container-smoke: refusing live go-test without --i-understand-this-runs-apple-container-linux-tests" >&2
			exit 2
		fi
		if ! check_prereqs; then
			if [ "$SKIP_IF_MISSING" -eq 1 ]; then
				echo "linux-apple-container-smoke: skipped because prerequisites are missing" >&2
				print_missing_prereqs
				exit 0
			fi
			print_missing_prereqs
			exit 2
		fi
		run_go_test_suite
		;;
esac
