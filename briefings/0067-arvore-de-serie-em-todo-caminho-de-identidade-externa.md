# 0067 — Árvore de série (temporada/episódio) em TODO caminho de identidade externa

> `Enriquecer` (usado por Corrigir, Confirmar candidato, auto-match e enriquecimento
> retroativo/`enrich-missing`) hoje grava identidade/pôster/título de uma série mas
> **nunca constrói temporada/episódio** — só `importarSerie` (via `POST /import`) faz
> isso. Como toda série escaneada da biblioteca nasce `draft`/`cataloged` sem id
> externo e normalmente ganha o id **fora** de `POST /import` (revisão manual,
> auto-match, retroativo), esse é hoje o caminho mais comum de import de série na
> prática — e o mais quebrado. Este briefing faz `Enriquecer` sincronizar a árvore
> sempre que `tipo == 'tv'`, em UM ponto só, sem tocar cada chamador.

| | |
|---|---|
| **ID** | 0067 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (vertical séries) |
| **Docs relacionadas** | [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md), [0031](0031-series-fundacao-modelo-enriquecimento.md), [0043](0043-re-enrich-atualiza-arvore-serie.md), [0050](0050-auto-match-fila-de-revisao.md), [0054](0054-auto-match-no-import-manual-e-job-de-saude.md), [0056](0056-enriquecimento-externo-retroativo-job-saude.md), [0059](0059-grupo-acoes-rapidas-por-item.md), [0060](0060-previa-de-candidato-na-fila-de-revisao.md) |
| **Status** | done |
| **Depende de** | 0031, 0043 |

---

## 1. Objetivo

Reportado pelo dono: importou uma série (`MIB: Homens de Preto`, tmdb id `1153`) e ela
ficou com os 50 arquivos ligados à entity, título/pôster/sinopse corretos, mas **zero**
temporada/episódio como entity filha (`GET /entities/{id}/members` vazio). Investigação
confirmou a causa: existem **dois caminhos completamente diferentes** para uma série
ganhar identidade externa no código:

1. **`POST /import`** → `Importar()` → para `type=tv`, `importarSerie()`
   (`internal/enrich/serie.go:53`) busca a árvore inteira do provider e cria/atualiza
   cada temporada/episódio como entity filha (`importarTemporada`/`acharOuCriarEpisodio`).
   Este é o **único** lugar que hoje constrói a árvore.
2. **Qualquer caminho que aplica identidade externa a uma entity JÁ EXISTENTE** —
   "Corrigir" (`aplicarIdentidadeExterna`, `internal/server/review.go:174`), o branch
   externo de "Confirmar candidato" (`confirmCandidate`), o auto-match
   (`aplicarCandidatoExterno`, `internal/server/automatch.go:265`) e o enriquecimento
   retroativo do job de saúde + o botão manual por item
   (`tentarEnriquecerOuSinalizar`/`enrichMissing`, `internal/server/retroativo.go`) — todos
   chamam só `s.enrich.Enriquecer()` (`internal/enrich/enrich.go:96`), que grava
   `external_ids`, o payload base e o pôster/título, mas **nunca** caminha a árvore.

O briefing [0043](0043-re-enrich-atualiza-arvore-serie.md) já registrou essa exclusão
EXPLICITAMENTE em 2026-07-12 (§3, "Fora"): *"Endpoint dedicado de refresh... O `POST
/entities/{id}/enrich`/`Enriquecer` não caminha a árvore e fica fora."* Na época era uma
lacuna pontual. Desde então, quatro NOVOS chamadores de `Enriquecer` para série foram
adicionados (0050, 0054, 0056, 0059, 0060) — todos herdando o mesmo buraco. Como o
scanner ([0034](0034-scanner-de-series-arquivo-episodio.md)) sempre cria a série como
draft/cataloged sem id externo, e o dono raramente usa `POST /import` diretamente numa
pasta já escaneada (ele revisa, corrige ou deixa o auto-match/job resolver), **a maioria
das séries reais da biblioteca passa pelo caminho quebrado**, não pelo correto.

## 2. Jornada do usuário

1. O scanner encontra uma pasta de série nova (`MIB - Homens de Preto (1997)/Season
   01/...`) e cria uma entity `type='tv'` (draft ou `cataloged` de baixa confiança,
   50 arquivos ligados a ela, nenhuma temporada/episódio ainda — normal nesta fase).
2. O dono resolve o item pela fila de Revisão (Corrigir/Confirmar candidato) — **ou**
   o auto-match/job de saúde resolve sozinho, à noite, sem o dono perceber.
3. **Hoje**: o item ganha título, pôster, sinopse e `external_ids.tmdb` — parece
   "importado" na listagem — mas abrir o detalhe mostra zero temporadas, e os 50
   arquivos continuam soltos na entity da série, sem nenhuma organização por episódio.
4. **Depois deste briefing**: o mesmo evento (Corrigir, Confirmar candidato, auto-match
   ou o job retroativo/botão "baixar informações faltando") também constrói a árvore de
   temporada/episódio, exatamente como um `POST /import` direto construiria — o dono não
   precisa saber qual caminho foi usado, o resultado é sempre completo.
5. Se a busca da árvore falhar (provider fora do ar) o resto do enriquecimento (id,
   título, pôster) continua valendo — a árvore fica pendente e é retentada na próxima
   ação sobre aquele item (idempotente: `importarTemporada`/`acharOuCriarEpisodio` já
   são find-or-create com refresh, 0043).

## 3. Escopo

**Dentro:**
- Extrair o "rabo" de `importarSerie` (busca da árvore + capacidade do provider,
  gravação de `extension_data` da série, enriquecimento rico cruzado, loop de
  temporadas, inventário de artes) em métodos nomeados e reusáveis em
  `internal/enrich/serie.go`, sem mudar o comportamento de `POST /import` (extração
  mecânica — mesmo espírito do 0056 ao extrair `aplicarCandidatoExterno`).
- Fazer `Enriquecer` (`internal/enrich/enrich.go:96`) sincronizar a árvore sempre que
  `tipo == 'tv'`, como último passo, **best-effort** (loga e degrada — nunca faz o
  `Enriquecer` inteiro falhar por causa da árvore).
- Evitar a ida dupla ao provider quando `importarSerie` reusa uma série já existente
  (branch de `acharPorIDExterno`/draft-casado em `importarItemBase`, que hoje chama
  `Enriquecer` internamente): esses dois call sites internos passam a chamar uma
  variante privada que **pula** a sincronização da árvore, porque `importarSerie` já
  vai fazer isso, exatamente uma vez, logo em seguida — sem alterar o número de
  chamadas ao provider que `POST /import` já faz hoje.
- Cobertura de teste nova em `internal/server` (automatch, review/correct,
  confirm-candidate, retroativo/enrich-missing) provando que uma entity `type='tv'`
  ganha temporada/episódio por CADA um desses quatro caminhos — hoje nenhum teste
  desses arquivos exercita `tv` de verdade (o `fakeProvider` compartilhado de
  `internal/server/search_test.go` não implementa `provider.SerieProvider`).
- Backfill manual (fora de código): re-tocar a entity reportada pelo dono
  (`ba252e9c-2b68-4cdc-a6b6-02136dd9b3d1`, MIB: Homens de Preto) após o deploy, via
  `POST /entities/{id}/enrich-missing` ou "Corrigir", para confirmar em produção que a
  árvore aparece.

**Fora (explicitamente):**
- **Backfill em massa automático** de todas as séries já afetadas pelo bug (qualquer
  `type='tv'` com `external_ids_json` preenchido e zero filhas). Não há uma flag/coluna
  hoje para diferenciar "série sem temporada de propósito" (não existe tal caso) de
  "série afetada pelo bug"; um script/endpoint de reparo em lote é um follow-up válido
  se o dono quiser resolver TODAS de uma vez em vez de re-tocar item a item.
- **Sinalização de UI** para "árvore incompleta/pendente" — o item sinalizado continua
  invisível como qualquer enriquecimento parcial hoje (mesmo comportamento do 0043 para
  falha de imagem). Um indicador dedicado é follow-up.
- **Retry automático** de árvore que falhou uma vez: uma série que já tem
  `external_ids` preenchido sai do escopo de `ListarElegiveisEnriquecimentoRetroativo`
  (0056, filtra `external_ids_json IS NULL`) — se a sincronização da árvore falhar
  best-effort neste briefing, ela só é retentada na PRÓXIMA ação manual sobre aquele
  item (Corrigir de novo, ou o botão "baixar informações faltando" do 0059), não
  automaticamente pelo job. Ver §7.
- **Pruning** de temporada/episódio removidos no provider — já fora desde o 0043.
- Mudar `matching.LimiarAutoMatch`, o algoritmo de pontuação, ou qualquer contrato de
  API/response — nenhum endpoint muda de shape (`EnrichMissingResult`, `ConfirmCandidate`
  etc. permanecem iguais).
- Unificar o branch de CRIAÇÃO de `importarItemBase` (hoje `CriarEntity` + `persistir`
  manual, sem `curarTitulo`) para passar por `Enriquecer` — investigado, é seguro
  (`curarTitulo` é idempotente/não-destrutivo e `PayloadFresco` é cache-first, então a
  2ª chamada não bate na rede de novo), mas não é necessário para este bug (esse branch
  só cria série NOVA dentro de `importarSerie`, que já sincroniza a árvore
  explicitamente depois, independente do que o branch de criação faça). Fica como
  simplificação follow-up, não faz parte do critério de aceite deste briefing.

## 4. Contrato técnico

### Dados / modelo
Nenhuma migration. Nenhum campo novo.

### API
Nenhuma mudança de contrato (`openapi.yaml` intocado). O efeito é observável em
`GET /entities/{id}/members` (deixa de vir vazio) e `GET /entities/{id}/episodes` para
qualquer série que passe por Corrigir/Confirmar candidato/auto-match/retroativo/
enrich-missing — mesmos endpoints, resultado mais completo.

### Backend (Go)

**`internal/enrich/serie.go`** — extrair de `importarSerie` (hoje linhas 53-90):

```go
// arvoreDaSerie valida a capacidade do provider e busca a arvore bruta (temporadas +
// episodios) na fonte -- sem tocar nenhuma entity. Extraido de importarSerie para ser
// reusado por Enriquecer (0067): la, a entity ja existe e so a arvore precisa ser
// buscada/aplicada; aqui, POST /import busca ANTES de criar a serie (falha rapido sem
// deixar entity orfa quando a fonte nao suporta serie).
func (s *Servico) arvoreDaSerie(ctx context.Context, source, sourceID string, langs []string) (provider.SerieArvore, error) {
	prov, ok := s.provFonte(source)
	if !ok {
		return provider.SerieArvore{}, fmt.Errorf("enrich: fonte %q sem provider", source)
	}
	sp, ok := prov.(provider.SerieProvider)
	if !ok {
		return provider.SerieArvore{}, fmt.Errorf("enrich: fonte %q nao suporta serie", source)
	}
	return sp.BuscarSerieArvore(ctx, sourceID, langs)
}

// aplicarArvoreSerie grava a arvore JA BUSCADA numa serie que JA EXISTE (serieID
// conhecido): extension_data da serie, enriquecimento rico cruzado (best-effort),
// cada temporada/episodio (find-or-create com refresh, 0043) e o inventario de artes.
// So AtualizarExtensionData propaga erro (escrita de banco real); o resto e best-effort,
// exatamente como importarSerie ja fazia.
func (s *Servico) aplicarArvoreSerie(ctx context.Context, serieID, source, sourceID string, arvore provider.SerieArvore, langs []string) error {
	if err := s.escrita().AtualizarExtensionData(ctx, sqlcgen.AtualizarExtensionDataParams{
		ExtensionDataJson: nsValid(extensaoSerie(arvore)),
		ID:                serieID,
	}); err != nil {
		return err
	}
	s.enriquecerSerieRico(ctx, serieID, source, sourceID, langs)
	for _, t := range arvore.Temporadas {
		s.importarTemporada(ctx, serieID, source, arvore.DefaultSeasonType, t, langs)
	}
	s.enriquecerArtes(ctx, serieID, tipoSerie, langs)
	return nil
}

// sincronizarArvoreSerie combina os dois passos acima para um chamador que so tem
// (serieID, source, sourceID) -- o caso de Enriquecer (0067), que nao tem uma arvore
// pre-buscada como importarSerie tem. Erro cobre tanto capacidade/rede (arvoreDaSerie)
// quanto a escrita do extension_data (aplicarArvoreSerie); o chamador (Enriquecer)
// trata como best-effort.
func (s *Servico) sincronizarArvoreSerie(ctx context.Context, serieID, source, sourceID string, langs []string) error {
	arvore, err := s.arvoreDaSerie(ctx, source, sourceID, langs)
	if err != nil {
		return err
	}
	return s.aplicarArvoreSerie(ctx, serieID, source, sourceID, arvore, langs)
}

func (s *Servico) importarSerie(ctx context.Context, source, sourceID string, langs []string) (string, bool, error) {
	arvore, err := s.arvoreDaSerie(ctx, source, sourceID, langs)
	if err != nil {
		return "", false, fmt.Errorf("enrich: buscar arvore da serie: %w", err)
	}
	serieID, criada, err := s.importarItemSemArvore(ctx, source, sourceID, tipoSerie, langs)
	if err != nil {
		return serieID, criada, err
	}
	if err := s.aplicarArvoreSerie(ctx, serieID, source, sourceID, arvore, langs); err != nil {
		return serieID, criada, err
	}
	return serieID, criada, nil
}
```

Comportamento IDÊNTICO ao `importarSerie` de hoje para `POST /import` — mesma ordem
(capacidade/busca falha ANTES de criar a entity), mesmos erros propagados, mesmos
passos best-effort. `importarItemSemArvore` é o novo nome/variante de `importarItem`
usada aqui — ver abaixo.

**`internal/enrich/enrich.go`**:
- `Enriquecer` (pública, assinatura INTOCADA) vira um wrapper fino:
  ```go
  func (s *Servico) Enriquecer(ctx context.Context, entityID, source, sourceID, tipo string, langs []string) error {
  	return s.enriquecerBase(ctx, entityID, source, sourceID, tipo, langs, true)
  }
  ```
- Novo `enriquecerBase` contém o corpo de hoje de `Enriquecer` (`PayloadFresco` →
  `mesclarExternalIds` → `persistir` → `curarTitulo`) e ganha um parâmetro
  `sincronizarArvore bool`. Ao final, só quando `tipo == tipoSerie && sincronizarArvore`:
  chama `s.sincronizarArvoreSerie(ctx, entityID, source, sourceID, langs)` e, em erro,
  **loga e degrada** (`log.Printf`, não retorna erro) — o contrato de `Enriquecer`
  (identidade + base aplicadas) não pode falhar por causa de uma falha secundária na
  árvore, e o chamador (`aplicarIdentidadeExterna`/`aplicarCandidatoExterno`) já
  avançaria demais (external_ids/persistir já gravados) para reportar "falhou" com
  honestidade se propagasse.
- `importarItemBase` (dois call sites hoje em `s.Enriquecer(...)`, linhas 151 e 172):
  passam a chamar `s.enriquecerBase(ctx, id, source, sourceID, tipo, langs, false)` —
  pulam a sincronização da árvore porque, quando `tipo == tipoSerie`, o único chamador
  desses dois branches é `importarSerie` (via `importarItemSemArvore`, ver abaixo), que
  já sincroniza a árvore explicitamente logo depois. Para `movie`/`collection` o
  parâmetro é inofensivo (o branch `tipo == tipoSerie` dentro de `enriquecerBase` nunca
  roda).
- `importarItem` (usada por `Importar` para `movie` e por `ligarMembros` para membros de
  coleção) continua chamando `importarItemBase` como hoje, sem mudança de assinatura.
  `importarSerie` passa a chamar uma variante `importarItemSemArvore` que é
  `importarItemBase` diretamente (sem o gatilho de artes do 0048, que já não se aplica à
  série — o comportamento de hoje já pula esse gatilho para `tipoSerie`, só o nome muda
  para deixar explícito que este caminho não sincroniza árvore sozinho).

### Frontend (Vite/React)
Nenhuma mudança. As telas de detalhe/temporada/episódio já leem `GET
/entities/{id}/members`/`episodes` normalmente; passam a receber dado onde antes vinha
vazio.

### Arquivos afetados
- `internal/enrich/serie.go` — extrai `arvoreDaSerie`/`aplicarArvoreSerie`/
  `sincronizarArvoreSerie`, reescreve `importarSerie`.
- `internal/enrich/enrich.go` — `Enriquecer` vira wrapper de `enriquecerBase`; os dois
  call sites internos de `importarItemBase` passam `sincronizarArvore=false`;
  `importarItem`/`importarItemSemArvore` conforme acima.
- `internal/enrich/enrich_test.go` — testes novos: `Enriquecer` sobre entity `tv`
  sincroniza a árvore; falha da árvore degrada sem quebrar `Enriquecer`.
- `internal/enrich/serie_test.go` — nenhuma mudança de expectativa (mecânico); ver §5.
- `internal/server/search_test.go` (ou fixture equivalente) — o `fakeProvider`
  compartilhado ganha suporte OPCIONAL a `provider.SerieProvider` (um campo/flag para
  ligar/desligar, já que os testes de `movie` não devem mudar de comportamento).
- `internal/server/automatch_test.go`, `review_test.go`, `match_preview_test.go`,
  `retroativo_test.go` — um teste novo por arquivo cobrindo `type='tv'` através do
  caminho daquele arquivo (auto-match externo, Corrigir, Confirmar candidato externo,
  retroativo/enrich-missing) provando `members`/temporada aparecem depois.

## 5. Critérios de aceite

- [ ] `POST /entities/{id}/enrich` (ou "Corrigir"/`aplicarIdentidadeExterna`) sobre uma
  entity `type='tv'` sem árvore, apontando para um `source_id` válido, resulta em
  temporada(s)/episódio(s) criados como entity filha (`GET /entities/{id}/members`
  deixa de vir vazio).
- [ ] O mesmo vale para "Confirmar candidato" (`confirmCandidate`, branch externo),
  auto-match (`aplicarCandidatoExterno`) e o enriquecimento retroativo/`enrich-missing`
  (`tentarEnriquecerOuSinalizar`) — um teste por caminho.
- [ ] `POST /import` de uma série continua se comportando EXATAMENTE como hoje:
  `TestImportarSerieCriaArvore`, `TestImportarSerieGravaMediaExternalDoEpisodio`,
  `TestReimportarEpisodioReescreveMediaExternal`, `TestReimportarSerieIdempotente`,
  `TestImportarSerieSemSuporte`, `TestImportarSeriePorNome*` passam sem alteração de
  expectativa (diff mecânico).
- [ ] Falha da busca da árvore dentro de `Enriquecer` (provider indisponível) **não**
  faz `Enriquecer` retornar erro — a identidade/título/pôster continuam sendo aplicados
  e o chamador HTTP responde sucesso normalmente; só loga.
- [ ] Reimportar/re-corrigir uma série já existente via `POST /import` continua fazendo
  **exatamente uma** chamada a `BuscarSerieArvore` por execução (sem duplicar a ida ao
  provider no branch de reuse de `importarItemBase`).
- [ ] Entity `movie`/`collection` não muda de comportamento em nenhum teste existente
  (o parâmetro `sincronizarArvore` é um no-op fora de `tipo == 'tv'`).
- [ ] `go build/vet/test -count=1` verdes; catraca local não piora; `tsc -b` do web
  verde (nenhum arquivo de frontend tocado).
- [ ] Smoke real (pós-deploy): `POST /entities/ba252e9c-2b68-4cdc-a6b6-02136dd9b3d1/enrich-missing`
  (ou "Corrigir" na UI) na série MIB: Homens de Preto em produção resulta em
  `GET /entities/{id}/members` não-vazio.

## 6. Notas de implementação / decisões

- **Por que best-effort e não propagar erro**: no momento em que a sincronização da
  árvore rodaria dentro de `enriquecerBase`, `mesclarExternalIds`/`persistir` já
  gravaram no banco (external_ids, payload base, pôster, título). Propagar a falha da
  árvore como erro do `Enriquecer` inteiro faria o chamador HTTP responder "falhou"
  (502) sobre uma operação que, na prática, já mudou o estado da entity — mentira por
  omissão, e pior, os chamadores best-effort (`aplicarCandidatoExterno` no auto-match/
  retroativo) já tratam erro de `Enriquecer` como "sem aplicar, fica pendente" — um
  erro só-da-árvore faria uma identidade que JÁ bateu o limiar de confiança nunca ser
  aplicada, um retrocesso pior que o bug atual.
- **`sincronizarArvore=false` nos call sites internos de `importarItemBase`**: sem essa
  flag, o branch de reuse de `POST /import` sobre uma série já catalogada chamaria
  `BuscarSerieArvore` (dentro do `Enriquecer` interno) e DEPOIS `importarSerie`
  chamaria de novo (para aplicar) — duas idas ao provider por reimport, desperdício
  real de rate-limit em bibliotecas com séries grandes (TVDB/TMDB). A flag garante
  exatamente a mesma contagem de chamadas ao provider que hoje.
- **Sem coluna/flag de "árvore incompleta"**: uma série cuja árvore falhou
  best-effort fica com `external_ids` preenchido mas sem filhas — indistinguível, no
  schema atual, de uma série cuja árvore ainda não foi tentada. Isso já era verdade
  ANTES deste briefing (é o próprio bug); a diferença é que agora o caminho comum
  (Corrigir/auto-match) tenta construir a árvore em vez de nunca tentar. Uma tentativa
  falha ainda é estritamente melhor que a garantia atual de nunca tentar. Ver §7 para o
  caso de retry.
- **Escopo restrito a série (não coleção)**: `importarColecao`
  (`internal/enrich/enrich.go:201`) já liga seus membros via `ligarMembros` → `Importar`
  → `importarItem`, um caminho que não passa por `Enriquecer` isoladamente da mesma
  forma (cada filme membro é find-or-create completo) — sem o mesmo bug, fora de escopo.

## 7. Fora deste briefing / follow-ups

- **Backfill em massa** de séries já afetadas (query: `type='tv'` com
  `external_ids_json` preenchido e sem `entity_relation` filha de temporada) — hoje
  resolvido item a item (Corrigir de novo, ou "baixar informações faltando" do 0059).
  Um job/endpoint dedicado de reparo em lote é um follow-up se o volume justificar.
- **Retry automático de árvore que falhou uma vez**: uma vez que `external_ids` está
  preenchido, o item sai do escopo do `ListarElegiveisEnriquecimentoRetroativo` (0056,
  filtra por `external_ids_json IS NULL`) — uma falha best-effort da árvore não tem
  hoje um mecanismo de nova tentativa automática. Rastrear "árvore pendente" como um
  estado de primeira classe (ex.: reusar `catalog_completeness`, 0058) é follow-up.
- **Indicador de UI para árvore incompleta** — mesmo espírito do 0043/0059
  (`catalog_completeness` hoje mede artes/campos, não a presença de temporada/episódio).
- **Unificar o branch de criação de `importarItemBase` para passar por `Enriquecer`**
  (ganharia `curarTitulo` de graça para item novo) — investigado, seguro, mas não
  necessário para este bug (§3).
