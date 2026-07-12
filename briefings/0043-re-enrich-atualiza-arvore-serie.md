# 0043 — Re-enrich atualiza a árvore de série (temporada + episódio)

> Re-importar uma série já no catálogo passa a **atualizar** os metadados das
> temporadas e episódios já existentes (numbering, still_url, título, sinopse, datas),
> não só criar os novos — trazendo a árvore inteira à paridade com um import fresco,
> sem destruir overrides do usuário.

| | |
|---|---|
| **ID** | 0043 |
| **Tipo** | fix/feature |
| **Status** | done |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override (vertical séries) |
| **Docs relacionadas** | [0031](0031-series-fundacao-modelo-enriquecimento.md), [0034](0034-scanner-de-series-arquivo-episodio.md), [0039](0039-numbering-real-no-card-do-episodio.md), [0041](0041-i18n-de-episodio-media-external.md), [0042](0042-still-de-episodio-lazy-proxy.md), [0016](0016-editor-override-ui.md) |
| **Depende de** | 0042 |

---

## 1. Objetivo

Hoje o re-import de uma série **reusa** as temporadas/episódios já existentes sem
reescrever seus metadados (find-or-create que retorna cedo). Consequência: campos novos
ou melhorados pelo provider — em especial o **`still_url`** (0042) e o **`numbering`**
(0039), que vivem no `extension_data` escrito só no create — **não aparecem** em
episódios já importados; só nos novos. Isso é visível: uma biblioteca real (Simpsons,
Looney Tunes) importada antes do 0042 nunca ganha still sem recriar. Este briefing faz o
re-import **atualizar** a árvore existente, alinhado ao intuito de gestor de arquivos/
metadados tipo tinyMediaManager (re-scrape atualiza) — **preservando os overrides do
dono**.

## 2. Jornada do usuário

1. O dono re-importa/re-escaneia uma série já no catálogo (via `POST /import` ou o
   scanner). As temporadas e episódios existentes passam a refletir os **dados atuais do
   provider**: still de episódio aparece, numbering corrige, título/sinopse atualizam.
2. As **edições do dono (override)** permanecem intactas — o refresh mexe só na **base**
   (camada arquivo/externo), e o override continua vencendo na leitura.
3. Rodar o import duas vezes seguidas não muda nada na segunda (idempotente); não
   re-baixa arte já presente.

## 3. Escopo

**Dentro:**
- **Episódio (reuse)**: no branch de reuse de `idDoEpisodio`/`acharOuCriarEpisodio`,
  **reescrever a base** com os mesmos valores que o create já computa: `title`
  (`tituloEpisodio`), `description` (overview), `release_date` (AirDate),
  `external_ids_json` (`idsDoEpisodio`) e **`extension_data_json`** (`extensaoEpisodio` =
  numbering + absolute_number + **still_url**). O `media_external` (i18n) já é reescrito
  no reuse (0041) — manter.
- **Temporada (reuse)**: no reuse de `acharOuCriarTemporada`, reescrever `title`
  (`tituloTemporada`), `release_date` (Year) e `extension_data_json` (`extensaoTemporada`
  = ordering + season_number + year).
- **Poster de temporada — só-se-ausente**: `cachearArte` da temporada hoje **re-baixa
  sempre** no reuse; passar a baixar **apenas quando o asset não existe** (checar antes),
  para idempotência barata e não martelar o CDN a cada re-import.
- **Idempotência + preservação de override**: as escritas tocam **só** os campos de base
  vindos da fonte. **NÃO** tocar `status`, `tags`, `original_title` nem a tabela
  `media_override` (o resolver aplica override ⟵ externo ⟵ arquivo; refrescar a base é
  seguro — o override continua vencendo).

**Fora (explicitamente):**
- **Nó série e filme** — já refrescam no reuse (`importarItem` re-enriquece +
  `AtualizarExtensionData` no nó série). Nada a fazer.
- **Re-baixar arte que MUDOU** (novo poster do provider) — hoje é só-se-ausente; troca de
  arte por hash/If-None-Match ou flag `force` é follow-up.
- **Podar** episódios/temporadas que o provider não tem mais (pruning) — perigoso,
  follow-up separado.
- **Endpoint dedicado de "refresh"** — o caminho é o `POST /import` e o scanner já
  existentes; sem rota nova. (O `POST /entities/{id}/enrich`/`Enriquecer` não caminha a
  árvore e fica fora.)

## 4. Contrato técnico

### Backend (Go)
- **Query nova** `internal/db/queries/entity.sql` (ASCII — sqlc v1.31 corrompe acento):
  `AtualizarEpisodioBase :exec` reescrevendo `title, description, release_date,
  external_ids_json, extension_data_json` (+ `updated_at`) `WHERE id = ?`. **NÃO** incluir
  `status`/`tags`/`original_title` (não clobbar o dono). Rodar `sqlc generate`.
  - Para a **temporada**, reusar as queries existentes (`AtualizarTitulo`,
    `AtualizarReleaseDate`, `AtualizarExtensionData`) — sem descrição, 3 chamadas — **ou**
    uma `AtualizarTemporadaBase :exec` (`title, release_date, extension_data_json`) por
    simetria/economia de chamadas. Escolha da implementação (não usar `AtualizarEntity`,
    que é update completo e clobbaria status/tags).
- **`internal/enrich/serie.go`**:
  - `idDoEpisodio`/`acharOuCriarEpisodio`: no reuse, após obter o id existente, chamar
    `AtualizarEpisodioBase` com os mesmos params do create. **Extrair um helper** de
    montagem dos params compartilhado entre create e update (não duplicar `tituloEpisodio`
    /`extensaoEpisodio`/`idsDoEpisodio`). Manter `persistirEpisodioExterno` (i18n) sempre.
  - `acharOuCriarTemporada`: no reuse, reescrever title/release_date/extension_data.
  - `importarTemporada`: envolver o `cachearArte` do poster em **só-se-ausente** (checar o
    asset existente — ex.: `ObterAssetPorEntityKindOrigin` ou equivalente — antes de
    baixar). Cuidado com gocyclo ≤10 (extrair helper se crescer).
- **Sem migration** (só query nova + lógica).

### Frontend
- **Nenhuma mudança.** É comportamento de import; a UI já lê still/numbering/i18n dos
  briefings anteriores. (Testes de front não mudam.)

### OpenAPI
- Sem mudança de contrato.

### Arquivos afetados (previsão)
- `internal/db/queries/entity.sql` (+ `sqlcgen` regenerado), `internal/enrich/serie.go`
  (branches de reuse + helper de params + poster só-se-ausente), testes Go
  (`internal/enrich/serie_test.go`, `serie_still_test.go`). Sem migration, sem front.

## 5. Critérios de aceite

- [ ] Re-importar uma série já existente **atualiza** os episódios existentes:
      `extension_data` ganha `still_url`/`numbering` novos, e `title`/`description`/
      `release_date`/`external_ids` refletem o provider atual.
- [ ] Temporadas existentes atualizam `title`/`release_date`/`extension_data`.
- [ ] **Override preservado**: um episódio/temporada com override do dono continua
      resolvendo pelo override após o re-import (a base muda, o override vence).
- [ ] `status` e `tags` das entities **não** são alterados pelo re-import.
- [ ] Poster de temporada **não** é re-baixado quando já presente (só-se-ausente);
      idempotente.
- [ ] Catraca completa verde (Go cov, e2e, tsc 0, web cov, jscpd, gocyclo ≤10).
- [ ] Smoke real: uma série importada ANTES do 0042 (Simpsons/Looney) — re-import via
      `POST /import` → episódios existentes passam a ter `still_url` e mostram o still na
      UII; rodar de novo não muda nada.

## 6. Notas de implementação / decisões

- **Refresh só da base, override intocado** (isolamento do resolver): reescrever
  `entity.title`/`extension_data_json` é seguro porque o resolver aplica override por
  cima; nunca tocar `media_override`, `status`, `tags`.
- **Helper de params create/update** evita divergência entre o que o create grava e o que
  o refresh reescreve (senão o refresh esqueceria um campo novo no futuro).
- **Poster só-se-ausente**: idempotência barata; refrescar arte que mudou é outra
  decisão (follow-up), para não re-baixar tudo a cada import.
- **Sem `AtualizarEntity`**: é update completo (status/tags/original_title) — clobbaria
  estado do dono. Queries cirúrgicas por campo de fonte.

## 7. Fora deste briefing / follow-ups

- **Refresh de arte que MUDOU** (hash/If-None-Match/flag `force`) — hoje só-se-ausente.
- **Pruning** de temporadas/episódios removidos no provider (com salvaguardas: não apagar
  o que tem arquivo/override).
- Aplicar o mesmo padrão de refresh a outras árvores (coleção) se necessário.
- Still via FFmpeg / thumbnail escalado no proxy (follow-ups do 0042).
