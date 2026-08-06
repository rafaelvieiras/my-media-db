# AGENTS.md — orientação para agentes (humanos e LLM)

> Leia isto **antes de codar**. É o ponto de entrada: princípios que não se
> quebram, práticas operacionais que evitam retrabalho, e por onde seguir. Este
> arquivo **aponta** para as fontes autoritativas — não as duplica. Quando algo
> aqui divergir de uma doc/ADR/briefing, a fonte específica vence e este arquivo
> deve ser corrigido.

## O que é o projeto (em uma frase)

MyMediaDB é um **gestor de biblioteca de mídia local-first** ao estilo
tinyMediaManager: cataloga, enriquece e **baixa artes/legendas/trailers/temas
para o DISCO** (não é só um DB). Backend Go (chi + `modernc.org/sqlite` + sqlc +
goose), frontend Vite/React (TanStack Router/Query, shadcn/ui). Módulo
`github.com/rafaelvieiras/my-media-db`.

## Princípios que não se quebram

Os cinco primeiros estão no [README](README.md) e no
[CONTRIBUTING](CONTRIBUTING.md); o sexto é diretriz firme do dono do projeto.

1. **O dono é a autoridade.** O banco local é a fonte de verdade; dado externo é
   sugestão. Toda edição do usuário é um **override** que vence sempre.
2. **O banco é maior que a biblioteca.** Cataloga o que se tem *e* o que ainda
   não se tem (via `status`) — nunca gambiarra.
3. **Universal e extensível sem tocar no núcleo.** Verticais e integrações vivem
   em `extension_data` *namespaced*, `entity.type` livre e `entity_relation`
   genérica com `kind`. **Nunca** colunas ou tabelas de junção por-tipo. Uma
   vertical nova (música: artista/álbum; videoclipe: artista) entra **sem
   migration do core**.
4. **Local-first e leve.** Um binário, uma imagem, memória mínima. Fontes
   externas são enriquecimento **opcional** e **puxado** (pull, nunca sync
   automático).
5. **Não-destrutivo.** Nada apaga dado do dono automaticamente; purga é sempre
   explícita.
6. **Independência de provider + enriquecimento por capacidade.** A mídia
   sobrevive a **qualquer** provider registrado. Se o admin cadastrou só uma
   chave (TMDB *ou* TVDB), ele pesquisa **e importa** filmes **e** séries por
   ela; um provider não modelar um campo apenas o deixa vazio — **nunca** bloqueia
   a vertical inteira. Com múltiplos providers, eles se **cruzam e enriquecem**.
   Providers são declarados por **capacidade** (interfaces opcionais:
   `SerieProvider` = árvore, `SerieRicoProvider` = enriquecimento,
   `ColecaoProvider`, e futuramente `ArteProvider` etc.), descobertas via
   type-assert e degradadas em silêncio quando ausentes. Isso **fecha** a decisão
   aberta "universal scraper vs escolha explícita" ([docs/11](docs/11-decisoes-abertas.md))
   e supera o "MVP: TMDB" de [docs/07](docs/07-enriquecimento-providers.md).

Um PR/patch que contraria um princípio provavelmente será recusado.

## Como o projeto trabalha (decisão antes de código)

- **Dirigido por briefings.** Nada se implementa sem um
  [briefing](briefings/README.md) — ordem de serviço autocontida, uma por
  feature/fix. Comece pelo briefing correspondente.
- **Decisões de fundação viram ADR.** Mudança estrutural (schema, resolver, API,
  princípio) → propor um **ADR** em [docs/adr/](docs/adr/) (e superar o anterior,
  não editar a spec direto). A [agenda de decisões](docs/00-agenda-de-decisoes.md)
  é o backlog vivo.
- **As `docs/` descrevem *como a aplicação é*; os briefings, *o que construir
  agora*.** Leia [docs/03 arquitetura](docs/03-arquitetura.md) e
  [docs/04 modelo de dados](docs/04-modelo-de-dados.md) para o mapa.

## Arquitetura em três linhas

- **Resolver de 3 camadas** (`internal/resolver`): `override ⟵ externo ⟵
  arquivo`, mesclado por **classe de campo** e por **namespace** (não por nome
  fixo) — campo novo só precisa de uma entrada no registro. Camadas externas
  chegam ordenadas por prioridade.
- **Providers** (`internal/provider`): core mínimo (`Nome/Buscar/BuscarDetalhe/
  MapearDetalhe`) + capacidades opcionais. Registry N-providers com ordem =
  prioridade. Enriquecimento cruzado grava uma linha `media_external` de outra
  `source` sem clobbar a identidade da fonte-base (ver briefing 0045).
- **Artes** (`asset`): `kind` é TEXT livre no banco (poster/fanart/still hoje;
  extensível a clearlogo/cdart/banner). Precedência: preferida→user→external→
  base→404. Desde o briefing 0062, a arte "preferida" (`is_principal`) pode
  também estar **materializada na pasta real da biblioteca** (`asset.local_path`,
  distinto do cache em `assetsDir`/`path_or_uri`) com nome canônico
  Kodi/Emby — e a entity pode ter um `.nfo` exportado (`entity.nfo_path`).
  NFO é exportação one-way (banco → disco); nunca é relido além do id
  determinístico que o scanner já usa. Ver [docs/08](docs/08-artwork-e-assets.md)
  e briefings 0044, 0062.

## Práticas operacionais (o que evita retrabalho)

**Valide de verdade — o cache mente.**
- `go build ./...` • `go vet ./...` • `go test ./... -count=1` (o `-count=1` é
  obrigatório: `go test` cacheia e mascara falhas). Relógio de teste ancora no
  tempo real, nunca em data fixa.
- Web: `tsc -b` é **zero-tolerância**; `npm run lint`; `npm run test:coverage`.
- Catraca: `make gate-local`. Ela só deixa as métricas **melhorarem**
  (ratchet) e agora é **sensível ao diff** — só roda os coletores do
  lado (Go/Web) que seu diff realmente tocou; "pulada" nas métricas do
  outro lado é esperado, não é bug. **NUNCA rode `make baseline`** — só
  o dono avança o baseline. gocyclo é aviso não-bloqueante (mantenha
  funções novas ≤10 mesmo assim).
- Durante a implementação (iterando numa correção), use
  `make gate-fast` — mesmo filtro por diff, sem golangci-lint/
  govulncheck/gocyclo/jscpd/npm audit. Reserve o `make gate-local`
  completo para **uma** rodada final, logo antes do commit/push — não
  repita o gate completo "pra confirmar".
- Se um coletor da catraca falhar (não "achou issues" — falhou de
  rodar), o terminal mostra só as últimas linhas; a saída completa fica
  em `reports/<metrica>-failure.log` — leia esse arquivo antes de
  tentar adivinhar a causa pela saída truncada.
- Após trabalho de subagente, os erros do `new-diagnostics`/gopls costumam ser
  **stale/falsos** ("não achou módulos", `sqlcgen ... undefined`). Confirme com
  `go build` real antes de acreditar.

**Convenções que quebram o CI se ignoradas.**
- **sqlc v1.31 corrompe acento** em `.sql` de query → queries em **ASCII**;
  migrations podem ser pt-BR.
- Arquivos gerados (`gen:types`, sqlc) devem sair **já formatados** (o hook do
  prettier roda da raiz; `web/.prettierignore` não vale no commit).
- Dep nova em `web/package.json` só aparece após
  `docker compose -f compose.dev.yaml restart web` (node_modules é volume
  anônimo). `package-lock.json` precisa de npm ≥ 11.16, senão `npm ci` quebra.
- **Multi-idioma é fundacional**: título/overview/tagline por idioma via
  `media_external`; não assuma idioma único.

**Biblioteca em disco: scan/watch (desde o briefing 0064).**
- O scan (`internal/scanner`) é **incremental**: só re-hasheia um arquivo
  quando `mtime`/tamanho mudaram frente ao `media_file` já gravado. Não
  reintroduza hash incondicional "pra garantir" — isso reverte o ganho de
  performance que motivou o briefing.
- `fsnotify` **não funciona sobre mounts FUSE remotos** (rclone/gdrive, o
  deploy real do projeto) para mudanças originadas do lado remoto — é uma
  limitação de kernel, não um bug a corrigir. Nunca trate o watch como única
  fonte de verdade; o job periódico é a base, o watch só acelera.

**Commits e comunicação.**
- Conventional Commits em **pt-BR** (`feat:`/`fix:`/`docs:`/`refactor:`…),
  imperativo, **assunto ≤ 100 chars** (o hook `commit-msg` recusa acima).
  Finalize com `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`.
- Escreva docs, comentários e mensagens em **pt-BR com acentuação correta**.
- **Segredos nunca no código** — chaves de API só por variável de ambiente.

**Escala e contexto.**
- Delegue pesquisa/implementação/validação a **subagentes** e receba
  resumos — rodar a catraca **completa** repetidas vezes dentro de uma
  única implementação é o maior desperdício de tempo/contexto; prefira
  `make gate-fast` ao iterar e reserve `make gate-local` completo para
  o final (ver acima).
- **A catraca completa (`make gate-local`) só roda no agente dedicado de
  validação/commit/push — nunca dentro de um agente de implementação.**
  Rodá-la em cada agente de implementação duplica uma saída verbosa que a
  etapa final já vai rodar de novo. Agente de implementação valida com
  `go build`/`go vet`/`go test -count=1` (escopado aos pacotes tocados
  durante a iteração; `./...` só na passada final dele) e, se quiser,
  `make gate-fast`; `make gate-local` fica reservado pro agente final.
- **Tarefa grande e multi-camada → pipeline de agentes estreitos por
  camada, não um agente monolítico.** No briefing 0084, um único agente de
  backend cobrindo 10 subsistemas (migration+sqlc, algoritmo, capacidade
  de provider, 4 pontos de integração, scanner, testes) consumiu ~349k
  tokens/206 tool-calls/~30min; o agente de frontend do mesmo briefing,
  com escopo de 3 arquivos, consumiu 77k. Prefira quebrar por camada (ex.:
  dados/sqlc → algoritmo/provider → integração/threading → testes), cada
  agente com brief detalhado e escopo de arquivo estreito.
- Ambiente de dev: `docker compose -f compose.dev.yaml up` (web :5173, api :8080
  com hot-reload via air). DB em `tmp/mymediadb.sqlite`.

## Por onde seguir agora

- **Índice de briefings:** [briefings/README.md](briefings/README.md).
  **Roadmap:** [docs/12](docs/12-roadmap.md).
- **Frente atual — independência de provider:** briefings **0046** (série sem
  TVDB: TMDB como provedor de árvore), **0047** (robustez de fonte-default) e
  **0048** (fundação de providers de arte/trailer/tema para fanart.tv etc.).
- **Próximas verticais previstas** (via providers/plugins, sem tocar o núcleo):
  fanart.tv (arte dedicada), trailer, tema musical; depois videoclipe e música.
