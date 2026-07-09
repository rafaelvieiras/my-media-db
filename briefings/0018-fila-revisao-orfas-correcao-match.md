# 0018 — Fila de revisão + órfãs + correção de match

> Fecha o loop de curadoria que a Fase 2 só **sinalizou**: o badge de baixa confiança
> e os `draft` não-identificados ganham **ação** — confirmar, corrigir (re-identificar)
> ou purgar — e as **órfãs** (arquivo sumiu, zero investimento do dono) entram numa
> fila de limpeza que nunca apaga sozinha.

| | |
|---|---|
| **ID** | 0018 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 3 — Enriquecimento + override |
| **Docs relacionadas** | [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md), [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [ADR-0008](../docs/adr/0008-identidade-e-matching-kodi-jellyfin.md), [ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md) |
| **Status** | draft |
| **Depende de** | [0013](0013-resolver-tres-camadas.md), [0016](0016-editor-override-ui.md) |

---

## 1. Objetivo

O scanner ([0009](0009-scanner-fswalk-ffprobe-matching.md)) **nunca trava a
biblioteca**: casa o melhor palpite e **sinaliza** o incerto ([06](../docs/06-biblioteca-e-scan.md)).
A Fase 2 mostrou o badge, mas não deu o que fazer com ele. Este briefing entrega a
**curadoria**: confirmar/corrigir matches de baixa confiança, tratar drafts
não-identificados e gerenciar **órfãs** — sempre não-destrutivo, com **purga** como
única ação destrutiva e sempre explícita.

## 2. Jornada do usuário

1. O dono abre **Revisão**: itens casados com baixa confiança (*"mapeado com 60% —
   remover?"*) e `draft` não-identificados.
2. Num item, **confirma** (remove o alerta), **corrige** (re-associa ao item certo)
   ou resolve na origem (adiciona o id no nome e re-escaneia).
3. Abre **Órfãs**: entidades auto-criadas cujo arquivo sumiu e sem investimento do
   dono. Pode **purgar** em lote (explícito) ou ignorar; nada some sozinho.

## 3. Escopo

**Dentro:**
- **Fila de revisão**: listar itens com `match_confidence` baixo (limiar
  `LIMIAR_BAIXA_CONFIANCA`, do [0011](0011-frontend-grid-lista-detalhe.md)) e `draft`
  não-identificados ([ADR-0008](../docs/adr/0008-identidade-e-matching-kodi-jellyfin.md)).
- **Confirmar match** — aceita o palpite (limpa o alerta); a correção do dono é
  **autoritativa** (entra pelo mecanismo de override do resolver, [06](../docs/06-biblioteca-e-scan.md)).
- **Corrigir match** — re-associa o `media_file` à entity certa / grava o id externo
  em `external_ids` (identidade vence, MD4); dispara re-resolução.
- **Fila de órfãs** (MD5/[ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md)):
  entity auto-criada, arquivo `missing`, **sem** override/asset custom/coleção manual,
  `status` ainda `cataloged`.
- **Purga** explícita (individual/lote): remove entity + `media_external`/`media_override`/
  assets (FKs CASCADE do [0012](0012-schema-fase3-camadas-externo-override.md)/[0007](0007-schema-nucleo-minimo.md)).
- **UI** de revisão e de órfãs; ligar o badge do [0011](0011-frontend-grid-lista-detalhe.md) à fila.
- Testes: classificação da fila; confirmar/corrigir; órfã só quando sem investimento;
  purga remove derivados; nada apaga sem ação explícita.

**Fora (explicitamente):**
- **Marcação de `missing`/offline** em si — já é do scanner ([0009](0009-scanner-fswalk-ffprobe-matching.md)/[ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md)); aqui só se **consome** o estado.
- Correção de **créditos** (MD6) — briefing próprio.
- Merge/split de entidades além do re-associar simples.

## 4. Contrato técnico

### API (`/api/v1`)
- **GET** fila de revisão (baixa confiança + drafts) e **GET** fila de órfãs.
- **POST** confirmar match; **POST/PUT** corrigir match (re-associar / gravar
  `external_ids`); **POST** purgar (individual/lote). Envelope de erro padrão.

### Dados / modelo
- Sem tabela nova: a fila é **query** (`match_confidence` < limiar; `status='draft'`;
  órfã = derivada por MD5). Correção grava `external_ids`/re-associa `media_file`;
  purga usa CASCADE. Definir "investimento do dono" como predicado (sem override, sem
  asset `origin=user`, sem coleção manual).

### Backend (Go)
- Queries de classificação; operação de re-associação (escrita curta, pool de escrita);
  purga transacional. Reusa o resolver ([0013](0013-resolver-tres-camadas.md)) para
  devolver o item re-resolvido.

### Frontend (Vite/React)
- Telas **Revisão** e **Órfãs**; ações confirmar/corrigir/purgar com confirmação
  explícita na purga; o badge de revisão vira link para a fila.

### Arquivos afetados
- Criar: queries de fila, handlers (revisão/órfãs/confirmar/corrigir/purgar), telas, testes.
- Alterar: `openapi.yaml` (+ regen tipos); badge/rotas do [0011](0011-frontend-grid-lista-detalhe.md).

## 5. Critérios de aceite

- [ ] A fila de revisão lista baixa confiança + drafts; **confirmar** limpa o alerta; **corrigir** re-associa/grava `external_ids` e re-resolve.
- [ ] A fila de órfãs lista só entidades auto-criadas, `missing` e **sem investimento do dono**; um item com override/asset custom **nunca** aparece como órfã.
- [ ] **Purga** é sempre explícita e remove os derivados (external/override/assets via CASCADE); nada é apagado como efeito colateral.
- [ ] A correção do dono é autoritativa (identidade vence, MD4) e reflete no resolver. Catraca verde.

## 6. Notas de implementação / decisões

- **Não-destrutivo é a regra** ([ADR-0012](../docs/adr/0012-ciclo-de-vida-nao-destrutivo.md)):
  a única porta destrutiva é a **purga**, e ela é sempre do dono, nunca do sistema.
- **Órfã ≠ missing**: `missing` é fato de arquivo; órfã é o predicado "auto-criada +
  sumida + sem investimento". Só a segunda entra na fila de limpeza.
- **Correção = override**: re-associar/gravar identidade passa pelo mesmo mecanismo
  autoritativo ([06](../docs/06-biblioteca-e-scan.md)/[04](../docs/04-modelo-de-dados.md)) — coerência com o resto da fase.

## 7. Fora deste briefing / follow-ups

- Correção de créditos (MD6); merge/split de entidades; tuning do limiar de confiança
  ([06](../docs/06-biblioteca-e-scan.md), decisão aberta de tuning, não de modelo).
