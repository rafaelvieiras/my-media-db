# 0104 — Trailer/tema: detecção em massa, job periódico e gatilho automático no scan

> 0102/0103 deram um botão manual por entity. Este briefing fecha o pedido do
> dono: achar/baixar trailer e tema **em massa** e integrar isso às rotinas de
> scan/sync e ao sistema de jobs — sem inventar mecanismo novo, reaproveitando
> o molde já validado de `midias_faltantes` (job) e `autoMaterializeArtworkSettings`
> (toggle de scan automático).

| | |
|---|---|
| **ID** | 0104 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Trailer e música-tema |
| **Status** | done |
| **Docs relacionadas** | [ADR-0026](../docs/adr/0026-trailer-tema-job-e-gatilho-automatico-no-scan.md), [ADR-0025](../docs/adr/0025-trailer-tema-musical-ytdlp-themerrdb.md) |
| **Depende de** | 0101, 0102, 0103 |

---

## 1. Objetivo

0102/0103 só resolvem "eu, dono, abro UM item e clico em baixar". Numa
biblioteca com centenas de itens isso não escala, e o dono pediu explicitamente
que isso vire parte da rotina normal (scan/sync) e do sistema de jobs — mesma
experiência que já existe hoje para arte/NFO via `midias_faltantes` (0064) e
para enriquecimento via o toggle de auto-materializar artes (0089).

## 2. Jornada do usuário

**Job periódico (detecção + download em massa):**
1. O dono abre `/dashboard/jobs`, vê o job novo "Trailer e tema musical
   faltantes" (desabilitado por padrão, como todo job novo do sistema).
2. Liga o agendamento (ou clica "rodar agora").
3. O job varre a biblioteca, baixa trailer/tema pra quem tem candidato
   disponível e ainda não tem o arquivo local; itens sem candidato (TMDB sem
   trailer, ThemerrDB sem tema) ficam de fora, sem erro.
4. O histórico do job mostra quantos trailers/temas foram baixados na
   execução.

**Gatilho automático no scan:**
1. O dono abre `/dashboard/settings`, liga "Baixar trailer e tema
   automaticamente" (mesma seção do card yt-dlp já existente).
2. Ao importar/auto-casar um item novo (scan ou watch), o sistema tenta
   trailer/tema em background, sem atrasar a resposta do scan.
3. Se a tentativa falhar por qualquer motivo (yt-dlp ainda sem binário, rate
   limit, candidato momentaneamente indisponível), o job periódico (se ligado)
   cobre depois — o dono não precisa saber disso, só eventualmente vê o
   arquivo aparecer.

## 3. Escopo

**Dentro:**
- Extrair de `postEntityTrailerDownload`/`postEntityThemeDownload`
  (`internal/server/localmedia_handlers.go`) uma versão pura (sem
  `http.ResponseWriter`) de "baixar trailer de UMA entity" e "baixar tema de
  UMA entity", reaproveitando o que já existe (`baixarTrailerAtomico`,
  `escolherTrailerYoutube`, `baixarTemaAtomico`, `candidatoTemaURL`,
  `resolverDestino`, `encontrarArquivoTrailer`/`encontrarArquivoTema`,
  `s.ytdlp.Status`/`Baixar`).
- Job novo `internal/job/trailertema` (nome curto: `trailer_tema_faltantes`),
  registrado via `server.WithTrailerTemaJob()` (molde de
  `WithMidiasFaltantesJob`, `internal/server/server.go:253-264`), seed de
  `job_definition` com `enabled=0` (molde de `0016_watch_e_midias_faltantes.sql`).
  Itera `ListarEntitiesComMediaFilePresente` (já existe), best-effort por
  entity, checa `s.ytdlp.Status` uma vez por execução (não por entity).
- Toggle novo em `app_settings` (`autoDownloadTrailerTemaSettings{Enabled bool}`,
  chave `"baixar_trailer_tema_automaticamente"`), molde 1:1 de
  `autoMaterializeArtworkSettings` (`internal/server/settings.go`): struct,
  default `false`, cache (+entrada em `invalidarSettings`), rotas
  `GET/PUT /api/v1/settings/auto-download-trailer-tema`.
- Gatilho no scan: no mesmo ponto de `MaterializarSeAutoDownloadLigado`
  (`internal/server/midias_faltantes.go:161`), se o toggle novo estiver
  ligado, disparar `go s.tentarTrailerTemaAutomatico(context.Background(), entityID)`
  (nome final a critério da implementação) — best-effort, **nunca bloqueia** a
  resposta HTTP do scan/import. Contexto próprio (não o do request, que morre
  quando a resposta é enviada), com timeout generoso (ex.: 2 min) e log de
  erro (sem propagar).
- Frontend: novo switch em `/dashboard/settings` (seção "Trailer e tema
  musical", ao lado do card "yt-dlp" já existente), mesmo padrão de
  `AutoMaterializeArtworkSwitch` (grava a cada mudança, sem botão salvar).
- `openapi.yaml`: novo par de rotas de settings, seguindo o padrão de
  `/settings/auto-materialize-artwork`.

**Fora (explicitamente):**
- Não criar coluna de banco "tem trailer/tema local" — estado continua
  derivado via `os.Stat`, mesmo padrão de `midias_faltantes` (evita
  divergência banco↔disco, é o padrão já estabelecido no projeto).
- Não expor contador "N sem trailer/N sem tema" em `health_system.go` ou no
  resumo do `saude_biblioteca` — o histórico de execução do job novo já dá
  visibilidade suficiente (quantos baixados por execução); adicionar isso é
  follow-up se o dono sentir falta.
- Não validar "o link do YouTube ainda existe" antes de tentar baixar — nem
  hoje (0102/0103) isso é feito; falha de download já é tratada (502
  `youtube_download_failed`), sem mudança de contrato aqui.
- Não separar o toggle em dois (trailer vs. tema) — um único toggle cobre os
  dois, decisão deste briefing (ver §6).

## 4. Contrato técnico

### Dados / modelo
- `app_settings` ganha chave `"baixar_trailer_tema_automaticamente"` — mesmo
  shape/mecanismo de `"materializar_artes_automaticamente"`, sem migração de
  schema (é `app_settings`, chave-valor).
- Migration nova (molde de `internal/db/migrations/0016_watch_e_midias_faltantes.sql`)
  seedando `job_definition` para `trailer_tema_faltantes`: `enabled=0`, cron
  sugerido `'30 4 * * *'` (meia hora depois do `midias_faltantes`, para não
  concorrer pelo mesmo I/O/rede).

### API
- `GET/PUT /api/v1/settings/auto-download-trailer-tema` — corpo
  `{ "enabled": bool }`, mesmo formato de `auto-materialize-artwork`.
- Job novo aparece automaticamente em `GET /api/v1/jobs` e ganha
  enable/disable/run-now/histórico de graça via o sistema de jobs existente —
  **nenhuma rota nova de job precisa ser escrita**, só o registro do Handler.

### Backend (Go)
- `internal/job/trailertema/trailertema.go` (novo pacote, molde de
  `internal/job/midiasfaltantes/midiasfaltantes.go`): `Handler` com
  `Nome() string` (`"trailer_tema_faltantes"`) e `Executar(ctx, cfg) (string, error)`;
  interfaces pequenas injetadas (`EntityLister`, e uma nova
  `TrailerTemaBaixador` com os dois métodos puros do item abaixo) para evitar
  ciclo de import — mesmo padrão de `midiasfaltantes.go:28-42`.
- `internal/server/localmedia_handlers.go`: extrair
  `func (s *Server) baixarTrailerEntity(ctx context.Context, entityID string) (baixado bool, err error)`
  e `func (s *Server) baixarTemaEntity(ctx context.Context, entityID string) (baixado bool, err error)`
  a partir do corpo hoje acoplado a HTTP de `postEntityTrailerDownload`/
  `postEntityThemeDownload`; os handlers HTTP passam a chamar essas funções e
  só traduzem o retorno pra status code — **sem duplicar lógica**.
- `internal/server/server.go`: `WithTrailerTemaJob()` (molde de
  `WithMidiasFaltantesJob`, `:253-264`), injeta `trailertema.New(s, s)`.
- `internal/server/midias_faltantes.go`: no ponto de
  `MaterializarSeAutoDownloadLigado` (`:161`), somar a checagem do toggle novo
  e o disparo em goroutine best-effort (context próprio, timeout, log-only em
  erro).
- `internal/server/settings.go`: nova struct/chave/cache/GET/PUT, mesmo molde
  de `autoMaterializeArtworkSettings` (`:83-92`, `:211-236`, `:434-481`); somar
  entrada em `invalidarSettings` (`:306-313`).
- `cmd/mymediadb/main.go`: chamar `server.WithTrailerTemaJob()` junto de
  `WithMidiasFaltantesJob()`/`WithWatch()` (`:209`).

### Frontend (Vite/React)
- `web/src/lib/api.ts`: `putAutoDownloadTrailerTemaSettings` + tipo
  `AutoDownloadTrailerTemaSettings`, molde de `putAutoMaterializeArtworkSettings`.
- `web/src/lib/queries.ts`: `autoDownloadTrailerTemaSettingsQueryOptions`.
- `web/src/routes/settings.tsx`: nova seção "Trailer e tema musical" (pode
  ficar entre "Artes" e "yt-dlp", ou logo acima do card "yt-dlp" — critério de
  quem implementar) com `AutoDownloadTrailerTemaSwitch`, molde exato de
  `AutoMaterializeArtworkSwitch`.
- Job novo aparece automaticamente na tela `/dashboard/jobs` já existente —
  nenhuma tela nova.

### Arquivos afetados (previsão)
- Novo: `internal/job/trailertema/trailertema.go` (+ teste).
- Novo: migration `internal/db/migrations/00NN_seed_trailer_tema_job.sql`.
- Editar: `internal/server/localmedia_handlers.go`, `internal/server/server.go`,
  `internal/server/settings.go`, `internal/server/midias_faltantes.go`,
  `cmd/mymediadb/main.go`, `openapi.yaml`, `web/src/lib/api.ts`,
  `web/src/lib/queries.ts`, `web/src/routes/settings.tsx`.

## 5. Critérios de aceite

- [x] Job `trailer_tema_faltantes` aparece em `/dashboard/jobs`, desabilitado
      por padrão; "rodar agora" baixa trailer/tema faltante de itens com
      candidato disponível, sem erro para itens sem candidato.
- [x] Rodar o job de novo sobre a mesma biblioteca é idempotente (não
      rebaixa o que já está no disco — reusa `encontrarArquivoTrailer`/
      `encontrarArquivoTema`).
- [x] Toggle "Baixar trailer e tema automaticamente" em `/dashboard/settings`
      persiste via PUT, default `false`.
- [x] Com o toggle ligado, importar um item novo (via scan) resulta,
      eventualmente (best-effort, sem bloquear a resposta do scan), no
      trailer/tema local quando há candidato — e a resposta HTTP do scan não
      fica mais lenta por causa disso (medir: request não espera o
      download).
- [x] Com o toggle desligado (default), nenhum download automático acontece
      no scan — só o botão manual (0102/0103) e o job (se o dono ligar
      manualmente) materializam.
- [x] `go build ./... && go vet ./... && go test ./... -count=1` e
      `make gate-local` limpos.

## 6. Notas de implementação / decisões

- **Um toggle só, não dois** (trailer vs. tema separados): os dois
  compartilham o mesmo downloader (yt-dlp) e a mesma motivação ("automatizar o
  que hoje é botão manual"); separar é complexidade de UI sem pedido explícito
  do dono. Se no futuro o dono quiser controle fino (ex.: tema tem cobertura
  bem menor via ThemerrDB e pode gerar mais "tentativas vazias"), separar os
  dois toggles é um follow-up de baixo custo (mesmo molde, só duplicar).
- **Por que job separado de `midias_faltantes`, não fundido nele**: já
  recomendado na pesquisa que embasou a ADR-0025 — trailer/tema tem custo de
  rede/risco de ToS diferente de arte (fanart.tv/TMDB via API key autorizada);
  manter jobs separados deixa o dono ligar um sem o outro e não muda o
  comportamento default de `midias_faltantes` (que já roda pra quem já tinha
  ligado antes desta feature existir).
- **Por que goroutine no scan, não chamada síncrona**: download de vídeo via
  yt-dlp é ordens de magnitude mais lento que uma imagem (registrado no
  briefing 0102 §6); bloquear o request HTTP do scan por isso violaria a
  responsividade que o resto do scan já garante. Ver [ADR-0026](../docs/adr/0026-trailer-tema-job-e-gatilho-automatico-no-scan.md)
  para o raciocínio completo (inclusive por que isso não viola o princípio 4
  do `AGENTS.md`).
- **Cron do job novo 30min depois do `midias_faltantes`**: puramente para não
  concorrer pelo mesmo I/O de rede/disco na mesma janela; não há dependência
  funcional entre os dois.

## 7. Fora deste briefing / follow-ups

- Contador "N sem trailer/N sem tema" em `saude_biblioteca`/`health_system.go`
  — considerar se o histórico de execução do job novo não for visibilidade
  suficiente na prática.
- Separar o toggle único em dois (trailer/tema independentes) — só se o dono
  pedir depois de usar.
- Revalidar periodicamente se um link de YouTube já baixado ainda é o
  "melhor" candidato (ex.: trailer oficial substituído por um teaser mais
  recente no TMDB) — hoje o candidato nunca é re-checado uma vez baixado.
