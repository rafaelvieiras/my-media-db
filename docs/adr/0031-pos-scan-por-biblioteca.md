# ADR-0031 — Pós-scan por biblioteca: o scan fecha o próprio ciclo

- **Status:** Aceita
- **Data:** 2026-11-08
- **Agenda:** [SCAN-POS](../../docs/00-agenda-de-decisoes.md) · **Spec:** [06-biblioteca-e-scan](../06-biblioteca-e-scan.md), [07-enriquecimento-providers](../07-enriquecimento-providers.md)

## Contexto

O scan é o único gatilho de entrada de mídia no catálogo, mas ele **não** é dono
do ciclo de enriquecimento. Hoje:

- o pipeline de match roda **uma única vez por arquivo**, dentro do scan;
- o que falhou fica em rascunho até o job periódico `saude_biblioteca`, que
  reconcilía no máximo 15 itens por tipo por execução, com throttle de 15 dias
  por item (briefing 0120/0122);
- o gatilho de auto-match do filme novo (0089) foi implementado **dentro da
  transação de escrita do lote** e falha sempre (`sql: no rows in result set`,
  medido em produção em 08/nov/2026) — e travaria o scan se tentasse escrever,
  porque o pool de escrita tem uma única conexão (ADR-0010).

Resultado observado: item novo entra cru (sem metadado, sem arte, sem
`external_ids`) e o dono não tem como saber que existe retry pendente. O
princípio nº 4 do `AGENTS.md` ("enriquecimento opcional e puxado") continua
respeitado — o gatilho é a **ação do dono** de escanear a biblioteca, não uma
varredura periódica do catálogo.

## Decisão

1. **Nada de enriquecimento dentro da transação do scan.** Gatilhos pós-import
   (auto-match, materialização opcional) rodam **depois do `Commit`** do lote.
   Qualquer leitura/escrita do enrich dentro do `BeginTx…Commit` é proibida: a
   linha não existe para o pool de leitura e a escrita trava na conexão única.
2. **O scan tem uma etapa final de pós-scan**, por biblioteca, best-effort e
   capada: reconcilía as faixas e séries em rascunho **daquela fonte** e retenta
   o auto-match dos pendentes daquela fonte, materializando a arte de quem passou
   a ter identidade quando o toggle de auto-download está ligado.
3. **O pós-scan é parte do item `scan` da fila** (ADR-0028), não um item novo:
   mesma concorrência 1, mesmo `ctx` cancelável, progresso reportado como
   mensagem da etapa.
4. **Não há throttle de dias no pós-scan.** Ele é o retry do scan que o dono
   acabou de pedir; o throttle de 15 dias continua valendo só para o
   enriquecimento retroativo periódico. O custo é limitado por caps por execução.

## Consequências

- Item novo passa a sair do scan com identidade e arte (quando a fonte responde),
  e o rascunho que passaria na segunda tentativa sai da fila de revisão no mesmo
  clique.
- O scan fica mais longo por biblioteca (chamadas de rede na etapa final); o
  cap por tipo mantém o pior caso previsível, e o dono vê a etapa na mensagem de
  progresso.
- Regra nova para quem mexer no scanner: **nenhum efeito de enriquecimento dentro
  de transação**. Teste de regressão cobre o caso do importador que escreve.
- Fica de fora (evolução futura): fila dedicada de enriquecimento com prioridade,
  retry com backoff próprio, e reconciliação de episódio provisório (0133).
- Relacionadas: [ADR-0010](0010-concorrencia-sqlite-wal-escritor-unico.md),
  [ADR-0028](0028-fila-de-execucao-unificada-scan-jobs.md),
  [ADR-0029](0029-import-de-musica-local-first-por-faixa-possuida.md).
