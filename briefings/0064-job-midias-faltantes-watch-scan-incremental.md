# 0064 — Job "baixar mídias faltantes" + scan incremental + watch de filesystem

> Fecha o ciclo do [0062](0062-midias-locais-nfo-e-artes-na-pasta.md) em
> escala: um job periódico materializa na pasta real o que já está resolvido
> mas falta no disco, o scan deixa de re-hashear tudo a cada rodada
> (mtime/size), e pastas podem opcionalmente "escutar" mudanças do
> filesystem — sempre com o job periódico como rede de segurança, porque
> mounts remotos (rclone/gdrive) não emitem esses sinais.

| | |
|---|---|
| **ID** | 0064 |
| **Tipo** | feature |
| **Fase (roadmap)** | Transversal / infraestrutura operacional — mesma família do [0052](0052-fundacao-sistema-de-jobs.md)/[0053](0053-job-saude-biblioteca.md) |
| **Status** | done |
| **Docs relacionadas** | [06-biblioteca-e-scan](../docs/06-biblioteca-e-scan.md), [08-artwork-e-assets](../docs/08-artwork-e-assets.md) |
| **Depende de** | 0009, 0052, 0053, 0062 |

---

## 1. Objetivo

Um filme pode estar totalmente enriquecido (metadado + arte já resolvidos via
TMDB/fanart.tv, visíveis na tela) sem que UM ÚNICO byte disso exista dentro da
pasta real do filme no disco — porque hoje nada materializa automaticamente
(o [0062](0062-midias-locais-nfo-e-artes-na-pasta.md) só materializa quando o
dono clica). Este briefing fecha isso com um job periódico que roda em
background e vai colocando essas mídias no disco sozinho, **sem** varrer
pastas ao vivo (proibitivo em bibliotecas grandes, especialmente montadas via
`rclone mount` contra Google Drive — confirmado em pesquisa: FUSE remoto nem
sequer consegue emitir evento de mudança para arquivos alterados do lado do
provedor, então **qualquer estratégia aqui precisa assumir polling
periódico como base**, não só watch).

Faz três coisas relacionadas, na mesma pedra porque compartilham a mesma
motivação (evitar I/O de disco desnecessário/repetido):

1. **Job "baixar mídias faltantes"** — periódico, só lê o banco (nunca lista
   diretório), materializa o que falta.
2. **Scan incremental** — o scan que já existe (dentro do job de saúde, 0053)
   para de re-hashear TODO arquivo em TODA execução; passa a pular arquivos
   cujo `mtime`+tamanho batem com o que já está gravado.
3. **Watch de filesystem opcional por pasta** — melhor esforço via
   `fsnotify`, só reduz a LATÊNCIA até o próximo scan da fonte; nunca
   substitui o job periódico.

## 2. Jornada do usuário

1. Dono cadastra/edita uma `library_source` e pode marcar "escutar mudanças
   desta pasta" (opt-in, desligado por padrão — ver §6).
2. Sem nenhuma ação do usuário, um job roda periodicamente (mesmo mecanismo
   dos jobs 0052/0053) e materializa na pasta real qualquer arte/NFO que já
   estava resolvida mas ainda faltava no disco — o dono simplesmente percebe,
   ao abrir a pasta no explorador de arquivos (ou no Kodi apontando pra ela),
   que os arquivos foram aparecendo sozinhos.
3. Quando o dono adiciona um arquivo novo numa pasta com watch ligado, o
   sistema detecta a mudança em segundos/minutos (best-effort) e antecipa o
   próximo scan daquela fonte, em vez de esperar o intervalo padrão.
4. Quando a pasta é um mount remoto (rclone/gdrive) sem emissão de evento
   confiável, o dono ainda assim vê o item aparecer — só que no próximo ciclo
   do job periódico, não instantaneamente. Nenhuma ação manual é necessária
   em nenhum dos dois casos.

## 3. Escopo

**Dentro:**
- Novo `Handler` de job `midias_faltantes`: por entity com `media_file`
  presente, materializa (via o mecanismo do 0062) a arte "hoje vencedora" de
  cada `kind` relevante quando ainda não tem `local_path`, e o `.nfo` quando
  `entity.nfo_path` é nulo.
- Scan incremental por `mtime`+tamanho em `internal/scanner/scan.go`
  (`coletarVideos`): pula re-hash/re-extração quando ambos batem com o
  `media_file` já gravado para aquele `path`.
- Coluna `library_source.watch_enabled` + watcher `fsnotify` opcional por
  fonte, com debounce por fonte disparando um scan direcionado (não o
  pipeline completo de saúde) daquela fonte.
- Degradação graciosa: falha ao registrar watch (limite de inotify, fonte
  offline, mount que não suporta) nunca impede o boot nem interrompe outras
  fontes — só faz aquela fonte depender só do polling periódico.

**Fora (explicitamente):**
- Qualquer varredura de diretório ao vivo disparada por requisição HTTP — o
  job só lê o que o scan (assíncrono, já existente) já persistiu.
- Auto-selecionar uma arte quando NENHUMA foi resolvida ainda para aquele
  `kind` (nem via seleção manual, nem via o "vencedor" de precedência hoje
  calculado por `montarGruposArtwork`) — o job materializa o que **já** é a
  escolha vigente; nunca inventa uma escolha nova. Se não há nenhum candidato
  resolvido, o job não faz nada para aquele `kind` (fica para um enriquecimento
  prévio, fora deste briefing).
- Hash de conteúdo como fallback quando `mtime` é ambíguo/suspeito (ex.
  `size` igual mas o dono desconfia que o conteúdo mudou) — aceito como
  limitação conhecida (ver §6), não implementado.
- Watch recursivo "de fábrica" via alguma lib de alto nível — usamos
  `fsnotify` (sem recursão nativa) com registro manual por subpasta, como
  documentado.
- UI para o dono configurar o intervalo/cron do job (fica com o mecanismo
  genérico de edição de `job_definition` que o 0052 já expõe, se existir —
  não é um controle novo específico deste briefing).

## 4. Contrato técnico

### Dados / modelo

```sql
ALTER TABLE library_source ADD COLUMN watch_enabled INTEGER NOT NULL DEFAULT 0;
```

Seed de `job_definition` para o novo job (mesma migration ou seguinte,
seguindo o padrão do 0053): nome `midias_faltantes`, cron default
`0 4 * * *` (diário, 04:00 — fora do horário provável de uso, mesma lógica de
"tempos em tempos" pedida; ajustável depois pelo mecanismo já existente de
edição de `job_definition`).

Nenhuma tabela nova para o watcher — estado do watcher é **só em memória**
(mapa `source_id → *fsnotify.Watcher` + timer de debounce), reconstruído no
boot a partir de `library_source.watch_enabled`.

### API

- `PATCH`/`PUT` do endpoint já existente de edição de `library_source`
  (0008) ganha o campo `watch_enabled: boolean`.
- Nenhum endpoint novo dedicado ao job `midias_faltantes` — ele roda só pelo
  agendador (mesma superfície genérica de jobs do 0052: listar execuções,
  disparar manualmente, editar cron — já existe para qualquer `Handler`).

### Backend (Go)

- **Scan incremental** (`internal/scanner/scan.go`, função `coletarVideos` —
  nome confirmado por pesquisa no código): antes de abrir o arquivo para
  `hashDoArquivo`, buscar o `media_file` existente por `path` (já indexado,
  `path` é `UNIQUE`); se existir E `mtime` (via `os.Stat().ModTime()`,
  formatado igual ao já gravado) E `size` baterem exatamente com os valores
  já persistidos, PULAR o hash e a re-extração de mediainfo — reusar
  `content_hash`/`mediainfo_json` já gravados, só atualizar `last_seen`.
  Qualquer divergência (novo arquivo, `mtime` ou `size` diferentes) segue o
  caminho atual (hash + ffprobe completos). Isso é aceito como heurística
  "melhor esforço" (ver §6) — não uma garantia criptográfica de que o
  conteúdo não mudou.

- **Watcher** (`internal/watch/`, novo pacote):
  - No boot do servidor, para cada `library_source` com `watch_enabled=1` e
    `state='online'`: `filepath.WalkDir` uma vez registrando
    `watcher.Add(dir)` em CADA subdiretório (fsnotify não observa recursão
    nativa — confirmado em pesquisa). Falha em `Add()` de um subdiretório
    específico (ex. limite `fs.inotify.max_user_watches`) gera um log de
    aviso e CONTINUA para os demais — nunca aborta o registro da fonte
    inteira nem o boot do servidor.
  - Ao receber `fsnotify.Create` para um caminho que é diretório, chama
    `watcher.Add()` nele dinamicamente (cobre pastas novas criadas depois do
    boot).
  - Para qualquer evento (`Create`/`Write`/`Remove`/`Rename`) de uma fonte,
    reinicia um timer de debounce de **15 segundos** específico daquela
    `library_source` (mapa `source_id → *time.Timer`, coalescendo rajadas de
    eventos numa única disparada). Ao expirar, chama
    `scanner.Manager.Start(source)` **só daquela fonte** — não o pipeline
    completo do job de saúde (auto-match, recálculo de completude
    continuam só no cron normal do 0053).
  - Erros do canal `watcher.Errors` são logados; o watcher NUNCA para o
    servidor nem tenta se auto-recuperar em loop — a fonte simplesmente passa
    a depender só do polling periódico até o próximo restart.
  - `Close()` de todos os watchers no shutdown gracioso do servidor (mesmo
    padrão de lifecycle já usado pelos outros componentes long-running).
  - O handler `PATCH`/`PUT` de `library_source` (0008), ao mudar
    `watch_enabled` de `false→true`, chama a MESMA rotina de registro usada
    no boot para aquela fonte (sem reiniciar o servidor); de `true→false`,
    chama `Close()` no watcher daquela fonte e remove a entrada do mapa em
    memória. Nenhum dos dois casos exige restart.
  - Nova dependência: `github.com/fsnotify/fsnotify` (`go get`, ainda não
    está em `go.mod`).

- **Job `midias_faltantes`** (`internal/job/midiasfaltantes/`, mesmo padrão
  de interfaces pequenas injetadas do `internal/job/libraryhealth` — evita
  ciclo de import, conforme 0053): para cada `entity` com pelo menos um
  `media_file.state='present'`:
  - Para cada `kind` relevante (`poster`, `fanart`, `banner`, `clearlogo`,
    `discart`, `landscape`): pega o PRIMEIRO item do mesmo agregador que a
    galeria usa (`montarGruposArtwork`, 0044/0048/0049 — reusar, não
    duplicar a lógica de precedência). Se esse item já é um `asset` com
    `local_path` preenchido, pula. Se é um `asset` sem `local_path`, ou um
    candidato ainda não materializado como `asset` mas já resolvido/
    cacheável (mesmo caminho do endpoint `.../materialize` do 0062),
    materializa. Se não há NENHUM candidato para aquele `kind`, pula sem
    erro.
  - Se `entity.nfo_path` é nulo, gera o NFO (mesma função do 0062).
  - Processa todas as entities elegíveis numa única execução (sem paginação
    dedicada — o custo real é rede/download, não a iteração do banco);
    segue o mesmo padrão dos outros `Handler`s (sem timeout próprio, conforme
    já é o comportamento do `Manager`).

### Frontend (Vite/React)

- Formulário de edição de `library_source` (tela de admin de bibliotecas):
  novo toggle "Escutar mudanças desta pasta" (mapeia para `watch_enabled`).

### Arquivos afetados (previsão)

- `internal/db/migrations/00XX_watch_e_job_midias_faltantes.sql`
- `internal/scanner/scan.go` (short-circuit por mtime/size)
- `internal/watch/watch.go`, `internal/watch/watch_test.go`
- `internal/job/midiasfaltantes/midiasfaltantes.go` (+ testes)
- `internal/server/server.go` (wiring do watcher no boot/shutdown, `WithMidiasFaltantesJob` análogo ao `WithLibraryHealthJob`)
- `go.mod`/`go.sum` (fsnotify)
- `openapi.yaml` (campo `watch_enabled` no schema de `library_source`)
- `web/src/**` (toggle no formulário de biblioteca)
- `AGENTS.md` (ver abaixo)

## 5. Critérios de aceite

- [ ] Rodar o scan duas vezes seguidas sem tocar nos arquivos: a segunda
      rodada NÃO recalcula `content_hash` de nenhum arquivo já indexado
      (teste com contador/instrumentação de chamadas a `hashDoArquivo`).
- [ ] Alterar `mtime` de um arquivo já indexado (sem mudar conteúdo) faz o
      próximo scan re-hashear aquele arquivo especificamente.
- [ ] Entity com arte já resolvida (asset existente sem `local_path`, ou
      candidato de topo do agregador de artwork) e sem NFO: após rodar o job
      `midias_faltantes` manualmente, `local_path` e `nfo_path` passam a
      existir, sem nenhuma chamada de API externa feita pelo usuário.
- [ ] Entity sem nenhum candidato de arte resolvido para um `kind`: o job não
      quebra e não cria nada para aquele `kind`.
- [ ] Criar um arquivo novo dentro de uma pasta com `watch_enabled=1` dispara
      um scan daquela fonte em até ~15-30s (teste local, filesystem real —
      não em mount FUSE).
- [ ] Pasta com `watch_enabled=0` nunca recebe `watcher.Add()` — nenhum
      watch de filesystem é registrado para ela.
- [ ] Falha simulada de `watcher.Add()` (ex. path removido entre o walk e o
      add) não impede o registro das demais subpastas nem derruba o boot.
- [ ] `PATCH` de `library_source` com `watch_enabled: true` sobre uma fonte
      já em execução ativa o watcher para ela SEM precisar reiniciar o
      servidor (o handler chama a mesma rotina de registro do boot para
      aquela fonte); `watch_enabled: false` chama `Close()` no watcher
      daquela fonte e remove sua entrada do mapa em memória — nenhum dos dois
      casos exige restart.

## 6. Notas de implementação / decisões

- **Watch é aceleração, não fonte de verdade.** Confirmado por pesquisa:
  `rclone mount` sobre FUSE não tem como emitir evento de mudança originada
  do lado remoto (limitação de kernel/libfuse, não bug do rclone — issues
  rclone/rclone #4381 e #2882). Por isso o job periódico (scan incremental)
  é a base obrigatória; o watcher só reduz latência quando o SO consegue
  observar a mudança localmente.
- **`mtime`+`size` é heurística, não garantia.** Sob `rclone --vfs-cache-mode
  full`, o `mtime` só é atualizado localmente depois que o upload para o
  remoto termina (rclone/rclone#8894) — pode haver uma janela onde o scan
  ainda vê o `mtime` antigo. Aceitamos esse risco (é "melhor esforço", nunca
  pior que o comportamento atual que sempre re-hasheava tudo) em vez de cair
  de volta para hash sempre, que anularia o ganho de performance pedido.
- **`watch_enabled` desligado por padrão.** Fontes montadas via rede (a
  maioria dos casos reais do projeto, dado o deploy via rclone/gdrive) não
  ganham quase nada do watch — ligar por padrão só gastaria watches de
  inotify (`fs.inotify.max_user_watches`, limitado) à toa. O dono liga
  explicitamente quando sabe que a fonte é um disco local de verdade.
- **Debounce por fonte, 15s.** Evita disparar um scan por evento individual
  numa cópia de muitos arquivos de uma vez; 15s é curto o suficiente para
  "sentir" rápido e longo o suficiente para coalescer uma rajada típica.
- **Job reusa `montarGruposArtwork`, nunca reimplementa precedência.** A
  "arte vencedora" que o job materializa é literalmente a mesma que a UI já
  mostraria — garante que o disco nunca diverge do que o dono vê na tela.

**Atualizar em `AGENTS.md`** (seção "Práticas operacionais", após o bloco
"Convenções que quebram o CI se ignoradas.", linha ~112) — adicionar um novo
bloco:

> **Biblioteca em disco: scan/watch (desde o briefing 0064).**
> - O scan (`internal/scanner`) é **incremental**: só re-hasheia um arquivo
>   quando `mtime`/tamanho mudaram frente ao `media_file` já gravado. Não
>   reintroduza hash incondicional "pra garantir" — isso reverte o ganho de
>   performance que motivou o briefing.
> - `fsnotify` **não funciona sobre mounts FUSE remotos** (rclone/gdrive, o
>   deploy real do projeto) para mudanças originadas do lado remoto — é uma
>   limitação de kernel, não um bug a corrigir. Nunca trate o watch como
>   única fonte de verdade; o job periódico é a base, o watch só acelera.

## 7. Fora deste briefing / follow-ups

- Auto-seleção de candidato de arte quando nenhum foi resolvido ainda (fica
  para um enriquecimento automático mais amplo, se decidido).
- Hash de conteúdo como fallback de `mtime` ambíguo.
- UI de configuração de intervalo/cron por job (usa o mecanismo genérico do
  0052, se/quando ele expuser isso).
- Aplicar o mesmo watcher a mudanças de METADADO externo (ex. TMDB mudou o
  pôster) — este briefing só cobre mudanças no FILESYSTEM local.
