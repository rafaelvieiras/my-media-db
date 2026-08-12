# 0100 — Cache externo sobrevive sem provider vivo

> Separa "capacidade de busca ao vivo" (exige credencial) de "capacidade de
> interpretar payload já cacheado" (função pura, deve funcionar sempre) —
> dado já catalogado nunca mais some da tela por falta/revogação de chave.

| | |
|---|---|
| **ID** | 0100 |
| **Tipo** | fix |
| **Fase (roadmap)** | Transversal — Infraestrutura operacional |
| **Status** | done |
| **Docs relacionadas** | [ADR-0024](../docs/adr/0024-cache-externo-sobrevive-sem-provider-vivo.md), [ADR-0011](../docs/adr/0011-external-ids-autoritativo-media-external-cache.md), [ADR-0023](../docs/adr/0023-integracoes-configuraveis-via-ui-precedencia-sobre-env.md), agenda [PR1](../docs/00-agenda-de-decisoes.md#sobrevivência-offline-do-catálogo--fundacional-levantada-a-partir-da-migração-de-12082026) |
| **Depende de** | — |

---

## 1. Objetivo

Na migração de produção de 12/08/2026 (Batocera), o ambiente subiu com as 4
credenciais de provider vazias. Resultado: **todo item já catalogado há
meses**, com `media_external`/`external_detail` intactos no banco (copiados
na migração), **perdeu sinopse/elenco/equipe/tags/similares na tela** — não
por perda de dado real, mas porque `internal/server/entities.go` e
`internal/externaldetail/externaldetail.go` usam o **mesmo** registro
(`provider.Registry`) tanto para "pode buscar ao vivo" (precisa de chave,
correto) quanto para "pode interpretar o payload que já está salvo"
(`MapearDetalhe`/`Similares`, funções PURAS, sem rede) — sem provider
registrado (chave ausente, revogada, ou provedor descontinuado), a linha
cacheada vira invisível mesmo com `payload_json` válido.

Isso contraria o princípio 6 do `AGENTS.md` ("a mídia sobrevive a QUALQUER
provider registrado") e a filosofia de sobrevivencialismo digital do projeto:
um item catalogado deve renderizar 100% do que já foi importado, mesmo
restaurado numa instância nova sem nenhuma chave configurada, mesmo que o
provider que o enriqueceu deixe de existir no futuro. Causa raiz completa,
com citação de código, em [ADR-0024](../docs/adr/0024-cache-externo-sobrevive-sem-provider-vivo.md)
— este briefing materializa a decisão já fechada lá.

## 2. Jornada do usuário

1. O dono tinha um filme catalogado há meses, com sinopse/elenco/tags/
   similares vindos do TMDB, credencial configurada na época.
2. A credencial é removida/revogada (rotação, migração para ambiente novo sem
   reconfigurar, provedor descontinuado) — o dono não fez nada com aquele
   item específico.
3. **Hoje**: ao abrir o item, sinopse/elenco/tags/similares somem da tela,
   como se o item nunca tivesse sido enriquecido. **Depois deste fix**: o
   item continua mostrando tudo que já foi importado — só deixa de ser
   possível *re-buscar* ou importar algo *novo* enquanto a chave estiver
   ausente (mensagem/estado já existente para "sem fonte configurada" nos
   fluxos de busca/import, sem mudança).
4. Mesma jornada para a tela de **detalhe externo rico** (`/external/{source}/
   {type}/{id}`, briefing 0022/0023): se o item já foi visitado antes (tem
   linha em `external_detail`) e o TTL de 24h expira sem credencial
   disponível para re-buscar, a tela serve o payload **stale** em vez de
   quebrar com 503.

## 3. Escopo

**Dentro:**
- Novo registro `codecs provider.Registry` no `Server`, SEMPRE populado (uma
  instância de cada provider que o binário conhece — tmdb/tvdb/fanarttv —
  com credencial vazia), usado **só** para `MapearDetalhe`/`Similares`/
  `TvdbCruzadoProvider` (capacidade de parse, nunca de rede).
- `internal/server/entities.go` (`montarExterno`/`camadasDaFonte`): resolver
  o provider de uma fonte via `codecs` primeiro, com fallback para o
  registro de busca ao vivo (`providers`) — cobre tanto o modo real
  (credencial presente ou não) quanto o `MYMEDIADB_FAKE_PROVIDER` dos testes
  E2E (que só registra em `providers`).
- `internal/externaldetail/externaldetail.go` (`PayloadFresco`): quando não
  há provider de busca ao vivo registrado para a fonte, OU a busca ao vivo
  falha, servir o payload cacheado **mesmo stale** (ignorando TTL/langs)
  em vez de propagar `ErrNaoEncontrado`/erro cru — só falha de verdade
  quando não há cache algum.
- `internal/server/external_detail.go` (`provedorDetalhe`/`getExternalDetail`):
  parar de 503 de cara quando a fonte não tem busca ao vivo registrada;
  resolver o provider de PARSE via `codecs` para poder chamar
  `MapearDetalhe` mesmo sem credencial, deixando `PayloadFresco` decidir
  internamente se serve cache fresco, stale, ou falha.
- `internal/server/server.go`: construir `s.externalDetail` sempre que há
  banco (não mais condicionado a `!s.providers.Vazio()`) — a ausência de
  credencial passa a degradar POR FONTE dentro do serviço, não a
  desligar o serviço inteiro.
- `cmd/mymediadb/main.go`: nova função `provedoresConhecidos()` que constrói
  os 3 providers com credencial vazia e os registra incondicionalmente via
  a option nova `server.WithCodecRegistry`.
- Testes cobrindo: entity com `media_external` cacheado renderiza campos
  ricos mesmo sem NENHUM provider de busca ao vivo registrado; detalhe
  externo (`/external/...`) serve payload stale quando o TTL expira sem
  provider de busca ao vivo disponível; comportamento inalterado quando a
  credencial ESTÁ presente (regressão zero nos fluxos existentes).

**Fora (explicitamente):**
- Formato exato de sinalizar "dado stale" na resposta da API (ADR-0024 deixa
  em aberto) — não fazer nada além do que já existe; a UI não muda nesta
  entrega.
- `enrich`/import (gravação de dado NOVO) continuam exigindo credencial —
  este briefing só corrige LEITURA do que já está salvo.
- Qualquer mudança em `internal/integration` (ADR-0023, credencial via UI) —
  ortogonal a este fix; a fonte da credencial não importa aqui, só o que
  acontece quando ela está ausente.
- Vínculo entre este fix e o job `saude_biblioteca`/`entity_catalog_completeness`
  — comportamento já correto (o job já recalcula certo quando roda); fora de
  escopo revisitar o cache de completude.

## 4. Contrato técnico

### Backend (Go)

**`internal/provider` — sem mudança de interface.** A distinção é de
REGISTRO (dois `provider.Registry` no `Server`), não de tipo.

**`cmd/mymediadb/main.go`:**
```go
// provedoresConhecidos constrói TODOS os providers que o binário sabe
// interpretar (tmdb/tvdb/fanarttv), com credencial VAZIA, INDEPENDENTE de
// qualquer chave configurada — usados só para a capacidade de PARSE de
// payload já cacheado (MapearDetalhe/Similares/TvdbCruzadoProvider), nunca
// para busca ao vivo. Registrar sempre garante que dado já catalogado
// sobrevive à ausência/revogação de credencial (ADR-0024).
func provedoresConhecidos() []provider.Provider {
    return []provider.Provider{
        tmdb.New(tmdb.Credencial{}, nil, ""),
        tvdb.New(tvdb.Credencial{}, nil, ""),
        fanarttv.New(fanarttv.Credencial{}, nil, ""),
    }
}
```
Conferir na implementação que `New(...)` das 3 fontes não falha/paniqueia com
credencial vazia (não deveria — só os métodos de rede a consultam). Adicionar
a option correspondente na composição de `opts` em `run()`/onde `opcoesBusca()`
é montado hoje — **fora** do branch `MYMEDIADB_FAKE_PROVIDER` (fixtures de
E2E não precisam disso; o fake provider já cobre `codecs` via fallback em
`providers`, ver abaixo).

**`internal/server/server.go`:**
- Novo campo `codecs provider.Registry` no struct `Server`, documentado
  análogo a `providers` (comentário citando ADR-0024).
- Nova option, espelhando `WithSearchProvider`:
  ```go
  // WithCodecRegistry REGISTRA um ou mais providers SÓ para a capacidade de
  // parse de payload já cacheado (MapearDetalhe/Similares/TvdbCruzadoProvider)
  // — nunca para busca ao vivo. Independente de WithSearchProvider: um
  // provider pode estar aqui sem estar lá (sem credencial) e vice-versa (modo
  // fake dos testes E2E). Ver ADR-0024.
  func WithCodecRegistry(provs ...provider.Provider) Option {
      return func(s *Server) {
          for _, p := range provs {
              s.codecs.Registrar(p)
          }
      }
  }
  ```
- Linha ~324 (construção de `s.externalDetail`): trocar
  `if database != nil && !s.providers.Vazio() {` por `if database != nil {`
  — o serviço passa a existir sempre que há banco; a degradação por falta de
  credencial acontece DENTRO do `PayloadFresco`, não mais no gate de
  construção. `enrich` (linha ~318) **não muda** — continua exigindo
  `!s.providers.Vazio()` (grava dado novo, precisa de busca ao vivo).

**`internal/server/entities.go`:**
- Novo helper:
  ```go
  // codecPara resolve o provider usado SÓ para interpretar payload já
  // cacheado (MapearDetalhe/Similares) de uma fonte: primeiro tenta o
  // registro de parse (codecs, sempre populado); cai para o registro de
  // busca ao vivo (providers) quando ausente — cobre o MYMEDIADB_FAKE_PROVIDER
  // dos testes E2E, que só registra em providers. ADR-0024.
  func (s *Server) codecPara(fonte string) (provider.Provider, bool) {
      if p, ok := s.codecs.Get(fonte); ok {
          return p, ok
      }
      return s.providers.Get(fonte)
  }
  ```
- `montarExterno` (linha ~1001): trocar `if s.providers.Vazio() {` por
  `if s.providers.Vazio() && s.codecs.Vazio() {` — só pula a query quando
  NENHUM dos dois registros tem nada (otimização preservada só para
  instalação que nunca teve nenhum provider).
- `camadasDaFonte` (linha ~1070): trocar `prov, ok := s.providers.Get(fonte)`
  por `prov, ok := s.codecPara(fonte)`.

**`internal/externaldetail/externaldetail.go`:**
- `PayloadFresco`: reestruturar para não descartar cache stale numa falha de
  busca ao vivo:
  ```go
  func (s *Servico) PayloadFresco(ctx context.Context, source, sourceID, tipo string, langs []string, force bool) (json.RawMessage, error) {
      if !force {
          if cached, ok := s.doCacheFresco(ctx, source, sourceID, langs); ok {
              return cached, nil
          }
      }

      prov, ok := s.providers.Get(source)
      if !ok {
          if cached, ok := s.doCacheQualquer(ctx, source, sourceID); ok {
              return cached, nil
          }
          return nil, provider.ErrNaoEncontrado
      }

      raw, err := prov.BuscarDetalhe(ctx, sourceID, tipo, langs)
      if err != nil {
          if cached, ok := s.doCacheQualquer(ctx, source, sourceID); ok {
              log.Printf("externaldetail: busca ao vivo de %s/%s falhou, servindo cache stale: %v", source, sourceID, err)
              return cached, nil
          }
          return nil, err
      }
      s.persistir(ctx, source, sourceID, tipo, raw, langs)
      return raw, nil
  }
  ```
- Novo `doCacheQualquer` (variante de `doCacheFresco` SEM checar TTL/langs —
  só existência de payload não-vazio):
  ```go
  // doCacheQualquer devolve o payload cacheado quando existe, IGNORANDO
  // frescor (TTL/langs) — usado como último recurso quando a busca ao vivo
  // não é possível (sem provider) ou falhou, para nunca perder dado já
  // salvo por indisponibilidade momentânea/permanente da fonte (ADR-0024).
  func (s *Servico) doCacheQualquer(ctx context.Context, source, sourceID string) (json.RawMessage, bool) { ... }
  ```
  (mesmo corpo de `doCacheFresco` até a checagem de `PayloadJson.Valid`,
  sem as checagens de `dentroDoTTL`/`idiomasContidos` — considerar
  extrair a leitura da linha para uma função compartilhada entre as duas,
  критério de implementação).

**`internal/server/external_detail.go`:**
- `provedorDetalhe`: parar de gatear em `s.providers.Get(source)` (busca ao
  vivo); resolver via `s.codecPara(source)` (a mesma resolução de
  `entities.go` — mover `codecPara` para um arquivo compartilhado se fizer
  sentido, ou duplicar se o acoplamento entre arquivos não valer a pena;
  decisão de implementação). 503 só quando a fonte é totalmente desconhecida
  do binário (nem em `codecs` nem em `providers`) — nesse caso `PayloadFresco`
  nem seria chamado com proveito.
- `getExternalDetail`: sem mudança de fluxo além de usar o provider
  resolvido por `codecPara` para o `prov.MapearDetalhe(raw)` (linha ~128) —
  `PayloadFresco` já decide sozinho fresh/stale/erro.

### Arquivos afetados
- `cmd/mymediadb/main.go`
- `internal/server/server.go`
- `internal/server/entities.go`
- `internal/server/external_detail.go`
- `internal/externaldetail/externaldetail.go`
- Testes: `internal/server/entities_test.go` (ou equivalente),
  `internal/externaldetail/externaldetail_test.go`,
  `internal/server/external_detail_test.go` — conferir nomes reais na
  implementação.

## 5. Critérios de aceite

- [x] Com `media_external` já cacheado para uma entity e **nenhum** provider
      de busca ao vivo registrado (nenhuma credencial), `GET /entities/{id}`
      continua devolvendo sinopse/elenco/tags/similares como antes da
      credencial sumir. (`TestEntityRicaComCodecRegistrySemBuscaAoVivo`,
      `internal/server/codec_registry_test.go`)
- [x] Mesmo cenário para `GET /external/{source}/{type}/{id}` com
      `external_detail` já cacheado e TTL expirado: devolve o payload stale
      em vez de 503/404, quando `providers` está vazio para aquela fonte.
      (`TestExternalDetailStaleSemBuscaAoVivo`,
      `internal/server/codec_registry_test.go`;
      `TestServeCacheQualquerSemProviderDeBusca`/
      `TestServeCacheStaleQuandoBuscaAoVivoFalha`,
      `internal/externaldetail/externaldetail_test.go`)
- [x] Com credencial presente (comportamento hoje), nenhuma regressão: busca
      ao vivo, import, re-enrich e detalhe externo fresco continuam iguais.
      (suíte completa `go test ./... -count=1` verde, sem alterar nenhum
      teste pré-existente)
- [x] `MYMEDIADB_FAKE_PROVIDER=1` (E2E) continua funcionando sem mudança
      (fallback `codecPara`→`providers` cobre o fake).
      (`internal/e2e/...` verde, `codecPara` cai para `providers` quando
      `codecs` está vazio — o harness fake não usa `WithCodecRegistry`)
- [x] Item nunca enriquecido (sem `media_external`/`external_detail` algum)
      continua sem campos ricos, sem erro — não é o mesmo caso do bug
      (nunca houve dado a perder). (comportamento preservado — `montarExterno`
      continua devolvendo `nil` cedo sem linhas de `media_external`; coberto
      pelos testes pré-existentes que criam entity sem provider algum)
- [x] `go build ./...`; `go vet ./...`; `go test ./... -count=1`; `make
      gate-local` limpo antes do commit final. (build/vet/test rodados pelo
      agente de implementação; `make gate-local` fica para o agente de
      validação/commit/push, conforme AGENTS.md)

## 6. Notas de implementação / decisões

- **Por que dois `Registry` e não uma flag por provider**: um provider já
  registrado com credencial real deveria continuar podendo interpretar
  payload — não faz sentido "desligar" a capacidade de parse quando a de
  busca está ligada. Dois registros independentes (`providers`=busca,
  `codecs`=parse, sempre populado) modelam isso sem introduzir um terceiro
  conceito (nenhuma mudança na interface `provider.Provider`).
- **Por que `codecs` sempre populado, mesmo sem nenhum uso real do binário**:
  os 3 tipos (tmdb/tvdb/fanarttv) são codecs conhecidos do binário — não
  segredos. Registrar sempre é o comportamento mais simples e determinístico
  (sem depender de "algum dia teve credencial" para decidir o que
  registrar); custo de memória de 3 structs vazios é desprezível.
- **`codecPara` com fallback para `providers`**: existe só para não quebrar
  o `MYMEDIADB_FAKE_PROVIDER` (que não passa por `provedoresConhecidos`).
  Não é gambiarra permanente — é a forma mais simples de manter os dois
  registros coerentes sem duplicar a fixture fake em `codecs` também.

## 7. Fora deste briefing / follow-ups

- Sinalizar "dado stale" explicitamente na API/UI (ADR-0024, item 4, deixado
  em aberto) — se o dono sentir falta na prática.
- Extender a mesma filosofia para outros caches futuros que venham a
  depender de provider (ex.: cache de busca `cacheExterno` — hoje já
  degrada bem por não ter esse problema, é TTL curto e não representa dado
  catalogado).
