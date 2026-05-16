#!/bin/sh
# Phaze Nexus entrypoint.
#
# If LITESTREAM_BUCKET is set, restore the SQLite DB from S3 (no-op if the
# bucket has no replica yet, e.g. first boot) and then run the server under
# `litestream replicate` so every WAL frame streams to S3.
#
# If LITESTREAM_BUCKET is unset, run the server directly. This keeps local /
# self-hosted deployments simple — no S3 dependency until you opt in.

set -eu

DB_PATH="${DB_PATH:-/data/nexus.db}"

if [ -n "${LITESTREAM_BUCKET:-}" ]; then
  echo "[entrypoint] litestream: restoring ${DB_PATH} from s3://${LITESTREAM_BUCKET}"
  litestream restore -if-replica-exists -config /app/litestream.yml "${DB_PATH}" || {
    echo "[entrypoint] restore failed or no replica yet; continuing with on-disk DB"
  }
  echo "[entrypoint] litestream: replicating ${DB_PATH} -> s3://${LITESTREAM_BUCKET}"
  exec litestream replicate -config /app/litestream.yml -exec "/app/nexus-server"
fi

echo "[entrypoint] litestream disabled (LITESTREAM_BUCKET unset); running without replication"
exec /app/nexus-server
