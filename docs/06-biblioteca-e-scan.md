# 06 — Biblioteca e scan

> **Status desta doc:** stub escopado. Decisões travadas listadas; detalhamento
> em briefings da Fase 1.

## Princípios

- **Datasources** são pastas do servidor apontadas pelo dono. Um datasource tem
  um `type` (ex.: filmes, séries) e pode conter várias pastas.
- **Scan não-destrutivo.** A varredura descobre arquivos e extrai metadados;
  **nunca** renomeia nem move nada sem ordem explícita. A verdade é o banco, não
  o nome do arquivo ([cenário E](01-visao-e-casos-de-uso.md)).
- **Mediainfo por arquivo.** Para cada arquivo de vídeo, extraem-se os fatos
  técnicos (resolução, codecs, HDR, faixas de áudio/legenda, duração real).
- **Dedup por conteúdo.** Cada arquivo tem um `content_hash` estável, para não
  duplicar registro ao renomear/mover.
- **Matching.** O arquivo é associado a uma `Entity` (e, opcionalmente, a um id
  externo) seguindo a convenção **Kodi/Jellyfin** — ver
  [Identidade e matching](#identidade-e-matching).

## Identidade e matching

Adota-se a **convenção de nomenclatura do Kodi/Jellyfin** — padrão de-facto do
ecossistema self-hosted (Emby, Plex e o stack \*arr seguem a mesma família).
**Não reinventamos**: o padrão já resolve o grão da entidade, as edições e a
chave de match. O scan é leitor desse padrão, nunca o impõe (não renomeia nada).

### Grão: 1 obra = 1 `Entity`, N arquivos (1:N plano)

- Uma pasta = um filme = uma `Entity`. Todo arquivo cujo nome começa com o nome
  da pasta é uma **versão da mesma obra** — não uma obra nova.
- Qualidades (2160p/1080p), edições (Corte do Diretor) e partes (cd1/cd2) são
  **`media_file`** ligados à **mesma** `Entity`, não entidades separadas.

```
Blade Runner 2049 (2017) [tmdbid-335984]/
├── Blade Runner 2049 (2017) [tmdbid-335984] - 2160p.mkv
├── Blade Runner 2049 (2017) [tmdbid-335984] - 1080p.mkv
└── Blade Runner 2049 (2017) [tmdbid-335984] - Directors Cut.mkv
```

- **Edição/versão = rótulo no `media_file`** (campo `edition`), nunca uma `Entity`
  duplicada.
- **Partes** (`-cd1`/`-part1`/`-disc1`) são **empilhadas** (*stacking*) como **um
  item lógico** de reprodução.

### Chave de match: id externo embutido no nome

- Id no nome (`[tmdbid-335984]` estilo Jellyfin, `{tmdb-335984}` estilo Kodi) →
  **match determinístico** (confiança 100%), sem adivinhação.
- Sem id → **título + ano → busca no provider**.
- **Música** segue o mesmo princípio de id determinístico via **MusicBrainz +
  tags embutidas** (não por nome de arquivo).

### Pontuação de confiança e fila de revisão

Todo match carrega um **score de confiança (0–100%)**. O scan **nunca trava a
biblioteca**: o melhor palpite é sempre aplicado; o que é incerto é **sinalizado**,
não bloqueado.

| Situação | Confiança | Ação |
|---|---|---|
| id externo no nome | 100% | casa automático, sem alerta |
| título + ano exatos, 1 resultado | alta | casa automático, sem alerta |
| título parcial / múltiplos hits / sem ano | baixa | casa (melhor palpite) **+ alerta de revisão** |
| nenhum resultado | — | vira `draft` **não-identificado** (visível, não perdido) |

- O **alerta de revisão** mostra o score e o alvo: *"esse arquivo foi mapeado
  para essa entrada com 60% de certeza. Remover esse alerta?"* O dono **confirma**
  (remove o alerta), **corrige** (re-associa) ou resolve na origem **adicionando o
  id no nome**.
- A correção do dono é **autoritativa** — entra pelo mecanismo de override do
  resolver (ver [04](04-modelo-de-dados.md)).

### Fora do padrão Kodi: verticais próprias

O padrão cobre **filmes, séries e música**. Verticais que ele não cobre trazem
**sua própria regra de identidade e seu próprio provider**, isoladas em
`extension_data` (ver [04](04-modelo-de-dados.md)) — **sem tocar no núcleo, no
schema nem no resolver**:

- **Videoclipe** → provider **IMVDB** (ex.: namespace `core.musicvideo@1`).
- **Quadrinhos** → `ComicInfo.xml` / ecossistema Komga-Kavita.

## Ciclo de vida do arquivo: `missing` e datasource offline

O scan é **não-destrutivo também na ausência**: sumiço nunca apaga registro.

- **Arquivo não encontrado, datasource acessível** → o `media_file` passa a
  **`missing`** (guarda `last_seen`), **não** é removido. Se reaparecer (mesmo
  `content_hash`), volta a `present`.
- **Datasource inteiro inacessível** (HD desmontado, share offline) → **nada** é
  marcado `missing`; o datasource é sinalizado **offline** e seu scan é suspenso.
  Evita um desmonte virar "sumiço" de milhares de itens.
- **Apagar de vez** (`purge`) é sempre **ação explícita** do dono — nunca efeito
  colateral do scan.

Consequência no modelo: um `media_file` `missing` **não conta para a posse**; a
entity segue viva e seu `status` fica intacto (ver
[04 — Ciclo de vida e deleção](04-modelo-de-dados.md)).

## Referência reaproveitável (v1)

Preservado em scratchpad da sessão de planejamento — reescrever em Go, não portar:

- **Extensões de vídeo válidas:** `.mp4 .mkv .avi .mov .wmv .flv .webm` (ponto de
  partida; ampliar).
- **Shape de mediainfo** (general/video/audio) já mapeado na v1 — bom contrato de
  referência para o que extrair.

## Decisões abertas (ver [11](11-decisoes-abertas.md))

- **Ferramenta de extração: `ffprobe` (inclinação) vs `mediainfo-cli`.**
- Scan incremental (mtime/hash) vs full; watching de filesystem vs sob demanda.
- Limiar de confiança que dispara o alerta de revisão (tuning, não modelo).

> **Estratégia de matching:** decidida — convenção Kodi/Jellyfin, ver
> [Identidade e matching](#identidade-e-matching). Id embutido em NFO existente
> conta como id determinístico (mesma família de chave).
