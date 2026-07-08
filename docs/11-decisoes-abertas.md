# 11 — Decisões abertas, riscos e trabalho relacionado

## Decisões já tomadas (registro)

Decisões de fundação viram [ADRs](adr/); a lista abaixo é o resumo rastreável.

- **Banco: SQLite.** Cogitou-se Postgres por medo de volume de requests, mas para
  o uso self-hosted **isso não é gargalo** — decisão revista para SQLite (um
  arquivo, zero-config, memória mínima). Busca via FTS5 + colunas geradas em vez
  de GIN (ver [04](04-modelo-de-dados.md)). Concorrência: WAL + escritor único
  ([ADR-0010](adr/0010-concorrencia-sqlite-wal-escritor-unico.md)).
- **Backend Go, frontend Vite/React, um container** (ver [03](03-arquitetura.md)).
- **Stack de implementação** ([ADR-0014](adr/0014-stack-de-implementacao.md)):
  router **chi**, driver **`modernc.org/sqlite`** (CGO-free), dados via **sqlc**,
  mediainfo via **ffprobe**, contrato de tipos via **OpenAPI gerado**.
- **`/api/tmdb` sem item local:** **passthrough + cache, nunca 404**; detalhe pode
  virar `draft` em fila de revisão ou ficar em cache oculto (config). Ver
  [ADR-0015](adr/0015-api-tmdb-passthrough.md) e [05](05-api.md).
- **Licença: MIT** (adoção por apps é objetivo). Ver [LICENSE](../LICENSE).
- **Escopo universal por design, MVP filmes/séries** (ver [README](../README.md)).
- **Plugins: preparados no modelo, maquinário fora do MVP** (ver abaixo).
- **Nome: MyMediaDB**, reusando o repo `rafaelvieiras/my-media-db`.

## Decisões abertas (por fase)

Não são indecisões de fundação — são detalhes **adiados** para a fase em que serão
implementados. A base já foi desenhada para acomodá-los.

- **Verticais além de filmes/séries** *(genuinamente aberto)*: quais primeiro
  (videoclipes? música? games? livros? quadrinhos?) e com quais providers.
  Priorizar pela **lacuna real** no servidor do dono. O modelo já as acomoda via
  `extension_data` sem tocar no núcleo ([ADR-0005](adr/0005-composicao-uniforme-extension-data.md)).
- **Sistema de plugins** *(pós-MVP)*: o modelo já suporta (`extension_data`
  namespaced + schema), mas o **maquinário** (manifest, registry, ciclo de vida
  install/enable/disable/purge, sandbox de execução, widgets de frontend dinâmicos)
  fica para depois da base estabilizar. Princípios firmes: **plugin nunca altera o
  schema do núcleo** e **código não-confiável roda isolado**.
- **Autenticação/permissões** *(Fase 4)*: esquema de token e escopos
  (ver [10](10-usuarios-e-permissoes.md)).
- **Escopo exato das rotas TMDB a espelhar** *(Fase 4)*: movie, tv, search,
  images… (ver [05](05-api.md)).
- **Layout de assets em disco** *(Fase 3)* e política de cache/derivadas
  (ver [08](08-artwork-e-assets.md)).
- **Scan: incremental (mtime/hash) vs full; watching de filesystem vs sob demanda**
  *(Fase 2)*; e o **limiar de confiança** que dispara o alerta de revisão (tuning,
  não modelo — ver [06](06-biblioteca-e-scan.md)).

## Riscos e notas

- ⚠️ **Segredo exposto na v1.** O repo antigo `my-media-db` (público no GitHub)
  tinha uma **API key do IMVDB hardcoded** no código. **Revogar/rotacionar essa
  chave.** Regra do projeto: **segredos nunca no código** — via env/config.
- **Integração real com Plex** é limitada (Plex é fechado); o alvo prático de
  consumo é Jellyfin/Kodi via `/api/tmdb`. Plex fica como "melhor esforço".
- **Ambição de escopo.** A v2 morreu por tentar o universo (plugins + N verticais)
  antes de entregar algo. Mitigação: **MVP focado**, modelo preparado, verticais
  incrementais.

## Trabalho relacionado / prior art

- **TinyMediaManager** — referência de features (scrapers, mediainfo, NFO,
  renamer). Diferença: TMM é *stateless* (NFO espalhado); MyMediaDB é banco
  autoritativo com API.
- **v1/v2 do próprio projeto** — removidas; princípios extraídos, código não
  copiado. Peças de referência (NFO comentado, shape de mediainfo, providers)
  preservadas fora do repo.
