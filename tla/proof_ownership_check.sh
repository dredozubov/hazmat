#!/bin/sh

set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
ROOT_DIR="$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)"
TLA_DIR="${ROOT_DIR}/tla"
LEDGER="${TLA_DIR}/proof_ownership.tsv"
VERIFIED="${TLA_DIR}/VERIFIED.md"
ROSTER="${TLA_DIR}/promoted_specs.tsv"
EXPECTED_PROMOTED_SPEC_COUNT=16

tmpdir="$(mktemp -d "${TMPDIR:-/tmp}/hazmat-proof-ownership.XXXXXX")"
cleanup() {
  rm -rf "$tmpdir"
}
trap cleanup EXIT INT TERM

fail() {
  echo "proof-ownership: $*" >&2
  exit 1
}

cfg_obligations() {
  for cfg in "${TLA_DIR}"/MC_*.cfg; do
    spec="$(basename "$cfg" .cfg)"
    awk -v spec="$spec" '
      function trim(s) {
        sub(/\\\*.*/, "", s)
        gsub(/^[ \t]+/, "", s)
        gsub(/[ \t]+$/, "", s)
        return s
      }
      function emit(kind, start,    i, item) {
        for (i = start; i <= NF; i++) {
          item = trim($i)
          if (item != "") {
            print spec "\t" kind "\t" item
          }
        }
      }
      /^[ \t]*INVARIANTS([ \t]|$)/ {
        mode = "invariant"
        emit("invariant", 2)
        next
      }
      /^[ \t]*PROPERTIES([ \t]|$)/ {
        mode = "property"
        emit("property", 2)
        next
      }
      /^[ \t]*PROPERTY[ \t]+/ {
        emit("property", 2)
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
        if (mode == "invariant" || mode == "property") {
          print spec "\t" mode "\t" line
        }
      }
    ' "$cfg"
  done
}

promoted_specs() {
  awk '
    /^[ \t]*run_spec[ \t]+MC_/ {
      spec = $2
      gsub(/"/, "", spec)
      print spec
    }
  ' "${TLA_DIR}/check_suite.sh" | sort
}

promoted_roster() {
  awk '
    /^[ \t]*run_spec[ \t]+MC_/ {
      spec = $2
      liveness = $3
      gsub(/"/, "", spec)
      gsub(/"/, "", liveness)
      print spec "\t" liveness
    }
  ' "${TLA_DIR}/check_suite.sh" | sort
}

[ -f "$LEDGER" ] || fail "missing ledger ${LEDGER}"
[ -f "$VERIFIED" ] || fail "missing ${VERIFIED}"
[ -f "$ROSTER" ] || fail "missing promoted spec roster ${ROSTER}"

awk -F '\t' '
  BEGIN {
    expected_header = "spec\tliveness"
    status = 0
  }
  NR == 1 {
    if ($0 != expected_header) {
      printf "proof-ownership: bad promoted spec roster header in %s\n", FILENAME > "/dev/stderr"
      status = 1
    }
    next
  }
  NF == 0 { next }
  NF != 2 {
    printf "proof-ownership: bad promoted spec roster column count at %s:%d\n", FILENAME, NR > "/dev/stderr"
    status = 1
    next
  }
  $1 !~ /^MC_[A-Za-z0-9_]+$/ {
    printf "proof-ownership: bad promoted spec name at %s:%d: %s\n", FILENAME, NR, $1 > "/dev/stderr"
    status = 1
  }
  $2 != "yes" && $2 != "no" {
    printf "proof-ownership: bad liveness value at %s:%d: %s\n", FILENAME, NR, $2 > "/dev/stderr"
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
  fail "duplicate specs in promoted spec roster"
fi

roster_count="$(wc -l < "${tmpdir}/rostered_specs.txt" | tr -d ' ')"
if [ "$roster_count" -ne "$EXPECTED_PROMOTED_SPEC_COUNT" ]; then
  fail "promoted spec roster lists ${roster_count} specs, expected ${EXPECTED_PROMOTED_SPEC_COUNT}"
fi

cfg_obligations | sort > "${tmpdir}/expected.tsv"

awk -F '\t' -v root="$ROOT_DIR" -v verified="$VERIFIED" '
  BEGIN {
    expected_header = "spec\tkind\tobligation\tverified_section\tdesign_note\towner"
    status = 0
    while ((getline line < verified) > 0) {
      if (line ~ /^### [0-9]+[ \t]/) {
        split(line, parts, /[ \t]+/)
        sections[parts[2]] = 1
      }
    }
    close(verified)
  }
  NR == 1 {
    if ($0 != expected_header) {
      printf "proof-ownership: bad header in %s\n", FILENAME > "/dev/stderr"
      status = 1
    }
    next
  }
  NF == 0 { next }
  NF != 6 {
    printf "proof-ownership: bad column count at %s:%d\n", FILENAME, NR > "/dev/stderr"
    status = 1
    next
  }
  $1 !~ /^MC_[A-Za-z0-9_]+$/ {
    printf "proof-ownership: bad spec at %s:%d: %s\n", FILENAME, NR, $1 > "/dev/stderr"
    status = 1
  }
  $2 != "invariant" && $2 != "property" {
    printf "proof-ownership: bad kind at %s:%d: %s\n", FILENAME, NR, $2 > "/dev/stderr"
    status = 1
  }
  $3 !~ /^[A-Za-z][A-Za-z0-9_]*$/ {
    printf "proof-ownership: bad obligation at %s:%d: %s\n", FILENAME, NR, $3 > "/dev/stderr"
    status = 1
  }
  !($4 in sections) {
    printf "proof-ownership: missing VERIFIED.md section %s for %s %s %s\n", $4, $1, $2, $3 > "/dev/stderr"
    status = 1
  }
  {
    note = root "/" $5
    if (system("[ -f \"" note "\" ]") != 0) {
      printf "proof-ownership: missing design note %s for %s %s %s\n", $5, $1, $2, $3 > "/dev/stderr"
      status = 1
    }
  }
  $6 == "" {
    printf "proof-ownership: empty owner at %s:%d\n", FILENAME, NR > "/dev/stderr"
    status = 1
  }
  {
    print $1 "\t" $2 "\t" $3 > actual
    print $1 > specs
  }
  END {
    exit status
  }
' actual="${tmpdir}/actual.unsorted.tsv" specs="${tmpdir}/ledger_specs.unsorted.txt" "$LEDGER"

sort "${tmpdir}/actual.unsorted.tsv" > "${tmpdir}/actual.tsv"
sort -u "${tmpdir}/ledger_specs.unsorted.txt" > "${tmpdir}/ledger_specs.txt"
promoted_specs > "${tmpdir}/promoted_specs.txt"
promoted_roster > "${tmpdir}/promoted.tsv"
find "${TLA_DIR}" -maxdepth 1 -type f -name 'MC_*.cfg' \
  | sed 's#.*/##; s#\.cfg$##' \
  | sort > "${tmpdir}/cfg_specs.txt"

duplicates="$(uniq -d "${tmpdir}/actual.tsv")"
if [ -n "$duplicates" ]; then
  printf '%s\n' "$duplicates" >&2
  fail "duplicate ledger obligations"
fi

missing="$(comm -23 "${tmpdir}/expected.tsv" "${tmpdir}/actual.tsv")"
if [ -n "$missing" ]; then
  printf '%s\n' "$missing" >&2
  fail "ledger is missing checked obligations"
fi

extra="$(comm -13 "${tmpdir}/expected.tsv" "${tmpdir}/actual.tsv")"
if [ -n "$extra" ]; then
  printf '%s\n' "$extra" >&2
  fail "ledger contains obligations not checked by any cfg"
fi

unpromoted_cfg="$(comm -23 "${tmpdir}/cfg_specs.txt" "${tmpdir}/promoted_specs.txt")"
if [ -n "$unpromoted_cfg" ]; then
  printf '%s\n' "$unpromoted_cfg" >&2
  fail "cfg specs are not promoted in check_suite.sh"
fi

roster_drift="$(comm -3 "${tmpdir}/roster.tsv" "${tmpdir}/promoted.tsv")"
if [ -n "$roster_drift" ]; then
  printf '%s\n' "$roster_drift" >&2
  fail "check_suite.sh promoted specs diverge from promoted_specs.tsv"
fi

missing_ledger_specs="$(comm -23 "${tmpdir}/promoted_specs.txt" "${tmpdir}/ledger_specs.txt")"
if [ -n "$missing_ledger_specs" ]; then
  printf '%s\n' "$missing_ledger_specs" >&2
  fail "promoted specs have no ledger rows"
fi

while IFS= read -r spec; do
  grep -q "\`tla/${spec}.tla\`.*\`tla/${spec}.cfg\`" "$VERIFIED" \
    || fail "VERIFIED.md does not list tla/${spec}.tla and tla/${spec}.cfg"
done < "${tmpdir}/promoted_specs.txt"

printf 'proof-ownership: ok (%s obligations)\n' "$(wc -l < "${tmpdir}/actual.tsv" | tr -d ' ')"
