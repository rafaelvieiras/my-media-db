# 0066 — NFO: `<thumb>` aponta para arquivo local (nunca API interna) e `<rating>` no formato atual do Kodi

> Corrige dois defeitos de conformidade encontrados na validação manual do
> `.nfo` gerado: `<thumb>`/`<actor><thumb>` apontavam para rotas da API
> interna do MyMediaDB (inúteis para Kodi/Jellyfin lendo o arquivo do disco);
> `<rating>` usava o formato pré-`<ratings>` que o Kodi atual pode ignorar.

| | |
|---|---|
| **ID** | 0066 |
| **Tipo** | fix |
| **Fase (roadmap)** | Transversal — Infraestrutura operacional |
| **Status** | done |
| **Docs relacionadas** | [09-nfo-e-export](../docs/09-nfo-e-export.md) |
| **Depende de** | 0062, 0064 |

---

## 1. Objetivo

Ao validar manualmente o primeiro `.nfo` gravado em produção (pós-0065),
dois defeitos de conformidade com o padrão Kodi/Jellyfin/Emby foram
encontrados e confirmados contra documentação oficial:

**A) `<thumb>` (pôster/fundo/ator) referenciava a API interna do
MyMediaDB, não um arquivo local nem uma URL utilizável externamente.**
O `.nfo` real gravado continha:
```xml
<thumb>/api/v1/entities/d059cbe8-.../assets/poster</thumb>
...
<actor>
  <thumb>/api/v1/images?src=https%3A%2F%2Fimage.tmdb.org%2F...jpg</thumb>
</actor>
```
`/api/v1/...` é uma rota RELATIVA da própria API do MyMediaDB (pensada para
o frontend web, que a resolve contra a origem do próprio app). Um
Kodi/Jellyfin lendo esse `.nfo` direto do disco não tem como resolver isso —
não é URL absoluta nem caminho local. Confirmado via wiki oficial do Kodi
(`kodi.wiki/view/NFO_files/Templates`): `<thumb>` deve ser URL absoluta
(`https://image.tmdb.org/...`) ou caminho local.

**Causa raiz:** `MontarNFO` reusa `e.PosterURL`/`e.BackdropURL`/
`Cast[].ProfileURL` do `resolver.Entidade` — os MESMOS campos que
`resolverEntity` já reescreve para a rota de proxy interna
(`s.proxyarCreditos`/`s.urlImagemServida`, pensada para o frontend web
decouplar do CDN da fonte) — sem nunca "desembrulhar" de volta para uma URL
direta, nem preferir o arquivo local quando ele já existe.

**B) `<rating>` usa o formato pré-`<ratings>` (provavelmente ignorado pelo
Kodi atual).** Hoje:
```xml
<rating name="tmdb">6.21</rating>
```
O template atual do Kodi (confirmado via `kodi.wiki/view/NFO_files/Templates`)
espera:
```xml
<ratings>
  <rating name="tmdb" max="10" default="true">
    <value>6.21</value>
  </rating>
</ratings>
```
Sem o wrapper `<ratings>` e o elemento `<value>`, o Kodi moderno pode
simplesmente não reconhecer essa nota ao importar/atualizar a biblioteca.

### Pesquisa que fundamenta a correção (não deixar para o agente decidir)

- **Kodi**: `<thumb>` = URL absoluta ou caminho local (wiki oficial,
  `NFO_files/Templates`). Kodi tem uma convenção de pasta `.actors/` dentro
  da pasta do vídeo para fotos de ator locais (`Ator_Nome.jpg`), MAS: (1) é
  um recurso OPCIONAL do TinyMediaManager ("Download actor images to
  .actors", desligado por padrão); (2) mesmo com a pasta presente, o Kodi
  pode preferir baixar de uma URL válida em `<thumb>` em vez de usar o
  arquivo local (relatos da comunidade, `xbmc/xbmc#25662`); (3) — decisivo —
  **o Jellyfin NÃO suporta essa convenção**: `jellyfin/jellyfin#5252` pede
  exatamente esse suporte e foi fechado como "not planned" pela equipe do
  Jellyfin, que usa sua PRÓPRIA estrutura interna
  (`/metadata/People/<letra>/<Nome>/poster.jpg`, dentro dos dados do
  PRÓPRIO Jellyfin, nunca na pasta de mídia).
- **Conclusão prática**: como o alvo de dogfood deste projeto é Jellyfin (e
  o usuário já observa hoje o Jellyfin encontrando o pôster do filme e a
  foto do ator sozinho, sem pasta `.actors` nem fanart.tv), replicar a pasta
  `.actors` do Kodi seria esforço para um alvo que o Jellyfin explicitamente
  não lê. A correção certa para ATOR é **URL direta do provider** (ex.:
  `https://image.tmdb.org/t/p/w185/...jpg`) — funciona tanto para Kodi
  (baixa e cacheia) quanto para Jellyfin (baixa e cacheia na própria pasta
  `metadata/People`), sem duplicar armazenamento na biblioteca.
- **Para PÔSTER/FUNDO de filme/série já é diferente**: o projeto já
  materializa esses arquivos na pasta real quando o usuário escolhe uma arte
  (0062) — e o Jellyfin já confirma (doc oficial,
  `jellyfin.org/docs/general/server/metadata/nfo/`): "Artwork defined in
  .nfo files with local paths or URLs have priority over remote image
  providers **and images in the media folders**" — ou seja, o Jellyfin JÁ
  escaneia a pasta por convenção de nome (`poster.jpg`) mesmo sem
  `<thumb>` — exatamente o comportamento que `MontarNFO` já tinha para o
  caso "já materializado" (pula o `<thumb>`, e está correto). O que faltava
  era GARANTIR que o pôster/fundo FOSSEM materializados no momento de
  salvar o NFO — hoje "Salvar NFO" não aciona esse download, só o job
  `midias_faltantes` (0064) o faz, em outro momento.

## 2. Jornada do usuário

1. O usuário clica em "Salvar Informações NFO" numa entity que já tem
   pôster/fundo escolhidos (por auto-match ou seleção manual), mas cujos
   arquivos ainda não foram baixados para a pasta real (só estão no cache
   interno).
2. **Hoje**: o `.nfo` é gravado imediatamente, com `<thumb>` apontando para
   a API interna do MyMediaDB — inútil fora do MyMediaDB.
3. **Depois deste briefing**: antes de gravar o `.nfo`, o MyMediaDB
   materializa (baixa/copia para a pasta real) o pôster e o fundo vencedores
   — a MESMA arte que a galeria já mostra como topo — reusando a lógica que
   o job `midias_faltantes` (0064) já usa. O `.nfo` gravado então referencia
   esses arquivos IMPLICITAMENTE pela convenção de nome
   (`poster.jpg`/`<vídeo>-poster.jpg`), sem precisar de `<thumb>` nenhum —
   exatamente como o TinyMediaManager faz (baixa a mídia, depois atualiza o
   NFO).
4. Fotos de ator continuam vindo da URL direta do provider (TMDB) — nunca
   materializadas em disco (ver §3 Fora, `.actors`) — mas agora é uma URL
   real que o Jellyfin/Kodi conseguem buscar sozinhos, em vez da rota da API
   interna do MyMediaDB.
5. Se não houver NENHUM candidato de pôster/fundo (entity ainda sem
   enriquecimento) ou a materialização falhar (rede, permissão), o `.nfo` é
   gravado mesmo assim, só sem `<thumb>` — nunca bloqueia a gravação do NFO
   por causa de arte (mesmo espírito best-effort do `midias_faltantes`).

## 3. Escopo

**Dentro:**
- `gravarNFO` materializa poster+fanart (best-effort, reusando
  `s.materializarKindVencedor` já existente do 0064) ANTES de montar o XML.
- `MontarNFO` para de escrever `<thumb>`/`<fanart><thumb>` a partir de
  `e.PosterURL`/`e.BackdropURL` — remove o parâmetro `InsumosArte`
  inteiramente (fica sem uso depois da mudança acima).
- `<actor><thumb>` passa a usar a URL DIRETA do provider (não a rota de
  proxy interna) — `resolverEntity` ganha um parâmetro para pular
  `proxyarCreditos` quando quem chama é a gravação de NFO.
- `<rating>` ganha o wrapper `<ratings>` + atributos `max`/`default` +
  elemento `<value>`, no formato atual do Kodi.
- Atualizar `docs/09-nfo-e-export.md` com uma nota curta sobre a decisão.

**Fora (explicitamente):**
- Pasta `.actors/` com fotos de ator materializadas localmente — o Jellyfin
  não lê essa convenção (`jellyfin/jellyfin#5252`, fechado "not planned");
  seria esforço para um alvo que este projeto não usa hoje (dogfood é
  Jellyfin). Se o suporte a Kodi puro virar prioridade no futuro, cabe um
  novo briefing.
- `<votes>` dentro de `<rating>` — o projeto não expõe `vote_count`
  hoje em `resolver.Entidade`/`richfields` (só `vote_average`); plumbing
  novo de ponta a ponta fica fora deste fix pontual. `<votes>` é opcional
  no schema do Kodi.
- Qualquer UI/toggle para o usuário escolher "URL vs. local" — a decisão é
  fixa (sempre local quando possível, sempre best-effort) igual ao resto do
  sistema de materialização (0064 já decidiu isso).
- Não mexe no comportamento do job `midias_faltantes` em si (já materializa
  e já grava NFO ausente corretamente) — só alinha o botão manual "Salvar
  NFO" a fazer a MESMA materialização antes de gravar.

## 4. Contrato técnico

### Backend (Go)

**`internal/server/localmedia_handlers.go` — `gravarNFO`:**
Logo após resolver `dir`/`nomeBaseVideo` (linha ~406) e antes de qualquer
coisa relacionada a arte, adicionar (best-effort, loga e segue — mesmo
padrão de `MaterializarMidiasFaltantes`):
```go
if _, err := s.materializarKindVencedor(ctx, ent, kindPoster); err != nil {
    log.Printf("gravarNFO: falha ao materializar poster da entity %q (best-effort, ignorado): %v", ent.ID, err)
}
if _, err := s.materializarKindVencedor(ctx, ent, kindFanart); err != nil {
    log.Printf("gravarNFO: falha ao materializar fanart da entity %q (best-effort, ignorado): %v", ent.ID, err)
}
```
Remover a chamada a `s.insumosArteDaEntity` e a variável `arte` — não são
mais necessárias. `MontarNFO` perde o argumento `arte`.

Remover a função `insumosArteDaEntity` inteira (fica sem nenhum chamador).

**`internal/localmedia/nfo.go`:**
- Remover o tipo `InsumosArte` (sem mais uso).
- `MontarNFO` perde o parâmetro `arte InsumosArte`:
  `func MontarNFO(e resolver.Entidade, usuario CamposUsuarioNFO) ([]byte, error)`.
- Remover os dois blocos:
  ```go
  if !arte.PosterLocal && e.PosterURL != nil { doc.Thumb = *e.PosterURL }
  if !arte.FanartLocal && e.BackdropURL != nil { doc.Fanart = &nfoFanart{Thumb: *e.BackdropURL} }
  ```
  `doc.Thumb`/`doc.Fanart` NUNCA mais são setados a partir de
  `resolver.Entidade` — o pôster/fundo agora é representado SÓ pela
  convenção de nome de arquivo na pasta (já materializada por `gravarNFO`
  antes de chamar `MontarNFO`). Isso torna os campos `Thumb`/`Fanart` de
  `nfoDoc` mortos em termos de ESCRITA — mas continuam existindo no struct
  porque `xml:",any"` (Extras, briefing 0065) precisa deles para reconhecer
  esses elementos como NOMEADOS (não caírem em `Extras`) caso um `.nfo`
  anterior os tenha (ex.: escrito por versão antiga do MyMediaDB, ou por
  outra ferramenta) — nesse caso eles são descartados silenciosamente na
  regravação (não são campo de usuário, são campo MANAGED que agora é
  "sempre vazio"), o que é o comportamento correto.
- Ajustar `nfoRating`/`nfoDoc` para o formato `<ratings>`:
  ```go
  // nfoRatings é o wrapper <ratings> (Kodi atual) em volta de uma ou mais <rating>.
  type nfoRatings struct {
      Ratings []nfoRating `xml:"rating"`
  }

  // nfoRating é <rating name="tmdb" max="10" default="true"><value>X</value></rating>.
  type nfoRating struct {
      Name    string  `xml:"name,attr"`
      Max     int     `xml:"max,attr"`
      Default bool    `xml:"default,attr"`
      Value   float64 `xml:"value"`
  }
  ```
  `nfoDoc.Rating *nfoRating` vira `nfoDoc.Ratings *nfoRatings \`xml:"ratings,omitempty"\``.
  Em `MontarNFO`:
  ```go
  if e.VoteAverage != nil {
      doc.Ratings = &nfoRatings{Ratings: []nfoRating{{Name: "tmdb", Max: 10, Default: true, Value: *e.VoteAverage}}}
  }
  ```
- `actorsDe` continua igual (`Thumb: derefStr(c.ProfileURL)`) — o que muda é
  QUAL `ProfileURL` chega em `e.Cast` (ver `resolverEntity` abaixo), não
  este helper.

**`internal/server/entities.go` — `resolverEntity`:**
Assinatura ganha um parâmetro:
```go
func (s *Server) resolverEntity(ctx context.Context, id, lang string, proxiarImagens bool) (resolver.Entidade, bool, error)
```
Na linha que hoje é `s.proxyarCreditos(resolvida.Cast, resolvida.Crew, resolvida.Companies)`:
```go
if proxiarImagens {
    s.proxyarCreditos(resolvida.Cast, resolvida.Crew, resolvida.Companies)
}
```
Quando `proxiarImagens=false`, `resolvida.Cast[].ProfileURL` (e
Crew/Companies, sem consequência prática — NFO não os usa) ficam com a URL
DIRETA do provider (`richfields.CastDe`/`urlCreditoDe`), exatamente o que
`actorsDe`/`<actor><thumb>` precisa.

**Chamadores de `resolverEntity` — atualizar todos (7 pontos):**
| Arquivo:linha | Contexto | `proxiarImagens` |
|---|---|---|
| `entities.go:617` (`getEntity`) | resposta JSON do frontend web | `true` |
| `artwork_missing.go:57` | endpoint de artwork | `true` |
| `completeness_recalc.go:97` | recálculo de completude, não usa imagem | `true` |
| `enrich.go:143` (`responderResolvida`) | resposta JSON pós enrich/import | `true` |
| `override.go:334` | resposta JSON pós override | `true` |
| `midias_faltantes.go:145` (`gravarNFOSeFaltante`) | grava `.nfo` | **`false`** |
| `localmedia_handlers.go:361` (`writeEntityNfo`) | grava `.nfo` | **`false`** |

### Arquivos afetados
- `internal/server/localmedia_handlers.go` (gravarNFO, remove insumosArteDaEntity)
- `internal/server/entities.go` (resolverEntity + getEntity)
- `internal/server/artwork_missing.go`, `completeness_recalc.go`,
  `enrich.go`, `override.go`, `midias_faltantes.go` (só o call site)
- `internal/localmedia/nfo.go` (InsumosArte removido, nfoRating/nfoRatings, MontarNFO)
- `internal/localmedia/nfo_test.go` (atualizar todos os call sites de
  `MontarNFO` — perde o argumento `arte`; atualizar asserts de
  `<thumb>`/`<rating>`; `TestMontarNFOThumbFanartOmitidosQuandoLocal` deixa
  de fazer sentido como está — pôster/fundo NUNCA mais viram `<thumb>` a
  partir do resolver, então o teste vira "prova que `<thumb>`/`<fanart>`
  NUNCA são escritos a partir de `e.PosterURL`/`e.BackdropURL`", independente
  de `PosterLocal`/`FanartLocal` — que nem existem mais)
- `internal/server/localmedia_handlers_test.go` e `internal/server/entities_test.go`
  (ou equivalente) — atualizar chamadas a `resolverEntity` com o novo
  parâmetro.
- `docs/09-nfo-e-export.md`

## 5. Critérios de aceite

- [ ] `.nfo` gravado para uma entity com pôster/fundo já escolhidos (mas
      ainda não materializados) passa a ter o arquivo físico
      (`poster.jpg`/`fanart.jpg` ou variante prefixada) na pasta, e o `.nfo`
      NÃO contém `<thumb>`/`<fanart>` (convenção de nome basta).
- [ ] `.nfo` gravado para uma entity SEM nenhum pôster/fundo disponível não
      quebra (grava normalmente, sem `<thumb>`/`<fanart>`).
- [ ] `<actor><thumb>` contém uma URL absoluta (`https://...`), nunca
      `/api/v1/...`.
- [ ] `<rating>` sai como `<ratings><rating name="tmdb" max="10"
      default="true"><value>N</value></rating></ratings>`; XML segue bem
      formado; `<ratings>` ausente por completo quando `VoteAverage` é nil
      (mesmo comportamento de omissão de hoje).
- [ ] Testes de `MontarNFO` existentes (0065: preservação de campos de
      usuário e de elementos desconhecidos) continuam passando sem
      alteração de intenção, só ajuste de assinatura.
- [ ] `resolverEntity` chamado por `getEntity` continua devolvendo URLs
      proxiadas (`/api/v1/images?src=...`) — sem regressão no frontend web.
- [ ] `go build ./...`, `go vet ./...`, `go test ./... -count=1` e
      `make gate-local` passam sem regressão.
- [ ] Reproduzir manualmente contra o container de produção (ou dev):
      gravar o `.nfo` da mesma entity usada na validação original
      (Sabrina, `d059cbe8-...`) e confirmar visualmente que `<thumb>`,
      `<actor><thumb>` e `<ratings>` estão corretos no arquivo final.

## 6. Notas de implementação / decisões

- A materialização best-effort de poster/fanart dentro de `gravarNFO`
  DUPLICA uma chamada a `montarGruposArtwork` (dentro de
  `materializarKindVencedor`) que o resto do fluxo de `writeEntityNfo` não
  fazia antes — aceito conscientemente: é o mesmo padrão que
  `MaterializarMidiasFaltantes` já usa chamando em loop para vários kinds, e
  o custo (uma leitura de assets + eventual download) é aceitável para uma
  ação explícita de usuário ("Salvar NFO" não é um endpoint de alta
  frequência).
- Decisão explícita (não deixar o agente escolher): erros de materialização
  de arte dentro de `gravarNFO` são **best-effort** (log + segue), NÃO
  abortam a gravação do `.nfo`. Motivo: manter "Salvar NFO" funcionando
  mesmo com uma falha pontual de rede/imagem, e consistência com o
  tratamento de erro já estabelecido em `MaterializarMidiasFaltantes`
  (0064) — um problema de imagem não devia impedir a escrita dos metadados
  textuais, que é sempre possível.
- Decisão explícita: `.actors` fica de fora (ver §3 Fora) — Jellyfin não lê
  essa convenção (confirmado via issue oficial do projeto), e replicar o
  comportamento do TinyMediaManager aqui seria construir para um alvo
  (Kodi puro) que não é o ambiente real de dogfood do projeto hoje.
- Pesquisa: Kodi wiki `NFO_files/Templates` (via proxy `r.jina.ai`, mesmo
  método já usado no briefing anterior — fetch direto a `kodi.wiki` retorna
  403); Jellyfin docs oficiais
  (`jellyfin.org/docs/general/server/metadata/nfo/`); GitHub issue
  `jellyfin/jellyfin#5252` (`.actors` fechado "not planned"); GitHub issue
  `xbmc/xbmc#25662` (Kodi às vezes ignora imagem local de ator a favor de
  URL); docs do TinyMediaManager (`tinymediamanager.org/docs/movies/settings`,
  "Download actor images to .actors" é opt-in, desligado por padrão).

## 7. Fora deste briefing / follow-ups

- Suporte a `.actors` (Kodi-only) — só se algum dia o dogfood também for
  Kodi puro.
- `<votes>` em `<rating>` — precisa de `vote_count` de ponta a ponta
  (richfields → resolver.Entidade → nfo.go); não crítico.
- Fallback de URL direta para pôster/fundo quando a materialização falha —
  hoje simplesmente omite `<thumb>`; se isso se mostrar insuficiente na
  prática (usuário reclama de pôster ausente no Kodi/Jellyfin por falha de
  rede pontual), cabe um briefing futuro adicionando esse fallback com a
  URL do candidato vencedor (`montarGruposArtwork`'s `SrcURL`).
