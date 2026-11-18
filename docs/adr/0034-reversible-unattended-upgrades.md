# ADR-0034 — An unattended upgrade has to be reversible

Status: Accepted · 2026-11-18

## Context

The image is published on a moving `latest` tag, and the target host is a NAS: Unraid
pulls the container on a schedule, recreates it, and nobody watches. Two things happen
without a person in the loop, and both had real consequences already.

**The container is recreated, not restarted.** On 17/nov a recreated container came up with
an empty environment and a broken port binding (`PortBindings=map[8080/tcp:[{invalid IP
8080}]]`), from a template that still carried the legacy `<Networking>`/`<Data>`/
`<Environment>` blocks next to the modern `<Config>` ones. The app itself was fine; the
host built a container that could not work with it.

**The schema moves forward on its own.** Migrations run at boot (ADR-0017), so a version
bump applies them immediately, inside a container start, at 04:00, with no one to press
anything. Two failures follow from that:

- a migration that turns out badly has no way back, because nothing copied the database;
- going back to a previous image later makes an OLD binary read a NEWER schema — the case
  where a plain `goose.Up` has nothing to say and the app quietly writes with the wrong
  assumptions.

## Decision

Make the upgrade path reversible and explicit, in three pieces:

1. **Copy the database before any migration.** `VACUUM INTO` writes a consistent
   single-file snapshot (WAL included) to `/data/backups/pre-migration-schema-<old>-
   <timestamp>.sqlite` whenever the database exists and a migration is pending. The three
   newest are kept (`MYMEDIADB_PRE_MIGRATION_BACKUPS`). If the copy cannot be written, the
   boot fails: an upgrade without a way back is worse than no upgrade.
2. **Refuse to boot on a database newer than the binary.** Before migrating, the app
   compares the schema version with the newest embedded migration. If the database is
   ahead, it stops with both numbers in the message and does not touch anything. This is
   the only safe answer to a rollback.
3. **Run as the host user, on the host's terms.** The image starts as root only to adopt
   `PUID`/`PGID` (99:100 by default, the Unraid convention), fix the ownership of `/data`
   when it drifted, and `exec` the binary through `su-exec`. `--user` still wins and skips
   all of it. This is what lets a scheduled recreate land on a bind mount that already
   belongs to somebody else.

## Consequences

- A scheduled upgrade becomes a two-step recovery at worst: roll the image back, then copy
  one file from `/data/backups` over the database.
- The pre-migration copies cost one database-sized file each, a few hundred MB in a real
  library; the retention cap bounds it.
- Skipping the copy is possible (`MYMEDIADB_SKIP_PRE_MIGRATION_BACKUP=1`) and logs a
  warning, because from then on the upgrade is one-way.
- Owners can move away from `--user 99:100` in their templates and use `PUID`/`PGID`, which
  is what Community Applications expects and what stops "permission denied" reports after
  a recreate.
- Not solved here: the template is the other half of the equation. A container template
  with legacy blocks can still be built wrongly, which is why the Unraid templates live in
  their own repository with their own rules.
