# 0062 — Mídias locais: NFO + artes na pasta da biblioteca

> Materializa metadado e arte na pasta REAL do filme/série/episódio, no padrão
> Kodi/Emby (NFO + nomes canônicos), com "arte principal" fisicamente refletida
> no nome do arquivo e a UI mostrando local vs. externo vs. principal.

| | |
|---|---|
| **ID** | 0062 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | ready |
| **Docs relacionadas** | [08-artwork-e-assets](../docs/08-artwork-e-assets.md), [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [05-api](../docs/05-api.md), [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md), [11-decisoes-abertas](../docs/11-decisoes-abertas.md) |
| **Depende de** | 0044, 0048, 0049, 0059 |

---

## 1. Objetivo

Hoje TODA arte enriquecida (pôster, fanart, banner, clearlogo, discart) e todo
metadado externo (sinopse, elenco, slogan, classificação) do MyMediaDB vive
**só** no banco/cache interno (`assetsDir` da própria aplicação) — nunca é
escrito na pasta real do filme/série na biblioteca do disco. Isso é o
oposto do TinyMediaManager, que materializa tudo (`.nfo` + arquivos de arte
com nome canônico) ao lado do vídeo, permitindo que **qualquer** outro player
(Kodi, Jellyfin, Emby, o próprio Plex) leia a biblioteca sem depender do
MyMediaDB rodando.

Este briefing fecha essa lacuna: dá ao dono um botão para **salvar** qualquer
arte encontrada remotamente (ou já cacheada) direto na pasta do item, marcar
uma delas como **principal** (e ver isso refletido em todo lugar — topo da
tela, galeria), e gerar/atualizar o `.nfo` da entity a partir do metadado já
resolvido. Fecha também as duas decisões abertas de
[docs/11](../docs/11-decisoes-abertas.md#decisões-a-tomar-por-fase):
"layout de assets em disco" (Fase 3).

**Princípio que não muda:** o MyMediaDB continua **banco autoritativo**, não
"stateless como o TMM" (ver [docs/11 §Trabalho relacionado](../docs/11-decisoes-abertas.md#trabalho-relacionado--prior-art)).
NFO é uma **exportação** (banco → disco), não um mecanismo de sincronização
bidirecional. A ÚNICA leitura de NFO que existe (id embutido, para matching
determinístico no scanner) já existe e **não muda** neste briefing.

## 2. Jornada do usuário

1. Usuário abre o detalhe de um filme já importado e com arquivo local
   presente (`media_file.state = 'present'`).
2. Na galeria de artes (0049), cada linha (poster/fanart/banner/clearlogo/
   discart/landscape) mostra os candidatos: os já salvos na pasta real (selo
   "Salvo na pasta"), o que está marcado como **principal** (selo "Principal",
   sempre o primeiro da linha), os cacheados internamente mas não salvos na
   pasta, e os externos ainda não baixados — nessa ordem.
3. Usuário clica "Salvar na pasta" numa arte externa → o sistema baixa (se
   ainda não tiver) e grava uma cópia física no diretório do item, com o nome
   canônico apropriado.
4. Usuário clica "Definir como principal" numa arte já salva na pasta → os
   arquivos físicos daquele tipo são renomeados para refletir a nova ordem (a
   principal sempre tem o nome "puro", ex. `poster.jpg`; as demais ganham
   sufixo numérico). Se a arte clicada ainda não estava salva na pasta, o
   clique dispara os dois passos (salvar + definir) em um único clique.
5. O pôster/fundo mostrados no topo da tela de detalhe atualizam
   imediatamente para refletir a nova arte principal.
6. No grupo de ações rápidas da entity (0059), um novo botão "Salvar
   informações (NFO)" escreve/reescreve o `.nfo` da entity na pasta real, com
   os campos já resolvidos hoje (sinopse, elenco, slogan, classificação,
   gêneros, ids externos).
7. Desvios: item sem nenhum `media_file` presente (só catálogo, `status`
   diferente de posse real) → botões "salvar na pasta"/NFO ficam desabilitados
   com tooltip explicando que não há pasta de destino; falha de I/O ao
   escrever (permissão, disco cheio) → erro claro na UI, nada fica em estado
   parcial (grava em arquivo temporário e faz rename atômico).

## 3. Escopo

**Dentro:**
- Convenção de nomes de arquivo de arte na pasta real (Kodi-first, com
  fallback prefixado por nome do vídeo quando a pasta tem mais de um vídeo).
- Rotação numerada (`fanart1.jpg`, `fanart2.jpg`, …) para múltiplas artes do
  mesmo `kind`.
- Escrita de NFO (`movie.nfo`/`tvshow.nfo`/`<episódio>.nfo`) via
  `encoding/xml` da stdlib, mapeamento campo a campo (ver §4).
- "Definir como principal" grava fisicamente no nome do arquivo (bare name).
- Endpoints novos por asset (materializar na pasta) e extensão do endpoint
  existente de "preferir" (0044/0048) para also renomear os arquivos físicos.
- Novo botão de ação rápida por entity: "salvar NFO na pasta" (estende grupo
  do 0059).
- UI: topo da tela de detalhe reflete a arte principal atual; galeria
  reordena (principal → local → externo).
- Persistência de qual asset foi materializado em qual caminho físico real
  (campo novo, distinto do `assetsDir` de cache) e qual é o "principal" atual
  por `kind` (formaliza em coluna — hoje é só `meta_json`).
- Deixar o design de `kind` aberto para um kind futuro `theme` (tema
  musical, `.mp3`) sem exigir mudança de schema — só não implementar agora.

**Fora (explicitamente):**
- Ler o `.nfo` já existente na pasta de volta para o banco além do que o
  scanner já faz hoje (id embutido conta como id determinístico — comportamento
  existente, **não expandido**). NFO é export one-way.
- Pasta de temporada dedicada / subpastas por temporada — o scanner hoje não
  separa fisicamente por temporada; `seasonNN-poster.jpg` vai sempre na raiz
  da pasta da SÉRIE (achada via `entity_relation`/ancestrais), como no Kodi.
- Pastas legadas `extrafanart/`/`extrathumbs/` — usar só o esquema numerado
  direto na pasta.
- Multi-episódio por arquivo único (sufixo `-SxxEyy` do Kodi v22+) — não é um
  caso que o scanner trata hoje.
- Implementar de fato o kind `theme` (tema `.mp3`) ou trailer físico — só
  garantir que o design não bloqueia isso depois.
- UI de configuração de convenção de nomes (Kodi vs. alternativas do TMM) —
  fixamos UMA convenção nesta versão, sem opção configurável.
- O job periódico "baixar mídias faltantes" que aplica esse mecanismo em
  massa pela biblioteca inteira — isso é o [briefing 0064](0064-job-midias-faltantes-watch-scan-incremental.md),
  que **depende** deste.
- Diferenciação de storage ocupado por essas mídias na tela de saúde — isso é
  o [briefing 0063](0063-storage-diferenciado-saude-biblioteca.md).

## 4. Contrato técnico

### Dados / modelo

Nova migration (próximo número disponível em `internal/db/migrations/`):

```sql
ALTER TABLE asset ADD COLUMN local_path TEXT;         -- caminho absoluto na pasta REAL (NULL = não materializado lá)
ALTER TABLE asset ADD COLUMN is_principal INTEGER NOT NULL DEFAULT 0; -- 0/1

ALTER TABLE entity ADD COLUMN nfo_path TEXT;           -- caminho do .nfo gravado por último (NULL = nunca gravado)
ALTER TABLE entity ADD COLUMN nfo_written_at TEXT;     -- timestamp ISO da última escrita
```

- `path_or_uri` (já existente) continua sendo o caminho no cache interno
  (`assetsDir`) ou a URL externa — **não muda de significado**. `local_path`
  é um campo NOVO e ortogonal: só é preenchido quando a arte também foi
  copiada para dentro da pasta da biblioteca.
- Backfill na própria migration: `UPDATE asset SET is_principal = 1 WHERE
  json_extract(meta_json, '$.preferred') = true` (ou equivalente ao que
  `metaPreferred()` hoje lê) — preserva o estado atual de "preferida" sem
  perda. `meta_json.preferred` pode continuar existindo por compat, mas
  `is_principal` passa a ser a fonte de verdade lida pelo código novo.
- Invariante aplicada em CÓDIGO (não é FK/trigger): no máximo um
  `is_principal = 1` por `(entity_id, kind)`. Ao marcar um novo principal, o
  código primeiro zera o anterior do mesmo par antes de setar o novo, na
  mesma transação.

### API

- `POST /api/v1/entities/{id}/assets/{assetID}/materialize` — baixa a arte
  para o cache (se `path_or_uri` ainda não estiver materializado lá — reusa a
  função de download do 0048) e em seguida grava uma CÓPIA física dentro da
  pasta real da biblioteca, no nome canônico calculado (§ Backend). Resposta:
  o `asset` atualizado, com `local_path` preenchido. Para um candidato que
  ainda não existe como `asset` (item ainda não selecionado da galeria
  lazy do fanart.tv/tmdb) — o fluxo primeiro passa pela seleção já existente
  (endpoint atual da 0048/0049 que cria o `asset`) e só então chama este; a UI
  encadeia as duas chamadas quando o item clicado ainda não é um `asset`.
- `PUT /api/v1/assets/{assetID}/prefer` (endpoint **já existe**, 0044) —
  estender: se o asset ainda não tem `local_path`, materializa primeiro (mesmo
  efeito do endpoint acima); depois RENOMEIA os arquivos físicos do mesmo
  `kind`/pasta para refletir a nova ordem (bare-name ↔ numerado, ver §
  Backend); seta `is_principal=1` nele e `0` no que era principal antes.
- `POST /api/v1/entities/{id}/nfo` — (re)gera e grava o(s) `.nfo` da entity na
  pasta real. Idempotente: sempre sobrescreve por completo (sem merge com NFO
  de outra origem). Atualiza `entity.nfo_path`/`nfo_written_at`. 404 se a
  entity não tiver nenhum `media_file` presente (sem pasta de destino).
- `GET /api/v1/entities/{id}/artwork` (endpoint existente) — cada item da
  resposta ganha dois campos novos: `is_local_in_library: boolean`
  (`local_path != null`) e `is_principal: boolean`. A ORDENAÇÃO de cada linha
  (por `kind`) passa a ser: `is_principal` desc, `is_local_in_library` desc,
  ordem original preservada asc — mesmo agregador existente
  (`montarGruposArtwork`), só adiciona o sort no fim.

### Backend (Go)

Novo pacote `internal/localmedia/`:

- **`naming.go`** — funções puras e testáveis:
  - `DiretorioDestino(entity, mediaFiles, ancestors) (dir string, ok bool)`:
    para `movie`/filme avulso e `episode`, usa `filepath.Dir()` do
    `media_file` mais antigo (`created_at` asc, `state='present'`) da própria
    entity. Para `season` (pôster de temporada) e `tv`/série, usa o
    `media_file` mais antigo de QUALQUER descendente (via `entity_relation`,
    mesma travessia que `montarAncestrais`/`herdarFanartTemporada` já fazem)
    — a pasta de destino é sempre a raiz da série, nunca uma subpasta de
    temporada (não existe hoje no scanner).
  - `EhPastaExclusiva(dir string, mediaFiles []MediaFile) bool`: true quando
    só existe UM `media_file.path` (de qualquer entity) com esse mesmo
    `filepath.Dir()` — consulta simples ao banco, não ao disco.
  - `NomeArquivoArte(kind string, principal bool, indiceRotacao int, pastaExclusiva bool, nomeBaseVideo string, ext string) string`:
    tabela fixa `kind → nome-base` (ver abaixo); se `pastaExclusiva`, nome é
    `<base>.<ext>` (bare) ou `<base><N>.<ext>` quando não-principal; senão
    prefixado `<nomeBaseVideo>-<base>.<ext>` (ou com sufixo numérico igual).
    `season poster` nunca é prefixado (a pasta da série é sempre "exclusiva"
    nesse sentido — um poster de temporada por número).
  - Tabela `kind → nome-base`: `poster→poster`, `fanart→fanart` (aceita
    `backdrop` como sinônimo de entrada), `banner→banner`,
    `clearlogo→clearlogo` (ext `.png`), `discart→discart` (ext `.png`),
    `landscape→landscape`, `thumb` de episódio → `<nomeBaseVideo>-thumb` (SEM
    variante bare — episódio sempre nomeia pelo vídeo), `season poster` →
    `season{NN}-poster` (`NN` = `season_number` zero-padded a 2 dígitos).
    Extensão default `.jpg` exceto onde indicado.
  - Rotação: ao marcar um asset como principal quando já existe outro
    `is_principal=1` do mesmo `(entity, kind)`, o código RENOMEIA o arquivo do
    antigo principal para o próximo sufixo numérico livre (maior índice já
    usado + 1 — sem reindexação/compactação de buracos) e grava/renomeia o
    novo principal para o nome bare. Rename é sempre `os.Rename` dentro do
    mesmo diretório (nunca copia bytes de novo).
  - Toda escrita de arquivo (arte ou NFO) é atômica: grava em
    `<destino>.tmp-<random>` e faz `os.Rename` para o nome final.

- **`nfo.go`** — monta o XML a partir de `resolver.Entidade` (o MESMO objeto
  que `(s *Server) resolverEntity(ctx, id, lang)` já produz — reusar essa
  função, nunca duplicar a resolução). Mapeamento exato:

  | Campo NFO | Origem em `resolver.Entidade` |
  |---|---|
  | `title` | `Title` |
  | `originaltitle` | `OriginalTitle` |
  | `plot` | `Description` |
  | `tagline` | `Tagline` |
  | `genre` (múltiplos `<genre>`) | `Tags` (docs/08: "gêneros não têm campo próprio — são as tags") |
  | `mpaa` | `Certification` |
  | `premiered` | `ReleaseDate` |
  | `rating` (`name="tmdb"`) | `VoteAverage`, omitido se nil |
  | `uniqueid` (um por chave de `ExternalIDs`, `type` = a chave) | `ExternalIDs`; o de `type="tmdb"` recebe `default="true"` quando presente, senão o primeiro na ordem `tmdb, imdb, tvdb` |
  | `actor` (`name`/`role`/`order`/`thumb`) | `Cast[].Name` / `.Character` / `.Order` / `.ProfileURL` |
  | `director` | `Crew[]` onde `Department == "Directing" && Job == "Director"` |
  | `credits` | `Crew[]` onde `Department == "Writing"` |
  | `studio` (múltiplos) | `Companies[].Name` |
  | `status` (só em `tvshow.nfo`) | `AiringStatus` |
  | `<thumb>`/`<fanart>` (fallback de URL) | URL direta do provider daquele `kind`, incluída SÓ quando não há `local_path` correspondente (arquivo local já é a fonte quando existe) |

  Nome do arquivo: `<nome-base-do-vídeo-sem-extensão>.nfo` para filme/episódio
  (mais portável entre Kodi/Emby, confirmado em pesquisa); `tvshow.nfo` fixo
  na raiz da pasta da série. Diretório de destino: mesma função
  `DiretorioDestino` de `naming.go`.

- Extensão do handler existente `PUT /assets/{id}/prefer`
  (`internal/server/artwork_select.go`) para acionar a rotina de rename.
- Novos handlers em `internal/server/` (nome de arquivo à escolha de quem
  implementar, seguindo o padrão dos módulos existentes de `artwork*.go`).

### Frontend (Vite/React)

- Componente de galeria de artes (0049): cada item ganha os dois selos
  ("Salvo na pasta" / "Principal") e os botões contextuais ("Salvar na
  pasta"/"Definir como principal"), com o encadeamento de um clique só
  descrito na jornada (§2, passo 4).
- Tela de detalhe (`RichDetailView`): pôster/backdrop do topo passam a
  refletir o item com `is_principal=true` retornado por `/artwork` (se
  `/entities/{id}` já deriva `poster_url`/`backdrop_url` do mesmo estado,
  reusar — não duplicar a lógica de qual é a arte ativa).
- Grupo de ações rápidas (0059, mesmo componente): novo botão "Salvar
  informações (NFO)".

### Arquivos afetados (previsão)

- `internal/db/migrations/00XX_midia_local.sql` (nova)
- `internal/localmedia/naming.go`, `internal/localmedia/naming_test.go`
- `internal/localmedia/nfo.go`, `internal/localmedia/nfo_test.go`
- `internal/server/artwork_select.go` (extensão)
- `internal/server/artwork.go` (campos novos + ordenação em `montarGruposArtwork`)
- `internal/server/localmedia_handlers.go` (novo, ou nome equivalente) + rotas em `internal/server/router.go`
- `openapi.yaml` (contrato primeiro — ver Notas)
- `web/src/**` (galeria de artes, RichDetailView, grupo de ações rápidas)
- `docs/08-artwork-e-assets.md`, `docs/11-decisoes-abertas.md` (fecha a decisão de layout — atualizar pós-implementação)
- `AGENTS.md` (ver abaixo)

## 5. Critérios de aceite

- [ ] Pasta com um único vídeo: "salvar na pasta" de um pôster grava
      `poster.jpg` (nome bare) no mesmo diretório do vídeo.
- [ ] Pasta com dois vídeos (múltiplas edições da mesma entity, ou duas
      entities na mesma pasta): o nome sai prefixado
      `<nome-do-vídeo>-poster.jpg`.
- [ ] Marcar uma segunda arte como principal do mesmo `kind` renomeia a
      antiga principal para `poster1.jpg` (ou próximo índice livre) e a nova
      assume `poster.jpg`; repetir 3x produz nomes crescentes sem colisão.
- [ ] `GET /artwork` retorna a lista de um `kind` ordenada: principal
      primeiro, depois locais, depois externas.
- [ ] `POST /entities/{id}/nfo` gera um `movie.nfo` cujo XML bate campo a
      campo com a tabela de mapeamento (teste automatizado com fixture de
      `resolver.Entidade` completo).
- [ ] Série: pôster de temporada (`season01-poster.jpg`) é gravado na RAIZ da
      pasta da série, não numa subpasta de temporada.
- [ ] Série: `tvshow.nfo` inclui `<status>` a partir de `AiringStatus`; filme
      não inclui esse campo.
- [ ] `uniqueid` marca `default="true"` no tmdb quando presente; cai para
      imdb/tvdb quando tmdb ausente.
- [ ] Chamar `POST /entities/{id}/nfo` duas vezes seguidas sobrescreve o
      arquivo por completo (idempotente), sem duplicar conteúdo.
- [ ] Entity sem `media_file` presente: `POST /nfo` e `.../materialize`
      retornam 404/erro claro, nada é escrito.
- [ ] Falha de I/O simulada (ex. diretório sem permissão de escrita) não deixa
      arquivo `.tmp-*` órfão nem `local_path`/`nfo_path` inconsistente com o
      disco.
- [ ] Topo do `RichDetailView` muda de pôster imediatamente após "definir
      como principal" (teste E2E/Playwright).

## 6. Notas de implementação / decisões

- **Banco autoritativo, NFO é export one-way.** Reforça
  [docs/11 §Trabalho relacionado](../docs/11-decisoes-abertas.md#trabalho-relacionado--prior-art):
  a diferença deliberada para o TMM é que o MyMediaDB NÃO fica "espalhado" —
  o NFO nunca é relido além do id determinístico que o scanner já usa.
- **Rotação sem reindexação.** Trocar a principal várias vezes cria uma
  sequência crescente de sufixos numéricos (nunca reaproveita buracos). É
  mais simples e evita uma corrida de rename em cascata; o "gasto" de índices
  não tem custo real (são só nomes de arquivo).
- **Seguir contrato-primeiro.** Por ser mudança de API, o primeiro agente
  fecha `openapi.yaml` antes de back-end/front-end trabalharem em paralelo
  (convenção do projeto a partir do briefing 0056).
- Ordem de implementação sugerida dentro deste briefing: migration → pacote
  `internal/localmedia` (com testes puros, sem servidor) → contrato OpenAPI →
  handlers HTTP + frontend em paralelo → E2E.

**Atualizar em `AGENTS.md`** (seção "Arquitetura em três linhas", bullet
"**Artes**", linha ~73-75) — trocar o texto atual:

> - **Artes** (`asset`): `kind` é TEXT livre no banco (poster/fanart/still hoje;
>   extensível a clearlogo/cdart/banner). Precedência: preferida→user→external→
>   base→404. Ver [docs/08](docs/08-artwork-e-assets.md) e briefing 0044.

por:

> - **Artes** (`asset`): `kind` é TEXT livre no banco (poster/fanart/still hoje;
>   extensível a clearlogo/cdart/banner). Precedência: preferida→user→external→
>   base→404. Desde o briefing 0062, a arte "preferida" (`is_principal`) pode
>   também estar **materializada na pasta real da biblioteca** (`asset.local_path`,
>   distinto do cache em `assetsDir`/`path_or_uri`) com nome canônico
>   Kodi/Emby — e a entity pode ter um `.nfo` exportado (`entity.nfo_path`).
>   NFO é exportação one-way (banco → disco); nunca é relido além do id
>   determinístico que o scanner já usa. Ver [docs/08](docs/08-artwork-e-assets.md)
>   e briefings 0044, 0062.

## 7. Fora deste briefing / follow-ups

- [Briefing 0063](0063-storage-diferenciado-saude-biblioteca.md): diferenciar
  storage ocupado por essas mídias (managed vs. cache vs. não-identificado) na
  tela de saúde da biblioteca.
- [Briefing 0064](0064-job-midias-faltantes-watch-scan-incremental.md): job
  periódico que aplica este mecanismo em massa (materializa o que falta na
  pasta de itens já enriquecidos), scan incremental por mtime/hash, e watcher
  de filesystem por pasta.
- Kind `theme` (tema musical `.mp3`) e trailer físico — próximo passo natural
  reaproveitando o mesmo `naming.go`/mecanismo de materialização; não
  implementado aqui.
- Leitura de NFO existente (import reverso completo, além do id
  determinístico) — se algum dia for desejado, precisa de uma decisão
  explícita de arquitetura (ADR), pois contraria o princípio "banco
  autoritativo" hoje firmado.
- UI de configuração de convenção de nomenclatura alternativa (TMM permite
  vários padrões por tipo) — fixamos uma convenção única por ora.
