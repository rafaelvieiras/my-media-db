# ADR-0023 — Integrations configurable through the UI: dynamic credential registry, database with precedence over environment variables

- **Status:** Accepted
- **Date:** 2026-08-10
- **Agenda:** [I1–I4](../00-decision-agenda.md) · **Spec:** [04-data-model](../04-data-model.md), [07-enrichment-and-providers](../07-enrichment-and-providers.md), [03-architecture](../03-architecture.md)

## Context

Today every external source credential (TMDB, TheTVDB, fanart.tv) exists only as
an environment variable read once in `cmd/mymediadb/main.go`
(`MYMEDIADB_TMDB_TOKEN`/`_API_KEY`, `MYMEDIADB_TTDB_API_KEY`,
`MYMEDIADB_FANARTTV_API_KEY`) and used to build the `provider.Registry` at boot.
Changing a key requires editing the `.env`/compose and **restarting the process** —
there is no UI, no friendly validation, and the owner has no way to see "what is
configured" without reading the container's environment variables.

The owner asked for a `/dashboard/integrations` screen, with a subpage per
integration and a dynamic form, to replace that hardcode — starting with TMDB —
and designed so that **future plugins** ([11](../11-open-decisions.md), machinery
still outside the MVP) can register their own integrations without touching the
core.

This runs into the letter of a rule currently fixed in [AGENTS.md](../../AGENTS.md)
("secrets never in the code — API keys only through environment variables"). That
rule was born to forbid a key **committed to the repository** (the hardcoded IMVDB
incident, see [11](../11-open-decisions.md)), not to forbid storage in a database
behind an administrative UI — and the project **already has a precedent** of moving
configuration from env to database+UI: L1
([ADR-0018](0018-multilanguage-model-language-as-a-layer-dimension.md)) did exactly
that for languages ("config in settings in the database, not in env"), and the
Jellyfin webhook already stores a per-instance secret (webhook token) in
`app_settings`. What is missing is generalizing the pattern to provider credentials
and opening the extension point for other integrations.

## Decision

**The database (through the UI) is the override layer; the environment variable
becomes only the bootstrap/deploy-zero-touch fallback — the same override-wins
shape as the resolver (`override ⟵ external ⟵ file`, R1–R7), applied to
integration config:**

```
effective value of a field  =  value in the database, IF PRESENT
                            ⟵  environment variable registered for the field
                            ⟵  absent (integration "not configured")
```

1. **New package `internal/integration`** (parallel to `internal/provider`, the
   same optional-capability spirit already used there): it defines `Descriptor`
   (the contract that ANY integration — core today, plugin tomorrow — implements to
   register itself) and `Registry` (same shape as `provider.Registry`:
   `Registrar/Get/All/Nomes`, registration order preserved, usable zero-value).

   ```go
   type Campo struct {
       Nome, Label, Tipo string // Tipo: "text"|"secret"|"bool"|"number"|"select"
       Obrigatorio        bool   // UI hint (asterisk); does NOT decide "configured"
       Ajuda, Placeholder string
       EnvVar             string   // bootstrap/fallback env var for this field ("" = none)
       Opcoes             []Opcao  // only for Tipo="select"
   }

   type Descriptor interface {
       Slug() string        // e.g. "tmdb" — registry and URL key
       Nome() string         // display name ("TMDB")
       Descricao() string
       Campos() []Campo
       // Configurada decides, from the EFFECTIVE values (database⟵env), whether the
       // integration is operational. It belongs to the Descriptor, not to the
       // framework, because the rule varies (TMDB: token OR api_key; another source
       // may require both).
       Configurada(efetivos map[string]string) bool
   }

   // OPTIONAL capability (same pattern as provider.ColecaoProvider etc.):
   // a Descriptor may implement Testador so the UI can offer "test connection".
   type Testador interface {
       Testar(ctx context.Context, valores map[string]string) error
   }
   ```

   Each provider that owns a credential (`internal/provider/tmdb`, then
   `tvdb`/`fanarttv`) gets an adapter that implements `integration.Descriptor`
   delegating to its own already existing `Credencial.Configurada()` — without
   duplicating the rule that already lives there.

2. **New table `integration_config`** (migration), ONE row per slug, mirroring the
   sparse blob of R4 (`media_override`): only the fields the owner effectively set
   through the UI go into the JSON.

   ```sql
   CREATE TABLE integration_config (
       slug        TEXT PRIMARY KEY,
       values_json TEXT NOT NULL DEFAULT '{}',
       updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
   );
   ```

   Same as R6 (key presence = active override): writing `null` into a field through
   the PUT **removes the key** from the JSON (it goes back to inheriting from the env
   var); the absence of the table/row is never an error, just "no override, fall back
   to env".

3. **No hot-reload in the MVP.** Changing a credential through the UI writes to the
   database immediately, but the in-memory provider only re-reads it on the next
   **process restart** (`docker restart`, no image rebuild — already a big gain over
   today, which requires editing `.env`/compose). The UI states this explicitly
   ("salvo — reinicie o servidor para aplicar"). Hot-reload (rebuilding the
   `provider.Registry` in-process on save) stays as an **explicit follow-up**, and
   does not block the first delivery.

4. **Secrets never come back raw through the API.** A field with `Tipo="secret"`
   returns only `definido: bool` and `origem: "banco"|"env"|"nenhum"` on the GET,
   never the value. Non-secret fields return the effective value normally. No "test
   connection" endpoint in the first delivery (that stays a follow-up); the owner
   validates by saving and restarting.

5. **AGENTS.md/CONTRIBUTING.md are updated:** the secrets rule goes from "only
   through environment variables" to "the database (through the Integrations UI) has
   precedence; the environment variable is only the bootstrap/fallback — never
   committed, never hardcoded in the code in either way".

## Consequences

- The owner configures/changes the TMDB token (and those of the next sources)
  without editing `.env` or redeploying the image — just restarting the container.
- It opens the **first concrete extension point** for the future plugin system
  ([11](../11-open-decisions.md)): a third-party plugin that needs its own credential
  implements `integration.Descriptor` and registers itself in the same `Registry` —
  without the full plugin machinery (manifest/sandbox/lifecycle) existing yet. It is
  not the plugin epic; it is the foundation that epic will reuse.
- `internal/integration` knows nothing about TMDB/TVDB/fanart.tv — each provider
  contributes its own `Descriptor` (same optional-capability philosophy as
  `internal/provider`).
- Left out of this delivery (follow-ups): hot-reload without restart, "test
  connection" (the `Testador` capability), moving the Jellyfin webhook token to this
  generic mechanism, encryption at rest of `values_json` (today: the same level of
  protection `app_settings` already has — a local SQLite file, outside version
  control).
- Related decisions: [ADR-0001](0001-resolver-partition-by-origin.md)/
  [ADR-0004](0004-override-as-sparse-json-blob.md) (R1/R4, the override-wins and
  sparse-blob pattern this decision reapplies);
  [ADR-0018](0018-multilanguage-model-language-as-a-layer-dimension.md) (L1, the
  precedent of settings in the database+UI); and the two deliveries that materialize
  it: the foundation (schema + registry + API + TMDB migration) and the
  `/dashboard/integrations` UI.
