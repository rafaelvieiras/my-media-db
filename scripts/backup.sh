#!/usr/bin/env sh
# backup.sh — consistent snapshot of the MyMediaDB PRODUCTION database.
#
# Why not copy the .sqlite directly: in WAL mode (ADR-0010) part of the writes
# lives in the -wal file not yet merged. Copying only the .sqlite with the app
# running can produce a corrupted/incomplete backup. `VACUUM INTO` produces a
# SINGLE, consistent and already compacted copy (no -wal/-shm), with the app up —
# no downtime.
#
# Usage:
#   ./scripts/backup.sh                 # writes to ./data/backups/
#   MMDB_BACKUP_DIR=/mnt/hd ./scripts/backup.sh
#   MMDB_KEEP=14 ./scripts/backup.sh    # keeps the 14 most recent (default 7)
#
# Schedule it on the host (user cron), daily at 3 a.m.:
#   crontab -e
#   0 3 * * * cd /path/to/my-media-db && ./scripts/backup.sh >> ./data/backups/backup.log 2>&1
set -eu

CONTAINER="${MMDB_CONTAINER:-mymediadb}"
DB_IN_CONTAINER="${MMDB_DB_PATH:-/data/mymediadb.sqlite}"
BACKUP_DIR="${MMDB_BACKUP_DIR:-./data/backups}"
KEEP="${MMDB_KEEP:-7}"

# Timestamp without colons (compatible with any filesystem).
STAMP="$(date +%Y%m%d-%H%M%S)"
OUT="mymediadb-${STAMP}.sqlite"

mkdir -p "$BACKUP_DIR"

# Preflight: the container must be up and have the sqlite3 CLI (baked into the
# Dockerfile). Images older than that change do not have the binary — in that case
# the message below says exactly what to do instead of throwing a raw error.
if ! docker exec "$CONTAINER" sh -c 'command -v sqlite3' >/dev/null 2>&1; then
  echo "[backup] ERRO: 'sqlite3' não existe no container '$CONTAINER'." >&2
  echo "[backup]   A imagem em execução é anterior ao 'apk add ... sqlite' do" >&2
  echo "[backup]   Dockerfile. Publique a imagem nova (push -> CI) e deixe o" >&2
  echo "[backup]   Watchtower atualizar, ou rode: docker compose -f compose.yaml pull && up -d" >&2
  exit 1
fi

# The container runs as uid:gid 100:101 and sees /data; it writes the snapshot
# there, which shows up in ./data/backups on the host through the bind mount.
echo "[backup] VACUUM INTO -> $BACKUP_DIR/$OUT"
docker exec "$CONTAINER" sqlite3 "$DB_IN_CONTAINER" \
  "VACUUM INTO '/data/backups/${OUT}'"

# Rotation: keeps only the $KEEP most recent snapshots.
echo "[backup] rotacionando (mantendo $KEEP mais recentes)"
ls -1t "$BACKUP_DIR"/mymediadb-*.sqlite 2>/dev/null \
  | tail -n +"$((KEEP + 1))" \
  | while IFS= read -r old; do
      echo "[backup] removendo antigo: $old"
      rm -f "$old"
    done

echo "[backup] ok: $BACKUP_DIR/$OUT"
