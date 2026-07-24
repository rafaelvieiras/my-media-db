# 0081 — Corrige o job de mídias faltantes para a árvore de série (temporada/episódio)

> O job "mídias faltantes" nunca materializa arte/NFO de temporada e episódio
> — e mesmo a SÉRIE (que o job já processa) grava no lugar ERRADO da pasta
> real quando ela tem subpastas por temporada (a estrutura normal de qualquer
> biblioteca real). Investigado ao vivo com dados reais do dev (série "MIB
> Homens de Preto", 4 temporadas, 53 episódios): 0/4 pôsteres de temporada no
> disco, 0/53 NFOs de episódio, e o pôster/fanart DA SÉRIE salvos dentro de
> `Season 01/`, nomeados como se fossem do episódio 1.

| | |
|---|---|
| **ID** | 0081 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 4 — Mídias locais |
| **Status** | ready |
| **Docs relacionadas** | [09-nfo-e-export](../docs/09-nfo-e-export.md) |
| **Depende de** | 0062, 0063, 0064, 0044 |

---

## 1. Objetivo

Uma investigação ao vivo (dev, dados reais) confirmou 2 causas raiz
independentes que se somam — nenhuma delas hipotética, ambas com evidência
concreta no banco/disco:

**Causa 1 — o job nunca enxerga temporada nem episódio.** A query de
elegibilidade do job (`ListarEntityIDsComMediaFilePresente`,
`internal/db/queries/media_file.sql:129-139`, consumida por
`ListarEntitiesComMediaFilePresente`, `internal/server/midias_faltantes.go:39-
47`) só devolve `entity_id` DIRETO de `media_file` — e todo `media_file` de
episódio aponta `entity_id` para a SÉRIE (ADR-0021, vínculo real é via a
junção genérica `media_file_entity`, kind `tv_episode`). Então temporada e
episódio JAMAIS aparecem no universo elegível do job — não é uma falha
pontual, a query estruturalmente não tem como alcançá-los. Evidência real: a
última execução do job processou "13 elegíveis" = 12 filmes + 1 série, ZERO
temporadas/episódios; as 4 temporadas da série de teste têm candidato de
pôster no banco (`is_principal=0`) mas `local_path` vazio (nunca copiado); os
53 episódios têm zero linhas de asset e `nfo_path` vazio.

**Causa 2 — mesmo a série (que o job JÁ processa) resolve a pasta errada.**
`DiretorioDestino`/`ArquivoMaisAntigo` (`internal/localmedia/naming.go:220-
263`) — a MESMA base usada tanto pelo job quanto pelos botões manuais
("Salvar informações (NFO)"/"baixar arte", via `resolverDestino`,
`internal/server/localmedia_handlers.go:230-252`) — escolhe o `media_file`
PRESENTE mais antigo (`created_at asc`) entre TODOS os da série e usa
`filepath.Dir()` dele como "a pasta da série". Isso funcionava quando toda
biblioteca era flat (todo episódio direto na pasta da série), mas numa
estrutura real com subpastas por temporada (`Série/Season 01/arquivo.mkv` —
o normal desde que a árvore de série por temporada foi implementada, 0031/
TV1-TV5), o arquivo mais antigo está DENTRO de `Season 01/`, então a "pasta da
série" calculada é essa subpasta, nunca a raiz. Isso contraria a exigência
explícita do 0062 ("a pasta de destino é sempre a raiz da série, nunca uma
subpasta de temporada") e explica por que, na série de teste, o pôster/fanart
da série ficaram salvos como
`Season 01/MIB - Homens de Preto (1997) - S01E01 - A Síndrome do Adeus-
poster.jpg` — visualmente indistinguível de uma arte de episódio.

**Não é bug (confirmado, documentar e não "corrigir"):** still de episódio
nunca é buscado automaticamente — é INTENCIONAL (briefing 0044, "still custom
vence provider, lazy": o dono escolhe manualmente pela galeria do episódio).
E season NFO como ARQUIVO PRÓPRIO também não existe hoje por decisão correta,
não lacuna: pesquisa feita nas convenções oficiais confirma que o **Kodi**
(convenção que `KindSeasonPoster`/`internal/localmedia/naming.go` já
adotam explicitamente para arte de temporada, comentário em
`naming.go:36-46`) não tem um arquivo de NFO de temporada — os metadados de
temporada vivem como sub-elemento `<season>` DENTRO do `tvshow.nfo` da série,
algo que este projeto ainda não implementa e que é FORA de escopo aqui (ver
§3). O guard atual em `gravarNFOSeFaltante`
(`internal/server/midias_faltantes.go:141-143`, "season nunca tem NFO
próprio") está correto e não deve ser removido.

## 2. Pesquisa de convenção (Kodi × Jellyfin) — usada para embasar o fix

| Nível | Kodi | Jellyfin |
|---|---|---|
| Série — NFO | `<raiz>/tvshow.nfo` | mesmo local |
| Série — artes | `<raiz>/poster.jpg`, `fanart.jpg` | mesmo local (aceita `cover.jpg`/`backdrop.webp` como sinônimos) |
| Temporada — NFO | **não existe arquivo próprio** — tag `<season>` dentro do `tvshow.nfo` | `Season NN/season.nfo` (arquivo próprio, dentro da pasta da temporada) |
| Temporada — artes | **na RAIZ da série**: `season01-poster.jpg`, `season01-banner.jpg` (zero-padded, sem hífen antes do número) | dentro da PRÓPRIA pasta da temporada: `Season 01/cover.jpg` |
| Episódio — NFO/still | `<raiz>/Season NN/<basename-do-vídeo>.nfo` / `...-thumb.jpg` | mesmo local |

Série e episódio NÃO divergem entre as fontes. Temporada DIVERGE (Kodi: tudo
na raiz da série, achatado por nome de arquivo; Jellyfin: pasta própria da
temporada). Este projeto **já decidiu** seguir a convenção Kodi para arte de
temporada (`KindSeasonPoster`, comentário explícito em `naming.go`) — este
briefing só CORRIGE a pasta-alvo para essa convenção funcionar (raiz real da
série, não uma subpasta), sem revisitar a escolha Kodi-vs-Jellyfin em si.
Fontes: kodi.wiki/view/NFO_files/TV_shows, kodi.wiki/view/Artwork/Season,
jellyfin.org/docs/general/server/media/shows,
jellyfin.org/docs/general/server/metadata/nfo.

## 3. Escopo

**Dentro:**
- Corrigir `DiretorioDestino`/`ArquivoMaisAntigo`
  (`internal/localmedia/naming.go`) para resolver a RAIZ REAL da série,
  mesmo com subpastas por temporada — não mais "o arquivo mais antigo".
- Estender a elegibilidade do job (`ListarEntitiesComMediaFilePresente`/
  `ListarEntityIDsComMediaFilePresente`) para incluir:
  - **Temporada**: elegível quando tem ao menos um episódio descendente com
    `media_file` presente (via a junção `media_file_entity`).
  - **Episódio**: elegível quando ele próprio tem um `media_file` presente
    (via a junção `media_file_entity`, kind `tv_episode`) — habilita o job a
    gravar o NFO do episódio automaticamente (kind já suportado por
    `nomeRaizNFO`/`raizEpisode`, só nunca era alcançado). Arte de episódio
    (`still`) continua FORA do loop de kinds do job (`kindsMidiasFaltantes`
    não ganha `still` — permanece lazy/manual, 0044).
- Confirmar que a mesma correção da raiz da série beneficia tanto o job
  quanto os botões manuais (`resolverDestino`, `internal/server/localmedia_
  handlers.go`), já que ambos compartilham `localmedia.DiretorioDestino`/
  `ArquivoMaisAntigo` — não deve haver DUAS implementações da resolução de
  raiz.

**Fora (explicitamente):**
- Season NFO como arquivo próprio, ou suporte à tag `<season>` dentro do
  `tvshow.nfo` — feature nova, não abordada por este briefing (Kodi não tem
  arquivo próprio; se o dono quiser o `<season>` embutido no `tvshow.nfo`
  futuramente, é briefing à parte).
- Convenção Jellyfin de temporada (pasta própria com `season.nfo`/
  `cover.jpg`) — o projeto já escolheu Kodi para temporada; dual-write nas
  duas convenções fica como possível follow-up, não critério de aceite
  aqui.
- Still de episódio deixar de ser lazy/manual (0044 continua valendo).
- Qualquer mudança na LÓGICA de qual arte é a "vencedora"
  (`montarGruposArtwork`/`ordenarGruposArtwork`) — este briefing só corrige
  ONDE ela é salva e QUAIS entities o job alcança, não qual arte é escolhida.

## 4. Contrato técnico

### Backend (Go)

- `internal/localmedia/naming.go` (`DiretorioDestino`/`ArquivoMaisAntigo`,
  linhas 220-263): trocar "arquivo mais antigo" por uma resolução de RAIZ
  real. Algoritmo sugerido:
  1. Reunir os diretórios (`filepath.Dir`) de TODOS os `media_file` present
     elegíveis para o alvo (própria entity para `tv`; ancestral `tv` para
     `season`, já resolvido por `serieAncestral`).
  2. Calcular o ANCESTRAL COMUM (common path prefix) de todos esses
     diretórios — para uma série com ≥2 pastas de temporada como irmãs, o
     prefixo comum já cai naturalmente na raiz certa.
  3. Caso de borda (série com 1 única temporada, ou todos os arquivos
     presentes concentrados numa só subpasta): o prefixo comum calculado
     no passo 2 ainda pode ser a PRÓPRIA pasta de temporada. Detectar esse
     caso (nome da pasta resultante bate com um padrão de nome de temporada,
     ex. `(?i)^season\s*\d+$`/`^s\d{2}$` — usar um regex tolerante, já que
     nomes de pasta de temporada variam) e subir mais um nível.
  4. Cobrir com teste um caso multi-temporada (o prefixo comum já resolve) E
     um caso de temporada única (precisa do passo 3).
- `internal/db/queries/media_file.sql` (perto de `ListarEntityIDsComMediaFile
  Presente`, linha ~129): adicionar query(s) nova(s) — via `sqlc` (rodar
  geração depois) — que devolvam:
  - IDs de `season` com ao menos um episódio descendente com `media_file`
    presente (join por `media_file_entity` kind='tv_episode' + `entity_
    relation` série→season/season→episode, ou equivalente já usado em outro
    lugar do código para resolver "episódios de uma temporada" — reusar
    query/padrão existente se houver, não duplicar lógica de travessia de
    árvore).
  - IDs de `episode` com `media_file` presente via `media_file_entity`
    (kind='tv_episode').
- `internal/server/midias_faltantes.go`:
  - `ListarEntitiesComMediaFilePresente`/`ListarEntityIDsComMediaFilePresente`:
    combinar o resultado da query existente (movie/tv direto) com as novas
    (season/episode).
  - Atualizar o comentário de `ListarEntitiesComMediaFilePresente` (linhas
    39-44) e de `gravarNFOSeFaltante` (linhas 130-136) — eles hoje afirmam
    "season/episódio nunca aparecem aqui", o que deixa de ser verdade para
    episódio (season continua sem NFO próprio, guarda continua válida).
  - Nenhuma mudança em `kindsMidiasFaltantes` (permanece sem `still`).
- `internal/job/midiasfaltantes/` (o job que consome
  `ListarEntitiesComMediaFilePresente`): conferir que iterar potencialmente
  centenas de episódios por execução não estoura timeout/rate-limit — a
  operação é idempotente (pula quando `nfo_path`/`local_path` já preenchidos)
  e best-effort (erro por entity é logado e pulado), então o custo extra é
  proporcional ao que HOJE falta, não a cada execução recorrente.

### Arquivos afetados

- `internal/localmedia/naming.go` (+ `naming_test.go`).
- `internal/db/queries/media_file.sql` (+ regeneração `sqlc`,
  `internal/db/sqlcgen/media_file.sql.go`).
- `internal/server/midias_faltantes.go` (+ `midias_faltantes_test.go`).
- `internal/server/localmedia_handlers.go` (se a correção da raiz exigir
  ajuste no chamador `resolverDestino`/`carregarMediaFilesLocalmedia`, além
  da correção em `internal/localmedia`).
- `internal/job/midiasfaltantes/` (se algum teste de integração precisar de
  fixture com árvore de série).

## 5. Critérios de aceite

- [ ] Rodar o job "mídias faltantes" numa série real com subpastas por
      temporada grava o pôster/fanart da série na RAIZ da série (não em
      `Season NN/`), com nome de arquivo de série (`poster.jpg`/`fanart.jpg`),
      não com prefixo de episódio.
- [ ] O mesmo job materializa pelo menos um pôster de temporada
      (`season0N-poster.jpg`) na raiz da série, para cada temporada com
      episódio presente e candidato de pôster disponível.
- [ ] O mesmo job grava o NFO de cada episódio com `media_file` presente e
      `nfo_path` ainda vazio (root XML `episodedetails`, já suportado por
      `nomeRaizNFO`).
- [ ] Episódio continua SEM still automático (comportamento 0044 preservado
      — teste garantindo que `still` não aparece em `kindsMidiasFaltantes`).
- [ ] Temporada continua SEM NFO próprio (guarda preservada, com teste).
- [ ] Teste de `DiretorioDestino`/`ArquivoMaisAntigo` cobrindo: série
      multi-temporada (prefixo comum resolve a raiz) e série de temporada
      única (precisa do fallback de subir um nível).
- [ ] Os botões manuais ("Salvar informações (NFO)"/"baixar arte") numa
      série/temporada também passam a gravar na raiz correta (mesma correção
      compartilhada, `resolverDestino`).
- [ ] `make gate-local` passa.

## 6. Notas de implementação / decisões

- **Por que a mesma correção de raiz serve pro job E pros botões manuais?**
  Ambos os caminhos já convergem em `localmedia.DiretorioDestino`/
  `ArquivoMaisAntigo` (o job via `materializarAsset`/`gravarNFO`, que
  reusam a mesma base que `resolverDestino` usa para os botões manuais) —
  corrigir na FONTE ÚNICA evita duas implementações divergentes.
- **Por que não migrar para a convenção Jellyfin de temporada (pasta
  própria)?** O código já fez essa escolha explicitamente (Kodi,
  `KindSeasonPoster`) antes deste briefing — mudar a convenção em si é uma
  decisão de produto maior (afeta compatibilidade com o player que o dono
  realmente usa), fora do escopo de um fix de bug. Fica documentado como
  possível follow-up.
- **Por que estender elegibilidade a episódio só para NFO, não para arte?**
  `still` de episódio é deliberadamente lazy (0044) — o dono escolhe pela
  galeria do episódio. NFO não tem essa mesma razão de ser manual (não há
  "vencedor entre candidatos" para o texto, é sempre o resolver), então
  estender só o NFO fecha o gap real sem contradizer uma decisão de produto
  já tomada.
