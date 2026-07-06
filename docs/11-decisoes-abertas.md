# 11 — Decisões abertas, riscos e trabalho relacionado

## Decisões já tomadas (registro)

- **Banco: SQLite.** Cogitou-se Postgres por medo de volume de requests, mas para
  o uso self-hosted **isso não é gargalo** — decisão revista para SQLite (um
  arquivo, zero-config, memória mínima). Busca via FTS5 + colunas geradas em vez
  de GIN (ver [04](04-modelo-de-dados.md)).
- **Backend Go, frontend Vite/React, um container** (ver [03](03-arquitetura.md)).
- **Escopo universal por design, MVP filmes/séries** (ver [README](../README.md)).
- **Plugins: preparados no modelo, maquinário fora do MVP** (ver abaixo).
- **Nome: MyMediaDB**, reusando o repo `rafaelvieiras/my-media-db`.

## Decisões abertas

1. **Router HTTP (Go):** `chi` (minimalista) vs `echo`. *Inclinação:* chi.
2. **Driver SQLite:** `modernc.org/sqlite` (CGO-free, cross-compile ARM trivial)
   vs `mattn/go-sqlite3` (mais rápido, exige CGO). *Inclinação:* modernc.
3. **Camada de query:** `sqlc` (SQL tipado → Go, explícito e leve) vs ORM.
   *Inclinação:* sqlc.
4. **Extração de mediainfo:** `ffprobe` (ubíquo) vs `mediainfo-cli` (usado na v1).
   *Inclinação:* ffprobe.
5. **Contrato de tipos:** gerar **OpenAPI** a partir da API como fonte de verdade
   dos tipos TS do front, vs manter tipos à mão. *Inclinação:* OpenAPI gerado.
6. **`/api/tmdb` sem item local:** `404` vs passthrough+cache do TMDB real.
   *Inclinação:* configurável, default `404`.
7. **Sistema de plugins:** o modelo já suporta (extension_data namespaced +
   schema), mas o **maquinário** (manifest, registry, ciclo de vida
   install/enable/disable/purge, sandbox de execução, widgets de frontend
   dinâmicos) fica **pós-MVP**. Desenhar a superfície quando a base estiver
   estável. Princípio firme: **plugin nunca altera o schema do núcleo** e
   **código não-confiável roda isolado**.
8. **Verticais além de filmes/séries:** quais primeiro (music videos? música?
   games? livros? quadrinhos?) e com quais providers. Priorizar pela lacuna real
   no servidor do dono.
9. **Autenticação/permissões:** esquema de token e escopos (ver [10](10-usuarios-e-permissoes.md)).
10. **Licença:** *Inclinação:* MIT ou Apache-2.0 (adoção por apps é objetivo).
11. **Layout de assets em disco** e política de cache/derivadas (ver [08](08-artwork-e-assets.md)).

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
