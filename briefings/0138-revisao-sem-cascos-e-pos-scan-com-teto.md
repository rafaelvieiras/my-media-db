# 0138 — Revisão sem cascos vazios e pós-scan com teto de tempo

**Status:** done
**Data:** 08/nov/2026
**Contexto:** medido em produção (08/nov/2026, 22:54–01:30 UTC) depois do deploy do
[0135](./0135-auto-match-fora-da-transacao-e-pos-scan.md). Complementa o 0135 e o
[0137](./0137-revisao-agrupada-compacta.md); não muda a decisão do 0018 sobre quem
entra na fila de revisão.

## Problema medido

O primeiro scan depois do deploy mostrava o serviço funcionando — 85 faixas
resolvidas, álbuns novos criados com ano certo (`Duran Duran` → 1993, `Notorious` →
1986, `The Singles 1986–1995` → 2004) — e **nenhum sinal disso na fila de revisão**:
ela continuava com 206 itens (185 faixas). Duas causas independentes:

1. **Cascos vazios.** `ReconciliarFaixaDraft`/`ReconciliarSerieDraft` reponteiam o
   `media_file` para a entity definitiva e deixavam a entity-rascunho para trás, sem
   arquivo e sem vínculo (preservação do princípio 5). Ela não saía da fila: continuava
   listada como rascunho, agora no grupo "sem arquivo". Medido: **114 faixas draft sem
   `media_file` e sem `entity_relation`** (a maioria criada em agosto/2026), além das
   71 que de fato ainda esperam match. Ou seja: a fila não diminuía nem quando a
   reconciliação resolvia o item.
2. **Pós-scan sem teto.** Sem piso de horas, TODO scan re-tentava as mesmas 200 faixas
   que tinham acabado de falhar. Custo medido: **45+ min de chamadas ao MusicBrainz
   por scan** (1 req/s, até 4 tentativas por faixa: 1 busca de artista + até 3 imports
   escopados + a busca por título), travando a fila de scans e sem resolver item novo.

## Decisão

### 1. A reconciliação purga o rascunho que ela substituiu

Duas queries novas em `internal/db/queries/entity.sql`:

- `ApagarRascunhoVazio(id)` — chamada por `purgarRascunhoSubstituido` logo depois do
  reponte do `media_file`, em `ReconciliarFaixaDraft` e `ReconciliarSerieDraft`.
- `ApagarRascunhosVazios()` — varredura em lote no fim do pós-scan, para limpar o
  estoque que já existe.

As duas só apagam rascunho (`status = 'draft'`) que **não tem arquivo, não tem vínculo
nenhum (nem como filho, nem como pai) e não tem override do dono**. Quatro condições,
todas no próprio `DELETE` — sem ler-e-depois-apagar, sem corrida.

**Por que isto não viola o princípio 5 (não-destrutivo):** o rascunho que a
reconciliação substitui foi criado pelo próprio matcher (`title` derivado do nome do
arquivo, sem `external_ids`, sem relação). O princípio 5 protege o que o **dono**
escreveu — override, edição manual, item que ele cadastrou. Nada disso existe nesse
casco, e cada condição do `DELETE` garante isso. Rascunho com arquivo (item de verdade
na fila, esperando o dono), com filhos (série que ainda tem episódios presos) ou com
override continua intocado.

### 2. Pós-scan com piso de horas e teto de tempo

- `scanner.PosScanHorasMinimas = 12`: `ListarFaixasDraftOrfasPorSource` e
  `ListarSeriesDraftPorSource` passam a pular quem foi tentado nas últimas 12h. É o
  mesmo espírito do `last_external_match_attempt_at` que o job periódico já usa, com
  janela muito menor (o job usa dias; aqui é o retry que o dono pediu ao escanear).
- Ordem `nunca tentado primeiro` (`last_external_match_attempt_at IS NULL DESC`): com
  teto de itens e de horas, a fila **avança** a cada scan em vez de re-tentar sempre o
  mesmo começo de lista.
- `posScanBudget = 4 min` (`internal/server/posscan.go`): orçamento de tempo de parede
  do pós-scan de UM scan. Estourou, para onde está e deixa o resto para o próximo (a
  ordem acima garante progresso). Sem teto, o pós-scan bloqueava a fila de scans.

## Verificação

- `TestPosScanPurgaRascunhoVazio`: casco sem arquivo/vínculo/override some no pós-scan.
- `TestPosScanNaoPurgaRascunhoComVinculo`: rascunho com arquivo (fila de verdade) e
  série-rascunho com episódio ligado ficam.
- `TestPosScanPulaItemTentadoRecentemente`: item carimbado agora não volta na hora.
- `TestReconciliarFaixaDraftReligaComSucesso` e
  `TestReconciliarFilhosOrfaosFaixaReligaComSucesso` passam a exigir a purga do
  rascunho substituído (era o contrário: exigiam a preservação).
- `TestJornadaReconciliacaoAutomaticaSerieDraft`: a série resolvida pode ser o próprio
  rascunho (find-or-create por nome reaproveita a entity existente). O teste passou a
  cobrar o RESULTADO — identidade aplicada, árvore de temporadas criada, os 2 arquivos
  ligados aos episódios certos — em vez de exigir uma entity nova.
- `go build ./... && go vet ./... && go test ./... -count=1` → 42 pacotes ok;
  `make gate-fast` passou.

## Follow-ups (não feitos aqui)

- **Cache negativo por álbum no caminho de match.** As 8 faixas de uma trilha repetem
  a mesma busca de artista e o mesmo "álbum não encontrado" (custo 8×, sem retorno). Um
  memo por (artista, álbum) e por título de álbum dentro de UMA passada cortaria a
  maior parte do custo do pós-scan. Hoje o teto de tempo já limita o dano.
- **Ano como evidência de desempate** em álbuns homônimos (ex.: `Duran Duran` de 1981 ×
  1993). O MusicBrainz escolheu o certo nos 5 casos medidos, mas o desempate por ano da
  pasta/tag deixaria isso determinístico. `mediainfo.AudioTags` ainda não lê ano.
