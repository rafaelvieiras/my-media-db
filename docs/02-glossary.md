# 02 — Glossary

Canonical vocabulary. Each term has a fixed meaning in every other doc.

| Term | Definition |
|---|---|
| **Entity** | The central unit of the model: any cataloged media. It has a `type` (`movie`, `tv`, `episode`, `collection`, `artista`, `album`, `faixa`, …). See [04](04-data-model.md). |
| **Override layer** | Sparse record of the owner's edits. It always wins over external data and over file data. This is what gives the user final authority. |
| **Universal override** | Property of the override: it reaches **any** field (core, technical, extension), with no shielded field. See [04](04-data-model.md). |
| **Resolver precedence** | The fixed composition order `override ⟵ external ⟵ file`: the override beats external, which beats the file. Read it as "pulls from" (⟵). |
| **Operation-based merge** | How the override edits lists and relations without replacing them as a block: it stores **adds and removes** (tags, genres) or **add/remove/edit** of credits, with the external base still alive underneath. See [04](04-data-model.md). |
| **Field provenance** | Which layer (`override`, `external`, `file`) produced the final value of a field. **Not exposed in the API** for now. |
| **External data** | Payload pulled from a source (TMDB and others), stored raw and identified by source, as the editorial base. Cached locally. |
| **File data (mediainfo)** | Technical facts extracted from the media file (codec, HDR, streams, duration). Not editable. |
| **Resolver** | The component that composes the final value of an entity: `override ⟵ external ⟵ file`. Every API response comes from the resolver, never from the raw record. |
| **`extension_data`** | Namespaced JSON blob with the fields of one vertical. It never changes the core schema. Validated by JSON Schema. |
| **Extension namespace** | Key shaped `<owner>.<slug>@<major>`, for example `core.tv@1` and, later, `plugin.games@1`. |
| **Vertical** | A media category with its own fields (movies, series, games, books…). In the MVP the verticals are internal (`core.*`). |
| **Provider** | Client of an external metadata source (TMDB, TheTVDB, MusicBrainz…). Used on demand. |
| **Library source** | A folder on the server that the owner points at for scanning. |
| **Scan** | Non-destructive walk of a library source: finds files and extracts mediainfo. |
| **Matching** | Association of a file or entry with an entity and, optionally, with an external id. |
| **`content_hash`** | Stable identity of a file by content, so a rename does not create a duplicate record. |
| **Asset** | Auxiliary media of an entity (poster, fanart, banner, logo…). Origin `external` or `user`. Binary on disk, metadata in the database. |
| **`status`** | The owner's **intent** for the entity, one state at a time: `cataloged` (default), `wanted`, `unreleased`, `draft`. It is **orthogonal to ownership**; there is no `owned` field, which was ownership in disguise. It supports "the catalog is larger than the library". See [04](04-data-model.md). |
| **Ownership** | A **derived fact**, not an editable field: does the entity have at least one `present` `media_file`? It answers "in the library" versus "only in the catalog". Orthogonal to `status`. |
| **`missing`** | State of a `media_file` whose file is gone from disk while the source is reachable. The record is **kept** with `last_seen`, it does not count for ownership, and it is reversible if the file comes back. See [06](06-library-and-scan.md). |
| **Orphan** | An **auto-created** entity whose file is gone and which holds **no investment from the owner** (no override, no asset, no collection, default `status`). It goes to the cleanup queue and is **never deleted on its own**. |
| **Purge** | The **only** destructive operation, always **explicit** from the owner: removes the entity and its derivatives (`media_external`, `media_override`, assets). |
| **`external_ids` (authoritative identity)** | Map of every known external id of the entity, searched or not. The source of truth for identity: matching writes here and lookup reads here. |
| **`media_external` (cache)** | Raw payload per source actually fetched. `source_id` is **subordinate** to `external_ids`; on conflict, identity wins. Editorial base of the resolver. |
| **`/api/tmdb`** | **TMDB-compatible** API layer, so existing clients (Jellyfin, Kodi) can point at it. |
| **`/api/v1`** | **Native** API, for everything TMDB does not model (override, library, mediainfo, users). |
| **`VITE_API_BASE`** | Front-end variable that sets the API base: empty means same origin (the Go binary), set means a remote API. |
| **Plugin** | Third-party extension that adds verticals or integrations. **Design target, outside the MVP** — see [11](11-open-decisions.md). |
| **Artist / Album / Track** | Music vertical hierarchy (`entity type='artista'|'album'|'faixa'`). The track is the canonical fine grain, anchored on an MBID. The album exists only for artwork, override, and grouping. The artist is a first-class entity and never reuses `person`. "Various Artists" is a normal artist with the canonical MusicBrainz MBID. See [ADR-0027](adr/0027-music-model-artist-album-track.md). |
| **`core.music@1`** | `extension_data` namespace of the music vertical: `track_number`, `disc_number`, `disc_total`, `isrc`. Read by `internal/musicext`, the same shape as `internal/tvext` for series. |
