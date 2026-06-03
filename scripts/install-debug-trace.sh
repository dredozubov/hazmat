#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
DEBUG_BIN="${HAZMAT_DEBUG_BIN:-$HOME/.hazmat/bin/hazmat-debug}"
TRACE_CLAUDE_BIN="${HAZMAT_TRACE_CLAUDE_BIN:-$HOME/.hazmat/bin/hazmat-trace-claude}"
GOFLAGS_VALUE="${HAZMAT_BUILD_GOFLAGS:--trimpath}"
LDFLAGS_VALUE="${HAZMAT_BUILD_LDFLAGS:-}"

debug_bindir="$(dirname "$DEBUG_BIN")"
trace_bindir="$(dirname "$TRACE_CLAUDE_BIN")"

echo "hazmat-debug: checking trace prerequisites..."
"$REPO_ROOT/scripts/configure-debug-trace.sh"

install -d -m 0755 "$debug_bindir"
install -d -m 0755 "$trace_bindir"

echo "hazmat-debug: building developer trace binary..."
(
	cd "$REPO_ROOT/hazmat"
	# shellcheck disable=SC2086
	GOFLAGS= go build $GOFLAGS_VALUE -tags hazmat_debug -ldflags "$LDFLAGS_VALUE" -o "$DEBUG_BIN" ./cmd/hazmat
)
chmod 0755 "$DEBUG_BIN"

cat >"$TRACE_CLAUDE_BIN" <<EOF
#!/bin/sh

set -eu

DEBUG_BIN=\${HAZMAT_DEBUG_BIN:-"$DEBUG_BIN"}
TRACE_ROOT=\${HAZMAT_TRACE_ROOT:-"\$HOME/.hazmat/traces"}
PROJECT=\${HAZMAT_TRACE_PROJECT:-"\$(pwd -P)"}
NAME=\${HAZMAT_TRACE_NAME:-"claude-interactive-\$(date -u +%Y%m%dT%H%M%SZ)"}
NO_BACKUP=1

usage() {
	cat <<'USAGE'
Usage: hazmat-trace-claude [--name LABEL] [-C DIR|--project DIR] [--out DIR] [--backup] [-- HAZMAT_OR_CLAUDE_ARGS...]

Starts an interactive Claude Code session under the developer-only Hazmat trace
binary. Trace bundles are written under ~/.hazmat/traces by default.
USAGE
}

while [ "\$#" -gt 0 ]; do
	case "\$1" in
		--name)
			shift
			[ "\$#" -gt 0 ] || { echo "hazmat-trace-claude: --name requires a value" >&2; exit 2; }
			NAME="\$1"
			shift
			;;
		--name=*)
			NAME="\${1#--name=}"
			shift
			;;
		-C|--project)
			shift
			[ "\$#" -gt 0 ] || { echo "hazmat-trace-claude: -C/--project requires a value" >&2; exit 2; }
			PROJECT="\$1"
			shift
			;;
		--project=*)
			PROJECT="\${1#--project=}"
			shift
			;;
		--out)
			shift
			[ "\$#" -gt 0 ] || { echo "hazmat-trace-claude: --out requires a value" >&2; exit 2; }
			TRACE_ROOT="\$1"
			shift
			;;
		--out=*)
			TRACE_ROOT="\${1#--out=}"
			shift
			;;
		--backup)
			NO_BACKUP=0
			shift
			;;
		--help|-h)
			usage
			exit 0
			;;
		--)
			shift
			break
			;;
		*)
			break
			;;
	esac
done

if [ ! -x "\$DEBUG_BIN" ]; then
	echo "hazmat-trace-claude: debug binary not found at \$DEBUG_BIN" >&2
	echo "hazmat-trace-claude: run: cd ~/workspace/hazmat && make hazmat-debug" >&2
	exit 1
fi

if ! /usr/bin/sudo -n -v >/dev/null 2>&1; then
	echo "hazmat-trace-claude: sudo is required for macOS DTrace probes." >&2
	echo "hazmat-trace-claude: run 'sudo -v' first, then retry this command." >&2
	exit 1
fi

cat >&2 <<INFO
hazmat-trace-claude: interactive developer trace
  debug binary: \$DEBUG_BIN
  project:      \$PROJECT
  trace root:   \$TRACE_ROOT
  trace label:  \$NAME

Use Claude Code normally. Exit Claude to finalize the trace bundle.
INFO

if [ "\$NO_BACKUP" -eq 1 ]; then
	exec "\$DEBUG_BIN" trace claude --out "\$TRACE_ROOT" --name "\$NAME" -- --no-backup -C "\$PROJECT" "\$@"
fi
exec "\$DEBUG_BIN" trace claude --out "\$TRACE_ROOT" --name "\$NAME" -- -C "\$PROJECT" "\$@"
EOF
chmod 0755 "$TRACE_CLAUDE_BIN"

cat <<EOF

Installed developer trace tools:
  $DEBUG_BIN
  $TRACE_CLAUDE_BIN

Interactive Claude trace:
  cd <project>
  sudo -v
  $TRACE_CLAUDE_BIN --name claude-interactive-repro

These binaries are developer-only and live outside the source checkout.
EOF
