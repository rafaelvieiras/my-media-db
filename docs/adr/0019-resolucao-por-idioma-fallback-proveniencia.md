# ADR-0019 — Resolução por idioma: cadeia de fallback + proveniência

- **Status:** Aceita
- **Data:** 2026-07-09
- **Agenda:** [L2](../00-agenda-de-decisoes.md) · **Spec:** [04](../04-modelo-de-dados.md), [05](../05-api.md)

## Contexto

Com valores por idioma nas camadas ([ADR-0018](0018-modelo-multi-idioma-dimensao-nas-camadas.md)),
a leitura precisa de uma regra **determinística**: qual idioma serve cada campo e o
que a API expõe sobre isso.

## Decisão

**A leitura resolve para UM idioma por requisição, com cadeia de fallback e
proveniência de idioma exposta.**

- **Um idioma por requisição:** `GET /entities/{id}?lang=<código>` (default = o
  **principal** das settings). O shape resolvido continua **PLANO** — cada campo
  traduzível vira um único valor (assinatura estável, a UI não muda de forma).
- **Cadeia de fallback por campo:** `idioma pedido → fallback → original_language →
  base neutra (arquivo)`. Vence a **primeira etapa com valor**, respeitando dentro
  de cada idioma a precedência `override ⟵ externo ⟵ arquivo`.
- **Autoridade do dono por idioma** (R6 por idioma): se o override tem a chave do
  idioma pedido — **mesmo `null`** — ele decide; `null` = *definir vazio de
  propósito* e **interrompe o fallback** (não cai para o próximo idioma). Chave
  ausente naquele idioma = segue a cadeia.
- **Proveniência exposta:** a resposta inclui um mapa `campo → idioma efetivo` (ex.:
  `"resolved_languages": {"title": "pt-BR", "description": "en"}`) e ecoa o `lang`
  pedido. É proveniência de **IDIOMA** (para a UI dizer "esta sinopse está em
  inglês") — distinta da proveniência de **camada** que o R6 decidiu não expor.
- **Assinatura `Resolver(Camadas) Entidade` preservada:** `Camadas` carrega o
  contexto de idioma (pedido + cadeia derivada das settings).

## Consequências

- Consumidores atuais seguem com o shape plano; a UI só passa o `lang`. O **editor**
  ([0016](../../briefings/0016-editor-override-ui.md)) lê os valores crus por idioma
  (override + externo) para editar cada idioma.
- Custo: composição **bidimensional** (camada × idioma). **Busca/FTS por idioma**
  fica follow-up.
- Relacionadas: [ADR-0018](0018-modelo-multi-idioma-dimensao-nas-camadas.md),
  [R2](0002-prioridade-entre-fontes-externas.md),
  [R6](0006-presenca-da-chave-sinaliza-override.md).
