#!/usr/bin/env bash
set -euo pipefail

PUID="${PUID:-1000}"
PGID="${PGID:-1000}"
APP_USER="${APP_USER:-moevideo}"
APP_GROUP="${APP_GROUP:-moevideo}"

if ! [[ "${PUID}" =~ ^[0-9]+$ ]]; then
  echo "invalid PUID: ${PUID}" >&2
  exit 1
fi
if ! [[ "${PGID}" =~ ^[0-9]+$ ]]; then
  echo "invalid PGID: ${PGID}" >&2
  exit 1
fi

if ! getent group "${PGID}" >/dev/null 2>&1; then
  groupadd -o -g "${PGID}" "${APP_GROUP}" >/dev/null 2>&1 || true
fi

if ! id -u "${APP_USER}" >/dev/null 2>&1; then
  useradd -o -m -u "${PUID}" -g "${PGID}" -s /usr/sbin/nologin "${APP_USER}" >/dev/null 2>&1 || true
else
  usermod -o -u "${PUID}" -g "${PGID}" "${APP_USER}" >/dev/null 2>&1 || true
fi

for path in /data /data/db /data/storage /data/temp /data/redis; do
  mkdir -p "${path}"
done
chown -R "${PUID}:${PGID}" /data >/dev/null 2>&1 || true

exec gosu "${PUID}:${PGID}" "$@"
