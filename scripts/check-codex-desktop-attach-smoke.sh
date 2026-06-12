#!/bin/sh

set -eu

SCRIPT_DIR="$(CDPATH='' cd -- "$(dirname "$0")" && pwd)"
REPO_ROOT="$(CDPATH='' cd -- "$SCRIPT_DIR/.." && pwd)"
APP_BUNDLE="${HAZMAT_CODEX_DESKTOP_SMOKE_APP:-/Applications/Codex.app}"
AGENT_USER="${HAZMAT_CODEX_APP_SERVER_SMOKE_AGENT_USER:-agent}"
AGENT_HOME="${HAZMAT_CODEX_APP_SERVER_SMOKE_AGENT_HOME:-/Users/agent}"
CODEX_BIN="$AGENT_HOME/.local/bin/codex"
LAUNCH_HELPER="${HAZMAT_CODEX_APP_SERVER_SMOKE_LAUNCH_HELPER:-/usr/local/libexec/hazmat-launch}"
MODE="dry-run"
APPROVED=0
WAIT_SECONDS="${HAZMAT_CODEX_DESKTOP_SMOKE_WAIT_SECONDS:-90}"
MISSING_PREREQS=""
SCRATCH=""
KEEP_SCRATCH=0

usage() {
	cat <<'EOF'
Usage: scripts/check-codex-desktop-attach-smoke.sh [options]

Prepare or run the explicit opt-in live Codex desktop attach smoke. By default
this script prints the host-state disclosure and exits without launching Codex.

Options:
  --dry-run                         Print the disclosure and planned live-run
                                    shape without creating scratch state.
  --print-disclosure                Print the required host-state disclosure.
  --check-prereqs                   Check non-invasive prerequisites; exits 2
                                    when a live run is not currently safe.
  --run                             Launch the stock Codex desktop app with a
                                    temporary CODEX_CLI_PATH recorder/proxy.
  --i-understand-this-may-launch-codex-app
                                    Required with --run. Confirms explicit
                                    human approval for the live app launch.
  --wait-seconds N                  Wait this long for CODEX_CLI_PATH activity
                                    after launch. Default: 90.
  --app PATH                        Codex.app bundle path. Default:
                                    /Applications/Codex.app.
  -h, --help                        Show this help.

The live run never quits, kills, or reconfigures an already-running Codex app.
It refuses to run if Codex is already running. Quit the app yourself before an
approved live probe.

This smoke is sudo-adjacent. The live run may launch Codex App, and
--check-prereqs performs non-interactive sudo capability probes with sudo -n.
Agents must ask before running either command.
EOF
}

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

print_disclosure() {
	cat <<EOF
Codex desktop attach smoke host-state disclosure
================================================

This probe is intentionally not part of autonomous app-server testing. It may
launch the stock Codex desktop app and cause that app to read or write its
normal host-user state.

State this script observes before launch:
- Codex app bundle existence and executable bit:
  $APP_BUNDLE
- Process list entries matching the stock Codex desktop app or its local
  app-server helpers.
- Local tool availability for open, go, node, sudo, id, ps, awk, mktemp, chmod,
  rm, sandbox-exec, and the Hazmat launch helper.

State this script creates directly during --run:
- A scratch directory under /tmp named
  /tmp/hazmat-codex-desktop-attach-smoke.*
- A scratch project directory used as HAZMAT_CODEX_APP_SHIM_PROJECT.
- A scratch Hazmat binary built from this checkout.
- A temporary CODEX_CLI_PATH Node proxy that records app-server JSON-RPC method
  names without logging request params by default.
- Probe logs under the scratch directory.

State the stock Codex app may observe or mutate if --run launches it:
- ~/.codex, including auth/session/sqlite/app metadata if the app uses it.
- ~/Library/Application Support/Codex
- ~/Library/Caches/com.openai.codex
- ~/Library/HTTPStorages/com.openai.codex
- ~/Library/Preferences/com.openai.codex.plist
- ~/Library/Logs/com.openai.codex
- ~/Library/Saved Application State/com.openai.codex.savedState
- Keychain items, TCC decisions, CrashReporter/Crashpad state, LaunchServices
  usage state, and normal app/window restoration state associated with Codex.
- Codex runtime temp/socket families such as /tmp/codex-browser-use,
  /tmp/codex-ipc, and /private/var/folders/.../T/codex-ipc if the app creates
  them.

State this script deliberately does not mutate:
- It does not edit ~/.codex, Codex Application Support, caches, HTTP storage,
  preferences, logs, keychain, or TCC databases.
- It does not quit, kill, focus, automate, or attach to an existing Codex app.
- It does not install or replace the user's Codex CLI or Hazmat binary.

Live-run behavior:
- Refuses to run if Codex is already running.
- Launches a new Codex app instance through /usr/bin/open with CODEX_CLI_PATH
  pointing at the temporary proxy.
- Passes HAZMAT_CODEX_APP_SHIM_PROJECT, HAZMAT_CODEX_APP_SHIM_NETWORK=none,
  HAZMAT_CODEX_APP_SHIM_NO_BACKUP=true, and
  HAZMAT_CODEX_APP_SHIM_SKIP_ASSETS_SYNC=true to the app environment.
- Leaves the scratch directory in place after launch so the app-server backend
  can keep running. Remove it only after quitting Codex.
EOF
}

running_codex_processes() {
	/bin/ps -axo pid=,args= | /usr/bin/awk '
		/Codex[.]app\/Contents\/MacOS\/Codex/ ||
		/Contents\/Resources\/codex app-server/ ||
		/com[.]openai[.]codex/ {
			print
		}
	'
}

open_supports_env() {
	/usr/bin/open --help 2>&1 | /usr/bin/grep -q -- '--env'
}

check_prereqs() {
	MISSING_PREREQS=""

	if [ "$(uname -s 2>/dev/null || printf unknown)" != "Darwin" ]; then
		add_missing_prereq "macOS/Darwin is required for the live desktop attach smoke"
	fi

	require_command go
	require_command node
	require_command sudo
	require_command id
	require_command mktemp

	if [ ! -x /usr/bin/open ]; then
		add_missing_prereq "/usr/bin/open is missing or not executable"
	elif ! open_supports_env; then
		add_missing_prereq "/usr/bin/open does not advertise --env support"
	fi
	if [ ! -x /bin/ps ]; then
		add_missing_prereq "/bin/ps is missing or not executable"
	fi
	if [ ! -x /usr/bin/awk ]; then
		add_missing_prereq "/usr/bin/awk is missing or not executable"
	fi
	if [ ! -x /usr/bin/grep ]; then
		add_missing_prereq "/usr/bin/grep is missing or not executable"
	fi
	if [ ! -d "$APP_BUNDLE" ]; then
		add_missing_prereq "$APP_BUNDLE is not installed"
	elif [ ! -x "$APP_BUNDLE/Contents/MacOS/Codex" ]; then
		add_missing_prereq "$APP_BUNDLE/Contents/MacOS/Codex is not executable"
	fi
	if [ ! -f "$REPO_ROOT/hazmat/go.mod" ]; then
		add_missing_prereq "$REPO_ROOT/hazmat/go.mod is missing; run from the Hazmat checkout"
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
	if command -v sudo >/dev/null 2>&1 && command -v id >/dev/null 2>&1 && id -u "$AGENT_USER" >/dev/null 2>&1; then
		if ! sudo -n -u "$AGENT_USER" /usr/bin/true >/dev/null 2>&1; then
			add_missing_prereq "passwordless non-interactive sudo to '$AGENT_USER' is unavailable"
		elif ! sudo -n -u "$AGENT_USER" test -x "$CODEX_BIN" >/dev/null 2>&1; then
			add_missing_prereq "Codex CLI is not installed for '$AGENT_USER' at $CODEX_BIN; run hazmat bootstrap codex"
		fi
	fi

	running="$(running_codex_processes || :)"
	if [ -n "$running" ]; then
		add_missing_prereq "Codex App appears to be running. Quit it manually before the live smoke:
$running"
	fi

	if [ -n "$MISSING_PREREQS" ]; then
		return 1
	fi
	return 0
}

print_missing_prereqs() {
	echo "codex-desktop-attach-smoke: live run is not currently safe:" >&2
	printf '%s\n' "$MISSING_PREREQS" >&2
}

cleanup() {
	if [ "$KEEP_SCRATCH" = "0" ] && [ -n "$SCRATCH" ]; then
		rm -rf "$SCRATCH"
	fi
}
trap cleanup EXIT INT TERM

make_proxy() {
	proxy="$1"
	cat >"$proxy" <<'NODE'
#!/usr/bin/env node
"use strict";

const fs = require("node:fs");
const readline = require("node:readline");
const { spawn } = require("node:child_process");

const logPath = process.env.HAZMAT_CODEX_DESKTOP_SMOKE_PROXY_LOG;
const stderrPath = process.env.HAZMAT_CODEX_DESKTOP_SMOKE_APP_SERVER_STDERR_LOG;
const hazmatBin = process.env.HAZMAT_CODEX_DESKTOP_SMOKE_HAZMAT_BIN;
const logParams = process.env.HAZMAT_CODEX_DESKTOP_SMOKE_LOG_PARAMS === "1";

function appendJSON(record) {
  const withTime = { time: new Date().toISOString(), ...record };
  fs.appendFileSync(logPath, `${JSON.stringify(withTime)}\n`, { mode: 0o600 });
}

function summarizeMessage(direction, message) {
  const summary = { event: "jsonrpc", direction };
  if (Object.prototype.hasOwnProperty.call(message, "id")) {
    summary.id = message.id;
  }
  if (typeof message.method === "string") {
    summary.method = message.method;
  }
  if (message.error) {
    summary.error = true;
    summary.errorCode = message.error.code;
    summary.errorMessage = message.error.message;
  }
  if (message.params && typeof message.params === "object" && !Array.isArray(message.params)) {
    summary.paramKeys = Object.keys(message.params).sort();
    if (logParams) {
      summary.params = message.params;
    }
  }
  return summary;
}

if (!logPath || !stderrPath || !hazmatBin) {
  process.stderr.write("hazmat desktop smoke proxy missing required environment\n");
  process.exit(127);
}

appendJSON({
  event: "invoke",
  pid: process.pid,
  ppid: process.ppid,
  uid: typeof process.getuid === "function" ? process.getuid() : null,
  cwd: process.cwd(),
  argv: process.argv.slice(2),
  env: {
    CODEX_CLI_PATH: process.env.CODEX_CLI_PATH,
    HAZMAT_CODEX_APP_SHIM_PROJECT: process.env.HAZMAT_CODEX_APP_SHIM_PROJECT,
    HAZMAT_CODEX_APP_SHIM_NETWORK: process.env.HAZMAT_CODEX_APP_SHIM_NETWORK,
    HAZMAT_CODEX_APP_SHIM_NO_BACKUP: process.env.HAZMAT_CODEX_APP_SHIM_NO_BACKUP,
    HAZMAT_CODEX_APP_SHIM_SKIP_ASSETS_SYNC: process.env.HAZMAT_CODEX_APP_SHIM_SKIP_ASSETS_SYNC,
  },
});

const child = spawn(hazmatBin, process.argv.slice(2), {
  env: process.env,
  stdio: ["pipe", "pipe", "pipe"],
});

child.on("spawn", () => {
  appendJSON({ event: "backend_spawned", pid: child.pid, hazmatBin });
});

child.on("exit", (code, signal) => {
  appendJSON({ event: "backend_exit", code, signal });
  if (typeof code === "number") {
    process.exitCode = code;
  }
});

child.on("error", (error) => {
  appendJSON({ event: "backend_error", message: error.message });
  process.exitCode = 127;
});

child.stderr.on("data", (chunk) => {
  fs.appendFileSync(stderrPath, chunk);
  process.stderr.write(chunk);
});

readline.createInterface({ input: process.stdin, crlfDelay: Infinity }).on("line", (line) => {
  if (line.trim() !== "") {
    try {
      appendJSON(summarizeMessage("app_to_backend", JSON.parse(line)));
    } catch (error) {
      appendJSON({ event: "non_json", direction: "app_to_backend", bytes: Buffer.byteLength(line) });
    }
  }
  child.stdin.write(`${line}\n`);
}).on("close", () => {
  child.stdin.end();
});

readline.createInterface({ input: child.stdout, crlfDelay: Infinity }).on("line", (line) => {
  if (line.trim() !== "") {
    try {
      appendJSON(summarizeMessage("backend_to_app", JSON.parse(line)));
    } catch (error) {
      appendJSON({ event: "non_json", direction: "backend_to_app", bytes: Buffer.byteLength(line) });
    }
  }
  process.stdout.write(`${line}\n`);
});
NODE
	chmod 700 "$proxy"
}

write_scratch_docs() {
	project="$1"
	cat >"$project/README-live-attach-smoke.md" <<'EOF'
# Hazmat Codex Desktop Attach Smoke Scratch Project

This project was created by `scripts/check-codex-desktop-attach-smoke.sh`.
Use it only for the explicit opt-in Codex desktop attach smoke.

Suggested manual prompt after the approved live launch:

Run a local smoke in this scratch project only: print the working directory,
write `desktop-live-smoke.txt`, read it back, run a standalone process if that
tool is available, and do not use the network.

After quitting Codex, inspect `proxy.jsonl` in the scratch directory. The log
should show whether the desktop app routed app-server methods such as
`command/exec`, `fs/readFile`, `fs/writeFile`, `fs/remove`, `process/spawn`,
and `thread/shellCommand` through the temporary Hazmat-backed proxy.
EOF
}

summarize_proxy_log() {
	log_path="$1"
	node - "$log_path" <<'NODE'
const fs = require("node:fs");
const path = process.argv[2];
const counts = new Map();
if (fs.existsSync(path)) {
  for (const line of fs.readFileSync(path, "utf8").split(/\n/)) {
    if (!line.trim()) continue;
    const record = JSON.parse(line);
    if (record.direction === "app_to_backend" && record.method) {
      counts.set(record.method, (counts.get(record.method) || 0) + 1);
    }
  }
}
if (counts.size === 0) {
  console.log("No app-to-backend JSON-RPC methods observed yet.");
} else {
  console.log("Observed app-to-backend JSON-RPC methods:");
  for (const [method, count] of [...counts.entries()].sort()) {
    console.log(`- ${method}: ${count}`);
  }
}
NODE
}

run_live_smoke() {
	if [ "$APPROVED" != "1" ]; then
		print_disclosure >&2
		echo >&2
		echo "codex-desktop-attach-smoke: --run requires --i-understand-this-may-launch-codex-app" >&2
		exit 2
	fi
	if ! check_prereqs; then
		print_missing_prereqs
		exit 2
	fi

	SCRATCH="$(mktemp -d /tmp/hazmat-codex-desktop-attach-smoke.XXXXXX)"
	PROJECT="$SCRATCH/project"
	LOG="$SCRATCH/proxy.jsonl"
	STDERR_LOG="$SCRATCH/app-server.stderr.log"
	PROXY="$SCRATCH/codex-cli-proxy"
	HAZMAT_BIN="$SCRATCH/hazmat"
	mkdir -p "$PROJECT"
	chmod 700 "$SCRATCH"
	chmod 755 "$PROJECT"
	: >"$LOG"
	: >"$STDERR_LOG"
	chmod 600 "$LOG" "$STDERR_LOG"
	write_scratch_docs "$PROJECT"
	make_proxy "$PROXY"

	echo "codex-desktop-attach-smoke: building scratch Hazmat binary..."
	(cd "$REPO_ROOT/hazmat" && go build -o "$HAZMAT_BIN" ./cmd/hazmat)
	chmod 700 "$HAZMAT_BIN"

	cat >"$SCRATCH/cleanup.sh" <<EOF
#!/bin/sh
set -eu
echo "Only run this after quitting the Codex app launched for the smoke."
rm -rf "$SCRATCH"
EOF
	chmod 700 "$SCRATCH/cleanup.sh"

	echo "codex-desktop-attach-smoke: launching Codex with temporary CODEX_CLI_PATH proxy..."
	/usr/bin/open -n -g \
		--env "CODEX_CLI_PATH=$PROXY" \
		--env "HAZMAT_CODEX_DESKTOP_SMOKE_PROXY_LOG=$LOG" \
		--env "HAZMAT_CODEX_DESKTOP_SMOKE_APP_SERVER_STDERR_LOG=$STDERR_LOG" \
		--env "HAZMAT_CODEX_DESKTOP_SMOKE_HAZMAT_BIN=$HAZMAT_BIN" \
		--env "HAZMAT_CODEX_APP_SHIM_PROJECT=$PROJECT" \
		--env "HAZMAT_CODEX_APP_SHIM_NETWORK=none" \
		--env "HAZMAT_CODEX_APP_SHIM_NO_BACKUP=true" \
		--env "HAZMAT_CODEX_APP_SHIM_SKIP_ASSETS_SYNC=true" \
		--env "NO_COLOR=1" \
		--env "HAZMAT_NO_UPDATE_CHECK=1" \
		"$APP_BUNDLE"

	KEEP_SCRATCH=1
	deadline=$(( $(date +%s) + WAIT_SECONDS ))
	while [ "$(date +%s)" -lt "$deadline" ]; do
		if [ -s "$LOG" ]; then
			break
		fi
		sleep 1
	done

	echo "codex-desktop-attach-smoke: scratch directory: $SCRATCH"
	echo "codex-desktop-attach-smoke: scratch project: $PROJECT"
	echo "codex-desktop-attach-smoke: proxy log: $LOG"
	echo "codex-desktop-attach-smoke: app-server stderr log: $STDERR_LOG"
	if [ -s "$LOG" ]; then
		echo "codex-desktop-attach-smoke: CODEX_CLI_PATH proxy was invoked."
		summarize_proxy_log "$LOG"
	else
		echo "codex-desktop-attach-smoke: CODEX_CLI_PATH proxy was not invoked within ${WAIT_SECONDS}s."
		echo "codex-desktop-attach-smoke: this falsifies immediate startup attach, but not lazy attach after UI interaction."
	fi
	echo "codex-desktop-attach-smoke: after quitting Codex, remove scratch state with: $SCRATCH/cleanup.sh"
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--dry-run)
			MODE="dry-run"
			;;
		--print-disclosure)
			MODE="disclosure"
			;;
		--check-prereqs)
			MODE="check"
			;;
		--run)
			MODE="run"
			;;
		--i-understand-this-may-launch-codex-app)
			APPROVED=1
			;;
		--wait-seconds)
			if [ "$#" -lt 2 ]; then
				echo "codex-desktop-attach-smoke: --wait-seconds requires a value" >&2
				exit 2
			fi
			WAIT_SECONDS="$2"
			shift
			;;
		--app)
			if [ "$#" -lt 2 ]; then
				echo "codex-desktop-attach-smoke: --app requires a value" >&2
				exit 2
			fi
			APP_BUNDLE="$2"
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "codex-desktop-attach-smoke: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

case "$WAIT_SECONDS" in
	''|*[!0-9]*)
		echo "codex-desktop-attach-smoke: --wait-seconds must be a positive integer" >&2
		exit 2
		;;
esac
if [ "$WAIT_SECONDS" -le 0 ]; then
	echo "codex-desktop-attach-smoke: --wait-seconds must be a positive integer" >&2
	exit 2
fi

case "$MODE" in
	dry-run)
		print_disclosure
		echo
		echo "Dry run only. To run after explicit approval:"
		echo "  scripts/check-codex-desktop-attach-smoke.sh --run --i-understand-this-may-launch-codex-app"
		;;
	disclosure)
		print_disclosure
		;;
	check)
		if ! check_prereqs; then
			print_missing_prereqs
			exit 2
		fi
		echo "codex-desktop-attach-smoke: prerequisites ok"
		;;
	run)
		run_live_smoke
		;;
	*)
		echo "codex-desktop-attach-smoke: internal error: unknown mode $MODE" >&2
		exit 2
		;;
esac
