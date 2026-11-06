# 0133 — Episódio provisório para arquivo que a árvore externa não tem

> Arquivo de série com numeração conhecida mas sem episódio correspondente na
> árvore importada deixa de ficar órfão: ganha um episódio provisório na
> temporada certa, com a numeração lida do arquivo, para o NFO sair e a
> biblioteca consumidora não perder o sinopse de novo.

| | |
|---|---|
| **ID** | 0133 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 2 — Biblioteca e scan |
| **Status** | done |
| **Docs relacionadas** | [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md), [08-artwork-e-assets](../docs/08-artwork-e-assets.md), [0120](0120-reconciliacao-de-filhos-orfaos-musica-serie.md) |
| **Depende de** | — |

---

## 1. Objetivo

O scanner de série casa o número do arquivo contra a árvore de episódios
importada da fonte externa (`tvmatch.Casar`). Quando o número **não existe**
naquela árvore, o arquivo fica com `media_file` apontando para a série, **sem
vínculo de episódio** e com `match_confidence` de revisão (40). Nada é criado:
nem episódio, nem `media_file_entity`, nem NFO.

O efeito aparece longe do scanner. Como o NFO nasce do episódio, o arquivo
órfão nunca ganha `.nfo`, e o servidor que lê a biblioteca do disco (Tinyfin)
mostra o episódio sem sinopse e sem título real. Medido no catálogo da sala em
06/nov/2026: **16 arquivos** nessa situação, todos os 16 sem `overview` no
Tinyfin:

| Série | Arquivos | Por quê |
|---|---|---|
| The Office (US) | `S03E24`, `S03E25`, `S06E25`, `S06E26` | a árvore tem 23 eps na T3 e 24 na T6 |
| Buzz Lightyear do Comando Estelar | `S02E06`..`S02E11` | a árvore só tem a Temporada 1 (62 eps) |
| Jeannie É um Gênio | `S03E27` | a árvore tem 26 eps na T3 |
| Du, Dudu e Edu | 5 arquivos em `Season 00` | nomes sem `SxxEyy` (`Groovies.mkv`, `Especial de Natal - ...`) |

Não é caso de arquivo corrompido nem de nome malformado: o disco tem o arquivo
certo, o número é legível, e a fonte simplesmente não tem aquele episódio (edição
estendida, episódio duplo, temporada que o provider não modela, especial sem
numeração). O princípio 2 do projeto — *o banco é maior que a biblioteca* — já
prevê o contrário do que acontece hoje: o que está no disco precisa existir no
banco, mesmo sem correspondência externa.

## 2. Jornada do usuário

1. O dono coloca no disco uma temporada que a fonte externa numera diferente
   (episódio duplo, especial, temporada ausente no provider).
2. Roda o scan da biblioteca.
3. **Antes:** o arquivo aparece na fila de revisão para sempre, sem episódio; o
   NFO nunca é gravado; o servidor de mídia mostra "Episódio" sem sinopse.
4. **Depois:** o arquivo vira um episódio provisório na temporada correta, com a
   numeração lida do nome e o título do arquivo; o NFO é gravado como para
   qualquer outro episódio; a biblioteca consumidora mostra título e episódio no
   lugar certo.
5. Quando a fonte passar a modelar aquele episódio, a reconciliação já existente
   (briefing 0120) religa o arquivo ao episódio externo, e o provisório fica como
   está — nunca é apagado (princípio 5, não-destrutivo).

## 3. Escopo

**Dentro:**
- Scanner de série: quando o arquivo é reconhecido como episódio (ou está numa
  pasta de temporada) e o casamento contra a árvore não acha correspondente,
  criar/reusar a temporada e o episódio **provisórios** e vincular o arquivo.
- Marca do provisório: `entity.status = 'draft'` e um campo no `extension_data`
  dizendo que a numeração veio do arquivo, não da fonte.
- `match_confidence` do arquivo sobe de 40 (revisão) para 90 (casado pelo
  número do arquivo), porque o número é o do disco e não há ambiguidade.
- Episódio provisório entra no universo do job `midias_faltantes`, então ganha
  NFO pelo caminho normal.

**Fora (explicitamente):**
- Buscar na fonte externa qual episódio "real" corresponde ao arquivo (match
  fuzzy por título). O provisório é provisório: quem resolve depois é a
  reconciliação do 0120.
- Criar episódio provisório para série em revisão (sem árvore carregada): aí não
  há como saber temporada nem número confiáveis.
- Mexer na numeração de episódios que já existem, ou renumerar temporada.
- Artwork do provisório (poster/still): fica sem, como qualquer episódio sem
  fonte.

## 4. Contrato técnico

### Dados / modelo
- Nenhuma migration. Usa o que existe: `entity` (type `season`/`episode`),
  `entity_relation` (`tv_season`, `tv_episode`) e `media_file_entity`.
- `extension_data` do episódio provisório:
  ```json
  {"core.tv@1": {"numbering": {"official": {"season": 3, "number": 24}},
                 "provisorio": {"origem": "arquivo", "arquivo": "The.Office.S03E24..."}}}
  ```
  A chave `provisorio` é o marcador — nenhum outro fluxo a escreve, e o
  `enrich` a ignora (ele grava só `numbering`/`still_url`).
- Título: com número, `Episódio N` — o mesmo padrão que o enriquecimento usa
  quando a fonte não dá título, e o que o resto do banco já mostra. Sem
  numeração (`Season 00` com `Groovies.mkv`), o título é o nome do arquivo
  limpo por `naming.Parse` (fallback: o nome cru). O nome original nunca se
  perde: ele é o `media_file.path`.
- Temporada provisória: título `Temporada N` (ou `Especiais` para N = 0),
  `sort_index` = N, criada só se não existir filho `tv_season` com aquele
  `sort_index`.

### Backend (Go)
- `internal/scanner/serie.go`: `factsSerie` passa a guardar temporada e número
  lidos do arquivo (`epSeason`, `epNumber`, `temNumero`) e a pasta de temporada
  (`folderSeason`, quando o nome não traz `SxxEyy`).
- `gravarArquivoSerie`: quando `len(f.episodeIDs) == 0` e a série está
  resolvida (com árvore, não em revisão), chama
  `acharOuCriarProvisorio(...)` e usa o id devolvido em `f.episodeIDs`.
- Novas funções, no mesmo arquivo, no padrão find-or-create já usado pelo
  scanner (queries sqlc existentes: `CriarEntity`, `CriarVinculoEntidade`,
  `ListarRelacoesFilhas`):
  - `acharOuCriarTemporadaProvisoria(ctx, q, serieID string, season int)` —
    dedup por (pai, `tv_season`, `sort_index`).
  - `acharOuCriarEpisodioProvisorio(ctx, q, tempID string, season int,
    number int, comNumero bool, titulo string)` — dedup por (pai, `tv_episode`,
    `sort_index` = number) quando há número; sem número, dedup por título exato
    na mesma temporada.
- Log: uma linha por provisório criado (`scanner: episódio provisório %q ->
  S%02dE%02d`), para o operador ver o que a fonte não modela.

### Arquivos afetados
- `internal/scanner/serie.go`
- `internal/scanner/serie_test.go` (ou teste novo `serie_provisorio_test.go`)

## 5. Critérios de aceite

- [ ] Arquivo `... S03E24.mkv` numa série cuja árvore tem até o 23 cria o
      episódio provisório na Temporada 3 com número 24, `status='draft'`,
      vinculado ao arquivo, e o `match_confidence` do `media_file` deixa de ser
      40.
- [ ] Re-scan do mesmo arquivo **não** cria um segundo episódio provisório
      (idempotência por temporada + número).
- [ ] Arquivo em `Season 00` sem `SxxEyy` cria episódio provisório na temporada
      0 com o título do arquivo, e o re-scan também é idempotente.
- [ ] Série em revisão (sem árvore) continua sem provisório — comportamento
      atual preservado.
- [ ] O job `midias_faltantes` grava o NFO do provisório e o arquivo passa a
      aparecer com `overview` no servidor que lê o disco.
- [ ] `go build ./... && go vet ./... && go test ./... -count=1` verdes.

## 6. Notas de implementação / decisões

- **Por que provisório e não "arquivo sem episódio" como hoje**: sem episódio
  não há onde pendurar NFO, arte nem sinopse; toda a cadeia a jusante depende da
  entity. O provisório é a menor peça que devolve o arquivo ao fluxo normal.
- **Por que `status='draft'`**: é o mesmo marcador que a série em revisão usa, a
  UI já o conhece, e ele diz a verdade — o dado veio do disco, não da fonte.
- **Por que o dedup do caso sem número é por título**: sem número não existe
  chave natural; o título provisório é o nome do arquivo, que é estável entre
  scans. Dois arquivos com o mesmo nome na mesma temporada são o mesmo episódio
  por definição do Kodi.
- **Extras não viram episódio**: nome com `featurette`, `sample`, `trailer`,
  `bonus`, `deleted`, `bloopers`, `interview`, `making of` (e afins) dentro de
  pasta de temporada continua indo para a revisão sem vínculo — criar episódio
  para material extra seria mentir sobre a biblioteca. Vale só para o caso SEM
  número; um `S01E05 - trailer.mkv` com número é episódio.
- **Precedente**: a música já faz exatamente isso — `criar faixa rascunho`
  (`internal/scanner/musica.go`) quando o match falha, e a reconciliação do 0120
  religa depois. Este briefing aplica o mesmo padrão à vertical de série.
- **Por que não marcar o provisório como `missing`/oculto**: ele tem arquivo de
  verdade no disco; esconder seria mentir sobre a biblioteca.

## 7. Fora deste briefing / follow-ups

- Escolher automaticamente o episódio externo "mais próximo" por título
  (fuzzy) quando o número não existe: fica para um briefing de match assistido.
- Renumerar/limpar provisórios que a fonte passou a modelar: o 0120 já religa o
  arquivo; apagar o provisório órfão seria uma decisão destrutiva à parte.
