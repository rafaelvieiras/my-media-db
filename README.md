# 🗃️ MyMediaDB

> **O seu banco de dados de mídia — local, self-hosted, com API própria e sem
> aprovação de ninguém. Você é a autoridade sobre o seu catálogo.**
> Um banco para ser **consumido por aplicativos** (Jellyfin, Kodi, Plex, seus
> scripts), não apenas um app de gestão.

**Status:** `Fase 0 — documentação em draft`.
**Data:** 2026-07-06.
**Licença pretendida:** open source (definir; ver [`docs/11-decisoes-abertas.md`](docs/11-decisoes-abertas.md)).

---

## O que é (em uma frase)

Um **banco de dados de mídia pessoal** que roda no seu servidor: lê a sua
biblioteca local, extrai metadados técnicos dos arquivos, puxa informação de
fontes externas **sob demanda** para complementar — e deixa **você** ser o dono
final de cada título, arte e classificação, expondo tudo por uma **API sem cota**
que qualquer cliente (Jellyfin, Kodi, Plex, agentes, scripts) pode consumir.

É, ao mesmo tempo, **um gerenciador de biblioteca** (no espírito do
TinyMediaManager) e **o seu próprio "TMDB privado"** — só que a fonte de verdade
é local e a sua palavra vence a de qualquer fonte externa.

## Por que existe (a lacuna)

As ferramentas atuais resolvem pedaços e sempre esbarram em quem manda no dado:

| Dor | Ferramenta atual | Resposta do MyMediaDB |
|---|---|---|
| Metadados ficam **espalhados** em `.nfo` por pasta, sem fonte consultável | TinyMediaManager é *stateless* — só cospe NFO | **Banco único com API**; NFO vira export opcional, não a verdade |
| Sua arte/edição precisa ser **aprovada** por moderador externo | TMDB / Fanart.tv | **Ninguém aprova nada**; suas artes e textos entram direto |
| Título brasileiro (VHS/TV) **diverge** do título oficial | Fica preso ao nome do TMDB | **Camada de override**: você renomeia, e o seu nome vence |
| Curta animado / caso fora da taxonomia oficial **some** | Filosofia do TMDB recusa | **Sua taxonomia**: você decide o que é episódio, coleção, o que for |
| Quer catalogar mídia que **nenhum banco cobre bem** (além de filmes/TV) | Não existe base decente | **Modelo universal e extensível** — outras verticais entram sem tocar no núcleo |
| A API pública tem **cota e regras** que não são suas | TMDB API | **API sua, sem limite**; você define usuários e permissões |
| Quer info de mídia que **ainda não tem** (planejamento) | O manager só enxerga o arquivo | O banco é **maior que a biblioteca** (campo `status`) |

## Princípios

1. **Você é a autoridade.** O banco local é a fonte de verdade. Dado externo é
   *sugestão*; a sua edição é uma **camada de override** que vence sempre.
2. **O banco é maior que a biblioteca.** Cataloga o que você tem *e* o que ainda
   não tem (wishlist/planejamento) — via `status`, não gambiarra.
3. **Universal por design, focado na entrega.** O núcleo (entidade + extensões)
   modela *qualquer* mídia. O MVP entrega **filmes e séries**, mas nada no núcleo
   assume que só existe isso.
4. **Extensível sem tocar no núcleo.** Verticais e integrações vivem em dados de
   extensão *namespaced* e validados. **Plugins de terceiros são um alvo do
   design — não do MVP** (ver [`docs/11`](docs/11-decisoes-abertas.md)).
5. **Local-first e leve.** Uma imagem, um binário, memória mínima. Fontes
   externas são enriquecimento **opcional**, nunca requisito para funcionar.
6. **Pull > Sync.** Sem sincronização automática. Você puxa da fonte externa
   quando quer, e o que é puxado é cacheado localmente.
7. **Banco para ser consumido.** Expõe API — inclusive uma camada
   **compatível com TMDB** — para que Jellyfin/Kodi/Plex e afins leiam o seu
   catálogo. O app de gestão é só **mais um cliente** dessa API.

## Escopo

- **Visão (para onde vai):** um catálogo pessoal **universal** de mídia —
  filmes, séries, e verticais adicionais (o que hoje não tem banco decente no
  seu servidor), extensível por plugins.
- **MVP (o que sai primeiro):** **Filmes + Séries**, com UI de gestão primeiro
  (validar o rumo), scan da biblioteca, mediainfo, enriquecimento on-demand e
  a camada de override. API compatível com TMDB e multiusuário vêm em seguida.

O modelo de dados e a arquitetura já nascem preparados para a visão; o
*maquinário* (plugin registry, verticais extras) fica para depois do MVP.

## Documentação

| Doc | Conteúdo |
|---|---|
| [01 — Visão e casos de uso](docs/01-visao-e-casos-de-uso.md) | Cenários normativos que a implementação precisa satisfazer |
| [02 — Glossário](docs/02-glossario.md) | Vocabulário canônico do projeto |
| [03 — Arquitetura](docs/03-arquitetura.md) | Go + Vite/React, container único, flag de API, camadas |
| [04 — Modelo de dados](docs/04-modelo-de-dados.md) | Entidade base, camada de override, dados de extensão, `status`, assets |
| [05 — API](docs/05-api.md) | Namespaces `/api/tmdb` (compat) e `/api/v1` (nativo) |
| [06 — Biblioteca e scan](docs/06-biblioteca-e-scan.md) | Datasources, varredura, mediainfo, matching, dedup |
| [07 — Enriquecimento e providers](docs/07-enriquecimento-providers.md) | Fontes externas on-demand, cache, proxy de imagem |
| [08 — Artwork e assets](docs/08-artwork-e-assets.md) | Armazenamento, tipos de arte, upload custom |
| [09 — NFO e export](docs/09-nfo-e-export.md) | Export NFO Kodi/Jellyfin como saída opcional |
| [10 — Usuários e permissões](docs/10-usuarios-e-permissoes.md) | Multiusuário, tokens, escopos |
| [11 — Decisões abertas](docs/11-decisoes-abertas.md) | O que ainda não foi decidido, riscos, plugins futuros |
| [12 — Roadmap](docs/12-roadmap.md) | Fases (UI-first) e como cada fase vira briefings |

O desenvolvimento é dirigido por **briefings** — ordens de serviço autocontidas,
uma por feature/fix. Ver [`briefings/README.md`](briefings/README.md).

## Não-objetivos

- **Não é um media server.** Não faz streaming nem transcode — isso é papel do
  Jellyfin/Plex, que *consomem* o MyMediaDB.
- **Não é um scraper que reescreve suas pastas.** A organização de arquivos é
  opcional e não-destrutiva; a verdade vive no banco, não no nome do arquivo.
- **Não é um serviço em nuvem.** Roda no seu servidor, para os seus usuários.
- **Não é uma base pública colaborativa.** Não há moderação global nem catálogo
  compartilhado; o dado é seu.
