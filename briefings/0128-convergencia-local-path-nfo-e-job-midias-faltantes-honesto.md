# 0128 — Convergência de local_path/nfo_path legados e exclusão silenciosa do job midias_faltantes

> O self-heal do briefing 0127 (regravar o binário quando o arquivo some do
> disco) resolve "arquivo sumiu", mas **não** resolve "arquivo está no lugar
> ERRADO desde antes do fix de naming do briefing 0081" nem "a entity nunca
> chega a ser tocada pelo job porque caiu fora do gate de elegibilidade num
> instante de corrida com o `saude_biblioteca`". Este briefing fecha os dois
> gaps, achados investigando por que duas entities reais em produção
> (Chapolin Colorado e MIB Homens de Preto) continuaram sem arte/NFO
> corretos mesmo depois do 0127 já estar em produção.

| | |
|---|---|
| **ID** | 0128 |
| **Tipo** | fix |
| **Fase (roadmap)** | Transversal — Infraestrutura operacional |
| **Status** | done (implementado, commitado `823a7b6`, deployado e validado em produção em 05/09/2026 — Chapolin idempotente, MIB convergiu poster/fanart para a raiz e regravou o NFO) |
| **Docs relacionadas** | [08-artwork-e-assets](../docs/08-artwork-e-assets.md) |
| **Depende de** | 0127, 0081, 0064 |

---

## 1. Objetivo

Depois do 0127 ir para produção (commit `ea53ed3`, 2026-09-04), duas entities
reais continuaram sem materialização correta na pasta real, por duas causas
**distintas**, nenhuma delas coberta pelo 0127:

**Caso A — Chapolin Colorado** (tv, `fd91e8c4-764f-4369-9e99-4a4fab9485a6`):
o job periódico `midias_faltantes` rodou hoje de manhã e reportou sucesso
("787 elegível(is), 787 processada(s)..."), mas a pasta da série não tinha
**nenhum** arquivo na raiz (nem poster/fanart/nfo herdados de antes, nem
novos). Disparar manualmente `POST /entities/{id}/materialize-missing-media`
para essa entity corrigiu na hora — o que prova que não é um bug de "o
código não sabe materializar isso", e sim de "a entity nunca chegou a ser
processada pelo job daquele dia".

Causa: `ListarEntitiesComMediaFilePresente` (universo elegível do job,
`internal/server/midias_faltantes.go:56-79`) exige `media_file.state =
'present'` (`internal/db/queries/media_file.sql:167-183` e demais). O job
`midias_faltantes` e o job `saude_biblioteca` — que re-escaneia as fontes e
pode reavaliar `state` via `os.Stat`/mtime sobre o MESMO mount FUSE
remoto (rclone/gdrive) — têm o **mesmo cron default** (`0 4 * * *`,
`internal/db/migrations/0016_watch_e_midias_faltantes.sql:24-31`) e rodam em
goroutines independentes. Evidência direta em produção: `saude_biblioteca`
terminou às `04:02:20Z` e `midias_faltantes` às `04:02:29Z` — **9 segundos
de intervalo**, contendendo pelo mesmo mount. Se o `state` de um
`media_file` estava temporariamente diferente de `'present'` no instante em
que `midias_faltantes` consultou o universo elegível, a entity fica **fora
da lista processada naquele dia** — e o job reporta "sucesso" sem qualquer
sinal de que algo ficou de fora por esse motivo. É o mesmo padrão de "sucesso
incondicional escondendo exclusão" que acabamos de reportar contra o Arcane
upstream (`getarcaneapp/arcane#3852`) — só que no nosso próprio código.

**Caso B — MIB Homens de Preto** (tv, `ba252e9c-2b68-4cdc-a6b6-02136dd9b3d1`):
`poster`/`fanart` têm `local_path` apontando para
`.../Season 01/MIB - Homens de Preto (1997) - S01E01 - A Síndrome do
Adeus-{poster,fanart}.jpg` (nome/local de EPISÓDIO), e `entity.nfo_path`
aponta para `.../Season 01/tvshow.nfo` — ambos gravados em **23/jul**, antes
do briefing 0081 (mesma data, mais tarde) corrigir `DiretorioDestino` para
tratar a raiz da série como pasta própria. `banner.jpg`/`logo.png` da MESMA
série foram gravados corretamente na raiz em 11/ago (já com o fix). Como os
arquivos legados **existem** fisicamente (só no lugar/nome errado), o
`arquivoLocalAindaExiste` do 0127 sempre devolve `true` e nunca aciona
regravação — o self-heal do 0127 assume que "arquivo existe" ⇒ "nada a
fazer", quando na verdade "existe, mas no destino errado" é um terceiro
estado que ele não distingue.

## 2. Jornada do usuário

**Caso A:**
1. O dono espera que o job noturno materialize arte/NFO de toda entity com
   arquivo presente — e na maioria dos dias funciona.
2. Ocasionalmente (corrida com o rescan do `saude_biblioteca` no mesmo
   mount), uma entity específica fica de fora daquela execução, sem
   nenhum sinal visível — o job reporta sucesso, a UI mostra que a arte
   está "faltando" (badge de completude), e nada nunca tenta de novo até o
   dono notar manualmente e forçar via "Baixar Artes Faltantes".
3. **Depois deste fix:** os dois jobs não competem mais pelo mesmo instante
   no mount remoto (cron escalonado), e o resumo do job passa a expor
   quando `media_file`s estão fora do estado `'present'` no momento do
   scan — um sinal que hoje simplesmente não existe em lugar nenhum.

**Caso B:**
1. Uma série importada antes de 23/jul acabou com `poster.jpg`/`fanart.jpg`/
   `tvshow.nfo` gravados dentro de uma subpasta de temporada, com nome de
   episódio — bug de naming já corrigido no código, mas o dado antigo nunca
   foi migrado.
2. **Hoje:** a galeria mostra "Salvo na pasta" (tecnicamente verdade — o
   arquivo existe, só no lugar errado); Kodi/Emby/Jellyfin, que esperam
   `poster.jpg`/`tvshow.nfo` na raiz da série, nunca encontram a arte; e
   nenhuma ação do sistema (job periódico, "Baixar Artes Faltantes", nem o
   self-heal do 0127) jamais corrige isso, porque todos concordam que "o
   arquivo existe" é suficiente para não fazer nada.
3. **Depois deste fix:** a próxima vez que essa entity for processada
   (job periódico ou ação manual), o sistema recalcula o destino canônico
   ATUAL, percebe a divergência, MOVE (rename, sem recopiar bytes) o
   asset para o nome/local correto e regrava o `.nfo` na raiz — sem
   precisar de nenhuma migração de dados em lote nem de o dono acionar
   `PUT .../prefer` manualmente por entity.

## 3. Escopo

**Dentro:**
- **Caso A:**
  - Escalonar o cron default de `midias_faltantes` para rodar depois de
    `saude_biblioteca` ter tempo de terminar (ver §4).
  - Tornar o resumo do job honesto sobre `media_file`s fora do estado
    `'present'` no momento do scan (contagem informativa, sem mudar
    comportamento) — mesmo espírito do que pedimos ao Arcane fazer com o
    próprio sync deles.
- **Caso B:**
  - Em `materializarKindVencedor` (asset já local e "in library"):
    recalcular o destino canônico atual e, se divergir do `local_path`
    registrado, MOVER (rename) para o local certo — reusando a lógica já
    existente e já seguraem `renomearParaBare` (evita a armadilha de
    "asset já principal tratado como não-principal" que motivou o 0127 a
    NÃO recalcular no caso de regravação simples).
  - Em `gravarNFOSeFaltante`: recalcular o destino canônico atual do
    `.nfo`; se divergir do `nfo_path` registrado, regravar no lugar certo
    (fluxo `gravarNFO` já faz isso) e remover o arquivo antigo órfão no
    local errado (best-effort, log-only em caso de falha).
- Testes cobrindo os dois casos de drift (asset e NFO) e o caso "sem
  drift" (nenhuma escrita/rename a mais).

**Fora (explicitamente):**
- Uma migração/backfill em lote que varra TODOS os assets/NFOs do banco de
  uma vez, independente de a entity ser tocada por um job ou ação manual —
  o mecanismo de auto-correção sob demanda (job diário + "Baixar Artes
  Faltantes") já converge o catálogo inteiro em, no máximo, uma execução
  diária do `midias_faltantes` por entity elegível; só vale reabrir isso se
  o auto-heal-on-touch se mostrar insuficiente na prática (catálogo grande
  demais, jobs periódicos desligados etc.).
- Um mecanismo de lock/mutex entre jobs para impedir QUALQUER corrida futura
  entre `saude_biblioteca` e outros jobs sobre o mesmo mount — o
  escalonamento de cron deste briefing é a mitigação mínima suficiente para
  o caso concreto observado; um lock genérico é escopo maior, só se o
  escalonamento se mostrar insuficiente.
- Revalidação de `local_path`/`nfo_path` no *hot path* de leitura
  (`GET .../artwork`, badges de completude) — continua fora, pela mesma
  razão do 0127 (custo de I/O contra mount remoto a cada request).
- Recasqueamento de path do datasource (`PATCH /library/{id}`) — problema
  adjacente já identificado como fora de escopo pelo próprio 0127 §3.

## 4. Contrato técnico

### Dados / modelo

**Migration nova** (`internal/db/migrations/0027_...sql`, goose): ajustar o
cron default do job `midias_faltantes` já seedado por instalações
existentes, **defensivamente** (só se o dono nunca customizou via
`PATCH /jobs/{id}`):

```sql
-- +goose Up
UPDATE job_definition
SET schedule_cron = '0 5 * * *'
WHERE id = 'midias_faltantes' AND schedule_cron = '0 4 * * *';

-- +goose Down
UPDATE job_definition
SET schedule_cron = '0 4 * * *'
WHERE id = 'midias_faltantes' AND schedule_cron = '0 5 * * *';
```

Também atualizar o seed original em
`internal/db_migrations/0016_watch_e_midias_faltantes.sql`? **Não** — seeds
de migration já aplicada nunca se editam retroativamente (goose não
re-executa); o valor default para instalações NOVAS deve ser corrigido numa
migration nova separada (a mesma acima já cobre instalação nova, pois
roda depois da 0016 na mesma sequência). Um hora de folga (`04:00` →
`05:00`) é generosa frente ao tempo observado do `saude_biblioteca` em
produção (segundos a poucos minutos para 3 fontes) — não precisa ser maior.

**Nova query** (`internal/db/queries/media_file.sql`, mesmo arquivo das
demais de elegibilidade):

```sql
-- name: ContarMediaFilesEmEstadoMissing :one
-- Contagem informativa para o resumo do job midias_faltantes (briefing
-- 0128): media_file 'missing' e um sinal de instabilidade do mount remoto
-- (rclone/gdrive) que pode ter excluido silenciosamente alguma entity do
-- universo elegivel daquela execucao (ListarEntityIDsComMediaFilePresente
-- exige state='present'). So leitura, nao muda nenhum comportamento.
SELECT COUNT(*) FROM media_file WHERE state = 'missing';
```

### Backend (Go)

**Caso A — resumo honesto do job**
- `internal/server/midias_faltantes.go`: expor um método (ex.:
  `s.ContarMediaFilesEmEstadoMissing(ctx)`, wrapper fino sobre a query
  sqlc nova) via uma nova interface pequena consumida pelo `Handler`
  (mesmo padrão de `EntityLister`/`Materializador`).
- `internal/job/midiasfaltantes/midiasfaltantes.go`:
  - Nova interface `ContadorMissing` (ou estender `EntityLister` — decisão
    de quem implementar: preferir interface nova e pequena, mesmo espírito
    das duas existentes).
  - `Executar` (linha 68-90): chamar a nova contagem (best-effort — falha
    na contagem não aborta o job, só omite o dado do resumo).
  - `montarMensagem` (linha 92-102): se a contagem for `> 0`, anexar algo
    como `", N media_file(s) em estado 'missing' no momento do scan (pode
    ter excluído alguma entity deste ciclo)"`.

**Caso B — convergência de naming (asset)**
- `internal/server/midias_faltantes.go:144-160` (`materializarKindVencedor`,
  branch `vencedor.IsLocalInLibrary`): trocar a chamada direta a
  `arquivoLocalAindaExiste` + `regravarBinarioAssetNoLocalPathExistente`
  por uma chamada a uma função nova, `convergirNomeCanonico`, **quando
  `asset.IsPrincipal != 0`** (ver nota de risco no §6 — só é seguro
  recalcular o destino para quem já É o principal; um asset local
  não-principal cujo `local_path` sumiu continua no fluxo antigo,
  inalterado).
  **Correção de design feita durante a implementação:** o briefing
  originalmente propunha reusar `renomearParaBare`
  (`internal/server/localmedia_handlers.go:689-714`) para isso — mas essa
  função **preserva o diretório atual** do asset
  (`dir := filepath.Dir(alvo.LocalPath.String)`) por design, porque foi
  escrita para o fluxo `PUT .../prefer` (onde o asset já está na pasta
  certa, só falta virar bare). O bug real do MIB é justamente o
  DIRETÓRIO errado (`Season 01/` em vez da raiz da série) — reusar
  `renomearParaBare` sem alteração só trocaria o nome do arquivo
  DENTRO da pasta errada, sem convergir de fato (confirmado por um teste
  que falhou exatamente assim durante a implementação). A função nova
  `convergirNomeCanonico` é uma cópia de `renomearParaBare` com uma única
  mudança: o diretório vem de `resolverDestino` (a mesma fonte usada por
  `gravarAssetNaPastaReal`), não de `filepath.Dir` do caminho atual. Fora
  isso, o comportamento é o mesmo: (a) recalcula destino; (b) se igual ao
  atual, faz o self-heal do 0127 (`arquivoLocalAindaExiste` + regravar
  in-place); (c) se diferente, RENOMEIA (`localmedia.RenomearArquivo`, sem
  recopiar bytes) para o destino correto — podendo mudar de diretório.
  `materializarKindVencedor` persiste o novo `local_path` via
  `AtualizarAssetLocalPath` quando o destino devolvido difere do
  `asset.LocalPath.String` original. A lógica de decisão do
  `IsLocalInLibrary` foi extraída para `reconfirmarAssetLocalNaLibrary`
  (mantém a complexidade ciclomática das duas funções abaixo de 10).

**Caso B — convergência de naming (NFO)**
- `internal/server/midias_faltantes.go:295-321` (`gravarNFOSeFaltante`):
  antes de decidir "já tem NFO, pula" (linha 300), recalcular o destino
  canônico atual: `dir, nomeBaseVideo, _, ok, err := s.resolverDestino(ctx, ent)`
  (mesma função usada por `gravarNFO`, `localmedia_handlers.go:526`) +
  `nome := localmedia.NomeArquivoNFO(ent.Type, nomeBaseVideo)`
  (`internal/localmedia/naming.go:454-459`) + `destinoAtual :=
  filepath.Join(dir, nome)`. Se `ok` e `destinoAtual != ent.NfoPath.String`
  → **não** pular, cair no fluxo normal de regravação (que já grava no
  destino certo via `gravarNFO`). Depois de `gravarNFO` suceder e
  `ent.NfoPath.String` (o valor ANTIGO, capturado antes da chamada) for
  diferente do novo, remover o arquivo antigo órfão
  (`os.Remove(nfoPathAntigo)`, best-effort — log e ignora falha, mesmo
  espírito do resto do arquivo). `gravarNFO`
  (`localmedia_handlers.go:525-563`) não muda — ele já sempre escreve no
  destino recém-calculado e sobrescreve `entity.nfo_path`; hoje só não é
  chamado por causa do guard cego da linha 300.

### Arquivos afetados
- `internal/db/migrations/0027_reagenda_midias_faltantes.sql` (novo).
- `internal/db/queries/media_file.sql` (nova query `ContarMediaFilesEmEstadoMissing`).
- `internal/server/midias_faltantes.go` (fix nos dois branches + novo
  wrapper de contagem).
- `internal/job/midiasfaltantes/midiasfaltantes.go` (nova interface +
  resumo estendido).
- Testes novos/ajustados nos `_test.go` correspondentes.

## 5. Critérios de aceite

- [ ] Instalação existente com `job_definition.schedule_cron = '0 4 * * *'`
      para `midias_faltantes`: após a migration, passa a ser `'0 5 * * *'`.
      Instalação onde o dono já customizou o cron (valor diferente do
      default): migration não altera nada.
- [ ] `go test` cobrindo: `montarMensagem` inclui a contagem de `missing`
      quando `> 0`; omite quando `0`.
- [ ] Asset com `local_path` legado apontando para nome/pasta de EPISÓDIO
      (fixture reproduzindo o caso MIB: `is_principal=1`, `local_path` sob
      uma subpasta de temporada com nome de episódio): ao rodar
      `MaterializarMidiasFaltantes` para a entity, o arquivo é RENOMEADO
      (não recopiado) para `poster.jpg`/`fanart.jpg` na raiz da série, e
      `asset.local_path` no banco é atualizado para o novo caminho.
- [ ] Mesmo fixture, mas asset com `is_principal=0`: comportamento do 0127
      preservado (regravação in-place, sem recalcular nome) — não regride.
- [ ] Entity com `nfo_path` legado apontando para `Season 01/tvshow.nfo`
      (arquivo físico presente nesse local errado): ao rodar
      `MaterializarMidiasFaltantes`, o `.nfo` é regravado na raiz da série,
      `entity.nfo_path` passa a apontar pra lá, e o arquivo antigo em
      `Season 01/` é removido.
- [ ] Caso sem drift (asset/NFO já no destino canônico atual): nenhum
      rename/escrita a mais acontece (mesma garantia de "caminho feliz sem
      regressão de performance" do 0127).
- [ ] `go build ./...`, `go vet ./...`, `go test ./internal/... -count=1`
      verdes.
- [ ] `make gate-local` (só no agente final) sem regressão.
- [ ] Validado em produção nas duas entities reais que motivaram este
      briefing:
      `GET /api/v1/entities/fd91e8c4-764f-4369-9e99-4a4fab9485a6/assets`
      (Chapolin Colorado) e
      `GET /api/v1/entities/ba252e9c-2b68-4cdc-a6b6-02136dd9b3d1/assets`
      (MIB Homens de Preto) — depois do job periódico rodar (ou de um
      disparo manual de `materialize-missing-media`), MIB deve ter
      `poster.jpg`/`fanart.jpg`/`tvshow.nfo` na RAIZ da pasta (confirmado
      via SSH/`docker exec`, não só pela resposta da API).

## 6. Notas de implementação / decisões

- **Por que só recalcular nome bare quando `IsPrincipal != 0`:**
  `renomearParaBare` monta o nome BARE do kind (`NomeArquivoArte(kind, true,
  0, ...)`) — o nome reservado ao asset principal. Se aplicássemos essa
  mesma lógica a um asset que NÃO é principal (edge case raro, mas
  possível: `vencedor` pode não ser o `is_principal=1` se, por algum motivo
  histórico, nenhum asset do kind tiver a flag), o arquivo renomeado
  colidiria com o nome que o VERDADEIRO principal deveria ocupar,
  duplicando "poster.jpg" para dois assets diferentes. Por isso o guard
  `IsPrincipal != 0` é obrigatório, não cosmético.
- **Por que mover (rename) em vez de regravar do zero:** mesma razão do
  0127 — o binário já está correto no disco, só o CAMINHO está errado;
  `localmedia.RenomearArquivo` é uma operação local ao mount (sem
  re-baixar do provider nem reler o cache), mais barata e sem risco de
  divergência de conteúdo.
- **Por que remover o `.nfo` órfão mas não o asset renomeado:** o asset
  renomeado NUNCA fica duplicado (é a mesma linha, o mesmo arquivo, só
  movido) — não há órfão a limpar. O NFO é diferente: `gravarNFO` cria um
  arquivo NOVO no destino certo sem tocar no antigo, então sem a remoção
  explícita o `.nfo` errado ficaria para sempre ao lado do certo,
  confundindo Kodi/Emby (que podem escanear ambos). Remover é seguro porque
  NFO é sempre um artefato exportado pelo sistema (nunca editado pelo dono
  fora do fluxo do MyMediaDB — AGENTS.md, "NFO é exportação one-way").
- **Por que a contagem de `missing` é só informativa, não bloqueia nada:**
  seguindo o princípio "não-destrutivo" e o espírito best-effort do resto
  do sistema de jobs (0053/0054/0056) — o objetivo aqui é dar visibilidade
  a um sinal que hoje não existe em lugar nenhum, não introduzir uma nova
  condição de falha ou retry automático (que poderia mascarar um problema
  real do mount atrás de tentativas silenciosas).
- **Por que escalonar cron em vez de resolver a corrida "de verdade" (lock
  entre jobs):** o `AGENTS.md` já documenta que o mount FUSE remoto é
  inerentemente instável para eventos/consistência imediata
  (`fsnotify não funciona sobre mounts FUSE remotos`); introduzir
  coordenação explícita entre jobs seria uma mudança estrutural bem maior
  para mitigar um sintoma que uma hora de intervalo já resolve na prática
  observada (rescan de 3 fontes leva segundos, não minutos).

## 7. Fora deste briefing / follow-ups

- Backfill em lote de todo o catálogo (WalkDir do scanner cruzado com
  `asset.local_path`/`entity.nfo_path`) — já era follow-up do 0127 §7;
  continua sendo, e este briefing reforça que só vale a pena se o
  auto-heal-on-touch (agora estendido para também convergir naming) se
  mostrar insuficiente em produção depois deste fix.
- Mesma revalidação/convergência de naming para o path do `PATCH
  /library/{id}` (recasqueamento) — mesmo follow-up já listado no 0127 §7.
- Um selo/alerta na UI (não só no `last_message` do job) quando a contagem
  de `media_file` `missing` for persistentemente alta — decisão de produto
  que não faz parte deste fix técnico.
