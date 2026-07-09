# ADR-0020 — Tradução de tags (genres localizados + keywords)

- **Status:** Aceita
- **Data:** 2026-07-09
- **Agenda:** [L3](../00-agenda-de-decisoes.md) · **Spec:** [04](../04-modelo-de-dados.md), [07](../07-enriquecimento-providers.md)

## Contexto

As **tags** (gêneros/keywords) também devem ter tradução. O TMDB **localiza gêneros**
(via `language`), mas **não traduz keywords** — é o ponto onde ele peca. Um modelo de
tags mono-idioma não serve ao acervo multilíngue.

## Decisão

**Tags viram um campo-lista POR IDIOMA, compostas por merge de operações dentro de
cada idioma.**

- **Forma:** `tags` passa a ser `idioma → lista`. Compõe por **merge de operações**
  (R3) **dentro de cada idioma**: base externa daquele idioma + `{add, remove}` do
  override naquele idioma; resolve pela cadeia de fallback do
  [ADR-0019](0019-resolucao-por-idioma-fallback-proveniencia.md).
- **Gêneros:** usar a **lista de gêneros localizada** do TMDB por idioma permitido
  (`GET /genre/{movie|tv}/list?language=`, **cacheável**), mapeando os ids de gênero
  do item para os nomes em cada idioma.
- **Keywords:** o TMDB **não traduz** — guardar sob o idioma obtido (ou o original);
  o dono traduz manualmente via override por idioma. **Sem tradução automática.**

## Consequências

- O acervo passa a ter **tags multilíngues**, superando o gap do TMDB.
- Custo: buscar/cachear as listas de gênero por idioma; keywords ficam **parciais**
  (só no idioma obtido) até tradução manual do dono.
- Relacionadas: [R3](0003-override-de-listas-por-operacoes.md),
  [ADR-0018](0018-modelo-multi-idioma-dimensao-nas-camadas.md),
  [ADR-0019](0019-resolucao-por-idioma-fallback-proveniencia.md).
