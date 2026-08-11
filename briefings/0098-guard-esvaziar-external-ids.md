# 0098 — Guard de confirmação ao esvaziar IDs externos

> Impede que o editor de IDs externos salve um mapa vazio sem aviso — hoje é
> possível zerar a identidade de uma entity com alguns cliques, sem nenhuma
> confirmação.

| | |
|---|---|
| **ID** | 0098 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 5+ — Histórico de consumo |
| **Status** | done |
| **Docs relacionadas** | [briefing 0027](0027-similar-search-ids-item-manual.md), [briefing 0093](0093-webhook-jellyfin-historico-consumo.md) |
| **Depende de** | — |

---

## 1. Objetivo

Em produção, uma entity ("O Foragido", TMDB 9417) teve seu `external_ids`
zerado (`{}`) sem que o cache `media_external` (TMDB) fosse tocado — os dados
ricos (elenco, tagline) continuaram aparecendo normalmente, mascarando o
problema, mas a identidade autoritativa da entity sumiu. Isso quebrou
silenciosamente o casamento do webhook de entrada do Jellyfin (0093), que
depende exatamente desse campo (`external_id_tmdb`/`external_id_imdb`/
`external_id_tvdb`, colunas geradas a partir de `external_ids_json`).

Investigação de código (não foi migration, não foi purge, não foi merge de
entities) apontou a causa mais plausível: o componente `ExternalIdsEditor`
(`web/src/components/external-ids-editor.tsx`) permite remover, linha a
linha, cada fonte de ID externo via um botão "X" — e salvar (`PUT
/entities/{id}/external-ids`, semântica de **substituição total** do mapa)
sem nenhuma confirmação. Bastam alguns cliques em "X" seguidos de "Salvar
IDs" (ou "Salvar e re-enriquecer") para zerar a identidade da entity por
engano.

Este briefing fecha esse buraco: quando o resultado do salvamento for um mapa
**vazio** e a entity **já tinha** pelo menos um ID externo antes, a UI deve
pedir confirmação explícita antes de mandar o `PUT`.

## 2. Jornada do usuário

1. O dono abre `/entities/{id}/edit/ids` de um item que já tem IDs externos
   (ex.: `tmdb: 9417`).
2. Remove a(s) linha(s) existente(s) via botão "X", ficando com a lista
   vazia.
3. Clica "Salvar IDs" (ou "Salvar e re-enriquecer").
4. **Novo**: como o resultado é um mapa vazio e havia pelo menos um ID antes,
   aparece uma confirmação (`window.confirm`, mesmo padrão já usado no
   projeto — ver `web/src/routes/webhook.tsx`, botão "Gerar novo token")
   explicando a consequência: *"Isso vai remover todos os IDs externos desta
   entity. Ela pode parar de casar com eventos de webhook e integrações
   externas até ser re-vinculada. Continuar?"*.
5. Se confirmar, o `PUT` segue normalmente (mapa vazio é uma operação válida
   e intencional quando confirmada — não vira uma trava permanente).
6. Se cancelar, nada é enviado; a UI permanece no estado editável, permitindo
   desfazer a remoção ou adicionar de volta.
7. Se o item **nunca teve** IDs externos (`linhasOriginais` já vazio) e o
   dono simplesmente não preencheu nada, salvar um mapa vazio continua sem
   confirmação — não é uma remoção, é um estado inicial normal.

## 3. Escopo

**Dentro:**
- Confirmação antes de `updateExternalIds` quando: (a) o mapa resultante é
  vazio, **e** (b) o mapa que a entity tinha ao carregar a tela não era
  vazio (ou seja, é uma remoção líquida, não a ausência original).
- Mesmo guard nos dois botões que disparam o salvamento ("Salvar IDs" e
  "Salvar e re-enriquecer", se forem ações distintas no componente).
- Teste de componente (Vitest) cobrindo: confirmar → PUT disparado; cancelar
  → PUT não disparado; salvar com mapa não-vazio → sem confirmação (não
  regride o fluxo normal).

**Fora (explicitamente):**
- Qualquer mudança de contrato da API (`PUT /entities/{id}/external-ids`
  continua aceitando mapa vazio — a trava é só na UI, o backend não deve
  bloquear operações válidas feitas via API direta/scripts do dono).
- Desfazer/histórico de alterações de `external_ids` (auditoria de quem
  mudou o quê) — fora de escopo aqui.
- O caso do "O Foragido" já foi corrigido manualmente em produção
  (`external_ids.tmdb` restaurado) antes deste briefing existir; não é
  trabalho pendente.

## 4. Contrato técnico

### Frontend (Vite/React)

- `web/src/components/external-ids-editor.tsx`: no handler de submit (antes
  de chamar `updateExternalIds`), calcular `mapaDe(linhas)` e comparar com o
  mapa carregado inicialmente (`linhasOriginais`/estado equivalente já
  existente no componente). Se `Object.keys(mapaNovo).length === 0 &&
  Object.keys(mapaOriginal).length > 0`, chamar `window.confirm(...)` antes
  de prosseguir; se `false`, `return` sem chamar a mutation.

### Arquivos afetados

- `web/src/components/external-ids-editor.tsx` (+ teste)

## 5. Critérios de aceite

- [x] Remover todos os IDs de um item que já tinha algum e salvar dispara
      `window.confirm` com mensagem explicando a consequência.
- [x] Cancelar a confirmação não envia o `PUT` — a tela continua editável.
- [x] Confirmar envia o `PUT` normalmente (mapa vazio continua sendo uma
      operação válida quando intencional).
- [x] Salvar um mapa não-vazio (fluxo normal) não é afetado — nenhuma
      confirmação nova aparece.
- [x] Item sem IDs desde o início, salvo vazio (nada preenchido), não
      dispara confirmação.
- [x] `tsc -b`; `npm run lint`; `npm run test:coverage` sem regressão (validado
      escopado ao componente nesta etapa; `test:coverage` completo roda na
      validação final do 0099 junto).

## 6. Notas de implementação / decisões

- Escolhido `window.confirm` (não um `AlertDialog` novo) por consistência
  com o padrão já usado no projeto para confirmações destrutivas simples
  (ver 0097, botão "Gerar novo token").
- A trava fica só no frontend, de propósito — o backend não deve impedir
  scripts/automação legítima do dono de zerar `external_ids` via API direta;
  o risco identificado foi especificamente o clique acidental na UI.

## 7. Fora deste briefing / follow-ups

- Ver briefing 0099 (eventos órfãos de webhook) — mitiga o *efeito colateral*
  de uma identidade zerada (evento descartado) além de prevenir a *causa*
  aqui.
