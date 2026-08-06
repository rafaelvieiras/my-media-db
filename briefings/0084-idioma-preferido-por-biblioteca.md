# 0084 — Idioma preferido por biblioteca + match contra títulos alternativos

> Cada `library_source` (pasta de mídia) ganha um idioma preferido opcional,
> usado tanto na QUERY aos provedores (TMDB/TVDB) quanto como padrão de
> EXIBIÇÃO dos itens vindos dela — paridade com o "preferred metadata
> language" por biblioteca do Jellyfin. Sem idioma preferido definido, o
> comportamento é idêntico ao de hoje (idioma global de `app_settings`). Some
> a isso a pontuação de candidato passar a considerar também os TÍTULOS
> ALTERNATIVOS/TRADUZIDOS do provedor (não só o título no idioma consultado)
> — os dois mecanismos juntos são o que de fato resolve casos como "A
> Pantera Cor-De-Rosa" vs "The Pink Panther" na fila de Revisão.

| | |
|---|---|
| **ID** | 0084 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | done |
| **Docs relacionadas** | [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md) |
| **Depende de** | 0019 (fundação multi-idioma), 0050 (auto-match), 0056 (enriquecimento retroativo), 0060 (prévia de match) |

---

## 1. Objetivo

Hoje existe **um único idioma global** (`app_settings.languages`, briefing
0019) usado tanto para a query de busca aos provedores externos quanto para
a resolução do idioma de exibição de qualquer item do catálogo. Isso não
reflete a realidade de bibliotecas mistas: o dono pode ter uma pasta de
filmes nomeados em português (títulos traduzidos/informais) e outra de
séries nomeadas em inglês (convenção "release scene"), e um único idioma de
busca global não serve bem às duas ao mesmo tempo.

Este briefing adiciona um **idioma preferido opcional por `library_source`**
que, quando definido, vira o idioma-alvo tanto da busca (scan automático,
auto-match, enriquecimento retroativo) quanto da exibição padrão dos itens
originados daquela biblioteca — sem exigir que o dono troque o seletor de
idioma de leitura toda vez que navega entre bibliotecas diferentes.

**Achado colateral #1, que este briefing também corrige**: hoje
`automatch.go`, `retroativo.go` e `match_preview.go` chamam
`s.buscarExterno(ctx, titulo, ent.Type, "")` com idioma **vazio** —
nenhum desses três fluxos respeita nem o idioma GLOBAL configurado
(`app_settings.languages.primary`), sempre caem no default do provedor
(TMDB sem `language=` cai em inglês). É por isso que um item de nome em
português aparece com candidato em inglês na fila de Revisão mesmo com
`primary: "pt-BR"` configurado. A introdução de um resolvedor único de
"idioma de busca para este item" (§4) fecha essa lacuna como efeito
colateral.

**Achado colateral #2, incorporado ao escopo por pedido explícito do
dono**: mesmo corrigindo #1, a pontuação (`matching.Pontuar`) hoje compara
o título do arquivo com **um único título candidato** — o que o provedor
devolveu no idioma consultado. Quando o provedor não tem tradução para
aquele idioma (caso do "The Pink Panther") ou quando o título do arquivo é
uma tradução/variante que não bate com NENHUM idioma pedido (caso da "Bela
Adormecida" com candidato em espanhol), o idioma preferido por biblioteca
sozinho não resolve — falta comparar contra o LEQUE de títulos alternativos
que o próprio provedor já conhece para aquele candidato (traduções +
títulos regionais/aka). Esse mecanismo entra no escopo deste briefing
(§3/§4), não fica mais como follow-up.

## 2. Jornada do usuário

1. O dono edita uma biblioteca (nova, em `/dashboard/library/new`, ou
   existente, em `/dashboard/library/{id}/edit`) e escolhe um "Idioma
   preferido" num select — as mesmas opções de `allowed` já cadastradas em
   Configurações (`/dashboard/settings`). Deixar em branco = "usar o idioma
   global" (comportamento atual, é o default).
2. Ao rodar um scan, auto-match em lote ("Auto-sincronizar elegíveis" na
   fila de Revisão) ou o job de enriquecimento retroativo sobre um item
   dessa biblioteca, a busca ao provedor usa o idioma preferido da
   biblioteca — melhorando a taxa de acerto/confiança quando a convenção de
   nome de arquivo daquela pasta bate melhor com aquele idioma.
3. Ao abrir o detalhe de um item vindo dessa biblioteca **sem** escolher
   explicitamente um idioma no seletor "Idioma de leitura" do topo, título,
   sinopse e tags aparecem no idioma preferido da biblioteca em vez do
   idioma global. Escolher um idioma explicitamente no seletor continua
   valendo para QUALQUER item, de qualquer biblioteca — a preferência da
   biblioteca é só o *default*, nunca uma trava.
4. Bibliotecas sem idioma preferido definido continuam 100% como hoje: tudo
   decidido pelo idioma global de `app_settings`.
5. Ao ver o score de um candidato na fila de Revisão (prévia ou
   auto-match), o palpite agora considera também os títulos
   alternativos/traduzidos que o provedor conhece para aquele candidato —
   um arquivo "A Pantera Cor-De-Rosa" pode pontuar alto mesmo se o TMDB só
   tiver esse título cadastrado como alternativo/regional, sem ser a
   tradução PT-BR "oficial" retornada pela busca. Quando o título vencedor
   não é o título principal do candidato, o card mostra qual título
   alternativo bateu — o dono entende POR QUE aquele score saiu daquele
   jeito, em vez de só ver um número.

## 3. Escopo

**Dentro:**
- Campo `preferred_language` (nullable) em `library_source`.
- `POST /api/v1/library` e `PATCH /api/v1/library/{id}` aceitam o campo;
  `GET /api/v1/library` devolve. Validado contra `app_settings.languages.
  allowed` (mesmo conjunto do seletor de leitura) — código fora da lista
  vira `422 invalid_body`.
- UI: select em `library-new.tsx` e `library-edit.tsx`, populado por
  `GET /api/v1/settings/languages` (mesma fonte do seletor do header),
  com opção "Usar idioma global" (valor vazio → `null`).
- Novo resolvedor de idioma de busca **por item**: dado um `entity.ID`,
  descobre a biblioteca de origem (via `media_file.library_source_id`) e
  devolve o idioma preferido dela, ou o `primary` global se não houver
  biblioteca com preferência definida.
- Esse resolvedor substitui os pontos onde hoje se usa `lang=""`
  ou o idioma global cego, listados em §4 (Backend Go).
- Resolução de EXIBIÇÃO: quando `?lang=` está ausente na requisição,
  `GET /api/v1/entities/{id}` (e as leituras que também derivam a cadeia de
  fallback do item — override, external_detail quando aplicável) usam o
  idioma preferido da biblioteca de origem como cabeça da cadeia, no lugar
  do `primary` global.
- **Pontuação contra títulos alternativos/traduzidos** (0050/0060): para os
  candidatos EXTERNOS não descartados por divergência de ano, o score deixa
  de comparar só contra `ResultadoExterno.Title` (o título no idioma
  consultado) e passa a comparar também contra a lista de títulos
  alternativos/traduzidos que o provedor conhece daquele candidato,
  usando o MAIOR score entre todos. Vale para `melhorCandidatoExterno`
  (`automatch.go`), consumido por `tentarAutoMatch`, `tentarEnriquecerOuSinalizar`
  e `matchPreview` — os três fluxos que já reusam essa função hoje.
- Custo controlado: só busca títulos alternativos para os **top-3**
  candidatos por score de título principal (entre os não descartados por
  ano) — não para a lista inteira. Resultado cacheado em memória por
  `(source, source_id)`, mesmo TTL do `cacheExterno` (`ttlCacheExterno`,
  60s) — reaproveita o padrão existente, sem inventar um novo parâmetro de
  tuning.
- Nova capacidade OPCIONAL de provider (mesmo padrão de `ColecaoProvider`):
  `BuscarTitulosAlternativos`, implementada para TMDB; candidatos LOCAIS
  (`melhorCandidatoLocal`) e providers sem essa capacidade (fanart.tv, e
  TVDB se a implementação decidir não valer a pena) ficam de fora — o
  finder degrada para o score de título único, como hoje.
- `matchPreviewTargetDTO` ganha `matched_title` (o título — principal ou
  alternativo — que de fato produziu o score mostrado), para a UI explicar
  a origem do palpite.

**Fora (explicitamente):**
- Não muda o modelo de storage multi-idioma (campos continuam traduzidos
  por valor, várias línguas convivem) — só muda qual idioma é o alvo
  PADRÃO de busca/exibição por item.
- Não introduz idioma por subpasta dentro de uma biblioteca — o preferido é
  por `library_source` inteiro.
- Não reprocessa retroativamente itens já casados/enriquecidos só por causa
  de uma biblioteca ganhar idioma preferido depois — o dono usa
  "Auto-sincronizar elegíveis" ou "Corrigir" se quiser forçar novo match.
- Não muda a busca/correção MANUAL (`POST /entities/{id}/correct`, busca da
  tela "Corrigir", `GET /api/v1/search`) — essas continuam usando o idioma
  de leitura ATIVO da sessão (`?lang=`), por serem ação explícita do dono
  buscando na hora, não automação em nome da biblioteca.
- fanart.tv fica fora da mudança de idioma de busca — a API dele não aceita
  `language` (devolve todas as artes de uma vez).
- Sem tela de resolução manual para entity com arquivos de bibliotecas com
  idiomas preferidos DIFERENTES — regra de desempate determinística (§4).
- Títulos alternativos NÃO se aplicam a candidatos LOCAIS
  (`melhorCandidatoLocal`) — o catálogo local já guarda nosso próprio
  título resolvido, não faz sentido buscar "alternativos" dele.
- Sem UI de configuração/tuning do teto de top-3 nem do TTL do cache — são
  constantes de código (mesmo espírito de `LimiarAutoMatch`/`ttlCacheExterno`
  hoje: "tuning, não modelo", ADR-0008).

## 4. Contrato técnico

### Dados / modelo
- Migration `internal/db/migrations/0017_library_source_idioma_preferido.sql`:
  ```sql
  ALTER TABLE library_source ADD COLUMN preferred_language TEXT;
  ```
  Sem `CHECK` (mesmo padrão de `content_type`, campo livre — o conjunto
  válido de idiomas é dinâmico, vive em `app_settings`, não faz sentido
  travar no schema SQLite). `NULL` = sem preferência (default).
- `internal/library/library.go`: struct `Source` ganha
  `PreferredLanguage *string`; `Service` ganha validação do código contra
  `allowed` na criação/patch (recebe a lista já resolvida pelo chamador em
  `internal/server`, para não importar o pacote de settings em `library`).
- Sem tabela nova para títulos alternativos: cache só em MEMÓRIA (mesmo
  desenho de `cacheExterno`), nunca persistido — é um resultado
  re-derivável a qualquer momento, não um dado de catálogo (ADR-0011).

### API (`openapi.yaml`)
- `Source`: novo campo opcional `preferred_language: { type: [string, "null"] }`.
- `CreateSource`: novo campo opcional `preferred_language: { type: string }`.
- `PatchSource`: novo campo opcional `preferred_language: { type: [string, "null"] }`
  (`null` explícito no corpo = limpa a preferência; ausente = inalterado,
  mesmo padrão de `path`/`watch_enabled`).
- Erros: `422 invalid_body` quando `preferred_language` não está em
  `allowed` (mesma resposta usada por `PUT /settings/languages`).
- `MatchPreviewTarget`: novo campo opcional `matched_title: { type: string }`.

### Backend (Go)
- **Novo helper único**, em `internal/server` (ex.: `idioma_biblioteca.go`):
  `s.idiomaDeBusca(ctx, entityID string) (string, error)` — resolve:
  1. Lê o(s) `library_source_id` dos `media_file` `state='present'` da
     entity, ordenados por `created_at ASC, id ASC` (primeiro arquivo
     ligado à obra é o desempate — mesmo critério "primeiro vence" já usado
     em outros pontos do projeto).
  2. Se o primeiro tiver `preferred_language` definido **e** todos os
     demais `library_source_id` distintos da entity concordarem (mesmo
     valor ou `NULL`), usa esse idioma.
  3. Em qualquer ambiguidade (fontes com preferências DIFERENTES entre si)
     ou ausência de preferência, cai no `primary` global
     (`obterLanguageSettings`) — comportamento atual, sem tela de decisão.
- Pontos que passam a chamar `s.idiomaDeBusca` em vez de `""`/idioma cego:
  - `internal/server/automatch.go:241` — `s.buscarExterno(ctx, titulo, ent.Type, "")` → idioma resolvido.
  - `internal/server/retroativo.go` (`tentarEnriquecerOuSinalizar`, chamada equivalente a `buscarExterno`) → idioma resolvido.
  - `internal/server/match_preview.go:125` — mesma troca (a prévia deve refletir o MESMO idioma que o auto-match de fato usaria).
  - Import automático de série: `internal/scanner/manager.go` (`Manager.idiomas`) e `internal/enrich/serie.go:370` — hoje usam a lista global injetada via `ConfigurarSeries`; passam a considerar `r.source.PreferredLanguage` quando presente (o `scanRun` já carrega `library.Source` completo, ver `internal/scanner/match.go`).
- Ponto que passa a usar o idioma preferido como default de EXIBIÇÃO:
  - `internal/server/entities.go:612-658` — quando `r.URL.Query().Get("lang") == ""` (nenhum override explícito), troca o `lang` usado em `cfg.cadeiaDeIdiomas(lang, ent.OriginalLanguage)` pelo idioma resolvido via `s.idiomaDeBusca` (que já cai no `primary` global se não houver preferência — sem duplicar a regra de fallback).
  - `?lang=` explícito na query SEMPRE vence, em qualquer entity — sem exceção.
- `internal/server/library.go`: `createRequest`/`patchSourceRequest`/`sourceDTO` ganham `PreferredLanguage` (ponteiro, mesmo padrão de `WatchEnabled`/`Path` no patch); handlers `createLibrary`/`patchLibrary` validam contra `allowed` antes de gravar.

**Títulos alternativos (novo mecanismo):**
- `internal/provider/provider.go`: nova capacidade OPCIONAL, mesmo padrão
  de `ColecaoProvider` (type-assert do `Provider`, degrada quando ausente):
  ```go
  type TituloAlternativoProvider interface {
      BuscarTitulosAlternativos(ctx context.Context, tipo, sourceID string) ([]string, error)
  }
  ```
- `internal/provider/tmdb/tmdb.go`: implementa via
  `GET /{movie|tv}/{id}?append_to_response=translations`, extraindo o
  `title`/`name` de cada tradução (mesma chamada leve, sem os blocos
  pesados de `credits`/`keywords`/`similar` que `BuscarDetalhe` já traz —
  chamada NOVA e mais barata, não reaproveita `BuscarDetalhe`).
- `internal/matching/matching.go`: nova função
  `PontuarComAlternativos(tituloConsulta string, anoConsulta int, tituloCandidato string, titulosAlternativos []string, anoCandidato int) (score float64, tituloVencedor string, descartado bool)`
  — mesma regra de ano de `Pontuar` (diff>1 descarta; diff==0 soma +5);
  a similaridade textual usa o MAIOR `textsim.Similaridade` entre
  `tituloCandidato` e cada item de `titulosAlternativos`; `tituloVencedor`
  devolve qual string produziu o score (para `matched_title`). `Pontuar`
  original continua existindo, sem mudança de assinatura (usado por
  `melhorCandidatoLocal`).
- `automatch.go`: `melhorCandidatoExterno` vira método `(s *Server)` (hoje é
  função de pacote) para acessar `s.providers`/`ctx`/cache; depois de
  ordenar os candidatos não-descartados pelo score de título principal
  (`Pontuar`), busca títulos alternativos SÓ para os top-3 (cache por
  `source+"\x00"+sourceID`, TTL `ttlCacheExterno`) e repontua esses três com
  `PontuarComAlternativos`; o vencedor final é o maior score entre os N
  originais (com título único) e os 3 repontuados.
- `match_preview.go`: `matchPreviewTargetDTO.MatchedTitle` recebe o
  `tituloVencedor` quando o método é `external`; omitido (`nil`) quando é o
  próprio `Title` do candidato (nada de alternativo bateu melhor) ou quando
  é `local`.

### Frontend (Vite/React)
- `web/src/routes/library-new.tsx`: novo `Select` "Idioma preferido",
  opções vindas de `languagesQueryOptions` (já existe, consumido por
  `language-selector.tsx`/`settings.tsx`), com opção "Usar idioma global".
- `web/src/routes/library-edit.tsx`: mesmo select, PATCH parcial (só envia
  o campo se o dono mexeu nele).
- `web/src/lib/api.ts`: tipos `CreateSource`/`Source`/`PatchSource`
  regenerados do OpenAPI (`gen:types`) — sem código novo de tipagem manual.
- Nenhuma mudança no seletor "Idioma de leitura" do header nem no
  `language-context.tsx` — o override explícito da sessão continua com a
  MESMA prioridade máxima que tem hoje.
- `web/src/routes/review.tsx` (`PreviewBloco`): quando `matched_title`
  vier preenchido e for diferente de `target.title`, mostra uma linha
  extra tipo `via título alternativo: "<matched_title>"` abaixo do score —
  mesmo bloco que já mostra `score {toFixed(1)}`.

### Arquivos afetados
- `internal/db/migrations/0017_library_source_idioma_preferido.sql` (novo)
- `internal/library/library.go`
- `internal/server/library.go`
- `internal/server/idioma_biblioteca.go` (novo — helper `idiomaDeBusca`)
- `internal/server/automatch.go`
- `internal/server/retroativo.go`
- `internal/server/match_preview.go`
- `internal/server/entities.go`
- `internal/scanner/manager.go`
- `internal/enrich/serie.go`
- `internal/matching/matching.go` (nova `PontuarComAlternativos`)
- `internal/provider/provider.go` (nova interface `TituloAlternativoProvider`)
- `internal/provider/tmdb/tmdb.go` (implementação)
- `openapi.yaml`
- `web/src/routes/library-new.tsx`
- `web/src/routes/library-edit.tsx`
- `web/src/routes/review.tsx`
- `web/src/lib/api.ts` (gerado)

## 5. Critérios de aceite

- [ ] Criar biblioteca com `preferred_language: "en"` e escanear itens sem
      id externo embutido: a chamada ao provedor (log/gravação) usa
      `language=en`, não o `primary` global.
- [ ] Rodar "Auto-sincronizar elegíveis" / GET match-preview num item de
      biblioteca com `preferred_language` definido: o score refletido usa
      candidato buscado nesse idioma.
- [ ] Biblioteca SEM `preferred_language` (ou campo omitido): scan,
      auto-match e retroativo se comportam exatamente como antes deste
      briefing (busca no `primary` global, não mais `""`).
- [ ] `GET /entities/{id}` sem `?lang=` num item de biblioteca com
      `preferred_language: "en"` devolve título/sinopse em inglês (se
      houver tradução), mesmo com `app_settings.languages.primary=pt-BR`.
- [ ] `GET /entities/{id}?lang=pt-BR` no MESMO item acima ignora a
      preferência da biblioteca e devolve pt-BR (override explícito vence).
- [ ] Entity com `media_file` de duas bibliotecas com `preferred_language`
      diferentes entre si: resolve para o `primary` global (regra de
      ambiguidade), sem erro.
- [ ] `POST /api/v1/library` com `preferred_language` fora de `allowed`
      responde `422 invalid_body`.
- [ ] `PATCH /api/v1/library/{id}` com `preferred_language: null` explícito
      limpa a preferência (volta a usar o global); campo ausente no corpo
      não altera o valor atual.
- [ ] UI: criar/editar biblioteca mostra o select de idioma preferido com
      as mesmas opções do admin de idiomas; "Usar idioma global" é o
      default ao criar.
- [ ] Candidato externo cujo título alternativo/traduzido bate melhor que o
      título principal: `matched_title` vem preenchido na resposta de
      match-preview e a UI mostra a linha "via título alternativo".
- [ ] Candidato cujo título PRINCIPAL já é o melhor score: `matched_title`
      vem vazio/omitido (nada de ruído quando não precisa).
- [ ] Só os top-3 candidatos por score de título principal disparam a
      chamada de títulos alternativos — candidatos além do 3º não geram
      chamada extra ao provedor (verificável por contagem de chamadas em
      teste com mock de provider).
- [ ] Duas buscas repetidas pelo MESMO candidato externo dentro do TTL do
      cache não disparam nova chamada de títulos alternativos.
- [ ] Candidato LOCAL (`melhorCandidatoLocal`) nunca dispara busca de
      títulos alternativos.

## 6. Notas de implementação / decisões

- **Por que o desempate "primeiro `media_file` ligado à entity" e não
  "biblioteca com mais arquivos"**: mais simples de explicar e de
  implementar (um `ORDER BY ... LIMIT 1`, sem `GROUP BY`/contagem), e o
  caso de uma entity com arquivos de DUAS bibliotecas diferentes já é raro
  (fusão de duplicata entre bibliotecas). Ambiguidade cai pro global de
  qualquer forma — a escolha do desempate só importa no caso raríssimo de
  duas fontes com preferências diferentes E a entity nunca ter sido
  reprocessada.
- **Por que não colocar `CHECK` no SQLite para `preferred_language`**: o
  conjunto válido (`app_settings.languages.allowed`) é dinâmico — o dono
  pode adicionar/remover idiomas permitidos depois de já existir uma
  biblioteca configurada. Validação fica só na camada de aplicação, no
  momento da escrita (mesma decisão já tomada para `content_type`).
- **Por que a busca/correção manual fica de fora**: são ações pontuais e
  explícitas do dono, já com um idioma de sessão visível na tela — usar a
  preferência da biblioteca ali seria uma surpresa (o dono pode estar
  corrigindo um item precisamente PORQUE o candidato veio no idioma
  errado).
- **Achado do §1 (buscas com `lang=""`)**: vale confirmar com teste antes
  de generalizar — pode ser proposital em algum ponto específico (ex.:
  cache compartilhado entre idiomas). Ler os três call sites listados em
  §4 com atenção antes de trocar, não assumir que os três têm exatamente o
  mesmo motivo.
- **Por que top-3 e não "todos os candidatos"**: buscar títulos
  alternativos é 1 chamada de rede extra POR candidato. A lista já vem
  filtrada por `Pontuar` (descarta ano divergente >1) e tipicamente tem
  poucos itens plausíveis; top-3 cobre o caso real (o candidato certo quase
  sempre está entre os 3 melhores por título principal) sem multiplicar
  chamadas numa varredura em lote (`retroativo.go` já é throttled a 15
  itens/execução — não faz sentido essa camada virar o novo gargalo de
  rate-limit).
- **Por que TMDB primeiro, TVDB como decisão de implementação**: o payload
  de tradução do TVDB já parece trazer `aliases` (visto em
  `internal/provider/tvdb/testdata/series_simpsons_trans_por.json`), mas
  não foi confirmado se isso vem SEM chamada extra no fluxo de busca atual.
  Cabe à implementação verificar; se precisar de chamada extra, aplica o
  MESMO teto de top-3 e cache — não um orçamento maior só porque é uma
  fonte diferente.
- **Efeito colateral esperado**: com a pontuação mais generosa (título
  alternativo pode elevar o score), mais itens devem cruzar o
  `LimiarAutoMatch` (92) sozinhos ao longo do tempo — é o comportamento
  pretendido (menos itens presos na fila de Revisão por um score
  artificialmente baixo), não uma regressão a investigar.

## 7. Fora deste briefing / follow-ups

- Idioma preferido por SUBPASTA dentro de uma biblioteca (fora de escopo
  aqui, complexidade desproporcional ao ganho hoje).
- Reprocessamento em lote automático quando uma biblioteca ganha/troca
  `preferred_language` (hoje é manual, via "Auto-sincronizar elegíveis").
- Expandir títulos alternativos para os candidatos LOCAIS
  (`melhorCandidatoLocal`) — hoje descartado por não fazer sentido
  (catálogo local já usa nosso próprio título resolvido), mas poderia
  ajudar duplicatas cadastradas com títulos em idiomas diferentes.
