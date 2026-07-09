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
| **Status** | draft |
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

## 3. Escopo

**Dentro:**
- **`POST /api/v1/entities/{id}/assets`** — upload de imagem (`origin=user`), gravando
  binário em disco + `asset` (kind `poster`/`fanart`/…). **Upload sem aprovação** ([08](../docs/08-artwork-e-assets.md)).
- **Escolha de preferida** (`PUT .../assets/{id}/prefer` ou flag) — a API serve a
  preferida por padrão; sem escolha, cai na `external`, depois no placeholder.
- **Servir** a preferida no endpoint de poster do [0010](0010-api-leitura-resolver-minimo.md)
  (mantendo a guarda de path traversal).
- **Não-destrutivo** ([ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md)):
  re-enriquecer nunca apaga arte `origin=user`.
- **UI** de galeria/upload/preferência no detalhe. Testes de upload, preferência e
  precedência de serviço.

**Fora (explicitamente):**
- Geração de **thumbnails/derivadas** e formatos — decisão aberta da [08](../docs/08-artwork-e-assets.md), follow-up.
- Edição de imagem (crop/resize) no cliente.
- Tipos de asset além de `poster`/`fanart` (ampliar por vertical depois).

## 4. Contrato técnico

### API (`/api/v1`)
- **POST** multipart → `201` com o `asset` criado; valida content-type/tamanho.
- **PUT** de preferência → `200`. **GET** de poster ([0010](0010-api-leitura-resolver-minimo.md))
  passa a resolver: preferida `user` → `external` → 404 (placeholder na UI).

### Dados / modelo
- `asset(entity_id, kind, origin='user', path_or_uri, meta_json)` ([0007](0007-schema-nucleo-minimo.md));
  binário no layout de cache/assets em disco ([0015](0015-enriquecimento-import-proxy-imagem.md)/[08](../docs/08-artwork-e-assets.md)).
- Preferência: flag em `meta_json` ou coluna — decidir na implementação (uma preferida por `kind`).

### Backend (Go)
- Upload validado (mime/tamanho), escrita atômica em disco, registro do `asset` pelo
  pool de escrita; servir com content-type correto e **guarda de path traversal**.

### Frontend (Vite/React)
- Galeria no `EntityDetail`: miniaturas, botão de upload, marcar preferida, estados
  de erro (tipo/tamanho) e vazio.

### Arquivos afetados
- Criar: handlers de upload/preferência, serviço de storage de asset, componentes de
  galeria, testes.
- Alterar: endpoint de poster ([0010](0010-api-leitura-resolver-minimo.md)) para a precedência; `openapi.yaml` (+ regen tipos); `EntityDetail`.

## 5. Critérios de aceite

- [ ] Upload cria `asset` `origin=user` com binário em disco; content-type/tamanho validados.
- [ ] Marcar preferida faz a API (e o grid) servirem a arte do dono; sem preferida, cai na `external` e depois no placeholder.
- [ ] Re-enriquecer ([0015](0015-enriquecimento-import-proxy-imagem.md)) **não** remove arte `origin=user`.
- [ ] O serviço de imagem mantém a guarda de path traversal do [0010](0010-api-leitura-resolver-minimo.md). Catraca verde.

## 6. Notas de implementação / decisões

- **A escolha do dono vence** ([08](../docs/08-artwork-e-assets.md)) é a regra que
  guia a precedência de serviço — vale inclusive para o que o Jellyfin consome.
- **Upload sem moderação** (é servidor pessoal); validar só o técnico (mime/tamanho),
  não conteúdo.
- **Uma preferida por `kind`**: escolher `poster` não afeta a preferência de `fanart`.

## 7. Fora deste briefing / follow-ups

- Thumbnails/derivadas e formatos; mais tipos de asset por vertical; edição de
  imagem no cliente.
