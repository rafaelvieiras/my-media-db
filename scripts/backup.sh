#!/usr/bin/env sh
# backup.sh — snapshot consistente do banco de PRODUÇÃO do MyMediaDB.
#
# Por que não copiar o .sqlite direto: em modo WAL (ADR-0010) parte das escritas
# vive no arquivo -wal ainda não integrado. Copiar só o .sqlite com o app rodando
# pode gerar um backup corrompido/incompleto. `VACUUM INTO` gera uma cópia ÚNICA,
# consistente e já compactada (sem -wal/-shm), com o app no ar — sem downtime.
#
# Uso:
#   ./scripts/backup.sh                 # grava em ./data/backups/
#   MMDB_BACKUP_DIR=/mnt/hd ./scripts/backup.sh
#   MMDB_KEEP=14 ./scripts/backup.sh    # mantém os 14 mais recentes (default 7)
#
# Agendar no host (cron do usuário), diário às 3h da manhã:
#   crontab -e
#   0 3 * * * cd /caminho/para/my-media-db && ./scripts/backup.sh >> ./data/backups/backup.log 2>&1
set -eu

CONTAINER="${MMDB_CONTAINER:-mymediadb}"
DB_IN_CONTAINER="${MMDB_DB_PATH:-/data/mymediadb.sqlite}"
BACKUP_DIR="${MMDB_BACKUP_DIR:-./data/backups}"
KEEP="${MMDB_KEEP:-7}"

# Timestamp sem dois-pontos (compatível com qualquer filesystem).
STAMP="$(date +%Y%m%d-%H%M%S)"
OUT="mymediadb-${STAMP}.sqlite"

mkdir -p "$BACKUP_DIR"

# Preflight: o container precisa estar de pé e ter o CLI sqlite3 (baked no
# Dockerfile). Imagens anteriores a essa mudança não têm o binário — nesse caso
# a mensagem abaixo diz exatamente o que fazer em vez de estourar um erro cru.
if ! docker exec "$CONTAINER" sh -c 'command -v sqlite3' >/dev/null 2>&1; then
  echo "[backup] ERRO: 'sqlite3' não existe no container '$CONTAINER'." >&2
  echo "[backup]   A imagem em execução é anterior ao 'apk add ... sqlite' do" >&2
  echo "[backup]   Dockerfile. Publique a imagem nova (push -> CI) e deixe o" >&2
  echo "[backup]   Watchtower atualizar, ou rode: docker compose -f compose.prod.yaml pull && up -d" >&2
  exit 1
fi

# O container roda como uid:gid 100:101 e enxerga /data; escreve o snapshot lá,
# que via bind-mount aparece em ./data/backups no host.
echo "[backup] VACUUM INTO -> $BACKUP_DIR/$OUT"
docker exec "$CONTAINER" sqlite3 "$DB_IN_CONTAINER" \
  "VACUUM INTO '/data/backups/${OUT}'"

# Rotação: mantém apenas os $KEEP snapshots mais recentes.
echo "[backup] rotacionando (mantendo $KEEP mais recentes)"
ls -1t "$BACKUP_DIR"/mymediadb-*.sqlite 2>/dev/null \
  | tail -n +"$((KEEP + 1))" \
  | while IFS= read -r old; do
      echo "[backup] removendo antigo: $old"
      rm -f "$old"
    done

echo "[backup] ok: $BACKUP_DIR/$OUT"
