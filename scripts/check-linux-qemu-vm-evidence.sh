#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
RUN=0
ACK=0
DISTRO=""
IMAGE_URL=""
IMAGE_SHA256=""
MODE="all"
GO_ROOT="${GOROOT:-}"
OUTPUT=""
SSH_PORT="${HAZMAT_LINUX_QEMU_SSH_PORT:-2222}"
SSH_TIMEOUT="${HAZMAT_LINUX_QEMU_SSH_TIMEOUT:-900}"
VM_MEMORY="${HAZMAT_LINUX_QEMU_MEMORY:-3072}"
VM_CPUS="${HAZMAT_LINUX_QEMU_CPUS:-2}"
TMP_ROOT=""
QEMU_PID=""

usage() {
	cat <<'EOF'
Usage:
  scripts/check-linux-qemu-vm-evidence.sh
  scripts/check-linux-qemu-vm-evidence.sh --run --i-understand-this-runs-linux-disposable-vm-evidence --distro debian|fedora|arch [options]

Default mode is disclosure-only. Live mode downloads distro cloud images, boots QEMU,
copies the current repository and Go toolchain into it, then runs the existing
Linux current-user and/or sudo-backed agent-user lifecycle evidence wrappers.

Options:
  --run                                                       Run the VM evidence harness.
  --i-understand-this-runs-linux-disposable-vm-evidence       Required acknowledgement for --run.
  --distro debian|fedora|arch                                 Distro image preset.
  --mode all|current-user|agent-user                          Evidence mode. Default: all.
  --image-url URL                                             Override distro image URL.
  --image-sha256 SHA256                                       Optional image checksum.
  --go-root DIR                                               Host Linux Go root to copy into the VM.
  --ssh-port PORT                                             Host SSH forwarding port. Default: 2222.
  --ssh-timeout SECONDS                                       SSH boot timeout. Default: 900.
  --output FILE                                               Write transcript to FILE as well as stdout.
  -h, --help                                                  Show this help.
EOF
}

while [ "$#" -gt 0 ]; do
	case "$1" in
		--run)
			RUN=1
			;;
		--i-understand-this-runs-linux-disposable-vm-evidence)
			ACK=1
			;;
		--distro)
			shift
			DISTRO="${1:-}"
			;;
		--mode)
			shift
			MODE="${1:-}"
			;;
		--image-url)
			shift
			IMAGE_URL="${1:-}"
			;;
		--image-sha256)
			shift
			IMAGE_SHA256="${1:-}"
			;;
		--go-root)
			shift
			GO_ROOT="${1:-}"
			;;
		--ssh-port)
			shift
			SSH_PORT="${1:-}"
			;;
		--ssh-timeout)
			shift
			SSH_TIMEOUT="${1:-}"
			;;
		--output)
			shift
			OUTPUT="${1:-}"
			;;
		--help|-h)
			usage
			exit 0
			;;
		*)
			echo "linux-qemu-vm-evidence: unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
	shift
done

if [ "$RUN" -ne 1 ]; then
	cat <<'EOF'
linux-qemu-vm-evidence: disclosure-only

This command boots disposable Debian/Fedora/Arch Linux VMs for:
  docs/linux-current-user-vm-smoke-matrix.md
  docs/linux-agent-user-vm-lifecycle-matrix.md
  sandboxing-xuar.3.5
  sandboxing-xuar.4.5

Live mode is approval-gated and downloads distro cloud images, boots QEMU,
forwards SSH to the VM, copies the repository and Go toolchain, and runs live
current-user and/or sudo-backed agent-user lifecycle evidence commands inside
the disposable VM.

Example:
  scripts/check-linux-qemu-vm-evidence.sh --run --i-understand-this-runs-linux-disposable-vm-evidence --distro debian --mode all --go-root "$(go env GOROOT)"
EOF
	exit 0
fi

if [ "$ACK" -ne 1 ]; then
	echo "linux-qemu-vm-evidence: refusing live run without --i-understand-this-runs-linux-disposable-vm-evidence" >&2
	exit 2
fi

if [ "$(uname -s)" != "Linux" ]; then
	echo "linux-qemu-vm-evidence: refusing live run outside Linux" >&2
	exit 2
fi

case "$MODE" in
	all|current-user|agent-user)
		;;
	*)
		echo "linux-qemu-vm-evidence: --mode must be all, current-user, or agent-user" >&2
		exit 2
		;;
esac

case "$DISTRO" in
	debian)
		: "${IMAGE_URL:=https://cloud.debian.org/images/cloud/bookworm/latest/debian-12-genericcloud-amd64.qcow2}"
		;;
	fedora)
		: "${IMAGE_URL:=https://download.fedoraproject.org/pub/fedora/linux/releases/44/Cloud/x86_64/images/Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2}"
		;;
	arch)
		: "${IMAGE_URL:=https://geo.mirror.pkgbuild.com/images/latest/Arch-Linux-x86_64-cloudimg.qcow2}"
		;;
	*)
		echo "linux-qemu-vm-evidence: --distro must be debian, fedora, or arch" >&2
		exit 2
		;;
esac

if [ -z "$GO_ROOT" ] && command -v go >/dev/null 2>&1; then
	GO_ROOT="$(go env GOROOT)"
fi
if [ -z "$GO_ROOT" ] || [ ! -d "$GO_ROOT" ]; then
	echo "linux-qemu-vm-evidence: --go-root must name a readable Linux Go root" >&2
	exit 2
fi

if [ -n "$OUTPUT" ]; then
	case "$OUTPUT" in
		/*)
			mkdir -p "$(dirname "$OUTPUT")"
			exec > >(tee "$OUTPUT") 2>&1
			;;
		*)
			echo "linux-qemu-vm-evidence: --output must be absolute" >&2
			exit 2
			;;
	esac
fi

for cmd in cloud-localds curl qemu-img qemu-system-x86_64 ssh ssh-keygen tar; do
	if ! command -v "$cmd" >/dev/null 2>&1; then
		echo "linux-qemu-vm-evidence: missing required command: $cmd" >&2
		exit 2
	fi
done

cleanup() {
	if [ -n "$QEMU_PID" ] && kill -0 "$QEMU_PID" >/dev/null 2>&1; then
		kill "$QEMU_PID" >/dev/null 2>&1 || true
		wait "$QEMU_PID" >/dev/null 2>&1 || true
	fi
	if [ -n "$TMP_ROOT" ]; then
		rm -rf "$TMP_ROOT"
	fi
}
trap cleanup EXIT HUP INT TERM

TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/hazmat-linux-qemu-vm.XXXXXX")"
IMAGE="$TMP_ROOT/image.qcow2"
DISK="$TMP_ROOT/disk.qcow2"
SEED="$TMP_ROOT/seed.iso"
SSH_KEY="$TMP_ROOT/id_ed25519"
CONSOLE="$TMP_ROOT/console.log"

echo "Linux disposable QEMU VM evidence"
echo "Date: $(date -u +%Y-%m-%d)"
echo "Commit: $(git -C "$REPO_ROOT" rev-parse HEAD)"
echo "Distro: $DISTRO"
echo "Mode: $MODE"
echo "Image: $IMAGE_URL"
echo "SSH port: $SSH_PORT"
echo "Go root: $GO_ROOT"
echo

echo "Download image:"
echo "1. curl -fsSL $IMAGE_URL -o $IMAGE"
curl -fsSL "$IMAGE_URL" -o "$IMAGE"

if [ -n "$IMAGE_SHA256" ]; then
	printf '%s  %s\n' "$IMAGE_SHA256" "$IMAGE" | sha256sum -c -
fi

qemu-img create -f qcow2 -F qcow2 -b "$IMAGE" "$DISK" >/dev/null
qemu-img resize "$DISK" 20G >/dev/null

ssh-keygen -q -t ed25519 -N '' -f "$SSH_KEY"
PUB_KEY="$(cat "$SSH_KEY.pub")"

cat >"$TMP_ROOT/user-data" <<EOF
#cloud-config
users:
  - name: runner
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    ssh_authorized_keys:
      - $PUB_KEY
package_update: true
packages:
  - sudo
ssh_pwauth: false
disable_root: false
growpart:
  mode: auto
  devices: ['/']
resize_rootfs: true
EOF

cat >"$TMP_ROOT/meta-data" <<EOF
instance-id: hazmat-$DISTRO-$(date +%s)
local-hostname: hazmat-$DISTRO
EOF

cloud-localds "$SEED" "$TMP_ROOT/user-data" "$TMP_ROOT/meta-data"

ACCEL_ARGS=(-accel tcg,thread=multi)
if [ -r /dev/kvm ] && [ -w /dev/kvm ]; then
	ACCEL_ARGS=(-enable-kvm)
fi

echo
echo "Boot VM:"
printf '1. qemu-system-x86_64 %s -m %s -smp %s ...\n' "${ACCEL_ARGS[*]}" "$VM_MEMORY" "$VM_CPUS"
qemu-system-x86_64 \
	"${ACCEL_ARGS[@]}" \
	-m "$VM_MEMORY" \
	-smp "$VM_CPUS" \
	-drive "file=$DISK,if=virtio,format=qcow2" \
	-drive "file=$SEED,if=virtio,format=raw,readonly=on" \
	-nic "user,model=virtio-net-pci,hostfwd=tcp:127.0.0.1:$SSH_PORT-:22" \
	-nographic >"$CONSOLE" 2>&1 &
QEMU_PID="$!"

SSH_OPTS=(
	-o BatchMode=yes
	-o StrictHostKeyChecking=no
	-o UserKnownHostsFile=/dev/null
	-o ConnectTimeout=5
	-i "$SSH_KEY"
	-p "$SSH_PORT"
)

deadline=$((SECONDS + SSH_TIMEOUT))
until ssh "${SSH_OPTS[@]}" runner@127.0.0.1 true >/dev/null 2>&1; do
	if [ "$SECONDS" -ge "$deadline" ]; then
		echo "linux-qemu-vm-evidence: timed out waiting for SSH" >&2
		echo "VM console tail:" >&2
		tail -200 "$CONSOLE" >&2 || true
		exit 1
	fi
	sleep 5
done

until ssh "${SSH_OPTS[@]}" runner@127.0.0.1 'command -v sudo >/dev/null 2>&1 && sudo -n true' >/dev/null 2>&1; do
	if [ "$SECONDS" -ge "$deadline" ]; then
		echo "linux-qemu-vm-evidence: timed out waiting for passwordless sudo" >&2
		echo "VM console tail:" >&2
		tail -200 "$CONSOLE" >&2 || true
		exit 1
	fi
	sleep 5
done

ssh "${SSH_OPTS[@]}" runner@127.0.0.1 'if command -v cloud-init >/dev/null 2>&1; then sudo timeout 300 cloud-init status --wait || true; fi'

echo
echo "Copy Go toolchain:"
ssh "${SSH_OPTS[@]}" runner@127.0.0.1 'sudo rm -rf /opt/hazmat-go && sudo mkdir -p /opt/hazmat-go && sudo chown runner:runner /opt/hazmat-go'
tar -C "$GO_ROOT" -cf - . | ssh "${SSH_OPTS[@]}" runner@127.0.0.1 'tar -C /opt/hazmat-go -xf -'

echo "Copy repository:"
ssh "${SSH_OPTS[@]}" runner@127.0.0.1 'rm -rf /home/runner/hazmat && mkdir -p /home/runner/hazmat'
tar -C "$REPO_ROOT" -cf - . | ssh "${SSH_OPTS[@]}" runner@127.0.0.1 'tar -C /home/runner/hazmat -xf -'

cat >"$TMP_ROOT/remote-run.sh" <<'EOF'
set -euo pipefail

export PATH="/opt/hazmat-go/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export HOME=/home/runner
export GOCACHE=/home/runner/.cache/go-build
export GOMODCACHE=/home/runner/go/pkg/mod
mkdir -p "$GOCACHE" "$GOMODCACHE"

install_deps() {
	if command -v apt-get >/dev/null 2>&1; then
		sudo apt-get update
		sudo DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
			acl ca-certificates coreutils findutils git passwd procps sudo
	elif command -v dnf >/dev/null 2>&1; then
		sudo dnf install -y \
			acl ca-certificates coreutils findutils git procps-ng shadow-utils sudo
	elif command -v pacman >/dev/null 2>&1; then
		sudo pacman -Sy --noconfirm --needed \
			acl ca-certificates coreutils findutils git procps-ng shadow sudo
	else
		echo "unsupported package manager" >&2
		exit 2
	fi
}

cd /home/runner/hazmat
git config --global --add safe.directory /home/runner/hazmat || true

echo "VM guest facts:"
echo "Distro:"
sed -n '1,12p' /etc/os-release 2>/dev/null || true
echo "Kernel: $(uname -srvmo 2>/dev/null || uname -a)"
echo "Arch: $(uname -m)"
echo "Go: $(go version)"
echo "Runner: ${HAZMAT_LINUX_VM_RUNNER}"
echo

echo "Install guest dependencies:"
install_deps

status=0
run_section() {
	echo
	echo "## $*"
	if "$@"; then
		echo "section result: pass"
	else
		rc=$?
		echo "section result: fail exit=$rc"
		status=$rc
	fi
}

case "$HAZMAT_VM_MODE" in
	all|current-user)
		run_section scripts/check-linux-vm-matrix-transcript.sh --mode current-user --run --skip-preflight
		run_section scripts/check-linux-current-user-live-smoke.sh --run --i-understand-this-runs-linux-current-user-live-smoke
		;;
esac

case "$HAZMAT_VM_MODE" in
	all|agent-user)
		run_section scripts/check-linux-vm-matrix-transcript.sh --mode agent-user --run --skip-preflight
		run_section scripts/check-linux-agent-user-lifecycle-smoke.sh --run --i-understand-this-runs-linux-agent-user-lifecycle-smoke
		;;
esac

exit "$status"
EOF

echo
echo "Run guest evidence commands:"
set +e
ssh "${SSH_OPTS[@]}" \
	runner@127.0.0.1 \
	"HAZMAT_LINUX_VM_RUNNER=qemu-$DISTRO HAZMAT_VM_MODE=$MODE bash -s" \
	<"$TMP_ROOT/remote-run.sh"
remote_status=$?
set -e

if [ "$remote_status" -ne 0 ]; then
	echo
	echo "VM console tail:"
	tail -200 "$CONSOLE" || true
	exit "$remote_status"
fi

echo
echo "VM evidence result: pass"
