# 0041 — Parallelism is inside a run and per provider, never at the queue level

Status: accepted (20/nov/2026)

## Context

The queue is observable ([ADR-0040](0040-queue-observability.md)) and it exposed the size of the
problem: one `saude_biblioteca` run took **10 h 28 min**, a scan of `/media/musica` (3 150 files)
took **5 h 50 min**, and a manual job once waited **8 h 46 min** in the queue (`wait_ms =
31 567 760`). With that data on the screen, the obvious reaction is "run more than one thing at a
time". Before deciding, the constraints were measured.

**1. MusicBrainz caps the client at 1 request/s per IP and blocks abuse.** The limiter is
**per client instance** (`novoLimitador` inside the constructor of
`internal/provider/musicbrainz`), so two clients in the same process would already multiply the
request rate and get the IP blocked. For music, no amount of concurrency beats that wall: the call
**count** is the only lever.

**2. Music matching does not live in the enrichment phase: it lives inside the scan.**
`factsMusica` → `resolverFaixa` costs 2–4 MusicBrainz calls per track (one artist search plus one
scoped import per candidate, `maxCandidatosArtista = 3`), each serialized by the limiter. The
measured library has 721 tracks matched by MBID, 2 087 by textual search and 342 drafts: those
2 087 tracks alone are ≈ 6 200 calls, i.e. ≈ 1 h 45 of pure rate-limit wait.

**3. SQLite is one writer** (ADR-0010). WAL plus the single write connection make concurrent
readers safe and serialize writes; scan writes are already short batches. A second concurrent run
would therefore be *correct*, but it would queue on the writer instead of on the API.

**4. Providers are not equal.** MusicBrainz: 1 req/s. TMDb: no comparable limit (the throttling
observed in the movie sweep is **latency**, ~200–400 ms per call, not a cap). AcoustID has its own
documented limit and is only a fallback. ffprobe/hash/fpcalc are local CPU and disk.

**5. The pieces for safe intra-run concurrency exist.** The progress registrar is a mutex-guarded
snapshot (`internal/execqueue/progresso.go`) and the trail is append-only with a per-item `seq`, so
several producers can report without corrupting it.

## Decision

**Concurrency stays 1 at the queue level** (ADR-0028 keeps its force: one persisted item runs at a
time, one writer). Parallelism, when it comes, is **inside one run** and **per provider**:

1. **A process-wide, per-provider limiter is the precondition.** No worker pool may exist before
   the limiter of the provider it calls is shared by the whole process, not owned by a client
   instance. This is the one change that must land first and alone.
2. **Workers are a per-phase decision, not a global switch.** A phase declares its own concurrency:
   network-bound calls to a provider whose limit is high (TMDb) can run 4–6 wide; music stays
   effectively serial because 1 req/s is the ceiling; local ffprobe/hash work can run 2–4 wide.
3. **Music is solved by calling less, not by running wider.** Album-first matching (one release
   fetch per album, tracks matched against the imported tracklist) and not re-attempting hopeless
   files are the levers; a worker pool there would only re-order the waiting.
4. **Queue-level concurrency (> 1) is not adopted.** The measured composition of the queue is one
   long disk+network run at a time; overlapping two of them would trade API waiting for writer
   contention and make "which item am I watching" ambiguous on the screen ADR-0040 just created.

## Consequences

- **The immediate wins are call-count reductions, not goroutines.** The music path goes first; the
  TMDb sweep is the first real candidate for workers, behind the global limiter.
- **The limiter becomes infrastructure.** It needs a home shared by every instance of a provider
  (per provider, per process), with its interval configurable per integration — a small, reviewable
  change that also documents the provider's documented limit next to it.
- **Ordering constraints are explicit.** A phase that must write in sequence (episodes of a series
  in owner order) cannot be parallelized without a deterministic collection step; that is a
  per-phase analysis, which is why nothing is switched globally.
- **Observability already supports it.** Per-item events plus the counters let a parallel phase be
  read on the queue screen without new plumbing; the trail stays bounded by the 200-row cap.
- **Outcome of "analyze the parallelism": it is not the answer to the 10-hour run.** The number
  that has to fall is the request count per item. This ADR records that conclusion so the next
  session does not re-derive it from scratch.

Agenda: Q2 (parallelism) — see [docs/00](../00-decision-agenda.md).
