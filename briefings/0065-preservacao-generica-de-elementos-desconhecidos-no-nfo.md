# 0065 — Preservação genérica de elementos desconhecidos no `.nfo`

> Ao regravar um `.nfo`, preservar QUALQUER elemento XML de nível superior que
> o MyMediaDB não gera/gerencia — não só a lista fechada de campos "de
> usuário" já coberta — para nunca destruir dado de terceiro/futuro sem
> querer.

| | |
|---|---|
| **ID** | 0065 |
| **Tipo** | fix |
| **Fase (roadmap)** | Transversal — Infraestrutura operacional |
| **Status** | done |
| **Docs relacionadas** | [09-nfo-e-export](../docs/09-nfo-e-export.md), [11-decisoes-abertas](../docs/11-decisoes-abertas.md) |
| **Depende de** | 0062 |

---

## 1. Objetivo

O commit `d9cc282` (2026-07-16, resposta direta a uma revisão de código, sem
briefing formal na hora) corrigiu um problema real: `gravarNFO` sobrescrevia o
`.nfo` por completo a cada regravação, destruindo avaliação do usuário,
contagem/estado de assistido, última reprodução, data de adição e ponto de
retomada — campos Kodi/Emby/Jellyfin que o `resolver.Entidade` nunca modelou.
A correção adicionou `CamposUsuarioNFO`, uma lista FECHADA desses seis campos,
extraída do `.nfo` anterior antes de sobrescrever.

Pesquisa comparativa com o **tinyMediaManager** (referência de paridade do
projeto — ver [11-decisoes-abertas](../docs/11-decisoes-abertas.md)) mostrou
que o comportamento padrão dele é mais genérico: a opção **"Write clean
NFO"** vem **desligada por padrão**, e nesse estado o TMM **embute de volta
qualquer dado desconhecido/não suportado do `.nfo` anterior** ao regravar —
só descarta esse conteúdo se o usuário explicitamente pedir "limpo"
(fontes: docs oficiais do TMM, DeepWiki do código-fonte, ver seção 6).

A lista fechada que temos hoje cobre os casos mais comuns, mas deixa de fora
qualquer tag que não previmos: `<tag>`, `<set>`, `<trailer>`,
`<epbookmark>`, `<top250>`, o bloco `<fileinfo><streamdetails>` que o
próprio Kodi escreve depois de identificar codecs, extensões de add-ons de
terceiro, ou qualquer coisa que um usuário adicione à mão. Cada uma dessas
exigiria uma nova entrada na lista e um novo release nosso — na prática,
vamos sempre ficar atrás. Este briefing generaliza o mecanismo: em vez de
uma lista fechada, preservar **qualquer elemento de nível superior que o
MyMediaDB não gera sozinho**, sem precisar conhecer o nome da tag de
antemão. Validado empiricamente (protótipo isolado, ver seção 6) que o
`encoding/xml` da stdlib já suporta isso nativamente via `xml:",any"` +
`xml:",innerxml"`, sem dependência nova.

## 2. Jornada do usuário

1. O usuário materializa um filme/série no MyMediaDB (gera o `.nfo` pela
   primeira vez, via "Salvar NFO" ou o job `midias_faltantes`).
2. Ele abre a pasta no Kodi/Jellyfin: assiste, avalia, marca favoritos, deixa
   tags/coleções — e a própria interface do media center regrava o `.nfo`
   com elementos que o MyMediaDB não conhece hoje (ex.: `<tag>`, `<set>`, um
   bloco `<fileinfo><streamdetails>` com os codecs detectados). Ou o usuário
   edita o arquivo manualmente, adicionando algo próprio.
3. Em algum momento o MyMediaDB regrava esse mesmo `.nfo` — o job
   `midias_faltantes` decide reescrevê-lo, ou o metadado do item mudou e o
   usuário aperta "Salvar NFO" de novo.
4. **Hoje**: qualquer elemento fora da lista fechada
   (`userrating`/`playcount`/`watched`/`lastplayed`/`dateadded`/`resume`)
   some, mesmo sem o MyMediaDB nunca ter tocado nele.
5. **Depois deste briefing**: qualquer elemento que o MyMediaDB não gera
   sozinho sobrevive à regravação, tal como estava — sem exigir que a gente
   conheça o nome da tag de antemão.

## 3. Escopo

**Dentro:**
- Generalizar `CamposUsuarioNFO` / `ExtrairCamposUsuario` / `MontarNFO`
  (`internal/localmedia/nfo.go`) para capturar, além dos campos nomeados já
  existentes, **qualquer elemento de nível superior** do `.nfo` anterior que
  `nfoDoc` não modela — via `xml:",any"` + `xml:",innerxml"` (mecanismo
  nativo do `encoding/xml`, confirmado neste briefing).
- Manter os campos nomeados já existentes (`UserRating`, `PlayCount`,
  `Watched`, `LastPlayed`, `DateAdded`, `Resume`) funcionando exatamente como
  hoje — são um subconjunto documentado do que passa a ser coberto de forma
  genérica; não removê-los (explícito > genérico silencioso para os campos
  mais comuns, mantém a legibilidade/teste atual).
- Cobrir com teste pelo menos: uma tag simples desconhecida repetida (ex.
  `<tag>Sci-Fi</tag><tag>Cult</tag>`), um bloco aninhado desconhecido (ex.
  `<fileinfo><streamdetails>...</streamdetails></fileinfo>`), e a garantia de
  que campos MANAGED pelo MyMediaDB (título, plot, elenco etc.) continuam
  **sempre** vindo do banco — nunca herdados do arquivo antigo.
- Atualizar o comentário de `nfo.go` e `docs/09-nfo-e-export.md` registrando
  a decisão (banco continua sendo a única fonte de verdade; isto é só sobre
  não destruir dado que não é nosso).

**Fora (explicitamente):**
- Qualquer opção tipo "gerar NFO limpo" (toggle para desligar a
  preservação) — não foi pedido; o padrão do MyMediaDB passa a ser sempre
  preservar (mais seguro por padrão; pode virar um follow-up se aparecer
  necessidade real).
- Importar/mesclar de volta para o **banco** qualquer dado descritivo
  (título, sinopse, elenco...) a partir de um `.nfo` pré-existente — isso
  replicaria o "scraper NFO" do TMM e contraria a decisão já registrada em
  [09](../docs/09-nfo-e-export.md): banco é a única fonte de verdade, NFO é
  só export. Fora de escopo aqui.
- Trava de campo por-usuário no banco (equivalente ao "Lock" do TMM) — já
  existe mecanismo equivalente via override (autoridade do dono); não é o
  que este briefing toca.
- Preservar `<uniqueid>` de terceiro (ex. um `type="anidb"` que outra
  ferramenta tenha adicionado): como esse campo já é modelado por nome
  (`nfoDoc.UniqueIDs`), ele nunca cai no catch-all genérico — continua
  sendo sempre regenerado a partir de `e.ExternalIDs` (comportamento já
  existente, não uma regressão introduzida aqui). Registrar como limitação
  conhecida, não corrigir agora.

## 4. Contrato técnico

### Dados / modelo
Nenhuma migração. Mudança é só no mecanismo de geração/leitura do arquivo
`.nfo` em disco.

### API
Nenhuma mudança de contrato HTTP — mesmo endpoint `POST
/api/v1/entities/{id}/nfo` e mesmo job `midias_faltantes`, mesmo
comportamento observável de fora (200 + `{nfo_path, nfo_written_at}`).

### Backend (Go)
- `internal/localmedia/nfo.go`:
  - Novo tipo:
    ```go
    // xmlElementoBruto captura um elemento XML de nivel superior que nfoDoc
    // nao conhece -- nome, atributos e conteudo interno exatamente como
    // estava no arquivo anterior, para reembutir sem interpretar.
    type xmlElementoBruto struct {
        XMLName xml.Name
        Attrs   []xml.Attr `xml:",any,attr"`
        Inner   string     `xml:",innerxml"`
    }
    ```
  - `nfoDoc` ganha `Extras []xmlElementoBruto \`xml:",any"\`` — captura
    qualquer sub-elemento que não bateu com nenhum campo nomeado (Go's
    `encoding/xml`: `,any` é sempre um catch-all, ordem de declaração na
    struct não afeta o unmarshal).
  - `CamposUsuarioNFO` ganha `Extras []xmlElementoBruto` (mantendo os campos
    já existentes intactos).
  - `ExtrairCamposUsuario` passa a devolver também `doc.Extras`.
  - `MontarNFO` passa a setar `doc.Extras = usuario.Extras` antes de
    marshalizar.
- `internal/server/localmedia_handlers.go`: **nenhuma mudança** — `gravarNFO`
  já lê o arquivo anterior e chama `ExtrairCamposUsuario`/`MontarNFO`; o novo
  campo é transparente para quem chama.
- `internal/job/midiasfaltantes/...`: **nenhuma mudança** — usa o mesmo
  `gravarNFO` por baixo (via `gravarNFOSeFaltante`).

### Frontend (Vite/React)
Nenhuma — mudança invisível para a UI.

### Arquivos afetados
- `internal/localmedia/nfo.go` (alterar)
- `internal/localmedia/nfo_test.go` (novos testes)
- `docs/09-nfo-e-export.md` (registrar a decisão)

## 5. Critérios de aceite

- [ ] Uma tag desconhecida de nível superior repetida (ex.
      `<tag>Sci-Fi</tag><tag>Cult</tag>`) sobrevive a uma regravação do
      `.nfo` da mesma entity, na mesma ordem.
- [ ] Um bloco aninhado desconhecido (ex.
      `<fileinfo><streamdetails>...</streamdetails></fileinfo>`) sobrevive
      intacto (mesma estrutura interna) a uma regravação.
- [ ] Os campos nomeados já cobertos (`userrating`, `playcount`, `watched`,
      `lastplayed`, `dateadded`, `resume`) continuam funcionando exatamente
      como antes — os testes existentes (`TestExtrairCamposUsuarioRoundTrip`,
      `TestWriteEntityNfoPreservaCamposDoUsuario`) passam sem alterar
      asserts.
- [ ] Campos MANAGED pelo MyMediaDB (`title`, `plot`, `genre`, `actor` etc.)
      continuam **sempre** refletindo o valor atual do banco, mesmo que o
      `.nfo` anterior tivesse um valor diferente — nunca herdados do
      arquivo.
- [ ] Um `.nfo` sem nenhum elemento desconhecido (caso comum) continua
      gerando o mesmo XML de antes — sem `<extras>`/elemento fantasma
      aparecendo.
- [ ] `go build ./...`, `go vet ./...` e `make gate-local` passam sem
      regressão.

## 6. Notas de implementação / decisões

- Esta é uma **evolução** do fix ad-hoc já mergeado no commit `d9cc282`
  (2026-07-16). Este briefing formaliza e generaliza aquele fix — não é uma
  reversão nem uma reescrita.
- Motivado por pesquisa comparativa com o tinyMediaManager, feita nesta
  mesma sessão: [Frequently Asked Questions](https://www.tinymediamanager.org/help/faq),
  [Movie Settings](https://www.tinymediamanager.org/docs/movies/settings)
  (opção "Write clean NFO": "escreve um .nfo limpo sem embutir dado
  desconhecido/não suportado de arquivos .nfo existentes" — logo, o padrão
  SEM essa opção é embutir de volta), [NFO File Generation
  (DeepWiki)](https://deepwiki.com/pa-0/tinyMediaManager/9.2-nfo-file-generation).
- Mecanismo escolhido (`xml:",any"` + `xml:",innerxml"`) é nativo do
  `encoding/xml` da stdlib — sem dependência nova. **Validado empiricamente**
  antes deste briefing com um protótipo isolado: elementos repetidos
  (`<tag>foo</tag><tag>bar</tag>`) e aninhados (`<resume><position>.../>
  </resume>`) fazem round-trip byte-a-byte corretamente via
  `xml.Unmarshal`→`xml.MarshalIndent`.
- Deliberadamente **não** se tenta "entender" o conteúdo dos elementos
  desconhecidos — não é um parser de schema Kodi. É passthrough opaco puro,
  na mesma filosofia do resto do NFO (`MontarNFO` é função pura, sem
  interpretar semântica externa, sem I/O).

## 7. Fora deste briefing / follow-ups

- Toggle "gerar NFO limpo" (equivalente ao "Write clean NFO" do TMM), caso
  surja demanda real de um usuário que queira o oposto do novo padrão.
- Qualquer forma de importar dado do `.nfo` de volta para o banco (fluxo
  scraper-NFO do TMM) — decisão de arquitetura já fechada em
  [09](../docs/09-nfo-e-export.md) contra isso; só reabrir com uma decisão
  estrutural nova, não como parte deste briefing.
