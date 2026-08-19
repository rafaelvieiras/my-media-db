# 0105 — Fundação do schema de música

> Introduz a vertical música (artista/álbum/faixa) no núcleo: tipos de
> `entity`, namespace `extension_data core.music@1`, coluna gerada
> `external_id_mbid` e os `kind` de `entity_relation` que amarram a hierarquia.

| | |
|---|---|
| **ID** | 0105 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Verticais e plugins (música) |
| **Status** | draft |
| **Docs relacionadas** | [ADR-0027](../docs/adr/0027-modelo-de-musica-hierarquia-artista-album-faixa.md), [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [12-roadmap](../docs/12-roadmap.md) |
| **Depende de** | — |

---

## 1. Objetivo

Botar no ar a **fundação de dados** da vertical música, sem provider, scanner
ou UI ainda — só o suficiente para que uma entity `type='artista'`, `'album'`
ou `'faixa'` seja um cidadão de 1ª classe do núcleo (resolver, override,
busca FTS, artwork, posse) exatamente como filme/série já são. Isso valida
empiricamente a promessa central do projeto (AGENTS.md §3): uma vertical
nova entra **sem migration destrutiva do core**, porque `entity` e
`entity_relation` já são genéricos o bastante.

Este briefing existe separado do provider/scanner/UI (que vêm em briefings
seguintes) pelo mesmo motivo do MD6 (elenco/equipe): decisão de fundação
primeiro, superfície de uso depois — dá para validar o modelo de dados com
dados criados manualmente antes de investir em MusicBrainz/scanner/telas.

## 2. Jornada do usuário

Este briefing não expõe nada novo na UI. O efeito observável, indireto, é:

1. O dono cria uma entity com `type='artista'` (hoje só via API/seed, a UI de
   "novo item" — briefing 0030 — continua funcionando para os tipos que já
   conhece; registrar `artista`/`album`/`faixa` no formulário dirigido por
   schema fica para o briefing de UI).
2. Ele cria `album`/`faixa` associados e liga a hierarquia via
   `entity_relation` (`music_album`, `music_artista_principal`).
3. A entity aparece no grid genérico como qualquer outra (título, tags,
   busca FTS funcionam) — mesmo sem nenhuma arte/dado externo, porque o
   núcleo não distingue tipo de vertical.

## 3. Escopo

**Dentro:**
- Migration: coluna gerada `entity.external_id_mbid` (`json_extract($.mbid)`)
  + índice, espelhando `external_id_tvdb` (migration 0007).
- Pacote `internal/musicext` (molde de `internal/tvext`): parser único do
  namespace `core.music@1` — `track_number`, `disc_number`, `disc_total`,
  `genero` (quando presente no álbum/artista).
- Constantes Go para os `kind` de `entity_relation` da vertical:
  `music_album`, `music_artista_principal`, `music_artista_feat`.
- Constante do MBID canônico de "Various Artists"
  (`89ad4ac3-39f7-470e-963a-56509c546377`), documentada e testada (não é
  caso especial de código — é só o valor certo a usar ao ligar uma
  compilação a esse artista).
- Testes cobrindo: `external_id_mbid` gerado/indexado corretamente,
  `musicext.Namespace` lendo `core.music@1`, hierarquia via
  `entity_relation` (artista→álbum→faixa, faixa com feat. divergente do
  albumartist) montável e navegável com as queries já existentes de
  `entity_relation` (sem query nova — reaproveita `internal/db/queries/entity_relation.sql`).

**Fora (explicitamente):**
- Provider MusicBrainz/Cover Art Archive (busca, import, artwork) — briefing
  seguinte, a numerar.
- Scanner/matching de arquivo↔faixa (mediainfo, tags ID3, ISRC) — briefing
  seguinte.
- UI: formulário de "novo item" reconhecendo os 3 tipos novos, tela de
  detalhe de artista/álbum — briefing seguinte.
- Validação formal de `extension_data` por JSON Schema — ainda não existe
  para nenhuma vertical (nem `core.tv@1`); não é este briefing que introduz
  o maquinário.
- Vínculo N:M arquivo↔faixa (medley/DJ-mix) — decisão adiada no ADR-0027
  item 5; não implementar antecipadamente.

## 4. Contrato técnico

### Dados / modelo
- Migration `internal/db/migrations/0023_musica.sql`:
  ```sql
  -- +goose Up
  ALTER TABLE entity ADD COLUMN external_id_mbid TEXT
      GENERATED ALWAYS AS (json_extract(external_ids_json, '$.mbid')) VIRTUAL;
  CREATE INDEX idx_entity_external_id_mbid ON entity (external_id_mbid);

  -- +goose Down
  DROP INDEX IF EXISTS idx_entity_external_id_mbid;
  ALTER TABLE entity DROP COLUMN external_id_mbid;
  ```
- `entity.type`: sem `CHECK` (já é `TEXT` livre) — `'artista'`, `'album'`,
  `'faixa'` passam a ser valores válidos por convenção, documentados no
  glossário ([02-glossario](../docs/02-glossario.md)), sem alterar schema.
- `extension_data` namespace `core.music@1`:
  ```json
  {
    "extension_data": {
      "core.music@1": {
        "track_number": 3,
        "disc_number": 1,
        "disc_total": 2,
        "isrc": "USRC17607839"
      }
    }
  }
  ```
  Gênero vive no `core.music@1` do **álbum** (ou do artista, se o álbum não
  tiver), não duplicado na faixa (ADR-0027 item 7).
- `entity_relation`: `kind='music_album'` (parent=álbum, child=faixa,
  `sort_index=track_number`); `kind='music_artista_principal'`
  (parent=artista, child=álbum **ou** faixa, `sort_index`=ordem de billing);
  `kind='music_artista_feat'` (parent=artista, child=faixa, colaboração
  pontual). Reaproveita a tabela e as queries existentes — nenhuma migration
  extra além da coluna gerada acima.

### API
- Nenhum endpoint novo neste briefing. A API genérica de entity/relation já
  serve os tipos novos sem alteração (é o próprio ponto do princípio #3).

### Backend (Go)
- `internal/musicext/musicext.go` (+ `_test.go`): reexporta/usa
  `tvext.Namespace`/`tvext.AsInt` internamente ou duplica o parser mínimo —
  decidir no PR pela forma mais simples; **não** criar dependência cruzada
  esquisita entre `tvext` e `musicext` só para evitar 20 linhas duplicadas.
- `internal/musicext/constantes.go`: `KindAlbum`, `KindArtistaPrincipal`,
  `KindArtistaFeat`, `MBIDVariousArtists`.

### Frontend (Vite/React)
- Nenhuma mudança neste briefing.

### Arquivos afetados
- `internal/db/migrations/0023_musica.sql` (novo)
- `internal/musicext/musicext.go`, `internal/musicext/musicext_test.go` (novo)
- `docs/02-glossario.md` (adicionar `artista`/`album`/`faixa` e os `kind` de
  relação novos)

## 5. Critérios de aceite

- [ ] `go test ./internal/musicext/... -count=1` cobre: leitura de
      `core.music@1`, coerção de `track_number`/`disc_number`/`disc_total`.
- [ ] Criar entity `type='faixa'` com `external_ids_json={"mbid":"..."}`  e
      confirmar `external_id_mbid` gerado e indexado (`EXPLAIN QUERY PLAN`
      usa o índice numa busca por `external_id_mbid`).
- [ ] Montar hierarquia artista→álbum→faixa via `entity_relation` (queries
      existentes) e navegar parent↔child nos dois sentidos.
- [ ] Faixa com artista divergente do álbum (feat.) representável só com
      `music_artista_principal` (no álbum) + `music_artista_feat` (na
      faixa), sem exigir estrutura nova.
- [ ] Entity `type='artista'` com MBID `89ad4ac3-...` (Various Artists)
      criável e reutilizável como `child` de `music_artista_principal` em
      múltiplos álbuns de compilação.
- [ ] `go build ./...`, `go vet ./...`, `go test ./... -count=1` verdes.
- [ ] `make gate-local` limpo antes do commit final.

## 6. Notas de implementação / decisões

Toda decisão estrutural (hierarquia, provider, vínculo arquivo↔faixa,
artista-vs-person, Various Artists) já foi tomada no
[ADR-0027](../docs/adr/0027-modelo-de-musica-hierarquia-artista-album-faixa.md) —
este briefing só materializa a fundação de dados. Se durante a implementação
surgir um caso não coberto pelo ADR, **parar e voltar para a decisão**, não
inventar solução ad-hoc no meio da migration.

## 7. Fora deste briefing / follow-ups

- **Provider MusicBrainz + Cover Art Archive** (busca local-first, import,
  artwork) — próximo briefing, a numerar.
- **Scanner/matching de arquivo↔faixa** (mediainfo, tags ID3/ISRC,
  auto-match) — briefing seguinte.
- **UI**: formulário de novo item reconhecendo os 3 tipos, tela de detalhe
  de artista/álbum, grid filtrável por vertical — briefing seguinte.
- **Enriquecimento cruzado** (Spotify/Deezer/Discogs/Last.fm como fontes
  adicionais) — fora de escopo até o provider base estar redondo.
