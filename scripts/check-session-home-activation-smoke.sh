#!/bin/sh

set -eu

SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)"
REPO_ROOT="$(CDPATH='' cd -- "$SCRIPT_DIR/.." && pwd)"
HAZMAT="${HAZMAT_SESSION_HOME_SMOKE_HAZMAT:-$REPO_ROOT/hazmat/hazmat}"
AGENT_USER="${HAZMAT_SESSION_HOME_SMOKE_AGENT_USER:-agent}"
LAUNCH_HELPER="${HAZMAT_SESSION_HOME_SMOKE_LAUNCH_HELPER:-/usr/local/libexec/hazmat-launch}"
MODE="run"
MISSING_PREREQS=""
SCRATCH=""

usage() {
	cat <<'EOF'
Usage: scripts/check-session-home-activation-smoke.sh [--check-prereqs|--skip-if-missing-prereqs]

Starts a Hazmat native exec session with HAZMAT_EXPERIMENTAL_SESSION_HOME=activate
and validates the session-local HOME/XDG layout plus go, npm, pip, cargo, and
git behavior inside the activated session.

Options:
  --check-prereqs           Only check local prerequisites; exit 0 when ready,
                            exit 2 with reasons when the machine is not ready.
  --skip-if-missing-prereqs Skip with exit 0 when prerequisites are missing.
  -h, --help                Show this help.

This smoke is sudo-adjacent because the live run uses Hazmat native helper-backed
containment. Agents must ask before running it.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--check-prereqs)
			MODE="check"
			;;
		--skip-if-missing-prereqs)
			MODE="skip"
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "session-home-smoke: unknown argument: $1" >&2
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
	if ! command -v "$1" >/dev/null 2>&1; then
		add_missing_prereq "$1 is not on PATH"
	fi
}

check_prereqs() {
	MISSING_PREREQS=""

	if [ "$(uname -s 2>/dev/null || printf unknown)" != "Darwin" ]; then
		add_missing_prereq "macOS/Darwin is required for native session-home activation"
	fi

	require_command sudo
	require_command id
	require_command mktemp

	if [ ! -x "$HAZMAT" ]; then
		add_missing_prereq "$HAZMAT is missing or not executable; run make first"
	fi
	if [ ! -x "$LAUNCH_HELPER" ]; then
		add_missing_prereq "$LAUNCH_HELPER is missing or not executable; run hazmat init"
	fi
	if [ ! -x /usr/bin/sandbox-exec ]; then
		add_missing_prereq "/usr/bin/sandbox-exec is missing; native seatbelt support is unavailable"
	fi
	if command -v id >/dev/null 2>&1; then
		if ! id -u "$AGENT_USER" >/dev/null 2>&1; then
			add_missing_prereq "agent user '$AGENT_USER' does not exist; run hazmat init"
		fi
	fi
	if command -v sudo >/dev/null 2>&1 && id -u "$AGENT_USER" >/dev/null 2>&1; then
		if ! sudo -n -u "$AGENT_USER" /usr/bin/true >/dev/null 2>&1; then
			add_missing_prereq "passwordless non-interactive sudo to '$AGENT_USER' is unavailable"
		fi
	fi

	if [ -n "$MISSING_PREREQS" ]; then
		return 1
	fi
	return 0
}

print_missing_prereqs() {
	echo "session-home-smoke: missing prerequisites:" >&2
	printf '%s\n' "$MISSING_PREREQS" >&2
}

if ! check_prereqs; then
	if [ "$MODE" = "skip" ]; then
		echo "session-home-smoke: skipped because prerequisites are missing" >&2
		print_missing_prereqs
		exit 0
	fi
	print_missing_prereqs
	exit 2
fi

if [ "$MODE" = "check" ]; then
	echo "session-home-smoke: prerequisites ok"
	exit 0
fi

SCRATCH="$(mktemp -d /tmp/hazmat-session-home-smoke.XXXXXX)"
PROJECT="$SCRATCH/project"

cleanup() {
	if [ -n "$SCRATCH" ]; then
		rm -rf "$SCRATCH"
	fi
}
trap cleanup EXIT INT TERM

mkdir -p "$PROJECT"
chmod 755 "$SCRATCH" "$PROJECT"

HAZMAT_EXPERIMENTAL_SESSION_HOME=activate \
	"$HAZMAT" exec \
	--docker=none \
	--network none \
	--no-backup \
	--integration go \
	--integration node \
	--integration python-pip \
	--integration rust \
	-C "$PROJECT" \
	-- /bin/sh -eu <<'SESSION_HOME_SMOKE'
case "$HOME" in
	/private/tmp/hazmat-home/*/home)
		;;
	*)
		echo "session-home-smoke: HOME is not session-local: $HOME" >&2
		exit 11
		;;
esac

test "$XDG_CACHE_HOME" = "$HOME/.cache"
test "$XDG_CONFIG_HOME" = "$HOME/.config"
test "$XDG_DATA_HOME" = "$HOME/.local/share"
test -d "$HOME"
test -f "$HOME/.hazmat-session-home"

printf '%s\n' "session-home write probe" >"$HOME/.session-home-write-probe"
test -f "$HOME/.session-home-write-probe"

go version >/dev/null
npm --version >/dev/null
python3 -m pip --version >/dev/null
cargo --version >/dev/null

git init -q git-probe
git -C git-probe config user.email session-home-smoke@example.invalid
git -C git-probe config user.name "Session Home Smoke"
printf '%s\n' "tracked" >git-probe/file.txt
git -C git-probe add file.txt
git -C git-probe commit -q -m "smoke"
test "$(git -C git-probe status --porcelain)" = ""

printf '%s\n' "session-home-smoke: HOME=$HOME"
printf '%s\n' "session-home-smoke: toolchain matrix ok"
SESSION_HOME_SMOKE
