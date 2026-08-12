# 0101 — Fundação: binário yt-dlp (download dinâmico em runtime + path customizável)

> Entrega o downloader de YouTube que os briefings 0102 (trailer) e 0103 (tema
> musical) vão reaproveitar: gestão do binário yt-dlp (baixado sozinho pelo
> próprio processo Go na primeira necessidade) + config de path customizado.

| | |
|---|---|
| **ID** | 0101 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Trailer e música-tema (ver [12-roadmap](../docs/12-roadmap.md#fase-5--verticais-e-plugins-visão)) |
| **Status** | done |
| **Docs relacionadas** | [ADR-0025](../docs/adr/0025-trailer-tema-musical-ytdlp-themerrdb.md), [ADR-0023](../docs/adr/0023-integracoes-configuraveis-via-ui-precedencia-sobre-env.md) |
| **Depende de** | — |

---

## 1. Objetivo

TMDB/TVDB (`/videos`) e ThemerrDB só entregam **links do YouTube**, nunca o
arquivo. Baixar trailer/tema pro disco (briefings 0102/0103) exige um
downloader de vídeo — **yt-dlp**, a mesma dependência que o tinyMediaManager já
embute. Este briefing entrega SÓ a fundação: como o binário chega até o
container, como o admin pode assumir controle, e um jeito de saber se está
disponível — **sem nenhum download de trailer/tema ainda** (isso é o 0102/0103).
Decisão de arquitetura já fechada em [ADR-0025](../docs/adr/0025-trailer-tema-musical-ytdlp-themerrdb.md)
(TR1); este briefing só materializa.

## 2. Jornada do usuário

1. Instância nova sobe. Nenhuma ação do admin é necessária: na primeira vez que
   algo pedir o yt-dlp (0102/0103), o próprio MyMediaDB baixa o binário oficial
   sozinho, dentro do container, e passa a usá-lo — sem rebuild de imagem, sem
   volume novo.
2. O admin que quer controle abre `/dashboard/integrations` (ou uma seção nova
   equivalente — ver §3), vê o card "yt-dlp" com o status atual (`disponível
   (baixado automaticamente)` / `disponível (path customizado)` /
   `indisponível`) e um aviso curto sobre o risco de ToS do YouTube.
3. O admin com um binário próprio informa o **path customizado** no mesmo card;
   a partir daí esse path passa a ser usado (precedência sobre o baixado
   automaticamente), sem precisar reiniciar o processo.
4. Se o download automático falhar (rede fora, GitHub inacessível) e não houver
   path customizado, o status mostra `indisponível` com o motivo — trailer/tema
   automáticos ficam desligados, mas nada mais no produto quebra.

## 3. Escopo

**Dentro:**
- Pacote novo `internal/ytdlp`:
  - `Gerenciador` (ou nome equivalente) que resolve o binário utilizável, na
    ordem: **path customizado configurado** (banco → env, ver abaixo) → binário
    já baixado no diretório de dados (cache local) → **baixa agora** (só na
    primeira chamada; chamadas seguintes reusam o cache) → falha.
  - Download da release oficial do yt-dlp
    (`github.com/yt-dlp/yt-dlp/releases/latest`) pro binário correto de
    `runtime.GOOS`/`runtime.GOARCH` (o binário standalone Linux — único SO do
    container de produção — cobre o caso real; suportar Windows/macOS é
    bônus, não obrigatório aqui). Grava em `<diretório-de-dados>/bin/yt-dlp`,
    `chmod 0o755`, valida rodando `--version` antes de aceitar como bom.
  - `Baixar(ctx, urlOrigem, destino string) error`: roda
    `exec.CommandContext(ctx, bin, "-o", destino, urlOrigem)` — função genérica,
    sem saber se é trailer ou tema (0102/0103 chamam essa mesma função).
  - `Status(ctx) (disponivel bool, origem string, detalhe string)` — usado pelo
    endpoint de config e pelos endpoints de download dos briefings seguintes
    (que checam antes de tentar, e respondem 503 sem yt-dlp, nunca 500).
- `resolveYtdlpDir` em `cmd/mymediadb/main.go`, mesmo padrão de
  `resolveAssetsDir`/`resolveBackupDir` (`internal/ytdlp` recebe o path pronto,
  não resolve env sozinho — mantém o pacote testável sem tocar `os.Getenv`).
- Config de path customizado: **reaproveita `app_settings`** (mesma tabela já
  usada por idioma/retroativo/auto-materialize — ver `internal/server/settings.go`),
  chave nova `ytdlp` com `{"custom_binary_path": "..."}`. Precedência
  banco → env var `MYMEDIADB_YTDLP_PATH` → ausente — mesmo espírito do
  ADR-0023, sem depender do registro genérico de integrações (`internal/integration`,
  briefings 0095/0096) porque **esse registro ainda não existe** (status `draft`,
  não implementado — confirmado nesta pesquisa); reavaliar migração pra lá
  quando 0095/0096 forem feitos, sem regressão de contrato (mesma ressalva já
  aplicada ao token de webhook, briefing 0093).
- API: `GET /api/v1/settings/ytdlp` (status + path customizado atual + origem
  efetiva) e `PUT /api/v1/settings/ytdlp` (grava/limpa `custom_binary_path`) —
  mesmo par GET/PUT dos outros `/settings/*` já existentes.
- Frontend: card "yt-dlp" numa página de settings existente (mais perto de
  `/dashboard/integrations`, que já existe hoje mesmo sem o registro genérico —
  é só uma página React; ver `web/src/routes` para a rota atual) — status,
  campo de path customizado, aviso de ToS. Sem botão de "baixar agora" nesta
  entrega (o download é lazy, disparado pelo primeiro uso real em 0102/0103);
  o card só reflete estado.

**Fora (explicitamente):**
- Qualquer download de trailer ou tema — só a fundação (isso é 0102/0103).
- Suporte a Windows/macOS no binário nativo do MyMediaDB (o projeto roda em
  container Linux; se o binário compilar pra outro SO, o download automático
  pode falhar sem drama — cai pro caso "indisponível", path customizado sempre
  funciona como fallback do admin).
- Migrar para o registro genérico de integrações (0095/0096) — ainda não
  existe; ver nota acima.
- Atualização automática/periódica do yt-dlp já baixado (fica versão fixa até
  o processo reiniciar e refazer o download, ou até o admin apagar o binário
  em cache manualmente) — sem cron de atualização nesta entrega.
- Verificação de assinatura/checksum criptográfico do release (fica como
  hardening futuro; validar rodando `--version` com sucesso é o piso desta
  entrega).

## 4. Contrato técnico

### Dados / modelo
- Nenhuma migration nova — `app_settings` já existe (`internal/db/migrations/0005_app_settings_idioma.sql`).
  Chave nova `ytdlp`, valor `{"custom_binary_path": string|null}`.

### API
- `GET /api/v1/settings/ytdlp` → `{"disponivel": bool, "origem": "custom"|"baixado"|"nenhum", "custom_binary_path": string|null, "detalhe": string|null}`.
  `detalhe` carrega o motivo quando `disponivel=false` (ex.: "falha ao baixar: <erro>").
- `PUT /api/v1/settings/ytdlp` body `{"custom_binary_path": string|null}` → mesmo shape do GET após gravar.
  `null`/string vazia limpa o override (volta a herdar env var/download automático).
- Endpoints já cobertos: seguir exatamente o par `getAutoMaterializeArtworkSettings`/`putAutoMaterializeArtworkSettings`
  (`internal/server/settings.go:333-380`) como modelo de forma/erro.
- Atualizar `openapi.yaml` com o novo path (mesmo bloco `/settings/*` de linha ~248-300) e regenerar tipos do front (`gen:types`).

### Backend (Go)
- Novo pacote `internal/ytdlp/ytdlp.go` (+ teste): `Gerenciador`, `Baixar`, `Status`. Sem dependência de `internal/server` (evita ciclo — mesmo princípio de `internal/localmedia`).
- `cmd/mymediadb/main.go`: `resolveYtdlpDir()` (mesmo molde de `resolveAssetsDir`, linha ~272-285) + construção do `ytdlp.Gerenciador` passado ao `Server` via novo `Option` (`server.WithYtdlp(...)`, mesmo padrão de `WithSearchProvider`/`WithCodecRegistry`).
- `internal/server/settings.go`: `ytdlpSettings` struct + `obterYtdlpSettings`/`getYtdlpSettings`/`putYtdlpSettings`, mesmo molde das três outras settings já no arquivo. Resolve a precedência banco→env chamando `s.ytdlp.Status(ctx)` (o `Gerenciador` já recebeu o env var resolvido no boot; a settings API só troca o path customizado do banco em runtime — sem precisar reiniciar o processo, ao contrário do TMDB do 0095, porque aqui não há credencial de provider envolvida no boot, só um path de binário).

### Frontend (Vite/React)
- Card "yt-dlp" na página `/dashboard/integrations` existente (`web/src/routes` — localizar a rota atual antes de implementar; se a página não existir ainda como tal, criar seção equivalente mínima, sem duplicar o padrão de "página de integração completa" dos briefings 0095/0096 que ainda não existem).
- Componente de formulário simples (path customizado + botão salvar) + badge de status, sem necessidade de TanStack Form completo (é um campo só).

### Arquivos afetados
- Novo: `internal/ytdlp/ytdlp.go`, `internal/ytdlp/ytdlp_test.go`.
- Editar: `cmd/mymediadb/main.go`, `internal/server/server.go` (novo campo + `Option`), `internal/server/settings.go`, `openapi.yaml`, rota de integrações do frontend.

## 5. Critérios de aceite

- [x] Sem `custom_binary_path` e sem binário em cache: primeira chamada a `Gerenciador.Baixar` baixa o yt-dlp pro diretório de dados, valida com `--version`, e reusa em chamadas seguintes (sem re-baixar). Coberto por `TestResolverBaixaNaPrimeiraChamadaEReusa` (`internal/ytdlp/ytdlp_test.go`), incluindo reuso após "reinício" simulado (novo `Gerenciador` no mesmo diretório).
- [x] Com `MYMEDIADB_YTDLP_PATH` setado (env), `Status` reporta `origem="custom"` usando esse path, sem tentar baixar. Coberto por `TestResolverCustomPathEnvVenceSemBaixar` (pacote) e `TestGetYtdlpSettingsOrigemEnv` (endpoint).
- [x] Com `custom_binary_path` gravado via `PUT /settings/ytdlp` (banco), esse path vence sobre a env var — precedência idêntica ao ADR-0023. Coberto por `TestSetCustomPathBancoVenceSobreEnv` (pacote) e `TestPutYtdlpSettingsBancoVenceSobreEnv`/`TestPutYtdlpSettingsLimpaVoltaParaEnv` (endpoint, efeito imediato sem reiniciar).
- [x] Falha de download (sem rede) faz `Status` reportar `disponivel=false` com `detalhe` explicando o motivo — nenhum outro endpoint do produto quebra ou retorna 500 por causa disso. Coberto por `TestStatusFalhaSemRede` (servidor HTTP local fechado, nunca a internet real) e `TestGetYtdlpSettingsSemGerenciador` (o endpoint sempre responde 200 com o estado, nunca 500).
- [x] `GET/PUT /settings/ytdlp` seguem o mesmo contrato de erro/forma dos outros `/settings/*`. Coberto por `TestPutYtdlpSettingsCorpoMalformado` (400 `invalid_body`) + o par GET/PUT espelha `getAutoMaterializeArtworkSettings`/`putAutoMaterializeArtworkSettings` linha a linha.
- [x] `go build ./... && go vet ./... && go test ./... -count=1` verdes; `tsc -b`/lint/`test:coverage` do web verdes. Confirmado nesta implementação (ver relatório do agente).

## 6. Notas de implementação / decisões

- **Por que não usar o registro genérico de integrações (0095/0096)?** Porque
  ele ainda não existe no código (status `draft`, confirmado por busca no
  repo) — depender dele infla este briefing pra implementar uma fundação
  inteira só pra um campo de path. `app_settings` já é usado para múltiplas
  configs independentes (idioma, throttle, auto-materialize) sem passar pelo
  registro genérico — path do yt-dlp segue o mesmo precedente. Quando
  0095/0096 forem implementados, migrar fica como follow-up sem quebrar
  contrato (mesma postura já registrada pro token de webhook, briefing 0093).
- **Por que download em runtime pelo próprio Go, não Dockerfile/entrypoint?**
  Decisão já fechada no ADR-0025 (confirmada explicitamente pelo dono): mantém
  a imagem enxuta, atualizável sem rebuild, e não introduz um script de shell
  a mais pra manter.
- Diretório de dados do yt-dlp: usar o MESMO padrão de `resolveAssetsDir`
  (dirname do DB + subpasta, ex.: `<dirname(DB)>/bin/`) — reaproveita o volume
  que já existe hoje, sem provisionar nada novo no compose de produção.
- **Validação final (agente dedicado, `make gate-local` completo)**: a
  primeira rodada acusou 3 regressões reais, todas dos três briefings
  0101/0102/0103 juntos — corrigidas antes do commit: (1) 1 issue de
  golangci-lint (`errcheck` num `fmt.Fprintf` de teste em
  `internal/ytdlp/ytdlp_test.go`, corrigido descartando o retorno
  explicitamente); (2) duplicação web acima do baseline em
  `settings.test.tsx`/`rich-detail-view.test.tsx` (boilerplate de teste
  repetido), reduzida com helpers compartilhados (`corpoDoPut`,
  `stubFetchComYtdlp`); (3) cobertura E2E (jornadas, `internal/e2e`) abaixo do
  baseline — os pacotes novos (`internal/ytdlp`, `internal/provider/themerrdb`)
  não são exercitados pelas jornadas HTTP black-box por design (o harness não
  injeta um `ytdlp.Gerenciador` real nem um provider ThemerrDB, pra nunca
  tocar rede real) — recuperada com duas jornadas novas
  (`internal/e2e/journeys_trailer_tema_test.go`) cobrindo a degradação em
  silêncio (sem yt-dlp/candidato) e a detecção de trailer/tema já colocados à
  mão na pasta. De passagem, `postEntityTrailerDownload` foi refatorada
  (extraídos `resolverVideoTrailer`/`resolverDestinoTrailerLivre`) pra manter
  gocyclo ≤10 nas funções tocadas.

## 7. Fora deste briefing / follow-ups

- 0102 (trailer) e 0103 (tema musical) consomem `ytdlp.Gerenciador.Baixar`/`Status` diretamente.
- Atualização automática de versão do yt-dlp — follow-up.
- Migração pro registro genérico de integrações quando 0095/0096 existirem — follow-up.
