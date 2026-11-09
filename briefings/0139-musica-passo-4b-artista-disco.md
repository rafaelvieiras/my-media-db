# 0139 — Passo 4b da música: escopo por artista, nome normalizado e desempate por disco

**Status:** done
**Data:** 09/nov/2026
**Contexto:** fecha a maior parte do que sobrou do [0138](./0138-revisao-sem-cascos-e-pos-scan-com-teto.md).
O passo 4b (`matchPorTituloDeAlbum`) existe desde o [0136](./0136-matching-musica-edicao-empate-compilacoes.md)
para trilha/compilação; aqui ele passou a funcionar também para álbum comum.

## Problema medido (produção, 51 faixas em rascunho, MusicBrainz ao vivo)

O passo 4b desistia por três motivos mecânicos, todos no mesmo lugar:

1. **A busca não era frase e não tinha artista.** O cliente montava
   `releasegroup:<termos>` sem aspas e sem `artistname`. O Lucene então tratava
   cada palavra como termo independente, e o sufixo de edição pesava mais que o
   nome do álbum. Medido:
   - `releasegroup:Rio  Collector's Edition` → dez release-groups chamados
     **"Collector's Edition"** (Blake Shelton, Tina Turner...); o "Rio" do Duran
     Duran não aparecia na lista.
   - `releasegroup:Love Songs` → "Love Songs" de Ray Conniff, Mr Brown e Regina
     Belle (todos com score 100 do próprio MusicBrainz); o Al Jarreau, que é o
     dono do arquivo, ficava fora do teto de 3 candidatos.
   - Mesmo padrão em `Lockjaw`, `Like Minds`, `Summer Songs`, `Wonderful Life`,
     `Beyond the Sea`, `Right Here Right Now`.

   Com a frase normalizada mais o artista, os mesmos casos vêm certos de primeira
   (`releasegroup:"Rio" AND artistname:"Duran Duran"` → 2 resultados, o primeiro é
   o Rio).

2. **A tag junta artistas com barra.** `Flume/Chet Faker` não é crédito da fonte
   (lá é `Flume & Chet Faker`); a cláusula de artista com a barra volta vazia e a
   busca caía no modo sem artista, pegando "Lockjaw" de outros artistas.

3. **Reedição repete a mesma faixa em dois discos.** "The Hurting (Deluxe
   Edition)" tem "Pale Shelter", "Mad World", "Change" no disco 1 e no disco 2,
   com título e duração iguais. O desempate existente era só por duração, então o
   caso ficava ambíguo e caía no rascunho — mesmo com a posição da faixa
   (`disc_number` + `track_number`) disponível na tag.

## Decisão

- **Cláusula de busca é frase, com o nome NORMALIZADO** (`matching.NormalizarNome`,
  do 0136): `releasegroup:"<nome>"`. O score da comparação continua sendo medido
  contra o nome cru da tag (`SimilaridadeNormalizada` já faz o máximo entre cru e
  normalizado).
- **O artista do álbum escopa a busca** (`AND artistname:"<album_artist>"`), com
  `artist` de fallback. Compilação (`Various Artists`, `Разные исполнители`,
  vazio) **não** escopa: o crédito genérico não filtra nada e ali o título é o
  único sinal (o que o 0136 já fazia).
- **Ordem das consultas, da mais específica para a mais frouxa** (cada uma é UMA
  requisição; as seguintes só rodam quando as anteriores não acham a faixa):
  1. nome normalizado + artista;
  2. nome normalizado + primeiro nome do artista quando a tag usa `/`;
  3. nome normalizado sem artista;
  4. nome **cru** — há edição que é release-group próprio na fonte
     ("Hybrid Theory (Bonus Edition)"), e a frase normalizada nunca devolve esse
     release-group.
- **Desempate por posição**: entre faixas empatadas no título, a que casa
  `(disc_number, track_number)` vence; falhando a posição exata, o disco sozinho
  vence (a numeração da tag vem de pasta/tag e nem sempre casa com a ordem do
  release). Sem posição declarada, o comportamento antigo (duração) continua, e o
  empate sem sinal segue rejeitado — nunca aplica um "parecido".
- **Fila de revisão conta episódio pela junção** `media_file_entity`: o arquivo de
  episódio aponta para a SÉRIE, e a revisão olhava só `media_file.entity_id`. Era
  o que fazia 19 episódios com arquivo `present` aparecerem como "sem arquivo"
  (`file_count` 0, `path` vazio) e agrupados errado. Mesmo idioma de
  `ObterEntityCard`/`ListarEntityCards` (0034), com `UNION ALL` +
  `COUNT(DISTINCT ...)` para não contar duas vezes.

## Verificação

- `TestBuscarAlbumPorTituloEscopaPorArtista`: a query enviada é a frase
  normalizada + `artistname`.
- `TestBuscarAlbumPorTituloSemArtistaEmCompilacao`: "Various Artists" não escopa.
- `TestBuscarAlbumPorTituloCaiParaBuscaSemArtista`: quando a busca escopada volta
  vazia, a seguinte (sem artista) resolve — sem erro.
- `TestReleaseContemFaixaAlvoDesempataPorDisco` / `TestMelhorFaixaPorTituloDesempataPorDiscoEFaixa`:
  faixa repetida em dois discos resolve pela posição; sem posição continua
  ambíguo.
- `TestMelhorFaixaPorTituloDiscoErradoNaoCasa`: disco/faixa inexistente não vira
  passe livre.
- `TestScanMusicaPassaArtistaEDiscoAoTituloDoAlbum`: o scanner manda
  `album_artist` e `disc_number` no alvo (plumbing ponta a ponta).
- `TestReviewEpisodioContaArquivoDaJuncao`: episódio com arquivo na junção entra
  com `file_count` 1 e agrupado pela pasta, não em "sem arquivo".
- `go build ./... && go vet ./... && go test ./... -count=1` → 42 pacotes ok;
  `make gate-fast` passou.

Medição com as tags reais das 51 faixas em rascunho (MusicBrainz ao vivo), antes
→ depois, nos casos que representam cada família:

| Caso | Antes | Depois |
|---|---|---|
| Al Jarreau — Love Songs | não achava o álbum | resolve |
| Duran Duran — Rio (Collector's) | 10 candidatos "Collector's Edition" | resolve (2 faixas) |
| Katie Melua — Wonderful Life | 3 homônimos com faixa 100% | resolve |
| Flume/Chet Faker — Lockjaw | 3 homônimos de outros artistas | resolve |
| Tears for Fears — The Hurting disco 2 | empate de título | resolve (Prisoner, Suffer the Children) |
| Scott Pilgrim (trilha) | não achava | resolve |
| Various Artists — Twilight | não achava | ainda falha (sufixo de versão) |

## Follow-ups (não feitos aqui)

- **O veto por duração descarta faixa certa.** "In the End" (Hybrid Theory): o
  arquivo tem 216,8s e o MusicBrainz diz 231,6s para o MESMO título único no
  álbum do mesmo artista; a diferença de 15s passa da
  `matching.ToleranciaDuracaoSegundos` (3s) e a faixa volta para o rascunho. O
  gate do 0121 §8 foi decidido como trava de segurança, então afrouxá-lo é
  decisão do dono, não efeito colateral de correção: as opções são tornar a
  duração só desempate quando o título é único e ≥98%, ou escalar a tolerância
  (ex.: 5% da duração).
- **Vocabulário de versão na normalização.** "(Twilight Soundtrack Version)",
  "(single edit)", "(2nd single version)" não são removidos: o segmento só cai
  quando TODAS as palavras são de edição, e "twilight" não é. 7 faixas da trilha
  do Twilight e 1 do Rule The World dependem disso.
- **Álbum que não existe na fonte** (compilação caseira, ex.: "Summer Songs" do
  Jamiroquai com faixas de discos diferentes) nunca resolve por título; hoje cai
  no rascunho, que é o comportamento correto — só vale registrar que não é bug.
- **Cache negativo por álbum na passada**: 9 faixas do mesmo álbum repetem a mesma
  busca; com o teto de 4 min do pós-scan (0138) isso limita o dano, mas ainda é
  custo repetido.
- **ISRC como passo 3.5** (`isrc:` na busca do MusicBrainz): a tag tem ISRC em
  boa parte dos arquivos, é candidato forte para o caso "título com sufixo de
  versão" acima. Continua sem validação de cobertura.
