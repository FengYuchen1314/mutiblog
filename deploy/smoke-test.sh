#!/usr/bin/env bash
set -Eeuo pipefail

smoke_image="${1:?usage: smoke-test.sh <image>}"
smoke_container="mutiblog-smoke"
smoke_volume="mutiblog-smoke-data"

cleanup() {
  docker logs "$smoke_container" 2>/dev/null || true
  docker rm -f "$smoke_container" >/dev/null 2>&1 || true
  docker volume rm -f "$smoke_volume" >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup

wait_ready() {
  for _ in $(seq 1 60); do
    if curl -fsS http://127.0.0.1:18080/health/ready >/dev/null; then
      return 0
    fi
    sleep 1
  done
  return 1
}

wait_home_page() {
  for _ in $(seq 1 60); do
    if curl -fsS http://127.0.0.1:18080/ >/dev/null; then
      return 0
    fi
    sleep 1
  done
  return 1
}

start_candidate() {
  docker run -d --name "$smoke_container" \
    -p 127.0.0.1:18080:8080 \
    -v "$smoke_volume:/var/lib/mutiblog" \
    "$smoke_image" >/dev/null
}

docker volume create "$smoke_volume" >/dev/null
start_candidate
wait_ready

curl -fsS -X POST http://127.0.0.1:18080/api/v1/setup \
  -H 'Content-Type: application/json' \
  --data '{"siteTitle":"MutiBlog Smoke","baseUrl":"http://127.0.0.1:18080","sourceLocale":"zh-CN","adminLocale":"en","timezone":"UTC","username":"admin","password":"smoke-test-password-123"}' >/dev/null

docker rm -f "$smoke_container" >/dev/null
start_candidate
wait_ready
wait_home_page
