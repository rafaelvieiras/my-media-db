# ADR-0037 — A movie is searched by its catalog title, not only by the release name

- **Status:** Accepted
- **Date:** 2026-11-18
- **Agenda:** [MD1](../00-decision-agenda.md)

## Context

The auto-match derived the search term from the **file name** for every type except
series. For a movie that is the release name — `Alligator2.91.BD1080p.MemoriadaTV.Maior` —
while the entity already carries a perfectly good title, taken from the **folder** the
owner curated: `Alligator 2 A Mutação`. The catalog title was never used.

The consequence is visible in production (18/nov/2026): of **1291 movies, 625 have no
external id**, and none of them had ever been checked by the retroactive enrichment
(`last_external_match_attempt_at` NULL in all 625). Sampling 25 of them and repeating the
search by hand:

| search term | outcome |
|---|---|
| release name (`Alligator2 91 BD1080p MemoriadaTV Maior`) | **0 candidates** — the source is literal ([ADR-0035](0035-suggested-match-hint.md)) |
| catalog title (`Alligator 2 A Mutação`) | the right movie, first result, score **100** |

Across the sample, **21 of 25** movies reach `LimiarAutoMatch` when searched by the catalog
title, against about **0** by the release name. Three failure shapes account for the rest:

1. **Garbage in the release name** — `Alligator2.91`, `TMNT2.91`, `VendedorArma.89`: the
   scan's parser cuts the year at `(year)`, and a dot-separated release keeps quality tokens
   (`BD1080p`, `MemoriadaTV`) glued to the title.
2. **Cross-language** — the file is in English (`Get.The.Gringo`, `Rush Hour 3`) and the
   folder in pt-BR (`Plano de Fuga`, `A Hora do Rush 3`). The provider answers in the
   library language, so the two never agree.
3. **Wrong year in the folder** — `Transformers 2 - A Vingança dos Derrotados (2011)`: the
   right candidate (2009) exists, but the year veto discards it. 1 case in 25; the veto
   stays, it is what protects against remakes.

The other types do not have this problem: a series already searches its **folder**
(`tituloAnoDaPastaDaSerie`), and an artist searches its own title because it has no file of
its own ([ADR-0027](0027-music-model-artist-album-track.md)).

## Decision

The search term becomes a **list of attempts, tried in order**, and the caller stops at the
first one that reaches `LimiarAutoMatch` (`Server.variantesTituloAno`, `automatch.go`):

| type | attempts |
|---|---|
| movie | **catalog title** (with the year of `release_date`), then the release name |
| series | the series folder, with the entity title as fallback |
| artist | the entity title, no year |
| anything else | the release name of the first file |

1. **Catalog first, file second.** The catalog title wins because it is the part a human
   organized and it is what the provider indexes; the release name stays as the second
   attempt because a draft titled `Draft` has nothing better than its file. Preference order,
   not an OR: the shortcut at the threshold keeps the cost identical to before whenever the
   first attempt resolves (one provider call, not two).
2. **No attempt without a file.** An entity with no media file gets an empty list: manual
   drafts and orphans are never searched on their own, the old contract of
   `tituloAnoDoArquivo`.
3. **Duplicates collapse.** Attempts whose titles are equal once normalized
   (`matching.NormalizarNome`) become one — a well-named release does not pay for a second
   identical search.
4. **One implementation, three callers.** The auto-match, the match preview and the
   retroactive enrichment all iterate the same list through
   `melhorCandidatoLocalEntre`/`melhorCandidatoExternoEntre`. The preview must show what the
   auto-match would do, and the retroactive job must be able to resolve what the queue could
   not.
5. **The year veto is untouched.** A candidate whose year diverges by more than one year is
   still discarded ([MD1](0008-identity-and-matching-kodi-jellyfin.md)). Fixing shape 3
   above would mean trusting a title match over a year mismatch — reopen the decision only
   with data that justifies it.

## Consequences

- `internal/server/automatch.go`: `consultaTituloAno`, `variantesTituloAno`,
  `dedupeConsultas`, `melhorCandidatoLocalEntre`, `melhorCandidatoExternoEntre`.
  `tituloAnoDoArquivo` no longer lists the files (the caller does) and returns the release
  name only.
- The retroactive enrichment cap goes from **15 to 100 items per run**
  (`limiteEnriquecimentoRetroativoPorExecucao`): at 15 the 625-item backlog needed 42 daily
  runs, and the items it now resolves are the ones it used to only flag.
- `match_preview` reports the best score across the attempts, so the number the owner sees
  is the number the auto-match would act on.
- Not solved, deliberately: a movie whose folder year is wrong still misses (shape 3), and
  the release name's quality tokens are still part of the second attempt. Both are separate
  changes with their own risk.
- The `doc 09`/NFO layer is unaffected: the export always used the catalog title.
