#!/usr/bin/env bash
set -Eeuo pipefail

test_log="$(mktemp)"
test_lock="$(mktemp)"
trap 'rm -f "${test_log}" "${test_lock}"' EXIT

docker() {
  case "${1:-} ${2:-}" in
    "inspect --format")
      printf '%s\n' 'sha256:same'
      ;;
    "image inspect")
      printf '%s\n' 'sha256:same'
      ;;
    "compose -f")
      printf '%s\n' "$*" >>"${MUTIBLOG_TEST_LOG}"
      ;;
    "exec mutiblog-caddy")
      ;;
    "pull ghcr.io/fengyuchen1314/mutiblog:latest")
      ;;
    *)
      printf 'unexpected docker command: %s\n' "$*" >&2
      return 1
      ;;
  esac
}

curl() {
  if [[ "$*" == *"%{http_code}"* ]]; then
    printf '404'
  fi
}

flock() { return 0; }
sleep() { return 0; }
export -f docker curl flock sleep
export MUTIBLOG_TEST_LOG="${test_log}"

MUTIBLOG_UPDATE_LOCK="${test_lock}" \
  MUTIBLOG_HEALTH_ATTEMPTS=1 \
  bash "$(dirname "$0")/update.sh"

grep -Fq 'up -d --pull missing --no-deps caddy' "${test_log}"
