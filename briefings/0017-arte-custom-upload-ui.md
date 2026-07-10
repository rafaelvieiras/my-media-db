# 0017 — Arte custom: upload (`/api/v1/entities/{id}/assets`) + UI

> A autoridade do dono sobre a **arte**: subir poster/fanart próprios (`origin=user`),
> escolher qual é a **preferida**, e a API passa a servir a escolha do dono por padrão
> — inclusive para clientes como o Jellyfin.

| | |
|---|---|
| **ID** | 0017 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [08-artwork-e-assets](../docs/08-artwork-e-assets.md), [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md) |
| **Status** | done |
| **Depende de** | [0015](0015-enriquecimento-import-proxy-imagem.md) |

---

## 1. Objetivo

O enriquecimento ([0015](0015-enriquecimento-import-proxy-imagem.md)) traz arte
`origin=external`; falta o outro lado da autoridade: o dono **sobe a sua** e ela
**vence** ([08](../docs/08-artwork-e-assets.md), cenário B/G). Arte custom e arte
puxada coexistem; a marcada como preferida é a que a API serve por padrão.

## 2. Jornada do usuário

1. No detalhe, o dono abre **Arte** e faz **upload** de um poster.
2. A nova arte entra na galeria ao lado da externa; o dono a marca **preferida**.
3. O grid e a API passam a servir a arte do dono; re-enriquecer **não** a remove.
4. O dono pode **remover** uma arte que subiu (a externa, não — ela é cache re-puxável).

## 3. A decisão de design que sustenta tudo

**A escolha do dono vence** ([08](../docs/08-artwork-e-assets.md)). Sem precisar de nova
tabela: a preferência é uma **flag em `meta_json`** (`{"preferred": true}`), **uma
preferida por `(entity, kind)`**. A **precedência de serviço** do pôster passa de "o
primeiro asset" para:

```
preferida (meta.preferred) → qualquer origin=user (mais recente) → origin=external → 404 (placeholder)
```

Ou seja: **subir arte já a faz vencer a externa** (o upload é a intenção); marcar
preferida só desempata entre várias do dono. A precedência é calculada **em Go** sobre
`ListarAssetsPorEntity` filtrado por `kind` — testável e sem SQL exótico. O binário do
dono mora no mesmo diretório confiável de assets do [0015](0015-enriquecimento-import-proxy-imagem.md)
(`{assetsDir}/{entityID}/…`), então a **guarda de path-traversal do [0010](0010-api-leitura-resolver-minimo.md)
já o cobre**.

## 4. Contrato técnico

### API (`/api/v1`)
- **POST `/entities/{id}/assets`** — upload **multipart/form-data**: campo `file` (o
  binário) + campo `kind` (`poster`|`fanart`, default `poster`). Valida content-type
  (`image/jpeg|png|webp`) e tamanho (teto como o [0015](0015-enriquecimento-import-proxy-imagem.md),
  20 MiB); grava o binário em disco (escrita atômica: tmp + rename) e cria o `asset`
  `origin='user'`. `201` com o asset criado. `404` entity inexistente; `415` tipo não
  suportado; `413` grande demais; `422` corpo malformado/sem arquivo.
- **GET `/entities/{id}/assets`** — **galeria**: lista JSON dos assets da entity
  (id, kind, origin, preferred, url do binário). `200` (`[]` se nenhum); `404` entity
  inexistente.
- **GET `/entities/{id}/assets/{assetId}/file`** — serve o **binário** de um asset
  específico (content-type correto + guarda de path-traversal), para as miniaturas da
  galeria. `404`/`403` como o pôster.
- **PUT `/entities/{id}/assets/{assetId}/prefer`** — marca o asset como **preferido**
  do seu `kind`: seta `meta.preferred=true` nele e **limpa** a flag dos outros do mesmo
  `(entity, kind)`. `200` com o asset. `404` asset/entity inexistente.
- **DELETE `/entities/{id}/assets/{assetId}`** — remove uma arte **`origin='user'`**
  (apaga a linha + o binário, best-effort). `409`/`422` se o asset é `origin='external'`
  (cache, não se apaga aqui — some no re-enrich). `200`; idempotente-ish (`404` se já
  não existe).
- **GET `/entities/{id}/assets/poster`** ([0010](0010-api-leitura-resolver-minimo.md)) —
  passa a aplicar a **nova precedência** (preferida `user` → `user` → `external` → 404),
  mantendo a guarda de path-traversal. `poster_url` dos cards/detalhe não muda de forma
  (segue apontando para este endpoint).

### Dados / modelo
- `asset(entity_id, kind, origin, path_or_uri, meta_json)` ([0007](0007-schema-nucleo-minimo.md));
  binário no layout de assets em disco ([0015](0015-enriquecimento-import-proxy-imagem.md)/[08](../docs/08-artwork-e-assets.md)).
- **Preferência = `meta_json.preferred` (bool)**, uma por `(entity, kind)`. Sem coluna
  nova, sem migração.
- **Queries:** reusa `CriarAsset`, `ListarAssetsPorEntity`, `ObterAsset`, `ApagarAsset`.
  Adiciona uma query sqlc (ASCII, colunas explícitas) para **atualizar só o `meta_json`**
  de um asset (ex.: `AtualizarAssetMeta(meta_json, id)`) — usada ao (des)marcar preferida.

### Backend (Go)
- **Upload:** parse `multipart` com `r.ParseMultipartForm(teto)`; valida mime (pelos
  magic bytes/`http.DetectContentType`, não só a extensão) e tamanho; escreve em
  `{assetsDir}/{entityID}/user-{assetId}.{ext}` de forma atômica (tmp no mesmo dir +
  `os.Rename`); registra o `asset` pelo pool de escrita. Diretório criado com `MkdirAll`.
- **Precedência de serviço:** um helper puro `escolherAssetServido(assets, kind)
  (asset, ok)` aplica preferida→user→external; o `getEntityPoster` passa a usá-lo (em
  vez de `ObterAssetPosterPorEntity`). Manter a guarda `caminhoSobRaizes`.
- **Preferir:** transação lógica — carrega os assets do `(entity, kind)`, seta
  `preferred=true` no alvo e remove a flag dos demais (merge no `meta_json` de cada,
  preservando outras chaves), grava pelo pool de escrita.
- **Não-destrutivo** ([ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md)): o
  cache externo do [0015](0015-enriquecimento-import-proxy-imagem.md) já casa só
  `origin='external'` (via `ObterAssetPorEntityKindOrigin`), então re-enriquecer nunca
  toca arte `origin='user'`. Cobrir com teste de regressão.

### API / tipos
- `openapi.yaml`: os endpoints acima + schemas `Asset`/`AssetList` (id, kind, origin,
  preferred, url). Regenerar tipos; `tsc -b` verde.

### Frontend (Vite/React)
- **Galeria no `EntityDetail`** (seção "Arte", junto do editor de override [0016](0016-editor-override-ui.md)):
  miniaturas dos assets (`GET /assets`), com selo **origin** (`sua` × `externa`) e
  **preferida**; botão **Upload** (input file, valida tipo no cliente antes de enviar,
  mostra erro 415/413 inline); ação **Preferir** (PUT) e **Remover** (DELETE, só nas do
  dono). Estados vazio/erro/carregando. Ao mudar a arte, invalida `['entity', id]`
  (o pôster do detalhe/grid reflete) e `['assets', id]`.

### Arquivos afetados
- Criar (backend): `internal/server/assets.go` (handlers upload/galeria/file/prefer/
  delete + precedência + validação), testes; query `AtualizarAssetMeta` em
  `asset.sql` (+ regen sqlc). Alterar `getEntityPoster` (precedência), `server.go`
  (rotas), `openapi.yaml` (+ tipos).
- Criar (frontend): componente de galeria + upload; `web/src/lib/api.ts`/`queries.ts`.
  Alterar `web/src/routes/entity-detail.tsx`.

## 5. Critérios de aceite

- [x] Upload cria `asset` `origin=user` com binário em disco; content-type (magic bytes)
  e tamanho validados (`415`/`413` nos limites).
- [x] Subir arte já a faz o pôster ser servido (vence a externa); marcar **preferida**
  desempata entre várias do dono; sem arte do dono, cai na `external` e depois no 404.
- [x] Re-enriquecer ([0015](0015-enriquecimento-import-proxy-imagem.md)) **não** remove
  arte `origin=user` (teste de regressão).
- [x] DELETE remove arte do dono (linha + binário); recusa apagar `origin=external`.
- [x] O serviço de imagem mantém a guarda de path-traversal do [0010](0010-api-leitura-resolver-minimo.md).
  UI: galeria mostra origem/preferida, faz upload e troca a preferida refletindo na
  hora. Catraca verde (Go + type-check web).

**Validação ao vivo (backend por curl + UI por Playwright, item Dune):** upload trocou
o pôster servido do JPEG externo (43 KB) para o PNG do dono automaticamente (precedência
user › external); `DELETE` de `external` recusado (`422`); upload de texto rejeitado por
magic bytes (`415`); `DELETE` de `user` voltou o pôster ao externo. Na UI: a seção
**Arte** listou a arte externa (selo "externa", só "Preferir"); enviar um PNG criou a
arte "sua" (com "Preferir" **e** "Remover"); "Remover" a apagou, voltando à externa.

## 6. Notas de implementação / decisões

- **A escolha do dono vence** ([08](../docs/08-artwork-e-assets.md)) guia a precedência
  — vale inclusive para o que o Jellyfin consome do endpoint de pôster.
- **Upload sem moderação** (servidor pessoal): validar só o técnico (mime/tamanho), não
  conteúdo. Detectar o mime pelos **magic bytes**, não confiar na extensão/no header do
  cliente.
- **Uma preferida por `kind`**: escolher `poster` não afeta a preferência de `fanart`.
- **Preferência em `meta_json`** (não coluna) evita migração e comporta futuros flags de
  asset; o custo é o merge no Go ao (des)marcar — trivial.

## 7. Fora deste briefing / follow-ups

- Thumbnails/derivadas e formatos ([08](../docs/08-artwork-e-assets.md)); mais tipos de
  asset por vertical; edição de imagem (crop/resize) no cliente; arte por idioma
  (fanart localizado) — follow-up do multi-idioma.
