#!/bin/sh
# Entrypoint for the MyMediaDB image.
#
# Why it exists: on a NAS the container is upgraded by the host, not by a person, and the
# bind mount has to keep belonging to the host user afterwards. Unraid expects the
# PUID/PGID pair (99:100 = nobody:users) and every other container on the box runs that
# way; without it the app cannot write /mnt/user/appdata and the install looks broken.
#
# Three cases, all supported:
#   1. started as root (the image default) -> fix the ownership of /data if it drifted,
#      then drop to PUID:PGID and run the binary;
#   2. started with --user UID:GID        -> nothing to adjust, just run;
#   3. started as root without PUID/PGID -> the Unraid defaults (99:100).
#
# No /etc/passwd entry is created: the binary is a static Go program and what matters to
# the kernel is the numeric uid/gid, which `su-exec` sets directly.
#
# The chown is RECURSIVE ONLY when the top of /data already belongs to somebody else.
# Doing it on every boot would walk the whole artwork cache, which costs minutes on a
# library with tens of thousands of downloaded files.
set -eu

PUID="${PUID:-99}"
PGID="${PGID:-100}"

# Case 2: somebody pinned the user, so there is no privilege to adjust anything with.
if [ "$(id -u)" != "0" ]; then
    exec /mymediadb "$@"
fi

mkdir -p /data
DONO_ATUAL="$(stat -c '%u:%g' /data 2>/dev/null || echo '?')"
if [ "$DONO_ATUAL" != "$PUID:$PGID" ]; then
    echo "mymediadb: ajustando o dono de /data ($DONO_ATUAL -> $PUID:$PGID)" >&2
    chown -R "$PUID:$PGID" /data 2>/dev/null ||
        echo "mymediadb: AVISO nao consegui ajustar /data; verifique a permissao no host" >&2
fi

HOME=/data
export HOME
exec su-exec "$PUID:$PGID" /mymediadb "$@"
