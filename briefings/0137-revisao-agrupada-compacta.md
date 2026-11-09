# 0137 — Revisão agrupada e compacta (álbum, série, pasta)

> A fila de revisão deixa de ser uma parede de 200 cards iguais: agrupa pelo
> contexto real do arquivo (álbum da faixa, série do episódio, pasta do filme),
> abre fechada com contagem e só busca candidato quando o dono pede.

| | |
|---|---|
| **ID** | 0137 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | done |
| **Docs relacionadas** | [05-api](../docs/05-api.md), [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md) |
| **Depende de** | 0018, 0050, 0060, 0135, 0136 |

---

## 1. Objetivo

Medido no catálogo real (08/nov/2026): `GET /api/v1/review` devolve **206 itens**
— 185 `faixa`, 19 `episode`, 2 `movie`. A tela renderiza **um card por item**,
cada card com pôster 80px, badge, bloco de prévia e três botões, e dispara **uma
requisição `GET /review/{id}/match-preview` por card ao montar** (N+1). Resultado
relatado pelo dono: "centenas de itens, nada é agrupado, o visual é confuso e
gigante".

Além do volume, a lista plana perde a informação mais útil para decidir: as 185
faixas em rascunho são só **43 álbuns**, e a causa do rascunho é quase sempre a
mesma dentro do grupo (álbum com sufixo de edição, compilação, sem tag). Resolver
"o álbum X" é uma decisão só, não 26 decisões.

## 2. Jornada do usuário

1. O dono abre **Revisão**. Vê ~50 seções fechadas (não 206 cards), na ordem:
   pastas de filme, séries, álbuns; cada cabeçalho com rótulo legível
   ("Duran Duran — Notorious (Deluxe Edition)"), contagem ("15 faixas") e o
   motivo dominante ("não identificado" / "baixa confiança (69%)").
2. Abre o grupo do álbum que quer resolver: aparecem **linhas compactas**
   (miniatura + título + tipo + motivo), sem bloco de prévia carregado.
3. Clica em **Ver candidato** numa linha: aí sim busca a prévia (`match-preview`)
   e mostra pôster/ano/score + os botões de confirmar/corrigir.
4. Pode pedir **Auto-sincronizar este grupo**: `POST /review/auto-match` com os
   ids do grupo, e o resumo volta no cabeçalho.
5. O botão global "Auto-sincronizar elegíveis" continua existindo no topo.

## 3. Escopo

**Dentro:**
- Backend: cada item de `GET /api/v1/review` passa a trazer um campo **opcional**
  `review_group` (`{kind, key, label, size}`), anotado no handler a partir do
  `media_file.path` da entity (mesma abordagem "anotado pós-listagem" de
  `ownership`/`numbering` no card, sem mexer no shape obrigatório):
  - `faixa` → `kind="album"`, chave = diretório do arquivo sem o segmento de
    disco (`Disc 1`, `CD 2`, `Disk 1`), rótulo = `<pasta do artista> — <pasta do
    álbum>` relativos à raiz da `library_source`;
  - `episode` → `kind="serie"`, chave = diretório do arquivo sem o segmento de
    temporada (`Season 3`, `Temporada 3`, `S03`), rótulo = nome da pasta da série;
  - `movie` (e demais tipos com arquivo) → `kind="pasta"`, chave = diretório do
    arquivo, rótulo = nome da pasta;
  - entity sem `media_file` → sem `review_group` (o front cai numa seção
    "Sem arquivo").
- Frontend (`web/src/routes/review.tsx`): seções colapsáveis por grupo, linhas
  compactas, prévia **lazy** por linha, ação de auto-match por grupo. Estado de
  grupo aberto em memória da tela (`useState`), sem persistência.
- `openapi.yaml`: campo `review_group` (nullable) em `EntityCard`, com a mesma
  nota de "opcional/anotado" usada por `ownership`.
- Testes: handler Go (agrupamento por tipo/path, sem `media_file`, path de disco)
  e a tela (agrupa, colapsa, não faz N+1, expande prévia sob demanda).

**Fora (explicitamente):**
- Mudar o predicado da fila (o que entra em revisão continua sendo o do 0018).
- Ações em lote novas no backend (o `POST /review/auto-match` com `ids` do 0050
  já cobre o "auto-sincronizar grupo"; nada de confirmar/corrigir em lote aqui).
- Paginação/virtualização da lista (com grupos o volume de DOM cai; se voltar a
  doer, é outro briefing).
- Persistir o estado aberto/fechado entre sessões.

## 4. Contrato técnico

### API
- `GET /api/v1/review` — resposta continua `EntityCard[]`; item ganha
  `review_group`:
  ```yaml
  review_group:
    description: >
      Grupo de revisão anotado no server (0137): a mesma causa/contexto do
      arquivo. OPCIONAL — presente só em GET /review.
    oneOf:
      - $ref: "#/components/schemas/ReviewGroup"
      - type: "null"
  ReviewGroup:
    type: object
    required: [kind, key, label]
    properties:
      kind: { type: string, enum: [album, serie, pasta, sem_arquivo] }
      key: { type: string }
      label: { type: string }
      size: { type: integer, format: int64 }
  ```
- Nenhuma rota nova. `POST /api/v1/review/auto-match` (0050) inalterado — o front
  passa `ids` do grupo.

### Backend (Go)
- `internal/db/queries/review.sql`: `ListarRevisao` ganha a coluna
  `path` (subquery `(SELECT m.path FROM media_file m WHERE m.entity_id = e.id
  ORDER BY m.state='present' DESC LIMIT 1)`) — ASCII, sqlc v1.31.
- `internal/server/review.go`: `agruparRevisao(rows) map[string]reviewGroupDTO`
  (função pura sobre path + tipo + raiz da fonte) e anotação no `entityCardDTO`.
  A raiz da fonte vem de `library_source.path` das entities (uma query por lista,
  não por item).
- `internal/server/grupo_revisao.go` (novo, se o handler crescer): parsing de
  segmento de disco/temporada e montagem do rótulo.

### Frontend (Vite/React)
- `web/src/routes/review.tsx`: `ReviewScreen` agrupa o array por
  `review_group.key` (fallback `sem arquivo`), ordena por `kind` (pasta, serie,
  album, sem_arquivo) e por tamanho desc; `ReviewGroups` → `ReviewGroupSection`
  (header `<button aria-expanded>`) → `ReviewRow` (compacta) → `ReviewRowDetalhe`
  (prévia lazy + ações).
- `web/src/lib/api.ts`: tipo `ReviewGroup` gerado do OpenAPI; `EntityCard` já vem
  do schema.

### Arquivos afetados
- `openapi.yaml`, `internal/db/queries/review.sql`, `internal/db/sqlcgen/*`
  (gerado), `internal/server/review.go`, `internal/server/review_test.go`.
- `web/src/routes/review.tsx`, `review.test.tsx`, `web/src/lib/api-types.ts`
  (gerado).

## 5. Critérios de aceite

- [ ] `GET /review` devolve `review_group` para itens com arquivo e `null`/ausente
      para itens sem arquivo.
- [ ] Faixas do mesmo álbum (mesmo diretório, ignorando `Disc N`) caem no mesmo
      grupo; episódios da mesma série idem; filme fica no grupo da própria pasta.
- [ ] A tela mostra N seções fechadas com contagem, não N cards.
- [ ] Nenhuma requisição `match-preview` é disparada antes de expandir uma linha
      (teste com MSW contando chamadas).
- [ ] "Auto-sincronizar este grupo" envia `ids` do grupo e atualiza a fila.
- [ ] `tsc -b`, `npm run lint`, `vitest`, `go test ./... -count=1` verdes.

## 6. Notas de implementação / decisões

- Agrupar por **path** e não por tag: 21 das 185 faixas não têm tag nenhuma, e o
  path é o que o dono reconhece na pasta real (fonte de verdade local-first).
- O rótulo é o caminho relativo à raiz da fonte, com 1–2 segmentos: é o que
  diferencia duas edições do mesmo álbum em pastas distintas.
- `size` no `review_group` evita o front contar; o backend já tem as linhas.

## 7. Fora deste briefing / follow-ups

- Ação "resolver grupo inteiro" (confirmar o mesmo candidato para todas as
  faixas do álbum) — precisa de decisão própria sobre risco de erro em bloco.
- Virtualização da lista, se o volume voltar a crescer.
