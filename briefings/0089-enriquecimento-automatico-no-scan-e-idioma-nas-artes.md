# 0089 — Enriquecimento automático de filme no scan, materialização opcional e prioridade de idioma nas artes

> Filme passa a se comportar como série já se comporta hoje: todo item novo
> detectado no scan sai automaticamente com metadados **e** inventário de
> artes, sem depender do job `saude_biblioteca` (que continua desligado por
> padrão) nem de ação manual. Opcionalmente (toggle novo, desligado por
> padrão), o dono liga a materialização automática — baixar/copiar pra pasta
> real, na hora, as artes que o enrich acabou de descobrir, sem esperar
> clique manual ou o cron do job `midias_faltantes`. E a escolha automática de
> qual arte "vence" (galeria e materialização) passa a considerar o idioma
> preferido do dono: idioma preferido → textless (sem texto) → inglês →
> qualquer outro.

| | |
|---|---|
| **ID** | 0089 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | draft |
| **Docs relacionadas** | [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md), [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md), [08-artwork-e-assets](../docs/08-artwork-e-assets.md) |
| **Depende de** | 0046, 0048, 0056, 0064, 0084, 0087 |

---

## 1. Objetivo

Hoje, série nova entra na biblioteca já enriquecida (metadados + inventário
de artes fanart.tv) automaticamente, via `scanner.SerieImporter` (0034/0087).
**Filme não**: o scanner só cria a `entity` (draft/cataloged) a partir do
nome do arquivo; o enriquecimento real só acontece via o job
`saude_biblioteca` (auto-match + retroativo, **desligado por padrão**,
`internal/db/migrations/0012_seed_saude_biblioteca.sql`) ou ação manual do
dono. Na prática, quem não liga esse job vê filmes novos ficarem "crus"
(sem pôster/fundo do provider, sem inventário de artes) até agir
manualmente — mesmo tendo chave de provider configurada.

O dono pediu três coisas nesta sessão:

1. Que filme se comporte como série: item novo no scan já sai enriquecido
   (metadados + inventário de artes), **sempre**, sem depender de toggle.
2. Uma opção (desligada por padrão, o dono liga quando quiser) para também
   **materializar** — baixar o binário e copiar pra pasta real — as artes
   encontradas que ainda não estão no disco, automaticamente, logo após o
   item ser enriquecido no scan.
3. Que a escolha automática de qual arte "vence" (a que a galeria mostra
   primeiro e a que a materialização automática baixa) priorize o idioma
   preferido do dono; na falta dele, prefira arte sem texto (textless);
   na falta de ambos, inglês; por último, qualquer idioma disponível.

Nenhuma dessas três mudanças contraria o princípio nº 4 do `AGENTS.md`
("fontes externas são enriquecimento opcional e puxado, pull nunca sync
automático"): o gatilho continua sendo a CHEGADA de um item novo (uma ação
concreta, não uma varredura periódica do catálogo inteiro) — o mesmo
raciocínio que já vale hoje para série. A materialização automática (item 2)
é adicional e **opt-in** (toggle desligado por padrão), preservando a
cautela documentada em `internal/db/migrations/0016_watch_e_midias_faltantes.sql`
("ação automática que baixa/escreve no disco não deveria começar sozinha sem
o dono revisar primeiro") — aqui o dono revisa e liga explicitamente.

## 2. Jornada do usuário

1. O dono tem `MYMEDIADB_TMDB_API_KEY` e/ou `MYMEDIADB_FANARTTV_API_KEY`
   configuradas. Um arquivo de filme novo aparece numa pasta já cadastrada
   (scan periódico ou watch).
2. Hoje: a entity nasce draft/cataloged, sem contato com provider nenhum,
   até o job `saude_biblioteca` (se ligado) ou uma ação manual rodar.
3. Depois deste briefing: assim que o `media_file` é gravado, o scanner
   tenta o MESMO auto-match já usado pelo job (mesmo limiar de confiança,
   `LimiarAutoMatch`). Achando candidato forte o bastante, o item já sai do
   scan com metadados e inventário de artes (fanart.tv incluso) — sem ação
   nenhuma do dono. Sem candidato forte, cai exatamente como hoje: entity
   cataloged/draft, pendente na fila de Revisão.
4. Se o dono ligou "Baixar artes automaticamente" em Configurações: assim
   que um item (filme OU série) é auto-importado pelo scan com sucesso, as
   artes do inventário já são baixadas e copiadas pra pasta real da
   biblioteca na mesma passada — sem precisar abrir o item e clicar
   "Baixar Artes Faltantes", nem esperar o cron do job `midias_faltantes`.
   Desligado (default), nada muda: a materialização continua só manual ou
   via job periódico, como hoje.
5. Ao abrir a galeria de artes de qualquer item (novo ou já existente), a
   arte que aparece primeiro em cada categoria (pôster, fundo, banner...)
   passa a priorizar: idioma preferido do dono → arte sem texto → inglês →
   qualquer outra. Isso vale tanto para exibição quanto para a
   materialização automática (item 4) — a artes escolhida pra baixar é
   sempre a mesma que a galeria mostraria em primeiro lugar.

## 3. Escopo

**Dentro:**
- **Auto-match imediato de filme no scan**, reusando `s.tentarAutoMatch`
  (mesma lógica/limiar do job `saude_biblioteca`), disparado uma vez por
  entity NOVA criada pelo scanner — não depende de `job_definition.enabled`.
- **Toggle `materializar_artes_automaticamente`** (novo, em `app_settings`,
  default desligado), consumido logo após qualquer auto-import bem-sucedido
  do scan (filme novo ou série nova) — reusa `MaterializarMidiasFaltantes`
  já existente (0064), sem lógica de materialização nova.
- **Prioridade de idioma na ordenação de artes** (`ordenarGruposArtwork`,
  `internal/server/artwork.go`), reusando `s.idiomaDeBusca` (0084) como
  fonte da preferência: idioma preferido → textless (`lang==""`) → `en` →
  qualquer outro, SEMPRE depois de `is_principal`/`is_local_in_library`
  (arte já escolhida/pinada pelo dono, ou já materializada, nunca perde
  lugar por causa de idioma).
- Cobertura de teste unitário para as três mudanças (sem depender de rede
  real — mocks/fakes já usados pelo resto do pacote).

**Fora (explicitamente):**
- Não muda o default de `enabled` dos jobs `saude_biblioteca` nem
  `midias_faltantes` — continuam desligados por padrão; seguem servindo de
  rede de segurança periódica (retry de falha transiente, backfill de item
  que escapou do auto-match imediato) para quem quiser ligá-los.
- Materialização automática só dispara nos caminhos de **auto-import do
  scan** (filme via `FilmeImportador` novo, série via `SerieImporter`
  existente) — NÃO dispara em ações manuais (Corrigir, Confirmar candidato,
  "Baixar Metadados"/`enrich force`, auto-match manual em lote), mesmo com
  o toggle ligado. Escopo mínimo do pedido ("inclusive após o scan");
  estender pra outros gatilhos fica de follow-up se o dono quiser (§7).
- Não adiciona idioma como filtro na API pública do inventário fanart.tv
  (`BuscarArtes` já ignora `langs`, decisão de implementação do 0048/0087 —
  a API deles devolve tudo de uma vez) — a priorização é só na ESCOLHA
  local de qual candidato vence, não na busca.
- Não estende títulos/campos com idioma para TMDB/TVDB — hoje só o
  inventário fanart.tv grava múltiplos candidatos com `lang` por kind; a
  arte "base" (pôster/fundo do provider de árvore) continua candidato
  único, sem concorrência de idioma.
- Não reprocessa/re-materializa artes já salvas na pasta real
  (`is_local_in_library=true`) só porque uma opção "melhor" no novo
  critério de idioma passou a existir — princípio 5 (não-destrutivo); a
  reordenação só afeta escolhas FUTURAS.
- Não cria configuração de idioma dedicada para artes — reusa o MESMO
  resolvedor `idiomaDeBusca` (por biblioteca, com fallback global) já usado
  pela busca de metadados (0084), sem inventar um segundo conceito de
  idioma.
- Não muda o comportamento de exibição/seleção MANUAL de arte
  (`POST /entities/{id}/artwork/select` continua deixando o dono escolher
  qualquer candidato, de qualquer idioma, ignorando a ordenação automática).

## 4. Contrato técnico

### Dados / modelo
- Nenhuma migration. Reusa `app_settings` (mecanismo genérico já usado por
  `languages`/0019 e `enrichment_retroativo`/0085) para o novo toggle, e o
  campo `artworkItem.Lang`/`provider.Arte.Lang` já persistido no inventário
  fanart.tv (0048/0087) para a ordenação por idioma — nada novo a gravar.

### API (`openapi.yaml`)
- Novo schema `AutoMaterializeArtworkSettings`: `{ enabled: boolean }`
  (campo obrigatório na resposta).
- `GET /api/v1/settings/auto-materialize-artwork` → 200
  `AutoMaterializeArtworkSettings` (default `{enabled: false}` quando a
  chave nunca foi gravada — mesma semântica de `obterLanguageSettings`/
  `obterRetroactiveEnrichmentSettings`).
- `PUT /api/v1/settings/auto-materialize-artwork` → corpo
  `AutoMaterializeArtworkSettings`; `200` com o valor gravado; `400
  invalid_body` corpo malformado.

### Backend (Go)

**1. Auto-match imediato de filme (`internal/scanner`, `internal/server`):**
- `internal/scanner/manager.go`: nova interface, ao lado de `SerieImporter`:
  ```go
  // FilmeImportador é o ponto de AUTO-MATCH que o scanner de filme consome
  // logo após criar uma entity NOVA: tenta casar+enriquecer (metadados +
  // inventário de artes) contra os providers configurados, reusando o MESMO
  // limiar de confiança do auto-match em lote (LimiarAutoMatch, saude_biblioteca).
  // Sem candidato forte o bastante, a entity permanece cataloged/draft,
  // pendente na fila de Revisão -- igual hoje. Interface para não acoplar o
  // scanner ao pacote server. Implementado por *Server.
  type FilmeImportador interface {
      // TentarAutoMatchFilme tenta casar+enriquecer a entity; matched=true só
      // quando um candidato >= LimiarAutoMatch foi de fato aplicado.
      TentarAutoMatchFilme(ctx context.Context, entityID string) (matched bool, err error)
  }
  ```
  `Manager` ganha campo `filmeImportador FilmeImportador` e
  `ConfigurarFilmes(imp FilmeImportador)`, mesmo padrão de
  `ConfigurarSeries`/`serieImporter`.
- `internal/server/automatch.go`: novo método adaptador
  `func (s *Server) TentarAutoMatchFilme(ctx context.Context, entityID string) (bool, error)`
  — busca a `sqlcgen.Entity` (mesma query já usada por `AutoMatchElegiveis`),
  chama `s.tentarAutoMatch(ctx, ent)` e devolve `matched = (resultado indica
  candidato aplicado)`. Erro só propaga em falha de infra (banco/rede
  irrecuperável); "sem candidato" não é erro (mesma semântica de
  `ErrSerieNaoEncontrada` para série — degrada em silêncio).
- `internal/scanner/match.go`:
  - `resolverEntity` (linha ~116) passa a devolver também `criada bool`
    (true só no branch que chega em `criarEntity`, linha ~136) — os outros
    dois branches (cache do scan, id externo/título+ano já existentes)
    devolvem `false`.
  - `gravarArquivo` (linha ~26): depois do `CriarMediaFile` bem-sucedido
    (linha ~55-70), se `criada && r.m.filmeImportador != nil`, chama
    `r.m.filmeImportador.TentarAutoMatchFilme(ctx, entityID)` — best-effort,
    loga e segue em caso de erro (mesmo padrão de log de
    `resolverSerieMiss`), NUNCA aborta a gravação do arquivo já feita.
  - Precisa do `media_file` já gravado ANTES de chamar
    `TentarAutoMatchFilme` (o auto-match lê título/ano via `media_file`,
    `tituloAnoDoArquivo`) — por isso o gatilho fica DEPOIS do
    `CriarMediaFile`, não dentro de `criarEntity`/`resolverEntity`.
- `internal/server/server.go` (ou onde `scannerManager.ConfigurarSeries(...)`
  já é chamado hoje, no wiring de startup): adicionar
  `scannerManager.ConfigurarFilmes(server)` ao lado.

**2. Materialização automática opcional:**
- `internal/server/settings.go`: ao lado de
  `retroactiveEnrichmentSettings`, mesmo molde (constante de chave, struct,
  default, cache no `settingsCache` com um novo bool `carregado` dedicado,
  `getX`/`putX` espelhando `getLanguageSettings`/`putLanguageSettings`):
  ```go
  const appSettingAutoMaterializeArtwork = "materializar_artes_automaticamente"

  type autoMaterializeArtworkSettings struct {
      Enabled bool `json:"enabled"`
  }

  func defaultAutoMaterializeArtworkSettings() autoMaterializeArtworkSettings {
      return autoMaterializeArtworkSettings{Enabled: false}
  }
  ```
- `internal/scanner/manager.go`: nova interface, análoga a `FilmeImportador`:
  ```go
  // Materializador é o ponto OPCIONAL que o scanner consome depois de
  // QUALQUER auto-import bem-sucedido (filme ou série) para, se o dono tiver
  // ligado "materializar artes automaticamente" (app_settings), materializar
  // na hora as artes que o enrich acabou de descobrir -- sem esperar o job
  // periódico midias_faltantes. Toggle desligado (default) = no-op.
  // Implementado por *Server.
  type Materializador interface {
      MaterializarSeAutoDownloadLigado(ctx context.Context, entityID string) error
  }
  ```
  `Manager` ganha `materializador Materializador` e
  `ConfigurarMaterializacao(m Materializador)`.
- `internal/server/midias_faltantes.go` (ou `settings.go`): novo método
  ```go
  func (s *Server) MaterializarSeAutoDownloadLigado(ctx context.Context, entityID string) error {
      cfg, err := s.obterAutoMaterializeArtworkSettings(ctx)
      if err != nil || !cfg.Enabled {
          return err
      }
      _, _, err = s.MaterializarMidiasFaltantes(ctx, entityID)
      return err
  }
  ```
- Call sites (best-effort, loga e segue, nunca aborta o scan):
  - `internal/scanner/serie.go`, `resolverSerieMiss` (linha ~173-177): logo
    após `ImportarSeriePorNome` retornar `err == nil`, antes do `return`,
    chama `r.m.materializador.MaterializarSeAutoDownloadLigado(ctx, id)`
    quando `r.m.materializador != nil`.
  - `internal/scanner/match.go`, `gravarArquivo`: logo após
    `TentarAutoMatchFilme` retornar `matched == true`, mesma chamada.
- `server.go`: `scannerManager.ConfigurarMaterializacao(server)` no mesmo
  wiring do item 1.

**3. Prioridade de idioma na ordenação de artes:**
- `internal/server/artwork.go`:
  - `montarGruposArtwork` (linha ~84-91) passa a resolver
    `idioma, _ := s.idiomaDeBusca(ctx, id)` (0084, já trata ambiguidade/
    fallback pro global) e propagar pra `ordenarGruposArtwork`.
  - `ordenarGruposArtwork` (linha ~93-109) ganha o parâmetro `idioma
    string` e um novo critério de desempate, ENTRE `is_local_in_library` e
    a preservação de ordem original:
    ```go
    func ordenarGruposArtwork(grupos map[string][]artworkItem, idiomaPreferido string) {
        base := idiomaBase(idiomaPreferido) // "pt-BR" -> "pt" (ver nota abaixo)
        for _, itens := range grupos {
            sort.SliceStable(itens, func(i, j int) bool {
                if itens[i].IsPrincipal != itens[j].IsPrincipal {
                    return itens[i].IsPrincipal
                }
                if itens[i].IsLocalInLibrary != itens[j].IsLocalInLibrary {
                    return itens[i].IsLocalInLibrary
                }
                ri, rj := rankIdiomaArte(itens[i].Lang, base), rankIdiomaArte(itens[j].Lang, base)
                if ri != rj {
                    return ri < rj
                }
                return false // empate: mantém a ordem original (stable)
            })
        }
    }

    // rankIdiomaArte: 0=idioma preferido, 1=textless (lang vazio, comum no
    // fanart.tv para arte sem texto), 2=inglês, 3=qualquer outro.
    func rankIdiomaArte(lang, preferidoBase string) int {
        switch {
        case preferidoBase != "" && lang == preferidoBase:
            return 0
        case lang == "":
            return 1
        case lang == "en":
            return 2
        default:
            return 3
        }
    }
    ```
  - **Atenção ao formato do código de idioma**: `idiomaDeBusca`/
    `app_settings.languages` usam locale completo (`"pt-BR"`), mas o
    `lang` que o fanart.tv devolve é ISO 639-1 puro (`"pt"`, `"en"`,
    `""`). Comparar direto (`"pt-BR" == "pt"`) NUNCA bateria — é
    obrigatório normalizar via `idiomaBase` (extrai a parte antes do `-`,
    minúscula) antes de comparar. Validar isso com um teste explícito
    (idioma preferido `"pt-BR"` deve rankear `lang: "pt"` como 0).
- `internal/server/artwork_missing.go` (`baixarMelhorArteDoKind`): hoje já
  reusa `montarGruposArtwork` e depois reordena por `Likes` — **verificar o
  código atual antes de mexer**: se esse segundo sort substitui a ordem
  vinda de `ordenarGruposArtwork`, ele precisa passar a respeitar o rank de
  idioma como critério PRIMÁRIO e `Likes` como desempate DENTRO do mesmo
  rank (não um sort independente que descarta a prioridade de idioma).

### Frontend (Vite/React)
- `web/src/routes/settings.tsx`: novo card "Artes" (ou seção dentro de uma
  existente) com um switch "Baixar artes automaticamente após o scan",
  carregado de `GET /settings/auto-materialize-artwork` e salvo com `PUT`,
  mesmo padrão de formulário dos cards de idioma/retroativo já existentes
  na mesma tela.
- `web/src/lib/api-types.ts`: regenerar via `npm run gen:types`.
- Nenhuma mudança adicional de UI — a galeria de artes já renderiza a
  ordem que a API devolve; a reordenação por idioma é transparente.

### Arquivos afetados
- `internal/scanner/manager.go`
- `internal/scanner/match.go` (+ `match_test.go`)
- `internal/scanner/serie.go` (+ `serie_test.go`)
- `internal/server/automatch.go` (+ `automatch_test.go`)
- `internal/server/settings.go` (+ `settings_test.go`)
- `internal/server/midias_faltantes.go`
- `internal/server/artwork.go` (+ `artwork_test.go`)
- `internal/server/artwork_missing.go` (verificar/ajustar)
- `internal/server/server.go` (wiring)
- `openapi.yaml`
- `web/src/routes/settings.tsx`
- `web/src/lib/api-types.ts` (gerado)

## 5. Critérios de aceite

- [ ] Filme novo detectado no scan, com candidato externo que bate
      `LimiarAutoMatch`: sai do scan já com `external_ids_json` preenchido,
      poster/fundo do provider de árvore E inventário
      `media_external(source='fanarttv')` (quando aplicável), sem
      `job_definition` nenhum habilitado e sem ação manual.
- [ ] Filme novo sem candidato forte o bastante: continua cataloged/draft,
      pendente na fila de Revisão — nenhuma regressão no fluxo atual.
- [ ] Filme já existente (reencontro por hash/path) NÃO dispara
      `TentarAutoMatchFilme` de novo (só entity recém-criada nesta
      passada de scan).
- [ ] `GET /settings/auto-materialize-artwork` sem configuração prévia
      devolve `{"enabled": false}`.
- [ ] Com o toggle desligado (default): scan de filme/série novos continua
      SEM materializar nada além do que já materializa hoje (poster/fundo
      canônico via `cachearArte`); `asset.local_path` de artes extras
      permanece nulo até ação manual ou job periódico.
- [ ] Com o toggle ligado: filme novo auto-importado com sucesso pelo scan
      termina a passada já com as artes do inventário copiadas pra pasta
      real (`asset.local_path` preenchido), sem clique manual.
- [ ] Com o toggle ligado: série nova auto-importada pelo scan tem o mesmo
      comportamento.
- [ ] Com o toggle ligado: item que ficou em Revisão (sem auto-match) NÃO
      materializa nada automaticamente (nada foi importado ainda).
- [ ] Auto-match manual em lote, "Corrigir", "Confirmar candidato" e
      "Baixar Metadados" (force) continuam SEM disparar materialização
      automática, mesmo com o toggle ligado (escopo restrito ao scan).
- [ ] Item com artes em `pt`, `en` e textless (`lang=""`) disponíveis no
      inventário, idioma preferido resolvido = `pt-BR`: a arte `pt` vem
      primeiro em `GET /entities/{id}/artwork` e é a materializada
      automaticamente.
- [ ] Mesmo item sem nenhuma arte em `pt`: a textless (`lang=""`) vem antes
      da `en` e de qualquer outro idioma.
- [ ] Item já com uma arte `is_principal=true` ou `is_local_in_library=true`
      num idioma "pior": continua vencendo, sem a prioridade de idioma
      derrubá-la.
- [ ] `go build ./... && go vet ./... && go test ./... -count=1` verde.
- [ ] `make gate-local` passa.

## 6. Notas de implementação / decisões

- **Por que reusar `s.tentarAutoMatch`/`LimiarAutoMatch` em vez de inventar
  um critério "casa por nome único" (como série faz hoje)**: filme já tem
  um scorer testado e calibrado (`matching.Pontuar`,
  `PontuarComAlternativos` desde 0084) usado pelo job em lote; duplicar
  outra heurística só pro caminho de scan criaria dois comportamentos
  diferentes pro "mesmo" auto-match, confuso de explicar e manter.
- **Por que a materialização automática é opt-in (toggle desligado por
  padrão) enquanto o enrich de metadados/inventário (item 1) não é**:
  metadados + inventário são só LEITURA de rede, sem escrever no disco do
  dono — mesma categoria de risco que já é automática pra série hoje.
  Materializar significa ESCREVER binário na pasta real da biblioteca,
  categoria que os jobs `saude_biblioteca`/`midias_faltantes` já tratam
  com cautela deliberada (migrations 0012/0016) — a mudança de "sempre
  automático" pediria uma segunda validação explícita do dono, que ele não
  deu; o toggle resolve isso sem bloquear quem quer a automação completa.
- **Por que a materialização automática só dispara no caminho de scan, não
  em toda chamada de enrich**: o pedido do dono foi específico
  ("inclusive após o scan"); disparar também em "Baixar Metadados"
  manual seria uma surpresa — o dono pode clicar ali só pra atualizar
  texto/sinopse, sem esperar um efeito colateral de escrita em disco.
- **Por que a prioridade de idioma vive em `ordenarGruposArtwork` e não em
  cada consumidor**: é o ÚNICO ponto que decide "vencedor" tanto pra
  galeria (`getEntityArtwork`) quanto pra materialização automática
  (`materializarKindVencedor`, via `montarGruposArtwork`) — resolver ali
  garante que os dois NUNCA divergem (a UI sempre mostra em primeiro o que
  a materialização de fato baixaria), mesma garantia que
  `materializarKindVencedor` já documenta hoje ("nunca escolhe um
  candidato diferente do que a UI já mostraria como topo").
- **Por que normalizar `pt-BR` → `pt` antes de comparar com o `lang` do
  fanart.tv**: é um bug real e silencioso se ignorado — sem a
  normalização, NENHUM item bateria idioma preferido (o app usa locale
  completo, o fanart.tv usa ISO 639-1 puro), e a prioridade de idioma
  pareceria "não fazer nada" em produção sem nenhum erro visível.
- **Por que `saude_biblioteca`/`midias_faltantes` continuam desligados por
  padrão**: já servem de rede de segurança (retry de falha transiente,
  itens que entraram por outro caminho que não o scan, backlog anterior a
  este briefing); ligar por padrão é uma decisão independente, não pedida
  aqui, e mudaria o comportamento de quem já tem esses jobs
  deliberadamente desligados.

## 7. Fora deste briefing / follow-ups

- Estender a materialização automática (item 2) para os outros gatilhos de
  enrich (Corrigir, Confirmar candidato, Baixar Metadados manual) — só se
  o dono pedir depois de ver o comportamento do scan funcionando.
- Configuração de idioma preferido dedicada a artes (hoje reusa
  `idiomaDeBusca`/preferência por biblioteca) — sem caso de uso concreto
  hoje pra idioma de artes divergir do idioma de busca da mesma
  biblioteca.
- Backfill em massa de itens já catalogados antes deste briefing (sem
  inventário, sem material materializado) — o job `saude_biblioteca`/
  `midias_faltantes`, se o dono ligar, já cobre isso organicamente; um
  reprocessamento manual dedicado fica como follow-up se quiser acelerar.
- Suportar TVDB como segunda fonte de candidatos com `lang` (hoje só
  fanart.tv grava múltiplos candidatos por kind) — sem caso de uso
  confirmado.
