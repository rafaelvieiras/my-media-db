# 0039 — Numbering real no card do episódio (SxxEyy, não posição ordinal)

> O card de episódio mostra o número REAL da ordenação (S02E05, absoluto) lido do
> `extension_data`, não a posição ordinal na lista — que quebra assim que falta um
> episódio ou a ordem não é sequencial.

| | |
|---|---|
| **ID** | 0039 |
| **Tipo** | fix/feature |
| **Status** | done |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (vertical séries) |
| **Docs relacionadas** | [0031](0031-series-fundacao-modelo-enriquecimento.md), [0034](0034-scanner-de-series-arquivo-episodio.md), [ADR-0021](../docs/adr/0021-modelo-series-tv.md) |
| **Depende de** | 0031 |

---

## 1. Objetivo

Hoje o número do episódio na UI vem da **posição ordinal** na lista de membros
(`CardNumerado` usa `indice+1`, ver `entity-members.tsx:113` e o comentário admitindo
o débito em `:55-57`). Isso está errado quando: falta um episódio na biblioteca
(catálogo tem todos, mas a numeração pula), a temporada não começa em 1, ou a ordem
não é 1..N contígua. O número correto (`season`, `number`, `absolute_number`) já é
gravado no import em `extension_data` (`core.tv@1`, `numbering:{ordem:{season,number}}`
+ `absolute_number`, ver `enrich/serie.go:295 extensaoEpisodio`), mas **não é exposto**
no DTO de card (`entityCardDTO`) nem selecionado nas queries de card. Este briefing
expõe o numbering e faz a UI exibir "S02E05" real (+ absoluto quando houver).

## 2. Jornada do usuário

1. Ao abrir uma **temporada**, cada episódio mostra seu número **real** — "E05",
   não a 5ª posição da lista. Se a biblioteca só tem o E01 e o E05, eles aparecem
   como E01 e E05 (não E01 e E02).
2. Episódios de anime / ordenação absoluta mostram também o número absoluto quando
   disponível (ex.: "E05 · #128").
3. Episódio ainda **não identificado** (draft, sem numbering) cai para o rótulo
   neutro ("Episódio" sem número), sem inventar posição.

## 3. Escopo

**Dentro:**
- Expor, **no card de episódio** do endpoint de membros (`GET /entities/{id}/members`),
  a numeração real: `season`, `number` e `absolute_number` (quando houver).
- O `number`/`season` são projetados na **ordenação da temporada-pai** que está sendo
  listada — a temporada carrega `ordering` + `season_number` no seu `extension_data`
  (`enrich/serie.go:282 extensaoTemporada`); o episódio carrega `numbering[ordering]`.
  Assim não depende de nenhuma "ordem global selecionada" (isso é o 0040). Fallback:
  se não houver entrada para a `ordering` da temporada, usa o `absolute_number`; se
  nem isso, o campo fica ausente.
- Frontend: `CardNumerado` passa a usar o numbering do card (não `indice+1`); exibe
  "E{number}" (2 dígitos) e, quando presente e distinto, o absoluto. Sem numbering →
  rótulo neutro.

**Fora (explicitamente):**
- **Seletor** de ordenação (trocar official/dvd/absolute na UI) — é o **0040**. Aqui
  a temporada é sempre listada na sua própria `ordering` (a default gravada no import).
- Reprojetar `sort_index` / reordenar episódios. A ordem da lista continua vindo do
  `sort_index` do backend (0031); só o **rótulo** do número muda.
- Numbering no card da **home/busca** (lá não há episódio de topo) — só no /members.

## 4. Contrato técnico

### Backend (Go)
- **DTO**: adicionar a `entityCardDTO` (`internal/server/entities.go:39`) um campo
  opcional `Numbering *episodeNumberingDTO` (`json:"numbering,omitempty"`), com
  `episodeNumberingDTO{ Season int json:"season"; Number int json:"number";
  AbsoluteNumber *int json:"absolute_number,omitempty" }`. Ausente (omitido) para
  todo tipo que não seja `episode` e para episódio sem numbering.
- **Anotação no server** (mesma fronteira de `Ownership`/`Ancestors`, 0034/0036 — NÃO
  na query SQL do card): em `montarMembros` (`entities.go:~285`), quando o pai é uma
  `season`, para cada filho `episode` ler o `extension_data_json` do episódio, extrair
  `core.tv@1.numbering` + `absolute_number`, e projetar na `ordering` da temporada-pai
  (obtida do `extension_data` da própria season que está sendo resolvida). Preencher o
  DTO. Degrada para `null`/ausente em qualquer falha (robusto, como as outras anotações).
  - Evitar N leituras caras: buscar o `extension_data_json` dos filhos já na leitura
    dos membros (estender a query de membros para retornar a coluna, ou uma leitura em
    lote por ids). Preferir o caminho que o sqlc infere sem perder tipo (lição do 0034:
    `UNION ALL`/`DISTINCT` quando `EXISTS/COUNT` viram `interface{}`). Se a leitura em
    lote complicar, uma leitura por episódio é aceitável para a escala atual (dezenas
    por temporada) — deixar `TODO` de otimização como no 0037.
- Reusar helpers de leitura de `extension_data` do namespace `core.tv@1` se já
  existirem (procurar em `internal/scanner/serie.go:334 extrairTVNS` / enrich); não
  duplicar o parser.

### Frontend (Vite/React)
- `web/src/lib/api.ts`: tipar `numbering` no `EntityCard` (`{ season: number; number:
  number; absolute_number?: number } | null`).
- `web/src/components/entity-members.tsx`: `CardNumerado` usa `card.numbering` em vez
  de `indice+1`. Rótulo: `E{number zero-padded 2}` (ex.: "E05"); se `absolute_number`
  existir e for diferente do `number`, anexar " · #{absolute}". Sem `numbering` →
  "Episódio" (neutro, sem número). Atualizar o comentário do débito (`:55-57`, `:110-112`).
- Manter o `indice` só como `key`/fallback visual se necessário, não como número.

### OpenAPI
- `openapi.yaml`: adicionar `numbering` (objeto `season`,`number`,`absolute_number?`)
  ao schema `EntityCard`. Rodar `gen:types` e garantir o arquivo gerado já formatado
  (Prettier roda da raiz no hook). `tsc -b` zero.

### Arquivos afetados (previsão)
- `internal/server/entities.go` (DTO + anotação em montarMembros), possivelmente
  `internal/db/queries/entity.sql` + `sqlc generate` (se estender a query de membros),
  `openapi.yaml` + tipos gerados, `web/src/lib/api.ts`,
  `web/src/components/entity-members.tsx`, testes (Go do server + `entity-members.test.tsx`).

## 5. Critérios de aceite

- [ ] `GET /entities/{seasonId}/members` traz `numbering{season,number,absolute_number?}`
      em cada card `episode`; ausente para não-episódios e episódios sem numbering.
- [ ] Card de episódio exibe o número REAL (E05), não a posição; biblioteca com E01+E05
      mostra E01 e E05 (não E01/E02).
- [ ] Absoluto aparece quando presente e distinto do número da ordem.
- [ ] Episódio draft (sem numbering) → rótulo neutro, sem número inventado.
- [ ] Ordem da lista inalterada (segue `sort_index`); catraca completa verde (Go cov,
      e2e, tsc 0, web cov, jscpd, gocyclo ≤10).
- [ ] Smoke real: Simpsons T1 mostra os episódios com E01, E02… reais casando o catálogo.

## 6. Notas de implementação / decisões

- **Projeção na ordenação da temporada-pai, não numa "ordem global":** cada temporada
  já tem sua `ordering` (a default do import). Isso mantém o 0039 puro e independente
  do seletor (0040), que vai introduzir a troca de ordem para a série inteira.
- **Anotação no server, não na query** (precedente Ownership/Ancestors): mantém a query
  de card enxuta e o resolver puro; a árvore de TV compõe no server.
- Número zero-padded a 2 dígitos por convenção (E05), unidade/absoluto acessíveis no
  texto (não só posição).

## 7. Fora deste briefing / follow-ups

- **0040** — seletor de ordenação (official/dvd/absolute) reprojetando a série inteira
  na leitura, com preferência persistida.
- Still de episódio (**0042**) e i18n de título de episódio (**0041**).
- Otimização por query agregada única se a leitura por-episódio pesar (perf).
