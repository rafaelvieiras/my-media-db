# 0117 — Fix: artista fica sem arte do fanart.tv e sem `mbid` quando enriquecido fora do import de árvore

> O caminho GENÉRICO de enriquecimento (auto-match, retroativo/0116, "Corrigir",
> "Confirmar candidato", `POST /entities/{id}/enrich`) nunca disparava a busca
> de arte para `artista` e gravava a identidade sob a chave errada
> (`"musicbrainz"` em vez de `"mbid"`) — o mesmo bug de classe que o 0087 já
> tinha corrigido para `movie`, só que ninguém tinha fechado ainda para música.

| | |
|---|---|
| **ID** | 0117 |
| **Tipo** | fix |
| **Fase (roadmap)** | Transversal / infraestrutura operacional — mesma classe de bug do [0087](0087-fix-inventario-fanarttv-fora-do-import-explicito.md), agora na vertical de música. |
| **Docs relacionadas** | [08-artwork-e-assets](../docs/08-artwork-e-assets.md), [ADR-0027](../docs/adr/0027-modelo-de-musica-hierarquia-artista-album-faixa.md) |
| **Status** | done |
| **Depende de** | [0048](0048-fundacao-providers-de-enriquecimento-arte-trailer-tema.md), [0106](0106-provider-musicbrainz-cover-art-archive.md), [0116](0116-enriquecimento-retroativo-musica-artista.md) |

---

## 1. Objetivo

Depois de deployar o 0116 (retroativo cobre `artista`), o dono rodou o job de
saúde/retroativo duas vezes em produção e reportou: a maioria dos artistas
continua sem foto — só os poucos importados manualmente pela árvore completa
("Importar") têm arte. Investigação encontrou **duas causas**, na mesma
função (`enriquecerBase`, `internal/enrich/enrich.go`):

1. **Sem gatilho de arte.** `enriquecerBase` só chama `enriquecerArtes` para
   `tipo==movie` e sincroniza árvore para `tipo==tv` — nunca para
   `tipo==artista`. `importarArtista`/`ImportarFaixaPossuida` (`musica.go`)
   chamam `enriquecerArtes` na criação, mas **nenhum dos dois passa por
   `enriquecerBase`** — só o import de árvore/faixa dispara. Qualquer artista
   que ganha a identidade pelo caminho genérico (auto-match, retroativo,
   "Corrigir", "Confirmar candidato", `POST /entities/{id}/enrich`) nunca
   tinha sua arte buscada. Mesma causa-raiz do 0087, nunca fechada para música.
2. **Chave errada no `external_ids`.** `mesclarExternalIds` grava
   `ids[source] = sourceID` — para `source=="musicbrainz"` isso grava a chave
   `"musicbrainz"`, não `"mbid"`. Mas **todo** o resto do sistema (coluna
   gerada `external_id_mbid`, `ObterEntityPorMbid` — dedup de
   artista/álbum/faixa — e `idsExternosDe`, que alimenta o `BuscarArtes` do
   fanart.tv) só lê a chave `"mbid"`. Resultado: mesmo corrigindo a causa 1,
   `idsExternosDe` continuaria devolvendo `ok=false` (nenhum id utilizável) e
   `BuscarArtes` nunca seria chamado de fato — e, pior, um artista aplicado
   por este caminho ficava **invisível ao dedup por MBID**, arriscando criar
   uma segunda entity para o mesmo artista real na próxima vez que uma faixa
   dele fosse importada por árvore/scan.

Como consequência direta da causa 2, a detecção de colisão
(`acharColisaoExterna`, briefing 0051) também não cobria `mbid`/`musicbrainz`
— nenhum caso de teste any tinha exercitado esse par fonte/chave até agora.

## 2. Escopo

**Dentro:**
- `enriquecerBase` passa a chamar `enriquecerArtes` também para
  `tipo==artista` (mesmo guard `sincronizarArvore` do `movie`, sem risco de
  dupla-chamada — nenhum call site de `artista` passa `sincronizarArvore=false`).
- `mesclarExternalIds` grava a identidade musicbrainz sob a chave `"mbid"`
  (`musicext.ChaveExternalIDMbid`), nunca sob o nome literal da fonte —
  desvio isolado em `chaveExternalID(source)`.
- `acharColisaoExterna` ganha um caso para `mbid`/`musicbrainz` (os dois
  valores de `source` que chegam à função, vindos de call sites diferentes —
  ver §6), com a query nova `ListarEntitiesPorMbidExcetoId` (espelha
  `ListarEntitiesPorTmdbExcetoId`).

**Fora (explicitamente):**
- Backfill dos artistas JÁ aplicados incorretamente em produção antes deste
  fix (external_ids gravado sob `"musicbrainz"`, sem arte buscada) — como
  `last_external_match_attempt_at` já foi carimbado nessas execuções, o
  throttle do job os deixa fora do próximo lote por `retryDays` (padrão 15
  dias); resolvido operacionalmente após o deploy (§7), sem código novo.
- Qualquer outra fonte além de MusicBrainz com esse mesmo desalinhamento
  nome-da-fonte↔chave — hoje é a única.

## 3. Contrato técnico

### Backend (Go)
- `internal/enrich/enrich.go`: novo branch `if tipo == tipoArtista &&
  sincronizarArvore { s.enriquecerArtes(...) }` em `enriquecerBase`; nova
  função `chaveExternalID(source string) string`; `mesclarExternalIds` usa
  `ids[chaveExternalID(source)]` em vez de `ids[source]`.
- `internal/musicext/musicext.go`: novas constantes `FonteMusicBrainz`
  ("musicbrainz") e `ChaveExternalIDMbid` ("mbid").
- `internal/server/duplicata.go`: `acharColisaoExterna` ganha
  `case musicext.FonteMusicBrainz, musicext.ChaveExternalIDMbid:` chamando
  `ListarEntitiesPorMbidExcetoId`.
- `internal/db/queries/entity.sql` + sqlc: nova query
  `ListarEntitiesPorMbidExcetoId` (espelha a de tmdb).

### Arquivos afetados
- `internal/enrich/enrich.go`, `internal/musicext/musicext.go`,
  `internal/server/duplicata.go`, `internal/db/queries/entity.sql`,
  `internal/db/sqlcgen/entity.sql.go` (gerado).
- Testes: `internal/enrich/artes_test.go`
  (`TestEnriquecerArtistaForaDoImportPersisteInventarioArtesEChaveMbid`),
  `internal/server/external_ids_dup_test.go`
  (`TestExternalIdsMbidDetectaColisaoEFunde`).

## 4. Critérios de aceite

- [x] Um `artista` sem `external_ids` que ganha identidade via
      `Enriquecer()` (fora de `importarArtista`/`ImportarFaixaPossuida`)
      passa a ter o inventário fanart.tv persistido (`media_external`
      source=fanarttv), igual ao import de árvore.
- [x] A identidade aplicada por esse caminho fica sob `external_ids["mbid"]`
      — nunca sob `"musicbrainz"`.
- [x] Repetir a chamada não duplica o inventário nem a identidade
      (idempotência preservada).
- [x] `PUT /entities/{id}/external-ids` com `{"mbid": "..."}` detecta colisão
      com outra entity já catalogada pelo mesmo mbid e funde automaticamente
      (sem investimento do dono) — mesma rede de segurança do 0051.
- [x] `go build ./...`, `go vet ./...`, `go test ./... -count=1` verdes.

## 5. Notas de implementação / decisões

- O desalinhamento nome-da-fonte↔chave é **exclusivo de MusicBrainz**: tmdb/
  imdb/tvdb sempre tiveram `Provider.Nome() == chave-do-external_ids`, então
  esse bug de classe nunca apareceu antes. `chaveExternalID` documenta essa
  exceção explicitamente em vez de esconder um `if` solto no meio da função.
- Dois valores de `source` diferentes chegam a `acharColisaoExterna` para o
  MESMO conceito: `aplicarCandidatoExterno`/`aplicarIdentidadeExterna` passam
  `Provider.Nome()` ("musicbrainz"), enquanto `PUT /external-ids` itera a
  chave BRUTA do JSON que o cliente grava ("mbid"). O `case` cobre os dois.
- Sem risco de dupla-chamada de `enriquecerArtes` para `artista`: o parâmetro
  `sincronizarArvore=false` só é usado pelos dois call sites internos de
  `importarItemBase`, que o dispatcher `Importar()` nunca invoca para
  `tipo=="artista"` (vai direto para `importarArtista`).

## 6. Fora deste briefing / follow-ups

- Um backfill explícito (endpoint ou job avulso) para re-tentar imediatamente
  os poucos artistas que a produção já aplicou com a chave errada, sem
  esperar o throttle de 15 dias — avaliar só se o dono notar que algum
  artista específico continua sem imagem depois do próximo ciclo do job.
