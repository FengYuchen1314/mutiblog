#!/usr/bin/env bash
set -Eeuo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
compose_file="${MUTIBLOG_COMPOSE_FILE:-${script_dir}/compose.production.yaml}"
image="${MUTIBLOG_IMAGE:-ghcr.io/fengyuchen1314/mutiblog:latest}"
public_url="${MUTIBLOG_PUBLIC_URL:-https://mutiblog.nl.chrono-well.top}"
preview_url="${MUTIBLOG_PREVIEW_URL:-https://preview.nl.chrono-well.top}"
lock_file="${MUTIBLOG_UPDATE_LOCK:-/run/lock/mutiblog-update.lock}"
rollback_image="mutiblog:rollback"
health_attempts="${MUTIBLOG_HEALTH_ATTEMPTS:-60}"
health_interval="${MUTIBLOG_HEALTH_INTERVAL_SECONDS:-2}"
rollback_attempts="${MUTIBLOG_ROLLBACK_ATTEMPTS:-30}"

ensure_caddy() {
  local selected_image="$1"
  # Reconcile even an already-running container. The deployment directory can
  # replace an older Compose project whose Caddy container still bind-mounts a
  # stale Caddyfile from another working directory. Compose leaves an
  # unchanged container alone and recreates one whose production definition
  # differs; --no-deps keeps the proven application candidate untouched.
  MUTIBLOG_IMAGE="${selected_image}" docker compose -f "${compose_file}" up -d --pull missing --no-deps caddy
  # Deployment files change independently from the application image. A
  # running Caddy container does not automatically reload a changed
  # bind-mounted Caddyfile, so validate and apply it before the public gate.
  docker exec mutiblog-caddy caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null
  docker exec mutiblog-caddy caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null
}

preview_is_isolated() {
  local status
  status="$(curl -sS -o /dev/null -w '%{http_code}' "${preview_url}/console/" 2>/dev/null || true)"
  [[ "${status}" == "404" ]]
}

wait_for_public_health() {
  local selected_image="$1"
  local attempts="$2"
  local container_health
  local caddy_configured="false"
  for attempt in $(seq 1 "${attempts}"); do
    container_health="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' mutiblog-app 2>/dev/null || true)"
    if [[ "${container_health}" == "healthy" && "${caddy_configured}" != "true" ]] && ensure_caddy "${selected_image}"; then
      caddy_configured="true"
    fi
    if [[ "${container_health}" == "healthy" && "${caddy_configured}" == "true" ]] && curl -fsS "${public_url}/health/ready" >/dev/null 2>&1 && preview_is_isolated; then
      return 0
    fi
    sleep "${health_interval}"
  done
  return 1
}

exec 9>"${lock_file}"
if ! flock -n 9; then
  exit 0
fi

current_image_id="$(docker inspect --format '{{.Image}}' mutiblog-app 2>/dev/null || true)"
docker pull "${image}"
candidate_image_id="$(docker image inspect --format '{{.Id}}' "${image}")"

if [[ -n "${current_image_id}" && "${current_image_id}" == "${candidate_image_id}" ]]; then
  # No image changed, so do not recreate a healthy application. Still prove
  # that the complete app+Caddy+TLS path is ready; this also finishes a first
  # installation whose certificate issuance outlived the previous timer run.
  if wait_for_public_health "${image}" "${health_attempts}"; then
    exit 0
  fi
  exit 1
fi

if [[ -n "${current_image_id}" ]]; then
  docker tag "${current_image_id}" "${rollback_image}"
fi

MUTIBLOG_IMAGE="${image}" docker compose -f "${compose_file}" up -d --pull never app

if wait_for_public_health "${image}" "${health_attempts}"; then
  docker image prune -f --filter 'until=168h' >/dev/null
  exit 0
fi

docker logs --tail 200 mutiblog-app >&2 || true
if [[ -n "${current_image_id}" ]]; then
  MUTIBLOG_IMAGE="${rollback_image}" docker compose -f "${compose_file}" up -d --pull never app
  wait_for_public_health "${rollback_image}" "${rollback_attempts}" || true
fi
exit 1
