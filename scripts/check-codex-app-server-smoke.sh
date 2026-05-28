#!/bin/sh

set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname "$0")" && pwd)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"

AGENT_USER="${HAZMAT_CODEX_APP_SERVER_SMOKE_AGENT_USER:-agent}"
AGENT_HOME="${HAZMAT_CODEX_APP_SERVER_SMOKE_AGENT_HOME:-/Users/agent}"
CODEX_BIN="$AGENT_HOME/.local/bin/codex"
LAUNCH_HELPER="${HAZMAT_CODEX_APP_SERVER_SMOKE_LAUNCH_HELPER:-/usr/local/libexec/hazmat-launch}"
MODE="run"
MISSING_PREREQS=""
SCRATCH=""
DENIED_DIR_CREATED=0

usage() {
	cat <<'EOF'
Usage: scripts/check-codex-app-server-smoke.sh [--check-prereqs|--skip-if-missing-prereqs]

Starts a short-lived Hazmat-contained `codex app-server --listen stdio://`
backend and validates JSON-RPC initialize, command execution, project file
access, filesystem mutation/removal, standalone process execution when exposed
by the installed app-server, credential path denial, thread shell command
execution, and --network none behavior.

Options:
  --check-prereqs           Only check local prerequisites; exit 0 when ready,
                            exit 2 with reasons when the machine is not ready.
  --skip-if-missing-prereqs Skip with exit 0 when prerequisites are missing.
  -h, --help                Show this help.

This smoke test never launches, quits, or attaches to the stock Codex desktop
app. It uses a scratch project and a fake agent-owned credential probe.
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
			echo "codex-app-server-smoke: unknown argument: $1" >&2
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
		add_missing_prereq "macOS/Darwin is required for the native seatbelt app-server smoke"
	fi

	require_command go
	require_command node
	require_command sudo
	require_command id

	if [ ! -x /usr/bin/nc ]; then
		add_missing_prereq "/usr/bin/nc is required for the network-none probe"
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
		elif ! sudo -n -u "$AGENT_USER" test -x "$CODEX_BIN" >/dev/null 2>&1; then
			add_missing_prereq "Codex CLI is not installed for '$AGENT_USER' at $CODEX_BIN; run hazmat bootstrap codex"
		fi
	fi

	if [ -n "$MISSING_PREREQS" ]; then
		return 1
	fi
	return 0
}

print_missing_prereqs() {
	echo "codex-app-server-smoke: missing prerequisites:" >&2
	printf '%s\n' "$MISSING_PREREQS" >&2
}

if ! check_prereqs; then
	if [ "$MODE" = "skip" ]; then
		echo "codex-app-server-smoke: skipped because prerequisites are missing" >&2
		print_missing_prereqs
		exit 0
	fi
	print_missing_prereqs
	exit 2
fi

if [ "$MODE" = "check" ]; then
	echo "codex-app-server-smoke: prerequisites ok"
	exit 0
fi

SCRATCH="$(mktemp -d /tmp/hazmat-codex-app-server-smoke.XXXXXX)"
PROJECT="$SCRATCH/project"
ALLOWED_FILE="$PROJECT/allowed.txt"
DENIED_DIR="$AGENT_HOME/.ssh"
DENIED_FILE="$DENIED_DIR/hazmat-app-server-smoke.$$"

cleanup() {
	if [ -n "$SCRATCH" ]; then
		rm -rf "$SCRATCH"
	fi
	sudo -n -u "$AGENT_USER" rm -f "$DENIED_FILE" 2>/dev/null || :
	if [ "$DENIED_DIR_CREATED" = "1" ]; then
		sudo -n -u "$AGENT_USER" rmdir "$DENIED_DIR" 2>/dev/null || :
	fi
}
trap cleanup EXIT INT TERM

mkdir -p "$PROJECT"
chmod 755 "$SCRATCH" "$PROJECT"
printf 'allowed from project\n' >"$ALLOWED_FILE"
chmod 644 "$ALLOWED_FILE"

if [ ! -d "$DENIED_DIR" ]; then
	sudo -n -u "$AGENT_USER" mkdir -p "$DENIED_DIR"
	DENIED_DIR_CREATED=1
fi
sudo -n -u "$AGENT_USER" /bin/sh -c 'umask 077; printf "%s\n" "fake smoke-test credential" >"$1"' sh "$DENIED_FILE"

node - "$REPO_ROOT" "$PROJECT" "$ALLOWED_FILE" "$DENIED_FILE" <<'NODE'
const { spawn } = require("node:child_process");
const readline = require("node:readline");
const path = require("node:path");

const [repoRoot, projectDir, allowedFile, deniedFile] = process.argv.slice(2);
const hazmatDir = path.join(repoRoot, "hazmat");

const child = spawn("go", [
  "run",
  ".",
  "codex",
  "--no-backup",
  "--skip-harness-assets-sync",
  "--network",
  "none",
  "-C",
  projectDir,
  "app-server",
  "--listen",
  "stdio://",
], {
  cwd: hazmatDir,
  stdio: ["pipe", "pipe", "pipe"],
  env: {
    ...process.env,
    NO_COLOR: "1",
    HAZMAT_NO_UPDATE_CHECK: "1",
  },
});

let nextId = 1;
let stderr = "";
let exited = false;
const pending = new Map();
const notificationWaiters = [];
const notificationsSeen = [];

child.stderr.setEncoding("utf8");
child.stderr.on("data", (chunk) => {
  stderr += chunk;
});

child.on("exit", (code, signal) => {
  exited = true;
  for (const { reject, timer, method } of pending.values()) {
    clearTimeout(timer);
    reject(new Error(`app-server exited before ${method} response: code=${code} signal=${signal}\n${stderr}`));
  }
  pending.clear();
  for (const { reject, timer, method } of notificationWaiters.splice(0)) {
    clearTimeout(timer);
    reject(new Error(`app-server exited before ${method} notification: code=${code} signal=${signal}\n${stderr}`));
  }
});

const lines = readline.createInterface({ input: child.stdout, crlfDelay: Infinity });
lines.on("line", (line) => {
  if (line.trim() === "") {
    return;
  }
  let message;
  try {
    message = JSON.parse(line);
  } catch (error) {
    fail(`non-JSON stdout from app-server: ${line}\n${stderr}`);
  }
  if (Object.prototype.hasOwnProperty.call(message, "id")) {
    const waiter = pending.get(message.id);
    if (!waiter) {
      return;
    }
    pending.delete(message.id);
    clearTimeout(waiter.timer);
    if (message.error) {
      const rpcError = new Error(message.error.message || JSON.stringify(message.error));
      rpcError.rpcError = message.error;
      waiter.reject(rpcError);
    } else {
      waiter.resolve(message.result);
    }
    return;
  }
  if (typeof message.method === "string") {
    dispatchNotification(message.method, message.params);
  }
});

function fail(message) {
  try {
    child.kill("SIGTERM");
  } catch (_) {
    // best effort
  }
  throw new Error(message);
}

function send(message) {
  if (exited) {
    throw new Error(`app-server already exited\n${stderr}`);
  }
  child.stdin.write(`${JSON.stringify(message)}\n`);
}

function request(method, params, timeoutMs = 20000) {
  const id = nextId++;
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      pending.delete(id);
      reject(new Error(`timeout waiting for ${method}\n${stderr}`));
    }, timeoutMs);
    pending.set(id, { resolve, reject, timer, method });
    send({ method, id, params });
  });
}

function waitForNotification(method, predicate, timeoutMs = 20000) {
  const matches = predicate || (() => true);
  const seen = notificationsSeen.find((notification) => (
    notification.method === method && matches(notification.params)
  ));
  if (seen) {
    return Promise.resolve(seen.params);
  }
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      const index = notificationWaiters.findIndex((waiter) => waiter.resolve === resolve);
      if (index !== -1) {
        notificationWaiters.splice(index, 1);
      }
      reject(new Error(`timeout waiting for ${method} notification\n${stderr}`));
    }, timeoutMs);
    notificationWaiters.push({
      method,
      predicate: matches,
      resolve,
      reject,
      timer,
    });
  });
}

function dispatchNotification(method, params) {
  notificationsSeen.push({ method, params });
  for (let i = 0; i < notificationWaiters.length; i += 1) {
    const waiter = notificationWaiters[i];
    if (waiter.method !== method) {
      continue;
    }
    let matches = false;
    try {
      matches = waiter.predicate(params);
    } catch (error) {
      notificationWaiters.splice(i, 1);
      clearTimeout(waiter.timer);
      waiter.reject(error);
      return;
    }
    if (matches) {
      notificationWaiters.splice(i, 1);
      clearTimeout(waiter.timer);
      waiter.resolve(params);
      return;
    }
  }
}

function isUnsupportedMethodError(error, method) {
  const message = String(error?.message || "");
  return (
    message.includes(`unknown variant \`${method}\``) ||
    message.includes(`unknown method ${method}`) ||
    message.includes(`Unknown method ${method}`) ||
    /method not found|unsupported method/i.test(message)
  );
}

function notify(method, params) {
  const message = params === undefined ? { method } : { method, params };
  send(message);
}

async function stopServer() {
  child.stdin.end();
  if (!exited) {
    child.kill("SIGTERM");
  }
  await new Promise((resolve) => {
    if (exited) {
      resolve();
      return;
    }
    const timer = setTimeout(resolve, 3000);
    child.once("exit", () => {
      clearTimeout(timer);
      resolve();
    });
  });
}

async function main() {
  const initialize = await request("initialize", {
    clientInfo: {
      name: "hazmat_app_server_smoke",
      title: "Hazmat app-server smoke",
      version: "0.0.0",
    },
    capabilities: {
      experimentalApi: true,
      optOutNotificationMethods: ["remoteControl/status/changed"],
    },
  }, 30000);
  if (!initialize || typeof initialize.codexHome !== "string") {
    fail(`initialize response missing codexHome: ${JSON.stringify(initialize)}`);
  }
  notify("initialized");

  const command = await request("command/exec", {
    command: ["/bin/sh", "-lc", "printf command-ok"],
    cwd: projectDir,
    sandboxPolicy: { type: "externalSandbox", networkAccess: "restricted" },
    timeoutMs: 10000,
  });
  if (command.exitCode !== 0 || command.stdout !== "command-ok") {
    fail(`command/exec unexpected result: ${JSON.stringify(command)}`);
  }

  const allowed = await request("fs/readFile", { path: allowedFile });
  const allowedText = Buffer.from(allowed.dataBase64, "base64").toString("utf8");
  if (allowedText !== "allowed from project\n") {
    fail(`fs/readFile allowed content mismatch: ${JSON.stringify(allowedText)}`);
  }

  const writtenFile = path.join(projectDir, "fs-write-remove.txt");
  const writtenText = "written by fs/writeFile\n";
  await request("fs/writeFile", {
    path: writtenFile,
    dataBase64: Buffer.from(writtenText, "utf8").toString("base64"),
  });
  const written = await request("fs/readFile", { path: writtenFile });
  const readBackWrittenText = Buffer.from(written.dataBase64, "base64").toString("utf8");
  if (readBackWrittenText !== writtenText) {
    fail(`fs/writeFile read-back mismatch: ${JSON.stringify(readBackWrittenText)}`);
  }
  await request("fs/remove", { path: writtenFile, recursive: false, force: false });
  let removedReadError = null;
  try {
    await request("fs/readFile", { path: writtenFile }, 10000);
  } catch (error) {
    removedReadError = error;
  }
  if (!removedReadError || !removedReadError.rpcError) {
    fail("fs/remove did not remove the project file");
  }

  let deniedError = null;
  try {
    await request("fs/readFile", { path: deniedFile }, 10000);
  } catch (error) {
    deniedError = error;
  }
  if (!deniedError || !deniedError.rpcError) {
    fail("fs/readFile unexpectedly succeeded for credential-deny path");
  }
  if (!/not permitted|permission denied|deny/i.test(deniedError.rpcError.message || "")) {
    fail(`fs/readFile credential probe did not fail with a sandbox denial: ${deniedError.rpcError.message}`);
  }

  let processSpawnStatus = "unsupported";
  let deniedProcessExitCode = null;
  const processHandle = `hazmat-smoke-process-${Date.now()}`;
  try {
    await request("process/spawn", {
      command: ["/bin/sh", "-lc", "printf process-ok"],
      processHandle,
      cwd: projectDir,
      outputBytesCap: 1048576,
      timeoutMs: 6000,
    }, 10000);
  } catch (error) {
    if (!isUnsupportedMethodError(error, "process/spawn")) {
      throw error;
    }
    processSpawnStatus = "unsupported by installed app-server";
  }
  if (processSpawnStatus === "unsupported") {
    const processResult = await waitForNotification(
      "process/exited",
      (params) => params && params.processHandle === processHandle,
      12000,
    );
    if (processResult.exitCode !== 0 || processResult.stdout !== "process-ok") {
      fail(`process/spawn unexpected result: ${JSON.stringify(processResult)}`);
    }
    processSpawnStatus = "ok";

    const deniedProcessHandle = `hazmat-smoke-denied-process-${Date.now()}`;
    await request("process/spawn", {
      command: ["/bin/sh", "-lc", "cat \"$DENIED_FILE\""],
      processHandle: deniedProcessHandle,
      cwd: projectDir,
      env: { DENIED_FILE: deniedFile },
      outputBytesCap: 1048576,
      timeoutMs: 6000,
    }, 10000);
    const deniedProcessResult = await waitForNotification(
      "process/exited",
      (params) => params && params.processHandle === deniedProcessHandle,
      12000,
    );
    deniedProcessExitCode = deniedProcessResult.exitCode;
    if (deniedProcessResult.exitCode === 0) {
      fail("process/spawn unexpectedly read credential-deny path");
    }
    if (!/not permitted|permission denied|deny/i.test(`${deniedProcessResult.stdout}\n${deniedProcessResult.stderr}`)) {
      fail(`process/spawn credential probe did not fail with a sandbox denial: ${JSON.stringify(deniedProcessResult)}`);
    }
  }

  const threadStarted = await request("thread/start", {
    cwd: projectDir,
    approvalPolicy: "never",
    sandbox: "read-only",
    ephemeral: true,
    serviceName: "hazmat_app_server_smoke",
    sessionStartSource: "startup",
  }, 10000);
  const threadId = threadStarted?.thread?.id || threadStarted?.id;
  if (typeof threadId !== "string" || threadId === "") {
    fail(`thread/start response missing thread id: ${JSON.stringify(threadStarted)}`);
  }
  const shellFile = path.join(projectDir, "thread-shell-command.txt");
  const shellCompleted = waitForNotification(
    "turn/completed",
    (params) => params && params.threadId === threadId,
    20000,
  );
  await request("thread/shellCommand", {
    threadId,
    command: "printf thread-shell-ok > thread-shell-command.txt",
  }, 10000);
  await shellCompleted;
  const shellRead = await request("fs/readFile", { path: shellFile });
  const shellText = Buffer.from(shellRead.dataBase64, "base64").toString("utf8");
  if (shellText !== "thread-shell-ok") {
    fail(`thread/shellCommand side-effect mismatch: ${JSON.stringify(shellText)}`);
  }
  await request("fs/remove", { path: shellFile, recursive: false, force: false });

  const network = await request("command/exec", {
    command: ["/bin/sh", "-lc", "/usr/bin/nc -G 2 -z 1.1.1.1 443"],
    cwd: projectDir,
    sandboxPolicy: { type: "externalSandbox", networkAccess: "enabled" },
    timeoutMs: 6000,
  }, 12000);
  if (network.exitCode === 0) {
    fail("network command unexpectedly succeeded under hazmat --network none");
  }

  console.log("codex-app-server-smoke: initialize ok");
  console.log("codex-app-server-smoke: command/exec ok");
  console.log("codex-app-server-smoke: fs/readFile project allow ok");
  console.log("codex-app-server-smoke: fs/writeFile and fs/remove project mutation ok");
  console.log(`codex-app-server-smoke: fs/readFile credential path denied: ${deniedError.rpcError.message}`);
  if (processSpawnStatus === "ok") {
    console.log("codex-app-server-smoke: process/spawn ok");
    console.log(`codex-app-server-smoke: process/spawn credential path denied with exit ${deniedProcessExitCode}`);
  } else {
    console.log(`codex-app-server-smoke: process/spawn skipped: ${processSpawnStatus}`);
  }
  console.log("codex-app-server-smoke: thread/shellCommand ok");
  console.log(`codex-app-server-smoke: network none denied with exit ${network.exitCode}`);
}

main()
  .finally(stopServer)
  .catch((error) => {
    console.error(error.stack || String(error));
    if (stderr) {
      console.error("--- app-server stderr ---");
      console.error(stderr.trimEnd());
    }
    process.exitCode = 1;
  });
NODE
