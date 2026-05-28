#!/bin/sh

set -eu

REPO_ROOT="$(git rev-parse --show-toplevel)"
SCRATCH="$(mktemp -d /tmp/hazmat-codex-app-server-smoke.XXXXXX)"
PROJECT="$SCRATCH/project"
ALLOWED_FILE="$PROJECT/allowed.txt"
DENIED_DIR="/Users/agent/.ssh"
DENIED_FILE="$DENIED_DIR/hazmat-app-server-smoke.$$"
DENIED_DIR_CREATED=0

cleanup() {
	rm -rf "$SCRATCH"
	sudo -n -u agent rm -f "$DENIED_FILE" 2>/dev/null || :
	if [ "$DENIED_DIR_CREATED" = "1" ]; then
		sudo -n -u agent rmdir "$DENIED_DIR" 2>/dev/null || :
	fi
}
trap cleanup EXIT INT TERM

mkdir -p "$PROJECT"
chmod 755 "$SCRATCH" "$PROJECT"
printf 'allowed from project\n' >"$ALLOWED_FILE"
chmod 644 "$ALLOWED_FILE"

if [ ! -d "$DENIED_DIR" ]; then
	sudo -n -u agent mkdir -p "$DENIED_DIR"
	DENIED_DIR_CREATED=1
fi
sudo -n -u agent /bin/sh -c 'umask 077; printf "%s\n" "fake smoke-test credential" >"$1"' sh "$DENIED_FILE"

if ! command -v node >/dev/null 2>&1; then
	echo "codex-app-server-smoke: node is required" >&2
	exit 1
fi

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
  console.log(`codex-app-server-smoke: fs/readFile credential path denied: ${deniedError.rpcError.message}`);
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
