# 0129 — Match de série deriva o título da pasta da série, não do arquivo de episódio

> Série com nome de episódio no padrão `Serie - SxxEyy - Título` volta a casar no
> auto-match, saindo da fila de Revisão sem candidato.

| | |
|---|---|
| **ID** | 0129 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 4 — Enriquecimento (auto-match e revisão) |
| **Status** | in-progress |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md), [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md) |
| **Depende de** | 0050 (auto-match), 0060 (prévia de candidato), 0084 (títulos alternativos) |

---

## 1. Objetivo

O auto-match de **série** hoje deriva o título de busca do primeiro arquivo de
mídia da entity — que, numa série, é um arquivo de **episódio**. O parser de
nome (`naming.Parse`) não remove o `SxxEyy` nem o título do episódio, então a
consulta sai poluída:

```
"Os gárgulas - S03E08 - Genesis Undone"  ->  "Os gárgulas S03E08 Genesis Undone"
"Star Wars A Guerra dos Clones - S04E14 - Amigo em perigo"
```

Com essa string o provider devolve pouco ou nada, e o `Pontuar` fica muito
abaixo do `LimiarAutoMatch` (92): 58.1 para o primeiro caso, 80.8 para o
segundo. Resultado observado em produção (05/nov/2026, sala): as duas séries
pararam na fila de Revisão com confiança 40, **sem candidato** (`match-preview`
→ `{"found":false,"method":"none"}`), sem id externo, sem sinopse, com NFO
gravado **vazio** e sem arte na pasta — e portanto invisíveis como "sem arte"
em qualquer cliente.

O nome da **pasta** da série carrega exatamente o que a busca precisa
(`Nome (Ano)`), e o mesmo `naming.Parse` já o extrai corretamente:

| pasta | `Parse` | score contra o candidato certo |
|---|---|---|
| `Os Gárgulas (1994)` | `Os Gárgulas` + 1994 | alto |
| `Star Wars - The Clone Wars (2008)` | `Star Wars The Clone Wars` + 2008 | 100.0 |
| `Chuck (2007)` | `Chuck` + 2007 | 100.0 |

## 2. Jornada do usuário

1. O dono cadastra a pasta de séries como fonte da biblioteca e roda o scan.
2. O scanner cria as entities de série (rascunho) e grava os arquivos.
3. O auto-match tenta identificar cada série.
4. **Antes:** a consulta era o nome do episódio; nada casava; a série ia para a
   fila de Revisão sem candidato e sem arte.
5. **Depois:** a consulta é o nome da pasta da série; o candidato aparece, o
   auto-match aplica (score ≥ 92) e o enriquecimento segue o caminho normal —
   sinopse, ids externos, artes no inventário e materialização na pasta real.
6. Quando o score não passar do limiar, a fila de Revisão mostra **candidato
   real** (`match-preview`) em vez de "nenhum resultado", e o dono decide.

## 3. Escopo

**Dentro:**
- Derivação de título/ano para `type='tv'` a partir da pasta da série.
- Extração da regra "pasta de temporada sobe um nível" para o pacote `naming`
  (hoje vive só em `internal/scanner`), reusada pelos dois fluxos.
- Testes de unidade da derivação e do parser.

**Fora (explicitamente):**
- Mudar o limiar de auto-match (92) ou o de baixa confiança (70).
- Mudar a pontuação (`matching.Pontuar`) ou o uso de títulos alternativos.
- Alterar `naming.Parse` para descartar `SxxEyy` em nomes de arquivo: o parser é
  compartilhado com o scan de filme e com o casamento de episódio; a correção
  aqui é sobre **qual string** é consultada, não sobre o parser.
- Re-match retroativo de tudo que já está na fila (o job de enriquecimento
  retroativo e a próxima passada do auto-match cobrem, sem código novo).

## 4. Contrato técnico

### Backend (Go)

- `internal/naming` ganha um arquivo próprio para a regra de pasta de série:
  - `EhPastaTemporada(nome string) bool` — a MESMA regra que hoje está em
    `internal/scanner/serie.go` (regexp `(?i)^(season|temporada|s)[ ._-]*\d{1,4}$`
    mais `specials`/`especiais`).
  - `PastaDaSerie(path string) string` — devolve a pasta da série de um arquivo:
    se a pasta imediata é pasta de temporada, sobe um nível; senão é a própria.
- `internal/scanner/serie.go` passa a usar `naming.EhPastaTemporada` e
  `naming.PastaDaSerie` em `derivarSerie`/`ehPastaTemporada` (remove o regexp
  duplicado; `root` continua sendo usado só para o caso de arquivo solto na raiz
  da fonte).
- `internal/server/automatch.go`, `tituloAnoDoArquivo`:
  - assinatura passa a receber a entity (`sqlcgen.Entity`) em vez de só o ID —
    os dois chamadores (`tentarAutoMatch`, `montarMatchPreview`) já a têm;
  - para `ent.Type == "tv"`: título/ano vêm de
    `naming.Parse(filepath.Base(naming.PastaDaSerie(arquivo.Path)))`;
  - guarda: se a pasta derivada for a **raiz da fonte** (`library_source.path`
    do arquivo, via `ObterLibrarySource`), não usar (evita buscar "Series");
    nesse caso cai para `ent.Title` + ano de `release_date`;
  - demais tipos (filme, faixa) mantêm o comportamento atual, byte a byte.
- `internal/server/leitura.go`/`entities.go`: nenhuma mudança de interface —
  `s.leitura()` já devolve `*sqlcgen.Queries`, com `ObterLibrarySource`.

### Arquivos afetados
- `internal/naming/serie_pasta.go` (novo)
- `internal/naming/serie_pasta_test.go` (novo)
- `internal/scanner/serie.go`
- `internal/server/automatch.go`
- `internal/server/automatch_test.go` (novo, derivação pura)

## 5. Critérios de aceite

- [ ] `naming.PastaDaSerie` sobe um nível para `Season 01`, `Temporada 1`,
      `S1`, `Specials`, `Especiais` e devolve a pasta imediata nos outros casos.
- [ ] `tituloAnoDoArquivo` de uma série cujo arquivo é
      `Os gárgulas - S03E08 - Genesis Undone.mkv`, na pasta
      `…/Os Gárgulas (1994)/Season 03/`, produz `Os Gárgulas` + 1994.
- [ ] O mesmo para `Star Wars A Guerra dos Clones - S04E14 - ….mkv` →
      `Star Wars - The Clone Wars` + 2008.
- [ ] Arquivo solto na raiz da fonte não produz o nome da fonte como título.
- [ ] `Pontuar` com o título derivado da pasta passa do `LimiarAutoMatch` (92)
      contra o candidato real do provider.
- [ ] Filme continua derivando do nome do arquivo (teste de regressão).
- [ ] `go build ./... && go test ./...` verde.

## 6. Notas de implementação / decisões

- **Por que não descartar `SxxEyy` no `naming.Parse`**: o parser serve ao scan
  (que precisa do nome do arquivo para casar episódio) e ao import de filme. Uma
  mudança ali atinge caminhos que hoje estão corretos. A causa raiz é perguntar
  ao **arquivo errado**; a correção fica na pergunta.
- **Por que a pasta e não `entity.title`**: o comentário original de
  `tituloAnoDoArquivo` recusa confiar no título bruto de um rascunho (o scan
  pode ter criado a entity com nome ruim). A pasta é a mesma fonte que o scan
  usou para nomear a série, mas com o ano, que o `entity.title` não guarda.
- **Por que extrair para `naming`**: a regra de "pasta de temporada" passa a ter
  uma dona só. `internal/naming` já é pacote folha (só depende de `textsim` na
  prática), então `internal/server` pode consumi-lo sem ciclo.
- **Por que a guarda da raiz da fonte**: sem ela, uma série cujos arquivos estão
  soltos em `…/Videos/Series/` consultaria o provider com "Series" e poderia
  casar com o título errado — falso-positivo silencioso, o pior caso do
  auto-match.

## 7. Fora deste briefing / follow-ups

- Briefing 0130: healthcheck da imagem respeita `PORT` (o check fixo em 8080
  deixa o container eternamente `unhealthy` na sala, onde a porta é 8097).
- Avaliar, em outro briefing, um "re-match em lote" a partir da fila de Revisão
  para séries já presas sem candidato (hoje cada uma precisa de clique).
