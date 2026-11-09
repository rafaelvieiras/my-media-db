# 0136 — Matching de música: nome de edição, empate de artista e compilações

> Tira a fila de revisão da música do lugar onde ela está: 185 faixas em rascunho
> (43 álbuns) por três causas mecânicas de comparação de texto, não por falta de
> correspondência na fonte.

| | |
|---|---|
| **ID** | 0136 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Verticais e plugins (música) |
| **Status** | done |
| **Docs relacionadas** | [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md), [ADR-0027](../docs/adr/0027-modelo-de-musica-hierarquia-artista-album-faixa.md), [ADR-0029](../docs/adr/0029-import-de-musica-local-first-por-faixa-possuida.md) |
| **Depende de** | 0115, 0120, 0121, 0124, 0135 |

---

## 1. Objetivo

Medido no catálogo real (08/nov/2026, 185 faixas em `status='draft'`, 43 álbuns):

| causa | faixas |
|---|---|
| álbum da tag tem sufixo de edição (`Hybrid Theory (Bonus Edition)`, `Rio (Collector's Edition)`, `The Singles (Expanded)`, `Right Here, Right Now (EP)`) e o release-group do MusicBrainz não tem esse sufixo | 104 |
| `album_artist = Various Artists` (compilação/trilha): a busca de artista é o ponto de partida do pipeline e não devolve um artista único | 34 |
| empate de artista (dois "Linkin Park" com score 100 → `melhorCandidatoTexto` devolve `false` por "ambíguo") e colaborações longas (`Gary Burton & Chick Corea & …`, score 83,7) | 47 |

O pipeline atual compara **texto cru** com corte único de 92% (Jaro-Winkler) em
três pontos: artista (`scanner.melhorCandidatoTexto`), nome do release-group
(`musicbrainz.melhorReleaseGroupPorTitulo`) e título da faixa
(`enrich.escolherFaixaAlvo`). Qualquer qualificador que o tagueador gravou e a
fonte não gravou derruba o score abaixo do corte — e um empate no topo é tratado
como "ambíguo, desiste", mesmo quando um dos candidatos contém o álbum da faixa.

## 2. Jornada do usuário

1. O dono escaneia a biblioteca Musicas (com as tags ID3 que os scripts dele
   gravam: `artist`, `album_artist`, `album`, `title`, `track_number`).
2. Faixas com álbum `(Deluxe Edition)`/`(Bonus Edition)`/`(Remastered)` passam a
   casar no release-group **sem** o qualificador.
3. Faixas de compilação (`Various Artists`) passam a casar o álbum pela busca de
   release-group por título + confirmação da faixa dentro da tracklist; o artista
   vem do próprio artist-credit do release (na prática, "Various Artists").
4. Empate de artista deixa de ser desistência: o scanner testa os candidatos
   empatados (até 3) contra **o álbum da tag** e aceita o primeiro que contiver a
   faixa-alvo.
5. Título de faixa com sufixo de versão (`Alright (Remastered)`,
   `Question And Answer (Album Version)`) casa contra a faixa `Alright` /
   `Question And Answer` da tracklist.
6. O que continua sem correspondência real permanece em rascunho — a regra de
   nunca aplicar um match "parecido" não muda.

## 3. Escopo

**Dentro:**
- `internal/matching`: `NormalizarNome(s)` (remove sufixos de edição/versão:
  deluxe, bonus, expanded, remaster(ed), collector, special, anniversary,
  edition, version, explicit, deluxe edition, EP/single quando entre parênteses,
  `- Remastered`, `[Explicit]`) e `SimilaridadeNormalizada(a, b)` = máximo entre
  a similaridade crua (comportamento atual) e a normalizada — **nunca pior que
  hoje**.
- `internal/scanner/musica.go`: `melhorCandidatoTexto` passa a devolver os
  candidatos empatados no topo (até `maxCandidatosArtista = 3`) e o pipeline
  tenta cada um com `BuscarAlbumPorNome` + faixa-alvo, aceitando o primeiro que
  resolver.
- `internal/provider/musicbrainz`: `melhorReleaseGroupPorTitulo` e o match de
  faixa do `buscarFaixasComAlvo`/`escolherFaixaAlvo` usam
  `SimilaridadeNormalizada`; nova capacidade opcional
  `provider.MusicaAlbumPorTituloProvider` (`BuscarAlbumPorTitulo(ctx, nomeAlbum,
  alvoFaixa, langs) (AlbumExterno, bool, error)`) que busca release-groups por
  título (`/release-group?query=…`), confirma a faixa-alvo na tracklist e devolve
  o artist-credit do release escolhido.
- `internal/enrich/musica.go`: `ImportarFaixaPossuida` refatorado para um
  `importarFaixaComAlbum` (corpo pós-artista) + novo
  `ImportarFaixaPorTituloDeAlbum(ctx, source, alvo, langs)` que resolve o álbum
  pela capacidade nova e usa o artist-credit dele como artista principal
  (compilações). Sem a capacidade, degrada em silêncio (draft, como hoje).
- `internal/scanner/musica.go`: passo 4 do pipeline ganha o fallback por título
  de álbum (só quando o caminho por artista não resolveu), e o `MusicBrainz`
  passa a ser usado como `MusicaAlbumPorTituloProvider`.

**Fora (explicitamente):**
- Fuzzy mais permissivo que 92% (decisão mantida: match inseguro contamina a
  árvore inteira).
- Reidentificação por ISRC (as tags têm ISRC; a API de busca do MusicBrainz aceita
  `isrc:` — fica como follow-up, precisa de validação de cobertura).
- Fingerprint acústico além do que o 0126/0127 já fazem (AcoustID segue sem
  client key configurada).
- Tocar no matching de filme/série.

## 4. Contrato técnico

### Backend (Go)
- `internal/matching` — API nova, pura e sem I/O:
  ```go
  func NormalizarNome(s string) string
  func SimilaridadeNormalizada(a, b string) float64
  ```
  `NormalizarNome` normaliza via `textsim.normalizar` e remove os segmentos
  entre parênteses/colchetes cujo conteúdo casa um token da lista de edição, mais
  os sufixos ` - <token de edição>` no fim. Nunca devolve vazio: se a remoção
  zerar a string, devolve a normalização crua.
- `provider.MusicaAlbumPorTituloProvider` (interface opcional, type-assert no
  enrich — mesmo padrão de `MusicaAlbumProvider`).
  ```go
  type MusicaAlbumPorTituloProvider interface {
      BuscarAlbumPorTitulo(ctx context.Context, nomeAlbum string, alvo AlvoFaixaAlbum, langs []string) (AlbumExterno, bool, error)
  }
  ```
- MusicBrainz: `BuscarAlbumPorTitulo` faz **1** busca (`/release-group?query=`)
  e no máximo `maxReleaseGroupsPorTitulo = 3` tentativas de tracklist
  (`buscarFaixasComAlvo`), respeitando o rate-limit de 1 req/s já existente.
- `enrich.ImportarFaixaPorTituloDeAlbum` só é usado pelo scanner; o import manual
  (`Importar` tipo artista) não muda.

### Arquivos afetados
- `internal/matching/matching.go` (+ testes), `internal/textsim` (reuso).
- `internal/provider/musicbrainz/musicbrainz.go` (+ testes).
- `internal/provider/provider.go` (interface nova).
- `internal/enrich/musica.go` (+ testes).
- `internal/scanner/musica.go` (+ testes).

## 5. Critérios de aceite

- [ ] `SimilaridadeNormalizada("Hybrid Theory (Bonus Edition)", "Hybrid Theory")`
      devolve ≥ `LimiarAutoMatch`; a similaridade crua entre as duas é < 92.
- [ ] Álbum com sufixo de edição resolve o release-group na fonte (teste com
      fixture do MusicBrainz).
- [ ] Empate de artista com o álbum disponível em só um dos candidatos resolve
      por evidência; sem evidência em nenhum, cai em rascunho (comportamento
      atual preservado).
- [ ] Compilação (`album_artist = Various Artists`) resolve o álbum por título e
      liga a faixa ao álbum certo, com o artista do artist-credit do release.
- [ ] Faixa com sufixo de versão no título casa a faixa sem sufixo.
- [ ] Nenhuma regressão nos testes existentes de música (0115/0121/0124/0126).

## 6. Notas de implementação / decisões

- O corte de 92% continua; o que muda é **o que** se compara (texto normalizado,
  e máximo entre cru/normalizado). Um nome completamente diferente continua não
  casando.
- Desempate por álbum custa chamadas extras ao MusicBrainz (1 req/s). Por isso o
  teto de 3 candidatos e a ordem: só os empatados no topo entram.
- `Various Artists` como artista local **não** é criado à mão: a identidade vem
  do artist-credit do release-group devolvido pela fonte.

## 7. Fora deste briefing / follow-ups

- Busca por ISRC (`isrc:` na query do MusicBrainz) como passo 3.5 do pipeline.
- Reaproveitar `NormalizarNome` no matching de filme/série (hoje só música).
