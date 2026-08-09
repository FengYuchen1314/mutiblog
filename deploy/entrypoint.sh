#!/bin/sh
set -eu

for dir in content data media config generated cache backups; do
  path="/app/$dir"
  mkdir -p "$path"
  if ! chown -R blog:blog "$path" 2>/dev/null; then
    echo "WARN: cannot change ownership of $path (possibly a network mount)" >&2
    if ! su-exec blog test -w "$path"; then
      echo "FATAL: $path is not writable by the blog user." >&2
      echo "Run on the host: sudo chown -R 1001:1001 ./$dir" >&2
      exit 1
    fi
  fi
done
if [ ! -f /app/config/config.yaml ]; then
  cp /app/config.default.yaml /app/config/config.yaml
fi
if [ -z "${BLOG_SESSION_SECRET:-}" ]; then
  secret_file=/app/config/.secrets.yaml
  if [ -f "$secret_file" ]; then
    BLOG_SESSION_SECRET=$(awk -F': ' '/^sessionSecret:/{print $2; exit}' "$secret_file")
  fi
  if [ -z "${BLOG_SESSION_SECRET:-}" ]; then
    BLOG_SESSION_SECRET=$(head -c 48 /dev/urandom | base64 | tr -d '\n')
    (umask 077; printf 'sessionSecret: %s\n' "$BLOG_SESSION_SECRET" > "$secret_file")
    chown blog:blog "$secret_file" 2>/dev/null || true
    echo "Generated a persistent session secret at config/.secrets.yaml; do not commit it." >&2
  fi
fi
export BLOG_SESSION_SECRET
exec su-exec blog "$@"
