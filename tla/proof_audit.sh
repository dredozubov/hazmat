#!/bin/sh

set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
ROOT_DIR="$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)"
TLA_DIR="${ROOT_DIR}/tla"
ROSTER="${TLA_DIR}/promoted_specs.tsv"
LOG_DIR=""
FAIL_ON_DRIFT=0
EXPECTED_PROMOTED_SPEC_COUNT=15

usage() {
  cat <<'EOF'
Usage: proof_audit.sh [--fail-on-drift] [--log-dir DIR]

Inventories Hazmat's promoted TLA+ proof base and, when TLC logs are supplied,
summarizes generated/distinct/depth counters for regression comparison.
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --fail-on-drift)
      FAIL_ON_DRIFT=1
      ;;
    --log-dir)
      shift
      if [ "$#" -eq 0 ]; then
        echo "error: --log-dir requires a directory" >&2
        exit 2
      fi
      LOG_DIR="$1"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
  shift
done

if [ -n "$LOG_DIR" ] && [ ! -d "$LOG_DIR" ]; then
  echo "error: log directory not found: $LOG_DIR" >&2
  exit 2
fi

tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/hazmat-proof-audit.XXXXXX")"
cleanup() {
  rm -rf "$tmpdir"
}
trap cleanup EXIT INT TERM

bytes_for() {
  if [ ! -f "$1" ]; then
    printf 'n/a'
    return 0
  fi
  wc -c < "$1" | tr -d ' '
}

gzip_bytes_for() {
  if [ ! -f "$1" ]; then
    printf 'n/a'
    return 0
  fi
  if command -v gzip >/dev/null 2>&1; then
    gzip -9 -c "$1" | wc -c | tr -d ' '
  else
    printf 'n/a'
  fi
}

sha256_for() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    printf 'n/a'
  fi
}

find_java() {
  if [ -n "${JAVA_BIN:-}" ] && [ -x "${JAVA_BIN}" ]; then
    printf '%s\n' "${JAVA_BIN}"
    return 0
  fi

  if [ -n "${JAVA_HOME:-}" ] && [ -x "${JAVA_HOME}/bin/java" ] \
    && [ "${JAVA_HOME}/bin/java" != "/usr/bin/java" ]; then
    printf '%s\n' "${JAVA_HOME}/bin/java"
    return 0
  fi

  if command -v java >/dev/null 2>&1; then
    candidate="$(command -v java)"
    if [ "${candidate}" != "/usr/bin/java" ]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  fi

  for candidate in \
    "$HOME"/java/*/Contents/Home/bin/java \
    /Library/Java/JavaVirtualMachines/*/Contents/Home/bin/java \
    /opt/homebrew/opt/openjdk/bin/java \
    /usr/local/opt/openjdk/bin/java
  do
    if [ -x "${candidate}" ]; then
      printf '%s\n' "${candidate}"
      return 0
    fi
  done

  if [ -x /usr/libexec/java_home ]; then
    candidate_home="$(/usr/libexec/java_home -v 17+ 2>/dev/null || true)"
    if [ -n "${candidate_home}" ] && [ -x "${candidate_home}/bin/java" ]; then
      printf '%s\n' "${candidate_home}/bin/java"
      return 0
    fi
  fi

  return 1
}

join_lines() {
  awk '
    BEGIN { first = 1 }
    NF {
      if (!first) {
        printf ","
      }
      printf "%s", $0
      first = 0
    }
    END {
      if (first) {
        printf "-"
      }
      printf "\n"
    }
  '
}

cfg_items() {
  cfg="$1"
  wanted="$2"
  awk -v wanted="$wanted" '
    function trim(s) {
      sub(/\\\*.*/, "", s)
      gsub(/^[ \t]+/, "", s)
      gsub(/[ \t]+$/, "", s)
      return s
    }
    function emit_rest(start,    i, out) {
      out = ""
      for (i = start; i <= NF; i++) {
        out = out (out == "" ? "" : " ") $i
      }
      out = trim(out)
      if (out != "") {
        print out
      }
    }
    /^[ \t]*INVARIANTS([ \t]|$)/ {
      mode = "invariants"
      if (wanted == "invariants") {
        emit_rest(2)
      }
      next
    }
    /^[ \t]*PROPERTIES([ \t]|$)/ {
      mode = "properties"
      if (wanted == "properties") {
        emit_rest(2)
      }
      next
    }
    /^[ \t]*PROPERTY[ \t]+/ {
      if (wanted == "properties") {
        emit_rest(2)
      }
      mode = ""
      next
    }
    /^[ \t]*(SPECIFICATION|CONSTANTS?|INIT|NEXT|SYMMETRY|CHECK_DEADLOCK)([ \t]|$)/ {
      mode = ""
      next
    }
    {
      line = trim($0)
      if (line == "" || substr(line, 1, 2) == "\\*") {
        next
      }
      if (mode == wanted) {
        print line
      }
    }
  ' "$cfg" | join_lines
}

find_log_for_spec() {
  spec="$1"
  [ -n "$LOG_DIR" ] || return 1

  if [ -f "${LOG_DIR}/${spec}.log" ]; then
    printf '%s\n' "${LOG_DIR}/${spec}.log"
    return 0
  fi

  find "$LOG_DIR" -type f -name "*${spec}*.log" -print | sort | sed -n '1p'
}

number_from_pattern() {
  pattern="$1"
  log="$2"
  grep -Eo "$pattern" "$log" 2>/dev/null \
    | tail -n 1 \
    | tr -d ',' \
    | awk '{print $1}'
}

depth_from_log() {
  log="$1"
  awk '
    /depth of the complete state graph search is/ {
      for (i = 1; i <= NF; i++) {
        if ($i ~ /^[0-9]+\.?$/) {
          value = $i
          gsub(/\./, "", value)
        }
      }
    }
    END {
      if (value == "") {
        print "n/a"
      } else {
        print value
      }
    }
  ' "$log"
}

result_from_log() {
  log="$1"
  if grep -q "No error has been found" "$log"; then
    printf 'pass'
  elif grep -q "Error:" "$log"; then
    printf 'fail'
  else
    printf 'unknown'
  fi
}

duration_from_log() {
  log="$1"
  grep -E '^Finished in ' "$log" \
    | tail -n 1 \
    | sed 's/[[:space:]]\+/ /g; s/ at .*//' \
    || true
}

[ -f "$ROSTER" ] || {
  echo "error: missing promoted spec roster: $ROSTER" >&2
  exit 2
}

awk -F '\t' '
  BEGIN {
    expected_header = "spec\tliveness"
    status = 0
  }
  NR == 1 {
    if ($0 != expected_header) {
      printf "error: bad promoted spec roster header in %s\n", FILENAME > "/dev/stderr"
      status = 1
    }
    next
  }
  NF == 0 { next }
  NF != 2 {
    printf "error: bad promoted spec roster column count at %s:%d\n", FILENAME, NR > "/dev/stderr"
    status = 1
    next
  }
  $1 !~ /^MC_[A-Za-z0-9_]+$/ {
    printf "error: bad promoted spec name at %s:%d: %s\n", FILENAME, NR, $1 > "/dev/stderr"
    status = 1
  }
  $2 != "yes" && $2 != "no" {
    printf "error: bad liveness value at %s:%d: %s\n", FILENAME, NR, $2 > "/dev/stderr"
    status = 1
  }
  {
    print $1 "\t" $2
  }
  END {
    exit status
  }
' "$ROSTER" > "${tmpdir}/roster.unsorted.tsv"
sort "${tmpdir}/roster.unsorted.tsv" > "${tmpdir}/roster.tsv"
cut -f1 "${tmpdir}/roster.tsv" > "${tmpdir}/rostered_specs.txt"

roster_duplicates="$(uniq -d "${tmpdir}/rostered_specs.txt")"
if [ -n "$roster_duplicates" ]; then
  printf '%s\n' "$roster_duplicates" >&2
  echo "error: duplicate specs in promoted spec roster" >&2
  exit 2
fi

roster_count="$(wc -l < "${tmpdir}/rostered_specs.txt" | tr -d ' ')"
if [ "$roster_count" -ne "$EXPECTED_PROMOTED_SPEC_COUNT" ]; then
  echo "error: promoted spec roster lists ${roster_count} specs, expected ${EXPECTED_PROMOTED_SPEC_COUNT}" >&2
  exit 2
fi

awk '
  /^[ \t]*run_spec[ \t]+MC_/ {
    spec = $2
    liveness = $3
    gsub(/"/, "", spec)
    gsub(/"/, "", liveness)
    print spec "\t" liveness
  }
' "${TLA_DIR}/check_suite.sh" | sort > "${tmpdir}/promoted.tsv"

find "$TLA_DIR" -maxdepth 1 -type f -name 'MC_*.tla' ! -name '*_TTrace_*.tla' \
  | sed 's#.*/##; s#\.tla$##' \
  | sort > "${tmpdir}/tla_specs.txt"

find "$TLA_DIR" -maxdepth 1 -type f -name 'MC_*.cfg' \
  | sed 's#.*/##; s#\.cfg$##' \
  | sort > "${tmpdir}/cfg_specs.txt"

cut -f1 "${tmpdir}/promoted.tsv" > "${tmpdir}/promoted_specs.txt"

echo "# Hazmat TLA Proof Audit"
echo
echo "## Toolchain"
java_bin="$(find_java || true)"
if [ -n "$java_bin" ]; then
  java_version="$("$java_bin" -version 2>&1 | sed -n '1p')"
  printf 'java_bin: %s\n' "$java_bin"
else
  java_version="not found"
fi
printf 'java: %s\n' "$java_version"

TLA2TOOLS_JAR="${TLA2TOOLS_JAR:-$HOME/workspace/tla2tools.jar}"
if [ -f "$TLA2TOOLS_JAR" ]; then
  printf 'tla2tools_jar: %s\n' "$TLA2TOOLS_JAR"
  printf 'tla2tools_sha256: %s\n' "$(sha256_for "$TLA2TOOLS_JAR")"
else
  printf 'tla2tools_jar: missing (%s)\n' "$TLA2TOOLS_JAR"
fi
echo

echo "## Inventory"
printf 'rostered_specs: %s\n' "$roster_count"
printf 'promoted_specs: %s\n' "$(wc -l < "${tmpdir}/promoted_specs.txt" | tr -d ' ')"
printf 'mc_tla_specs: %s\n' "$(wc -l < "${tmpdir}/tla_specs.txt" | tr -d ' ')"
printf 'mc_cfg_specs: %s\n' "$(wc -l < "${tmpdir}/cfg_specs.txt" | tr -d ' ')"
printf 'local_trace_artifacts: %s\n' "$(find "$TLA_DIR" -maxdepth 1 -type f \( -name '*_TTrace_*.tla' -o -name '*_TTrace_*.bin' \) | wc -l | tr -d ' ')"
trace_bytes="$(find "$TLA_DIR" -maxdepth 1 -type f \( -name '*_TTrace_*.tla' -o -name '*_TTrace_*.bin' \) -exec wc -c {} + 2>/dev/null | awk '$NF != "total" {sum += $1} END {print sum + 0}')"
printf 'local_trace_bytes: %s\n' "${trace_bytes:-0}"
echo

echo "## Drift Checks"
missing_tla="$(comm -23 "${tmpdir}/promoted_specs.txt" "${tmpdir}/tla_specs.txt" | join_lines)"
missing_cfg="$(comm -23 "${tmpdir}/promoted_specs.txt" "${tmpdir}/cfg_specs.txt" | join_lines)"
unpromoted_tla="$(comm -13 "${tmpdir}/promoted_specs.txt" "${tmpdir}/tla_specs.txt" | join_lines)"
unpromoted_cfg="$(comm -13 "${tmpdir}/promoted_specs.txt" "${tmpdir}/cfg_specs.txt" | join_lines)"
roster_without_promoted="$(comm -23 "${tmpdir}/rostered_specs.txt" "${tmpdir}/promoted_specs.txt" | join_lines)"
promoted_without_roster="$(comm -13 "${tmpdir}/rostered_specs.txt" "${tmpdir}/promoted_specs.txt" | join_lines)"
rostered_without_tla="$(comm -23 "${tmpdir}/rostered_specs.txt" "${tmpdir}/tla_specs.txt" | join_lines)"
rostered_without_cfg="$(comm -23 "${tmpdir}/rostered_specs.txt" "${tmpdir}/cfg_specs.txt" | join_lines)"
liveness_drift="$(
  awk -F '\t' '
    NR == FNR {
      want[$1] = $2
      next
    }
    ($1 in want) && want[$1] != $2 {
      print $1
    }
  ' "${tmpdir}/roster.tsv" "${tmpdir}/promoted.tsv" | join_lines
)"
printf 'roster_without_promoted: %s\n' "$roster_without_promoted"
printf 'promoted_without_roster: %s\n' "$promoted_without_roster"
printf 'rostered_without_tla: %s\n' "$rostered_without_tla"
printf 'rostered_without_cfg: %s\n' "$rostered_without_cfg"
printf 'roster_liveness_drift: %s\n' "$liveness_drift"
printf 'promoted_without_tla: %s\n' "$missing_tla"
printf 'promoted_without_cfg: %s\n' "$missing_cfg"
printf 'unpromoted_tla_specs: %s\n' "$unpromoted_tla"
printf 'unpromoted_cfg_specs: %s\n' "$unpromoted_cfg"
echo

if [ "$FAIL_ON_DRIFT" -eq 1 ]; then
  if [ "$missing_tla" != "-" ] \
    || [ "$missing_cfg" != "-" ] \
    || [ "$unpromoted_tla" != "-" ] \
    || [ "$unpromoted_cfg" != "-" ] \
    || [ "$roster_without_promoted" != "-" ] \
    || [ "$promoted_without_roster" != "-" ] \
    || [ "$rostered_without_tla" != "-" ] \
    || [ "$rostered_without_cfg" != "-" ] \
    || [ "$liveness_drift" != "-" ]; then
    echo "proof-audit: promoted proof inventory drift detected" >&2
    exit 1
  fi
fi

echo "## Promoted Specs"
printf '%s\n' 'spec	liveness	tla_bytes	cfg_bytes	tla_gzip_bytes	cfg_gzip_bytes	invariants	properties'
while IFS="$(printf '\t')" read -r spec liveness; do
  tla_file="${TLA_DIR}/${spec}.tla"
  cfg_file="${TLA_DIR}/${spec}.cfg"
  invariants="-"
  properties="-"
  if [ -f "$cfg_file" ]; then
    invariants="$(cfg_items "$cfg_file" invariants)"
    properties="$(cfg_items "$cfg_file" properties)"
  fi
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
    "$spec" \
    "$liveness" \
    "$(bytes_for "$tla_file")" \
    "$(bytes_for "$cfg_file")" \
    "$(gzip_bytes_for "$tla_file")" \
    "$(gzip_bytes_for "$cfg_file")" \
    "$invariants" \
    "$properties"
done < "${tmpdir}/promoted.tsv"
echo

if [ -n "$LOG_DIR" ]; then
  echo "## TLC Log Metrics"
  printf '%s\n' 'spec	result	generated	distinct	depth	duration	log'
  while IFS="$(printf '\t')" read -r spec _liveness; do
    log="$(find_log_for_spec "$spec" || true)"
    if [ -z "$log" ]; then
      printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$spec" "missing" "n/a" "n/a" "n/a" "n/a" "-"
      continue
    fi
    generated="$(number_from_pattern '[0-9][0-9,]* states generated' "$log")"
    distinct="$(number_from_pattern '[0-9][0-9,]* distinct states' "$log")"
    duration="$(duration_from_log "$log")"
    printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
      "$spec" \
      "$(result_from_log "$log")" \
      "${generated:-n/a}" \
      "${distinct:-n/a}" \
      "$(depth_from_log "$log")" \
      "${duration:-n/a}" \
      "$log"
  done < "${tmpdir}/promoted.tsv"
  echo
fi

echo "## Local Trace Artifacts"
find "$TLA_DIR" -maxdepth 1 -type f \( -name '*_TTrace_*.tla' -o -name '*_TTrace_*.bin' \) \
  | sort \
  | while IFS= read -r trace; do
      printf '%s\t%s\n' "$(basename "$trace")" "$(bytes_for "$trace")"
    done
