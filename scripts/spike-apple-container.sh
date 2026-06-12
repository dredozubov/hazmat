#!/bin/sh
# Spike: Apple Container host behavior for Hazmat (bead sandboxing-ajmn).
#
# Measures the behavior the Apple Container backend depends on before any
# runtime implementation lands (docs/plans/2026-06-10-apple-container-backend-design.md):
#
#   1. container system version/status JSON shapes
#   2. running the container CLI as the dedicated macOS `agent` user
#   3. bind-mount ownership and write semantics from the guest
#   4. numeric UID/GID behavior with --user
#   5. internal network support and host gateway reachability
#   6. named-container cleanup semantics
#   7. whether any true no-network mode exists
#
# Prerequisites (manual, admin authority — Hazmat never does these itself):
#   - macOS 26+ on Apple silicon
#   - apple/container >= 1.0.0 installed (https://github.com/apple/container/releases)
#   - `container system start` already run by the installing user
#   - `hazmat init` completed (the `agent` user exists) for the agent-user probes
#
# The script only creates session-scoped temp state and exact-named
# containers prefixed hazmat-spike-; it never prunes, never touches
# credentials, and removes only what it created.

set -u

IMAGE="${SPIKE_IMAGE:-alpine:latest}"
RESULTS_DIR="${SPIKE_RESULTS_DIR:-$(pwd)/spike-apple-container-results}"
STAMP="$(date +%Y%m%d-%H%M%S)"
OUT="${RESULTS_DIR}/spike-${STAMP}.md"
NAME_RUN="hazmat-spike-run-$$"
NAME_NET="hazmat-spike-net-$$"
NET_NAME="hazmat-spike-internal-$$"
MODE="disclosure"
ACK_RUN=0

usage() {
  cat <<'EOF'
Usage:
  scripts/spike-apple-container.sh
  scripts/spike-apple-container.sh --run --i-understand-this-runs-apple-container-spike

Default mode is disclosure-only. Live mode runs Apple Container probes, including
container system/container run/container network operations and sudo -n -u agent
checks. Agents must ask for explicit approval before running --run.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --run)
      MODE="run"
      ;;
    --i-understand-this-runs-apple-container-spike)
      ACK_RUN=1
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      echo "spike-apple-container: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

if [ "$MODE" != "run" ]; then
  cat <<EOF
spike-apple-container: disclosure-only

This research spike runs live Apple Container probes, creates exact-named
containers/networks prefixed hazmat-spike-, bind-mounts a temporary host
directory, and uses sudo -n -u agent for agent-user separation checks.

To run it, ask for explicit approval for this exact command:

  scripts/spike-apple-container.sh --run --i-understand-this-runs-apple-container-spike
EOF
  exit 0
fi

if [ "$ACK_RUN" -ne 1 ]; then
  echo "spike-apple-container: refusing live run without --i-understand-this-runs-apple-container-spike" >&2
  exit 2
fi

WORKDIR="$(mktemp -d /tmp/hazmat-spike-XXXXXX)"

mkdir -p "${RESULTS_DIR}"

log() {
  printf '%s\n' "$*" | tee -a "${OUT}"
}

section() {
  log ""
  log "## $*"
  log ""
}

run_probe() {
  desc="$1"
  shift
  log ""
  log "### ${desc}"
  log ""
  log '```'
  log "\$ $*"
  "$@" 2>&1 | tee -a "${OUT}"
  status=$?
  log "(exit ${status})"
  log '```'
  return ${status}
}

cleanup() {
  container rm "${NAME_RUN}" >/dev/null 2>&1
  container rm "${NAME_NET}" >/dev/null 2>&1
  container network rm "${NET_NAME}" >/dev/null 2>&1
  rm -rf "${WORKDIR}"
}
trap cleanup EXIT INT TERM

log "# Apple Container spike results — ${STAMP}"
log ""
log "Host: $(sw_vers -productVersion) ($(uname -m)), image: ${IMAGE}"

if ! command -v container >/dev/null 2>&1; then
  log ""
  log "FATAL: container CLI not found. Install apple/container >= 1.0.0 first."
  exit 1
fi

section "1. System version/status JSON"
run_probe "container system version" container system version --format json
run_probe "container system status" container system status --format json

section "2. CLI as the dedicated agent user"
log "Registry/config state separation matters: the agent user must not see"
log "the invoking user's registry credentials."
run_probe "whoami as agent" sudo -n -u agent whoami
run_probe "container system status as agent" sudo -n -u agent container system status --format json
run_probe "container ls as agent" sudo -n -u agent container ls --format json

section "3. Bind-mount ownership and write semantics"
echo "host-written" > "${WORKDIR}/host-file.txt"
run_probe "guest reads host file, writes guest file, stats mount" \
  container run --name "${NAME_RUN}" \
  --mount "type=bind,source=${WORKDIR},target=${WORKDIR}" \
  "${IMAGE}" sh -c "cat ${WORKDIR}/host-file.txt && echo guest-written > ${WORKDIR}/guest-file.txt && ls -ln ${WORKDIR} && id"
run_probe "host-side ownership of guest-written file" ls -ln "${WORKDIR}"
run_probe "remove probe container by exact name" container rm "${NAME_RUN}"

section "4. Numeric UID/GID behavior (--user)"
run_probe "id and write as 502:20" \
  container run --rm --user 502:20 \
  --mount "type=bind,source=${WORKDIR},target=${WORKDIR}" \
  "${IMAGE}" sh -c "id && echo uid-502-write > ${WORKDIR}/uid-502-file.txt && ls -ln ${WORKDIR}/uid-502-file.txt"
run_probe "host-side ownership of uid-502 file" ls -ln "${WORKDIR}/uid-502-file.txt"

section "5. Internal network and host gateway reachability"
run_probe "create internal network" container network create --internal "${NET_NAME}"
run_probe "inspect networks" container network ls --format json
log ""
log "Host gateway probe: discussion #719 says host services on 0.0.0.0 stay"
log "reachable from internal networks. Verify before any deny-mode claim."
run_probe "route + gateway reachability from internal network" \
  container run --name "${NAME_NET}" --network "${NET_NAME}" \
  "${IMAGE}" sh -c "ip route; GW=\$(ip route | awk '/default/ {print \$3}'); echo gateway=\$GW; nc -z -w 3 \$GW 22; echo nc-gateway-22-exit=\$?; nc -z -w 3 1.1.1.1 443; echo nc-egress-exit=\$?"
run_probe "remove network probe container" container rm "${NAME_NET}"

section "6. Cleanup semantics"
run_probe "list remaining spike containers" sh -c "container ls --all --format json | grep -c hazmat-spike || true"
log "Anonymous volume check: upstream docs say anonymous volumes are not"
log "removed automatically with --rm. Hazmat plans no anonymous volumes."
run_probe "volume list" container volume ls --format json

section "7. No-network mode"
run_probe "container run --help network flags" sh -c "container run --help 2>&1 | grep -iA1 network || true"
run_probe "attempt --network none" \
  container run --rm --network none "${IMAGE}" sh -c "ip addr; nc -z -w 3 1.1.1.1 443; echo egress-exit=\$?"
run_probe "attempt no-dns on internal network" \
  container run --rm --network "${NET_NAME}" --no-dns "${IMAGE}" sh -c "nslookup example.com; echo dns-exit=\$?" \
  || true

section "Summary"
log "Record findings in the sandboxing-ajmn bead and update"
log "docs/plans/2026-06-10-apple-container-backend-design.md open questions:"
log "- VirtioFS ownership mapping when CLI runs as agent (sections 3, 4)"
log "- Registry/credential separation for the agent user (section 2)"
log "- Host gateway reachability from internal networks (section 5)"
log "- Whether a true no-network mode exists (section 7)"
log ""
log "Results: ${OUT}"
