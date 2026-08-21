# 0119 — Arte local de música + precedência `local` acima de `external`

> Imagem que o dono já tem na pasta (poster.jpg/folder.jpg) passa a ser tratada
> como decisão dele: vence qualquer provider externo na tela, e o mecanismo de
> descoberta — que já existia pra filme/série — passa a alcançar artista/álbum
> de música também.

| | |
|---|---|
| **ID** | 0119 |
| **Tipo** | feature |
| **Fase (roadmap)** | Transversal / infraestrutura operacional — extensão do mecanismo de arte local do [0088](0088-acoes-forcadas-menu-gerenciar.md) para a vertical de música, complemento do [0117](0117-fix-artes-e-identidade-mbid-fora-do-import.md)/[0118](0118-reprocessar-artes-faltantes-entities-ja-identificadas.md). |
| **Docs relacionadas** | [08-artwork-e-assets](../docs/08-artwork-e-assets.md), [ADR-0027](../docs/adr/0027-modelo-de-musica-hierarquia-artista-album-faixa.md) |
| **Status** | done |
| **Depende de** | [0017](0017-arte-custom-upload-ui.md), [0048](0048-fundacao-providers-de-enriquecimento-arte-trailer-tema.md), [0088](0088-acoes-forcadas-menu-gerenciar.md), [0106](0106-provider-musicbrainz-cover-art-archive.md), [0116](0116-enriquecimento-retroativo-musica-artista.md) |

---

## 1. Objetivo

Ao validar o 0117/0118 em produção, o dono apontou dois problemas reais:

1. **Falso negativo de posse**: a investigação concluiu erroneamente que
   "Bomfunk MC's" não tinha arquivo na biblioteca (por `owned=false` na
   entity artista) — na verdade o arquivo existe
   (`Bomfunk MC's/Freestyler (2000)/05 - Freestyler.mp3`), o scanner o
   catalogou, só que a busca textual não achou candidato confiável e ele
   ficou como `faixa` rascunho (draft), solta da árvore — exatamente o
   comportamento documentado (item 5 do pipeline de `internal/scanner/
   musica.go`), esperando revisão manual do dono. **Não há bug aqui** — é
   este briefing que corrige a interpretação errada, não código.
2. **Fonte de arte só fanart.tv**: o dono quer que **arquivo/pasta local**
   também seja uma fonte de arte para música, se somando ao MusicBrainz
   (Cover Art Archive, já registrado como `ArteProvider`, briefing 0106) e ao
   fanart.tv (0048/0117) — e que, quando existir imagem local, ela **vença**
   qualquer provider.

O mecanismo de arte local (`descobrirPoster`/`scanner.RefreshLocalAsset`,
asset `origin='local'`) já existe para filme/série desde o 0088, mas nunca foi
ligado à música: artista/álbum **nunca têm `media_file` direto** (ADR-0027 —
só a faixa liga), então a pasta "da obra" precisa ser **derivada** do arquivo
de uma faixa descendente, o que o mecanismo genérico (pensado para uma
entity com arquivo próprio) não fazia.

## 2. Jornada do usuário

1. O dono coloca um `folder.jpg` na pasta do artista e/ou um `poster.jpg` na
   pasta do álbum (convenção Kodi/Jellyfin, igual à de filme/série).
2. No próximo ciclo do job `saude_biblioteca` (ou disparo manual), o sistema
   encontra essas imagens e grava um asset `origin='local'` para a entity
   certa — sem precisar de nenhum provider configurado.
3. Na tela do artista/álbum, essa imagem aparece e **vence** qualquer capa/
   foto vinda de MusicBrainz ou fanart.tv (a menos que o dono tenha marcado
   uma como preferida ou feito upload manual — esses continuam no topo).

## 3. Escopo

**Dentro:**
- Precedência de serviço (`escolherAssetServido`/`coletarCandidatos`,
  `internal/server/assets.go`) ganha uma camada `origin='local'` **entre**
  `user` e `external`: `preferida → user → local → external → base`. Vale
  para QUALQUER tipo (filme/série incluídos, não só música) — antes,
  `origin='local'` só vencia quando fosse a "base" (primeiro asset de
  qualquer origem), podendo perder para `external` dependendo da ordem de
  criação.
- `Server.DescobrirArteLocalMusica`: para toda entity artista/álbum com
  faixa possuída (`ListarEntitiesMusicaComFaixaPossuida`, mesmo universo do
  0116/midias_faltantes), deriva a pasta canônica a partir do path de uma
  faixa descendente (query nova `ObterCaminhoDescendentePresente`) — álbum =
  diretório do arquivo da faixa; artista = um nível acima — e delega a
  `scanner.RefreshLocalAsset` (mecanismo já existente, sem mudança nele).
- Novo passo no job `saude_biblioteca`, depois do reprocessamento de artes
  (0118): roda a cada execução (sem throttle — é um `stat` de disco barato e
  idempotente, mesmo espírito de `midias_faltantes` já rodar sem throttle).

**Fora (explicitamente):**
- Materializar a capa/foto ESCOLHIDA na pasta real de música (o inverso desta
  feature — já documentado como fora de escopo em
  `materializarAssetOuCache`, `internal/server/midias_faltantes.go`) —
  continua fora.
- Spotify como fonte (mencionado pelo dono como "futuramente") — nenhum
  código novo aqui, só a precedência já dá espaço para uma 4ª fonte quando
  ela existir.
- Corrigir a busca textual do scanner para achar mais casos como o do
  "Freestyler" (item 1 do objetivo) — isso é curadoria manual via fila de
  Revisão, não um bug de matching a esta altura.

## 4. Contrato técnico

### Dados / modelo
- Nenhuma migração nova.

### Backend (Go)
- `internal/server/assets.go`: `originLocal` + `candidatosAsset.local/temL` +
  `coletarCandidatos`/`escolherAssetServido` atualizados.
- `internal/db/queries/media_file.sql` (+ sqlc): `ObterCaminhoDescendentePresente`.
- `internal/server/arte_local_musica.go` (novo):
  `Server.DescobrirArteLocalMusica`/`descobrirArteLocalMusicaDe`.
- `internal/job/libraryhealth/libraryhealth.go`:
  `Matcher.DescobrirArteLocalMusica`, `rodarDescobertaArteLocalMusica`,
  wiring em `Executar`, cláusula nova em `montarMensagem`.

### Arquivos afetados
- `internal/server/assets.go`, `internal/server/assets_test.go`.
- `internal/db/queries/media_file.sql`, `internal/db/sqlcgen/media_file.sql.go` (gerado).
- `internal/server/arte_local_musica.go`, `internal/server/arte_local_musica_test.go`.
- `internal/job/libraryhealth/libraryhealth.go`, `internal/job/libraryhealth/libraryhealth_test.go`.
- `internal/db/sqlcgen/crud_test.go` (cobertura direta da query nova).

## 5. Critérios de aceite

- [x] Com asset `origin='local'` E `origin='external'` do mesmo kind, o
      servido é o `local` (a menos que exista `user`/preferida).
- [x] Um álbum com faixa possuída e `poster.jpg`/`folder.jpg` na pasta do
      álbum ganha um asset `origin='local'` apontando pra esse arquivo.
- [x] Um artista com faixa possuída (via álbum) e `folder.jpg` na pasta do
      artista (um nível acima da pasta do álbum) ganha o mesmo, mesmo sem
      arquivo próprio (ADR-0027).
- [x] Sem `poster.jpg`/`folder.jpg` em nenhuma das pastas: nada é criado, sem
      erro.
- [x] A mensagem final do job ganha "N entity(ies) de música com pôster
      local encontrado" só quando N > 0.
- [x] `go build ./...`, `go vet ./...`, `go test ./... -count=1` verdes;
      `make gate-local` sem regressão de cobertura.

## 6. Notas de implementação / decisões

- Zero mudança em `scanner.descobrirPoster`/`RefreshLocalAsset`: a "pasta
  certa" é alcançada só escolhendo QUAL path passar pra
  `RefreshLocalAsset(ctx, entityID, []string{p})` — que sempre aplica
  `filepath.Dir(p)` internamente. Para álbum, `p` = o path da PRÓPRIA faixa
  (seu dir já é a pasta do álbum); para artista, `p` = a pasta do álbum
  (`filepath.Dir(faixaPath)`, cujo dir é a pasta do artista, um nível acima).
  Nenhum arquivo real chamado `p` precisa existir — é só um vetor para
  `filepath.Dir` chegar no diretório certo.
- A precedência `local > external` é uma mudança de comportamento GLOBAL
  (não só música): qualquer filme/série com `origin='local'` E `origin=
  'external'` do mesmo kind agora serve o local, mesmo quando o external foi
  criado primeiro (antes, "primeiro de qualquer origem" podia coincidir com
  o external). Decisão do dono, não modelada como opt-in — a leitura é "uma
  imagem que já está na pasta da obra é uma escolha implícita dele".
- `ObterCaminhoDescendentePresente` teve que copiar a estrutura EXATA de
  `ListarEntitiesMusicaComFaixaPossuida` (`FROM entity e JOIN arvore t ON
  t.ancestor_id = e.id JOIN media_file m ON ...`) — uma tentativa mais direta
  (`FROM arvore t JOIN media_file m ...`, sem o join de `entity`) fez o sqlc
  falhar com `table alias "t" does not exist"`; não investigado a fundo
  (parece uma particularidade do analisador SQLite do sqlc com CTEs
  recursivas sem uma tabela "âncora" no FROM), mas a estrutura que já
  funciona no arquivo foi reaproveitada sem problema.

## 7. Fora deste briefing / follow-ups

- Se o dono quiser Spotify como fonte de metadado/arte no futuro, a
  precedência já reserva o lugar — só precisa de um novo `ArteProvider`
  registrado (mesmo padrão de capacidade opcional do AGENTS.md §6).
