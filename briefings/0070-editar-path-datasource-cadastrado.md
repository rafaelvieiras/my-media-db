# 0070 — Editar path de um datasource já cadastrado

> Permite trocar o caminho de uma biblioteca cadastrada (ex.: o disco/mount
> mudou de lugar) sem perder o vínculo do catálogo já construído.

| | |
|---|---|
| **ID** | 0070 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Status** | done |
| **Docs relacionadas** | [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md), [08-artwork-e-assets](../docs/08-artwork-e-assets.md) |
| **Depende de** | 0008, 0062, 0064 |

---

## 1. Objetivo

Hoje `PATCH /api/v1/library/{id}` (0064) só edita `watch_enabled`; não existe
NENHUMA forma de corrigir o `path` de uma fonte já cadastrada. Isso obriga o
dono a apagar e recriar o datasource sempre que o disco/mount muda de lugar
(ex.: reorganizou o NAS, trocou o ponto de montagem do rclone, migrou de HD) —
e apagar HOJE remove os `media_file` da fonte (embora preserve as `entity`),
jogando fora casamento com episódio/filme, `last_seen`, `content_hash` etc., e
forçando um re-scan + re-match do zero.

Este briefing fecha esse gap: o dono aponta o MESMO datasource para o novo
caminho (o conteúdo lá dentro é o mesmo — só mudou de onde é montado) e o
sistema **recasca** (rename) todos os caminhos absolutos derivados do path
antigo, preservando `media_file`, matches, artes locais e `.nfo` intactos.

## 2. Jornada do usuário

1. O disco onde `/mnt/midia/filmes` estava montado passa a aparecer em
   `/mnt/midia2/filmes` (mesmo conteúdo, outro ponto de montagem).
2. Na tela de Bibliotecas, o dono clica em "Editar caminho" no datasource
   afetado (hoje mostrado como `offline`, pois `/mnt/midia/filmes` não existe
   mais).
3. Informa o novo caminho; o sistema valida que existe, é diretório e legível
   — mesma validação do cadastro (0008).
4. Ao confirmar: a fonte passa a apontar para o novo path, volta a `online`, e
   TODOS os `media_file` (+ artes locais + `.nfo`) daquela fonte são
   recascados para o novo prefixo — sem re-scan, sem perder casamento.
5. Se o novo caminho colidir com outro datasource já cadastrado, ou não for
   válido (não existe / não é diretório / sem permissão), a mensagem de erro
   aparece inline no formulário, igual ao cadastro.

## 3. Escopo

**Dentro:**
- `PATCH /api/v1/library/{id}` passa a aceitar também `path` (string,
  opcional) no corpo — continua PATCH parcial: `path` e `watch_enabled` podem
  vir juntos ou cada um sozinho.
- Validação do novo `path`: mesmas regras de `Criar` (normalizar
  absoluto+clean, existir, ser diretório, ser legível) e mesma checagem de
  duplicata contra QUALQUER OUTRA fonte (não contra si mesma).
- Recasco em cascata, na MESMA transação da atualização do `library_source`:
  - `media_file.path` de todo arquivo com `library_source_id = {id}`;
  - `asset.local_path` e `entity.nfo_path` das entities alcançadas pelos
    `media_file` daquela fonte (via `media_file_entity`), SE E SOMENTE SE o
    valor gravado começa exatamente com o `path` antigo (prefixo literal) —
    ver §6 sobre o caso de entity com arquivos em mais de uma fonte.
- Regra de recasco é troca de PREFIXO em Go (não `SUBSTR` no SQL — evita
  divergência de indexação por caractere multibyte entre Go, que mede bytes, e
  SQLite, que mede codepoints, nos `.sql` em pt-BR com acento no path):
  `novo = novoPrefixo + strings.TrimPrefix(antigo, prefixoAntigo)`.
- Se o novo `path` normalizado for IGUAL ao atual, é no-op (200 com a fonte
  como está, sem tocar em nada — evita transação e stat desnecessários).
- Front-end: botão "Editar caminho" (ícone lápis) na `DatasourceRow`
  (`datasource-list.tsx`), abre um campo inline reaproveitando o padrão do
  `DatasourceForm` (Input + Salvar/Cancelar); erro inline via `ApiError`.

**Fora (explicitamente):**
- Editar `type`/`content_type` de uma fonte já cadastrada (não pedido; e
  mudar `content_type` teria efeito em cascata sobre matching de série, fora
  de escopo aqui).
- Mover fisicamente arquivos no disco — este briefing só REAPONTA o
  datasource para onde o conteúdo JÁ está; não copia nem move bytes.
- Fundir duas fontes em uma.
- Entity com `media_file` em MAIS de uma fonte diferente tendo seu
  `asset.local_path`/`entity.nfo_path` recascado corretamente quando o prefixo
  não bate 1:1 (caso raro — ver §6, fica sem tocar, não quebra pior do que já
  estava).

## 4. Contrato técnico

### Dados / modelo

Nenhuma migration nova — todas as colunas envolvidas (`library_source.path`,
`media_file.path`, `media_file.library_source_id`, `asset.local_path`,
`entity.nfo_path`) já existem.

Novas queries sqlc em `internal/db/queries/`:
- `library_source.sql`: `AtualizarLibrarySourcePath :one` — `UPDATE
  library_source SET path = ?, updated_at = strftime(...) WHERE id = ?
  RETURNING …`.
- `media_file.sql`: `ListarMediaFilePathsPorFonte :many` — `SELECT id, path
  FROM media_file WHERE library_source_id = ?` (só as duas colunas — o rename
  não precisa do resto da linha).
- `media_file.sql`: `AtualizarMediaFilePath :exec` — `UPDATE media_file SET
  path = ?, updated_at = strftime(...) WHERE id = ?`.
- `entity.sql` (ou novo arquivo `asset.sql` se mais claro): query para listar
  `(asset.id, asset.local_path)` e `(entity.id, entity.nfo_path)` das
  entities alcançadas por `media_file_entity` a partir de uma lista de
  `media_file.id` — ou, mais simples, `SELECT DISTINCT entity_id FROM
  media_file_entity WHERE media_file_id IN (...)` seguido de leituras já
  existentes (`ObterEntity`) — decisão de implementação, não de contrato.
- `asset.sql`: `AtualizarAssetLocalPath :exec`.
- `entity.sql`: `AtualizarEntityNfoPath :exec` (ou reaproveitar update
  existente, se já houver um genérico o bastante).

### API

`PATCH /api/v1/library/{id}` (já existe, briefing 0064) — `PatchSource` ganha
campo novo:

```yaml
PatchSource:
  properties:
    watch_enabled: { ... }   # já existe
    path:
      type: string
      description: >
        Reaponta o datasource para um novo caminho (ex.: o disco/mount mudou
        de lugar). Valida como no cadastro (existe, é diretório, legível,
        sem duplicar outra fonte). Recasca em cascata media_file.path,
        asset.local_path e entity.nfo_path desta fonte para o novo prefixo —
        NÃO dispara scan nem move bytes no disco.
```

Erros novos possíveis na resposta de `patchLibrary` (reaproveita
`mapLibraryError`, já mapeia todos):
- `400 invalid_body` — `path` veio como string vazia.
- `422 invalid_path` / `not_a_directory` / `path_unreadable` — mesmas de
  `Criar`.
- `409 duplicate_path` — colide com outra fonte.
- `404 not_found` — id inexistente.

### Backend (Go)

- `internal/library/library.go`: novo método `AtualizarPath(ctx, id,
  rawNovoPath string) (Source, error)`:
  1. Obter a fonte atual (404 se não achar).
  2. `rawNovoPath == ""` → `ErrPathVazio`.
  3. Normalizar+validar o novo path (reaproveita `normalizarPath`/
     `validarPath`).
  4. Se igual ao path atual → devolve a fonte como está (no-op).
  5. Checar duplicata via `ObterLibrarySourcePorPath`, ignorando a própria
     fonte (compara o id encontrado com `id`).
  6. Transação de escrita (mesmo padrão de `Apagar`):
     a. `AtualizarLibrarySourcePath`.
     b. `ListarMediaFilePathsPorFonte(id)` → para cada linha, computa o novo
        path via `strings.TrimPrefix` e `AtualizarMediaFilePath`.
     c. Descobre as entities alcançadas por esses `media_file` (via
        `media_file_entity`) e, para cada uma, recasca `asset.local_path`
        (todas as linhas de `asset` daquela entity cujo `local_path` começa
        com o path antigo) e `entity.nfo_path` (idem).
     d. Commit — tudo ou nada, igual `Apagar`.
- `internal/server/library.go`: `patchLibrary` passa a interpretar
  `req.Path *string`; se não-nil, chama `s.library.AtualizarPath` (pode
  compor com `watch_enabled` no mesmo request — aplicar path primeiro,
  watch depois, mesma resposta final).

### Frontend (Vite/React)

- `web/src/lib/api.ts` / `api-types.ts`: regenerar tipos (`npm run
  gen:types`) a partir do `openapi.yaml` atualizado; `patchLibrary` já aceita
  um corpo parcial tipado — só precisa o novo campo `path` no tipo.
- `web/src/components/datasource-list.tsx`: `DatasourceRow` ganha um botão
  "Editar caminho" (ícone `Pencil` do lucide-react) que alterna um pequeno
  formulário inline (Input com o path atual pré-preenchido + Salvar/Cancelar);
  ao salvar, `patchMutation.mutate({ path })`; erro da `ApiError` aparece
  inline, igual ao `DatasourceForm`. Sucesso invalida `['library']`.

### Arquivos afetados

- `internal/db/queries/library_source.sql`, `media_file.sql`, `entity.sql`
  (ou `asset.sql` novo) — novas queries + regenerar `internal/db/sqlcgen/*`.
- `internal/library/library.go` (+ `library_test.go`).
- `internal/server/library.go` (+ `library_test.go`).
- `openapi.yaml` (+ `web/src/lib/api-types.ts` gerado).
- `web/src/components/datasource-list.tsx` (+ `.test.tsx`).

## 5. Critérios de aceite

- [x] `PATCH /api/v1/library/{id}` com `{"path": "/novo/caminho"}` válido
      devolve 200 com a fonte atualizada (`path` novo, `state: online`).
- [x] Os `media_file` da fonte passam a ter o prefixo novo em `path`,
      preservando o restante (subpasta/nome de arquivo) — um `GET` no
      catálogo continua resolvendo o mesmo `media_file` (mesmo `id`,
      `content_hash`, matches) sem re-scan.
- [x] `asset.local_path` e `entity.nfo_path` das entities atingidas também
      têm o prefixo trocado, quando aplicável.
- [x] Path novo igual ao atual → 200 no-op, nada muda no banco.
- [x] Path novo colidindo com outro datasource → 409 `duplicate_path`.
- [x] Path novo inexistente/não-diretório/ilegível → 422 com o código
      correspondente (mesmos de `Criar`).
- [x] Path vazio (`""`) → 400 `invalid_body`.
- [x] Id inexistente → 404 `not_found`.
- [x] Compor `path` + `watch_enabled` no mesmo PATCH aplica os dois.
- [x] UI: botão "Editar caminho" funciona, mostra erro inline, invalida a
      lista ao salvar com sucesso.
- [x] `go test ./internal/library/... ./internal/server/... -count=1` e
      `make gate-local` passam.

## 6. Notas de implementação / decisões

- **Por que recascar em vez de só trocar o `path` da fonte?** Se só o
  `library_source.path` mudasse, o próximo scan incremental (0064) NÃO
  encontraria os `media_file` existentes pelo path antigo (que não existe
  mais) e criaria registros NOVOS para os mesmos arquivos físicos — perdendo
  matching, `content_hash`, override e o vínculo com `entity` já feito. O
  recasco em massa é o que torna a operação segura.
- **Por que Go e não `SUBSTR`/`REPLACE` no SQL?** `REPLACE(path, old, new)`
  trocaria QUALQUER ocorrência da substring em qualquer posição (arriscado se
  o path antigo aparecer duas vezes por coincidência). `SUBSTR` por offset
  numérico exigiria casar o `length()` do SQLite (que conta codepoints
  Unicode) com o `len()` do Go (que conta bytes) — divergem em paths com
  acento, e a memória do projeto já registra esse tipo de pegadinha de
  encoding em `.sql`. Fazer o `TrimPrefix` em Go e gravar o valor final
  pronto elimina os dois riscos.
- **Entity com arquivos em mais de uma fonte:** raro (ex.: uma edição
  importada de outro datasource), mas possível. Neste caso o
  `asset.local_path`/`entity.nfo_path` só é recascado se o valor gravado
  começa com o prefixo antigo EXATO — se a arte/NFO foi materializada com
  base no OUTRO datasource (prefixo diferente), fica como está. Não piora o
  estado atual (já não tinha garantia nenhuma nesse cenário) — registrar como
  limitação conhecida, não bug.
- Reaproveitar tudo que já existe: `normalizarPath`, `validarPath`,
  `ObterLibrarySourcePorPath`, o padrão de transação de `Apagar`, e o mesmo
  shape de erro (`mapLibraryError`) — nenhum erro de domínio novo.

## 7. Fora deste briefing / follow-ups

- Um botão "verificar/recascar" independente do PATCH, para o dono rodar o
  mesmo recasco manualmente se desconfiar de inconsistência (ex.: depois de
  uma correção manual no disco) — não pedido agora.
- Fundir duas fontes (útil quando o dono cadastrou sem querer duas vezes o
  mesmo conteúdo em paths diferentes) — mencionado só para registro, não é
  este briefing.
