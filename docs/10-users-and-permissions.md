# 10 — Users and permissions

> **Status:** scoped stub. Expanded as the Phase 4 work lands.

## Principles

- **The owner sets the limit.** There is no quota imposed by the system — the
  opposite of the TMDB public API
  ([Scenario H](01-vision-and-use-cases.md)).
- **Users and tokens.** The owner creates users and issues API access tokens.
- **Graduated permissions.** E.g.: `read`, `read+write`, `admin`. Exact scopes to
  be defined.

## Scope (to be detailed)

- The `user`, `token`, `permission` model.
- UI authentication (session) vs API authentication (token) — probably a
  unified scheme.
- Per-resource permission? (e.g. a user only sees certain libraries) — evaluate.

## Open decisions (see [11](11-open-decisions.md))

- Token scheme (JWT vs opaque + table), expiration, rotation.
- First boot: creation of the initial admin.
