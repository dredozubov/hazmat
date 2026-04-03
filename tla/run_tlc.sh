#!/bin/sh

set -eu

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

JAVA_BIN_RESOLVED="$(find_java)" || {
  echo "error: could not resolve a working Java 17+ runtime for TLC" >&2
  echo "set JAVA_BIN=/path/to/java or JAVA_HOME=/path/to/jdk" >&2
  exit 1
}

TLA2TOOLS_JAR="${TLA2TOOLS_JAR:-$HOME/workspace/tla2tools.jar}"
if [ ! -f "${TLA2TOOLS_JAR}" ]; then
  echo "error: tla2tools.jar not found at ${TLA2TOOLS_JAR}" >&2
  echo "set TLA2TOOLS_JAR=/path/to/tla2tools.jar" >&2
  exit 1
fi

TLC_JAVA_OPTS="${TLC_JAVA_OPTS:--XX:+UseParallelGC}"

# Intentional word splitting for JVM flags supplied via TLC_JAVA_OPTS.
# shellcheck disable=SC2086
exec "${JAVA_BIN_RESOLVED}" ${TLC_JAVA_OPTS} -jar "${TLA2TOOLS_JAR}" "$@"
