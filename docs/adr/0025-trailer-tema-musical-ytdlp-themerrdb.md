# ADR-0025 — Trailer e música-tema: yt-dlp aceito (binário dinâmico + path custom) + ThemerrDB

- **Status:** Aceita
- **Data:** 2026-08-12
- **Agenda:** [TR1–TR2](../00-agenda-de-decisoes.md#trailer-e-música-tema--fundacional-levantada-a-partir-de-pesquisa-sobre-paridade-com-tinymediamanager) · **Spec:** [07](../07-enriquecimento-providers.md), [08](../08-artwork-e-assets.md)

## Contexto

O dono pediu paridade com o tinyMediaManager (TMM) em duas frentes que hoje o
MyMediaDB não cobre: baixar **trailer** de filme/série pro disco, e baixar a
**música-tema** (o `theme.mp3` que Kodi/Jellyfin tocam ao navegar a biblioteca —
não a trilha sonora completa). Pesquisa dedicada (12/08/2026) levantou os fatos
que fecham esta decisão:

- **TMDB `/movie|tv/{id}/videos`** já é consumido hoje (`internal/provider/tmdb`,
  extraído por `internal/richfields`) e devolve `type`/`site`/`key` — mas **nunca
  o arquivo**: é sempre um link/id de vídeo no YouTube (raramente Vimeo). TVDB v4
  (`trailers`) segue o mesmo padrão. Não existe fonte que sirva o binário do
  trailer diretamente.
- O **tinyMediaManager** resolve isso hoje integrando o **yt-dlp** como binário
  (bundled desde a v5.1.2, com suporte a cookies pra evitar bloqueio de bot) —
  não existe outro caminho de facto no ecossistema Kodi/Jellyfin/Plex/TMM.
- **Música-tema** não tem campo em nenhum provider oficial (TMDB, TVDB,
  fanart.tv). A única fonte viva e mantida encontrada é o **ThemerrDB**
  (`github.com/LizardByte/ThemerrDB`, BSD-3-Clause): JSON estático publicado
  diariamente, indexável por `tmdb`/`imdb` (filme) ou só `tmdb` (série), curado
  por issue no GitHub. O campo devolvido (`youtube_theme_url`) é, de novo, **um
  link do YouTube** — não um arquivo de áudio. É a fonte que alimenta os plugins
  irmãos oficiais Themerr-plex/-jellyfin/-kodi.
- Conclusão inevitável: **trailer e música-tema compartilham a mesma peça de
  infraestrutura que falta** — um downloader de YouTube. É também a única peça
  desta ADR que foge do padrão de todos os providers já registrados no projeto
  (TMDB/TVDB/fanart.tv são uso **autorizado** via API key própria); baixar do
  YouTube via yt-dlp funciona tecnicamente, mas **contraria os Termos de Serviço
  do YouTube** — prática de-facto e tolerada há anos em todo o ecossistema
  Kodi/Jellyfin/Plex/TMM, mas um risco categoricamente diferente dos demais.
- Investigado também e **descartado como fora de escopo**: o recurso "skip
  intro" do Jellyfin (plugin Intro Skipper) não é um asset baixado de provider —
  é fingerprinting de áudio (chromaprint) rodando sobre os próprios arquivos de
  vídeo já catalogados, comparando episódios da mesma série. Não tem relação com
  providers externos; se vier a ser implementado, é vertical de análise local
  (scanner/ffprobe), não desta ADR.

## Decisão

1. **Aceitar yt-dlp como dependência externa opcional**, usada como o único
   downloader de vídeo/áudio do YouTube do projeto — reaproveitada tanto pra
   materializar trailer (fonte: TMDB/TVDB `/videos`, já extraído hoje) quanto
   música-tema (fonte: ThemerrDB). Downloads são sempre **pull explícito**
   (botão "baixar trailer"/"baixar tema" ou job que o admin liga de propósito),
   nunca automático a cada scan — princípio 4 (nunca sync automático).
2. **Estratégia de binário — download em runtime, pelo próprio processo Go,
   dentro do container:**
   - **Download sob demanda**: na primeira necessidade (1º boot ou 1º uso da
     feature), o **próprio binário do MyMediaDB** (não um script de entrypoint,
     não um estágio de build) baixa o binário oficial do yt-dlp pro
     SO/arquitetura do container a partir do release oficial do projeto e grava
     no **mesmo volume de dados que a aplicação já usa hoje** (a mesma área
     onde ficam o SQLite e os assets — nenhum volume novo a provisionar),
     validando a integridade antes de usar. **Não entra na imagem Docker no
     build** (`docker build`/Dockerfile não muda) — mantém o binário/imagem do
     MyMediaDB leve (princípio 4) e o yt-dlp atualizável sem rebuild de imagem;
     mas o download em si acontece **dentro do container em execução**, de
     forma totalmente automática, sem exigir nenhuma ação do admin (a opção de
     path customizado abaixo é só para quem QUER assumir o controle).
   - **Override do admin**: um campo de **path de binário customizado**,
     configurável na UI de Integrações (`/dashboard/integrations`, mesma
     precedência banco > variável de ambiente do ADR-0023) — cobre deploy
     air-gapped, admin que já gerencia sua própria versão/atualização do
     yt-dlp, ou ambiente que bloqueia download de binário arbitrário.
   - **Ordem de resolução**: path customizado configurado (banco, senão env) →
     binário baixado dinamicamente no path de dados → ausente. Falha em
     qualquer etapa **degrada em silêncio**: a feature de download fica
     indisponível (endpoint responde already-conhecido "sem yt-dlp
     disponível"), mas nada mais no produto quebra — o player embutido que já
     toca o link do YouTube direto (recurso existente) continua funcionando
     independente disso.
3. **Nova capacidade opcional de provider para música-tema**: `TemaMusicalProvider`
   (nome provisório, mesmo estilo de `SimilarProvider`/`TvdbCruzadoProvider`),
   implementada por um novo `internal/provider/themerrdb` (cliente HTTP simples,
   sem API key, consulta o JSON estático por id TMDB/IMDb). Trailer **não**
   precisa de capacidade nova — o `richfields.Video` já extraído do payload
   TMDB/TVDB é reaproveitado; falta só a etapa de materialização em disco.
4. **Convenção de armazenamento em disco** — estende o padrão já estabelecido no
   briefing 0062 (`asset.local_path`, nome canônico Kodi/Emby), sem novo
   mecanismo:
   - Trailer: `kind=trailer`, arquivo `<nome-canônico>-trailer.<ext>` na mesma
     pasta do arquivo de mídia (convenção Kodi/Jellyfin).
   - Música-tema: `theme.mp3` (ou extensão original) na raiz da pasta do item —
     **já é exatamente** o path que `GET /entities/{id}/theme` (briefing 0082)
     espera hoje; o download automatizado só passa a escrever nesse mesmo path
     quando ausente.
   - **Arquivo do dono sempre vence** (princípio 5, não-destrutivo): download
     automático nunca sobrescreve um `theme.mp3`/trailer que o dono já colocou
     manualmente — mesma precedência "user vence" do resto do resolver.
5. **ThemerrDB é best-effort** — cobertura é curada pela comunidade via issue no
   GitHub, portanto incompleta. Sem match, o campo fica vazio; nunca bloqueia a
   entidade nem o resto do enriquecimento (mesma postura de todo provider
   opcional do projeto, AGENTS.md §6).

## Consequências

- Trailer e música-tema passam a ter caminho de materialização em disco no
  padrão Kodi/Jellyfin, fechando a lacuna de paridade com o tinyMediaManager
  apontada pelo dono.
- Novo risco categórico: **violação de ToS do YouTube** via yt-dlp — precisa de
  aviso explícito na UI de Integrações (o admin liga a feature sabendo do
  risco), permanece 100% opt-in, e desabilitar (sem binário configurado/baixado)
  deixa o resto do produto — incluindo o player embutido que já existe — intacto.
- Novo tipo de superfície operacional: ciclo de vida do binário yt-dlp (download
  dinâmico, drift de versão, path customizado) — não pode comprometer o
  princípio local-first: ausência do binário nunca trava o boot nem qualquer
  outra feature, só desliga trailer/tema automatizados.
- Cobertura de música-tema via ThemerrDB é parcial por natureza (curadoria
  comunitária) — aceito como best-effort, consistente com o resto do projeto.
- Fora de escopo desta ADR (fica para o(s) briefing(s) de implementação):
  formato exato do campo de path customizado na UI de Integrações, URL/versão
  exata de release do yt-dlp a usar, texto exato do aviso de risco de ToS, UX
  exata do botão/job de download, e se trailer e tema saem no mesmo briefing ou
  em briefings separados (a peça de infraestrutura — o downloader — é
  compartilhada; as duas verticais podem evoluir independentemente depois dela
  existir).
- Decisões relacionadas: [ADR-0023](0023-integracoes-configuraveis-via-ui-precedencia-sobre-env.md)
  (precedência banco>env, reaproveitada pro path customizado do yt-dlp);
  briefing 0062 (materialização de asset na pasta real, `asset.local_path`,
  nome canônico); briefing 0082 (`theme.mp3` local-only — esta ADR adiciona uma
  fonte automatizada opcional ao que já era manual, sem mudar o contrato de
  `GET /entities/{id}/theme`); [ADR-0024](0024-cache-externo-sobrevive-sem-provider-vivo.md)
  (o payload de trailer/tema, uma vez cacheado, segue a mesma garantia de
  sobrevivência offline).
