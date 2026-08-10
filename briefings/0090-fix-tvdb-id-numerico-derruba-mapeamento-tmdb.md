# 0090 — Corrige `tvdb_id` numérico do TMDB derrubando o mapeamento inteiro

> O TMDB às vezes devolve `external_ids.tvdb_id` como número JSON em vez de
> string; o client Go só aceita string, o `Unmarshal` falha, e o erro derruba
> **todo** o mapeamento canônico daquela entity (título, sinopse, elenco,
> equipe, tagline, gêneros, nota) em silêncio — sem log, sem indício na UI
> além de "campo ausente". Investigado ao vivo com dado real de produção
> (`data/mymediadb.sqlite`, entity "Chapolin Colorado",
> `fd91e8c4-764f-4369-9e99-4a4fab9485a6`): `media_external(source='tmdb')`
> tem o payload completo e correto (overview, cast, tagline, tudo), mas
> `GET /entities/{id}` devolve `description: null` e todo o resto vazio — o
> mesmo bug de tipo faz `BuscarDetalhe` falhar ao vivo, gerando o 502
> "a fonte externa falhou ao entregar o detalhe" ao tentar Corrigir/Confirmar
> candidato/Baixar Metadados.

| | |
|---|---|
| **ID** | 0090 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | done |
| **Docs relacionadas** | [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md) |
| **Depende de** | 0020, 0022, 0087 |

---

## 1. Objetivo

`internal/provider/tmdb/tmdb_rico.go` declara
`externalIdsTMDB.TvdbID string` (`json:"tvdb_id"`). O TMDB é inconsistente
entre títulos/endpoints: para muitas séries esse campo vem como número JSON
(`212601`), não string. Como `TvdbID` é tipado `string` sem tolerância,
`json.Unmarshal` do detalhe inteiro falha com `UnmarshalTypeError` — e
`mapearEnvelope` (`tmdb.go:505-509`) aborta o mapeamento **inteiro** por
causa desse único subcampo secundário, mesmo com o resto do payload
perfeitamente válido. O erro sobe engolido e sem log em dois pontos:
`camadasDaFonte` (`internal/server/entities.go:1031-1034`, composição do
detalhe pra leitura) e, ao vivo, em qualquer chamada que force
`BuscarDetalhe` (Corrigir, Confirmar candidato, Baixar Metadados), onde vira
o 502 genérico "a fonte externa falhou ao entregar o detalhe" — indistinguível
de uma falha de rede real.

Efeito prático: qualquer filme ou série cujo `tvdb_id` do TMDB seja numérico
perde a camada `tmdb` inteira silenciosamente (título/sinopse ficam vazios,
"Catalogação Incompleta" nunca fecha), e o dono não consegue aplicar ações de
correção manual porque elas retornam o mesmo 502 — sem log nenhum apontando
a causa real.

## 2. Jornada do usuário

1. O dono cataloga uma série cujo `tvdb_id` (no TMDB) vem como número.
2. O item chega à fila de Revisão ou ao detalhe sem sinopse/elenco/tagline,
   mesmo com um candidato de match forte (score 100) disponível.
3. Ao clicar "Corrigir" ou "Confirmar candidato", recebe
   "a fonte externa falhou ao entregar o detalhe" — sem explicação do motivo
   real (não é falha de rede).
4. Depois deste fix: o `tvdb_id` numérico é aceito normalmente, o mapeamento
   completo (título/sinopse/elenco/tagline/gêneros/nota) é aplicado, e as
   ações de correção funcionam sem erro. Se qualquer outro erro de mapeamento
   ocorrer no futuro, fica registrado em log (não mais silencioso).

## 3. Escopo

**Dentro:**
- Tornar `externalIdsTMDB.TvdbID` tolerante a string OU número JSON (o TMDB
  não documenta consistência aqui — aceitar os dois formatos é o fix
  correto, não uma normalização especulativa: já reproduzimos o caso real).
- Logar (não mais engolir em silêncio) quando `camadasDaFonte`
  (`internal/server/entities.go`) descarta uma linha de `media_external` por
  erro de mapeamento — mesmo padrão `log.Printf` já usado em
  `midias_faltantes.go`/`retroativo.go`/`refresh_files.go` (best-effort,
  contexto + entity + erro).
- Teste de regressão reproduzindo o payload real (envelope com
  `external_ids.tvdb_id` numérico) contra `MapearDetalhe` e contra
  `TvdbIDDoDetalheSerie` (mesma struct, mesmo bug, ver 0087).
- Reprocessar em produção a entity real usada no diagnóstico (Chapolin
  Colorado) após o deploy, forçando um novo enrich, para confirmar o fix
  corrige o dado já persistido sem exigir nenhuma migração/backfill em massa
  (o payload já salvo em `media_external`/`external_detail` é reaproveitado
  como está — só o parsing muda).

**Fora (explicitamente):**
- Qualquer mudança de formato em `media_external`/`external_detail` — o
  payload gravado já está correto; só o parsing na leitura tinha o bug.
- Backfill em massa de outras entities eventualmente afetadas pelo mesmo bug
  — o enriquecimento retroativo (0085) já re-chama `Enriquecer`
  periodicamente para itens elegíveis, então se auto-corrigem com o tempo;
  reprocessamento manual em lote fica como follow-up se o dono quiser
  acelerar.
- Mudar o comportamento geral de "degradar a fonte sem derrubar a leitura"
  em `camadasDaFonte` — isso é intencional e correto (uma fonte não pode
  travar a entity inteira); só a VISIBILIDADE do motivo (log) e a
  TOLERÂNCIA de tipo estão sendo corrigidas aqui.

## 4. Contrato técnico

### Backend (Go)
- `internal/provider/tmdb/tmdb_rico.go`:
  - Novo tipo `idFlexivelTMDB string` com `UnmarshalJSON` próprio: aceita
    JSON string OU JSON number (via `json.Number`), normaliza para string.
  - `externalIdsTMDB.TvdbID` passa de `string` para `idFlexivelTMDB`.
  - `TvdbIDDoDetalheSerie` (linha ~356-369): ajustar o retorno para
    `string(d.ExternalIds.TvdbID)`.
- `internal/server/entities.go`, `camadasDaFonte` (linha ~1021-1038):
  adicionar `log.Printf` no `continue` de erro de `MapearDetalhe`, mesmo
  padrão de `midias_faltantes.go:88`. Import novo: `log`.

### Arquivos afetados
- `internal/provider/tmdb/tmdb_rico.go` (+ `tmdb_rico_test.go`).
- `internal/server/entities.go`.

## 5. Critérios de aceite

- [x] Um envelope TMDB com `external_ids.tvdb_id` como número JSON é mapeado
      com sucesso por `MapearDetalhe`, populando título/descrição/tagline/
      elenco normalmente (não só o `tvdb_id`).
- [x] `TvdbIDDoDetalheSerie` extrai corretamente o `tvdb_id` numérico como
      string, sem regressão no caso já coberto (string).
- [x] Um envelope com `tvdb_id` ausente ou string continua funcionando sem
      regressão (não quebra os testes já existentes de 0087).
- [x] Uma linha de `media_external` cujo mapeamento falha por outro motivo
      (payload realmente inválido) gera uma linha de log identificável
      (fonte + entity + erro), em vez de falhar em silêncio.
- [x] `go build ./... && go vet ./... && go test ./... -count=1` verde.
- [x] `make gate-local` passa.
- [x] Reprocessamento manual (via API, `force=true`) da entity real usada no
      diagnóstico confirma sinopse/elenco/tagline preenchidos após o fix
      estar em produção.

## 6. Notas de implementação / decisões

- **Por que aceitar os dois formatos em vez de só normalizar o dado na
  origem?** O bug está no parsing, não no dado — o payload já gravado em
  produção está correto (`tvdb_id: 212601`, JSON válido); o client Go que
  não contemplava essa variação real da API do TMDB.
- **Por que não abortar só o subcampo `tvdb_id` em vez de criar um tipo
  tolerante?** `json.Unmarshal` do Go não permite "pular" um subcampo malformado
  e continuar decodificando o resto da struct de forma built-in — o tipo
  tolerante com `UnmarshalJSON` próprio é a forma idiomática de aceitar as
  duas formas sem reescrever o unmarshal de `detalheTMDB` inteiro.
- **Por que logar em vez de só silenciar melhor?** O próprio silêncio foi o
  que escondeu esse bug por tempo indeterminado em produção — sem o log, o
  próximo caso (payload realmente corrompido, chave de API mudando de forma,
  etc.) vai levar o mesmo tempo pra ser diagnosticado.

## 7. Fora deste briefing / follow-ups

- Endpoint/script de reprocessamento em lote para acelerar a auto-correção
  de outras entities eventualmente afetadas pelo mesmo bug histórico — só se
  o dono perceber outros itens com o mesmo sintoma depois do fix.
- Auditoria mais ampla de outros campos do TMDB com risco de inconsistência
  string/número (o `tvdb_id` foi o caso encontrado e confirmado; não foi
  feita uma varredura exaustiva de todo o payload).
