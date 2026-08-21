# 0121 — MusicBrainz tenta outras releases do release-group quando a faixa-alvo não está na escolhida

> Uma release-group do MusicBrainz costuma ter várias releases (edições,
> maxi-singles, reedições). Hoje o provider escolhe UMA só (a "Official"/a
> primeira) pra montar o álbum inteiro — se a faixa que o dono tem no disco
> não estiver justo nessa edição, o import escopado falha mesmo a faixa
> existindo em outra release do mesmo grupo.

| | |
|---|---|
| **ID** | 0121 |
| **Tipo** | fix |
| **Fase (roadmap)** | Transversal / correção de dado — achado ao validar o 0120 em produção (caso real: Bomfunk MC's / Freestyler). |
| **Docs relacionadas** | [ADR-0027](../docs/adr/0027-modelo-de-musica-hierarquia-artista-album-faixa.md) |
| **Status** | in-progress |
| **Depende de** | [0115](0115-musica-import-local-first.md), [0120](0120-reconciliacao-de-filhos-orfaos-musica-serie.md) |

---

## 1. Objetivo

Ao validar o 0120 em produção, o job de reconciliação retentou a faixa
"Freestyler" do artista "Bomfunk MC's" e falhou de novo, agora com um motivo
concreto logado (não mais throttle/timing):

```
scanner: import escopado (busca textual artista "Bomfunk MC's" album
"Freestyler") falhou: enrich: faixa alvo nao encontrada no album
1ca542d9-eec9-4bad-9e20-7535431f5b1c
```

Investigação (leitura de código + consulta direta à API pública do
MusicBrainz) confirmou a causa raiz: o release-group `1ca542d9-...`
("Freestyler") tem mais de uma release. `escolherRelease`
(`internal/provider/musicbrainz/musicbrainz.go:437`) escolhe **só uma**
(primeira com `status=="Official"`; sem nenhuma, a primeira da lista) e
`montarAlbumOuErro` busca as faixas **só dessa**, definitivamente — nenhuma
outra release do mesmo grupo é tentada. No caso real, a release escolhida
(`32cdfa24-168b-4679-86fc-659eb9c5e651`) é um maxi-single de 4 faixas, todas
com sufixo de versão ("Freestyler (radio edit)", "(alternative radio edit)",
"(Happy Mickey Mouse mix)", "(Missing Link remix)") — nenhuma chamada só
"Freestyler". O arquivo do dono tem tag `title="Freestyler"` (sem sufixo,
`track=5`), então o match de faixa (`escolherFaixaAlvo`/`melhorFaixaPorTitulo`,
`internal/enrich/musica.go`) corretamente não aplica um match "parecido"
contra nenhuma das 4 — mas outra release do MESMO release-group (ex.: um
álbum completo) pode ter a faixa "Freestyler" pura na posição 5, e essa nunca
é tentada.

O mesmo problema estrutural também afeta, em tese, o caminho PRECISO
(`BuscarAlbumDaFaixa`, usado quando o arquivo já tem `RecordingMBID`
embutido): `escolherReleaseGroupDoRecording` sabe exatamente qual release
contém aquele `recordingMBID` (o dado vem do próprio endpoint
`/recording/{id}?inc=releases+release-groups`), mas descarta essa informação
e repassa só o `release-group ID` pra `montarAlbum`, que escolhe uma release
de novo via `escolherRelease` — **possivelmente diferente** da que realmente
contém o recording. Corrigido junto, pelo mesmo mecanismo, já que o alvo
(`RecordingMBID`) já está disponível nesse ponto.

`BuscarAlbumPorMbid` (caminho direto, MBID de release-group já conhecido, sem
faixa-alvo — usado pela tela externa de álbum e por reimport avulso) não
muda: sem alvo de faixa, mantém o comportamento atual (1 release, 1 chamada).

## 2. Jornada do usuário

1. O scanner identifica artista+álbum de uma faixa por match textual (ou o
   job de reconciliação do 0120 retenta), mas o release "canônico" do
   release-group no MusicBrainz não é a edição que o dono tem no disco (ex.:
   dono tem o álbum completo, o "Official" escolhido é um single/reedição
   regional com faixas remixadas).
2. Hoje: import falha com "faixa alvo nao encontrada no album", a faixa fica
   presa em revisão para sempre (mesmo com o 0120, porque o próprio provider
   nunca considera a release certa).
3. Depois deste briefing: quando a release escolhida não contém a faixa-alvo
   (por título/número, ou por `RecordingMBID` exato), o provider tenta as
   próximas releases do mesmo release-group (limitado a um teto, ordem
   Official-primeiro) até achar uma que contenha — ou esgotar o teto e
   degradar como hoje (falha, permanece em revisão, retry throttled do 0120
   cobre o resto).

## 3. Escopo

**Dentro:**

1. Novo tipo `provider.AlvoFaixaAlbum` (`internal/provider/provider.go`):
   ```go
   // AlvoFaixaAlbum identifica, opcionalmente, UMA faixa específica que o
   // chamador precisa encontrar dentro do álbum resolvido -- permite ao
   // provider tentar releases alternativas do mesmo release-group quando a
   // escolhida por padrão (ex.: "Official") não contém essa faixa. Zero value
   // (todos os campos vazios) preserva o comportamento de sempre escolher
   // só 1 release. Prioridade quando mais de um campo é preenchido:
   // RecordingMBID > FaixaTitulo > FaixaNumero (mesma ordem de
   // enrich.FaixaAlvo/escolherFaixaAlvo).
   type AlvoFaixaAlbum struct {
       RecordingMBID string
       FaixaTitulo   string
       FaixaNumero   int
   }
   ```
2. `MusicaAlbumProvider.BuscarAlbumPorNome` ganha um parâmetro
   `alvoFaixa AlvoFaixaAlbum` (entre `nomeAlbum` e `langs`). Único chamador de
   produção (`internal/enrich/musica.go:383`, `buscarAlbumAlvo`) já tem
   `alvo.FaixaTitulo`/`alvo.FaixaNumero` em mãos (struct `FaixaAlvo`) — passa
   a repassá-los.
3. `internal/provider/musicbrainz/musicbrainz.go`:
   - `montarAlbum`/`montarAlbumOuErro` ganham o parâmetro `alvoFaixa
     provider.AlvoFaixaAlbum`.
   - Novo `buscarFaixasComAlvo(ctx, releases []respReleaseResumo, alvoFaixa
     provider.AlvoFaixaAlbum) ([]provider.FaixaExterna, error)`: com
     `alvoFaixa` zero-value, mantém o comportamento atual EXATO (usa
     `escolherRelease`, 1 única chamada a `buscarFaixas`). Com `alvoFaixa`
     preenchido: ordena as releases por prioridade (Official primeiro, resto
     na ordem original — `ordenarReleasesPorPrioridade`, nova função) e tenta
     `buscarFaixas` release a release, na ordem, até achar uma cujas faixas
     satisfaçam `alvoFaixa` (`releaseContemFaixaAlvo`, nova função) — para no
     primeiro sucesso. Teto de `maxReleasesTentadosPorAlvo = 5` releases
     tentadas (custo/rate-limit; release-groups reais têm "dezenas" de
     releases, ver comentário de `escolherRelease`). Sem nenhuma bater dentro
     do teto: retorna as faixas da ÚLTIMA release tentada (mesma degradação
     silenciosa de hoje — quem decide "achou ou não achou a faixa" continua
     sendo `escolherFaixaAlvo` em `internal/enrich/musica.go`, este provider
     só melhora as CANDIDATAS oferecidas).
   - `releaseContemFaixaAlvo`: mesma prioridade/critério de
     `escolherFaixaAlvo`/`melhorFaixaPorTitulo` (`internal/enrich/musica.go`)
     mas AUTOCONTIDO neste pacote (evita import cycle — `provider` não pode
     depender de `enrich`, que já depende de `provider`): MBID exato > título
     por similaridade (`textsim.Similaridade`, mesmo limiar
     `matching.LimiarAutoMatch` já importado neste arquivo) > `TrackNumber`
     exato.
   - `BuscarAlbumDaFaixa`: passa a chamar `c.montarAlbum(ctx, rgID, "", "",
     provider.AlvoFaixaAlbum{RecordingMBID: recordingMBID})` (fecha o mesmo
     gap estrutural no caminho preciso, ver §1).
   - `BuscarAlbumPorMbid`: passa `provider.AlvoFaixaAlbum{}` (sem mudança de
     comportamento).
   - `BuscarAlbumPorNome`: repassa o novo parâmetro `alvoFaixa` recebido da
     interface, sem lógica extra própria.
4. `internal/enrich/musica.go`: `buscarAlbumAlvo` passa a chamar
   `ap.BuscarAlbumPorNome(ctx, artistaMBID, alvo.AlbumNome,
   provider.AlvoFaixaAlbum{FaixaTitulo: alvo.FaixaTitulo, FaixaNumero:
   alvo.FaixaNumero}, langs)`.
5. Atualizar assinatura em TODOS os fakes/mocks que implementam
   `MusicaAlbumProvider` (mecânico, sem lógica nova — cada um só ignora o
   parâmetro novo, exceto onde uma nova asserção fizer sentido):
   `internal/provider/fake/musicbrainz.go`,
   `internal/enrich/musica_test.go`, `internal/scanner/musica_test.go`,
   `internal/server/external_album_test.go`,
   `internal/server/features_0047_test.go`,
   `internal/server/reconciliar_orfaos_test.go`.
6. Testes novos em `internal/provider/musicbrainz/musicbrainz_test.go`:
   - Release-group com 2+ releases: a primeira ("Official") NÃO tem a
     faixa-alvo (por título), a segunda tem — `BuscarAlbumPorNome` (ou
     `montarAlbumOuErro` diretamente) com `alvoFaixa` preenchido deve
     retornar as faixas da SEGUNDA.
   - `alvoFaixa` zero-value: continua chamando `buscarFaixas` só 1 vez
     (nº de requisições HTTP ao endpoint de faixas não aumenta) — prova que o
     caminho sem alvo não regride custo/comportamento.
   - Teto `maxReleasesTentadosPorAlvo`: com mais releases que o teto e
     nenhuma contendo o alvo, tenta no máximo o teto e degrada (não itera o
     release-group inteiro).
   - `BuscarAlbumDaFaixa` com `RecordingMBID` cujo release real não é a
     "Official" do grupo: confirma que agora acha via `AlvoFaixaAlbum{
     RecordingMBID: ...}`.

**Fora (explicitamente):**
- Mudar o motor de similaridade textual (`textsim`) ou o limiar
  `matching.LimiarAutoMatch` — o gap aqui é "qual release perguntar", não
  "quão tolerante é o match de título".
- Qualquer UI nova — o resultado (faixa passa a ser encontrada) já se reflete
  nos fluxos existentes (import automático do scan, reconciliação do 0120).
- Aplicar a mesma lógica de "múltiplas candidatas" para `BuscarArtistaArvore`
  (import completo da discografia) — ali cada release-group vira UM álbum de
  qualquer forma (escolha de release já é só sobre a tracklist exibida, não
  sobre "achar uma faixa específica"), fora do escopo desta correção.

## 4. Contrato técnico

### Dados / modelo
- Nenhuma migração nova, nenhum novo endpoint HTTP do app.

### Backend (Go)
- `internal/provider/provider.go`: novo tipo `AlvoFaixaAlbum`; interface
  `MusicaAlbumProvider.BuscarAlbumPorNome` ganha o parâmetro.
- `internal/provider/musicbrainz/musicbrainz.go`: `montarAlbum`,
  `montarAlbumOuErro`, `BuscarAlbumPorNome`, `BuscarAlbumPorMbid`,
  `BuscarAlbumDaFaixa` (assinatura/chamada); novo `buscarFaixasComAlvo`,
  `ordenarReleasesPorPrioridade`, `releaseContemFaixaAlvo`, constante
  `maxReleasesTentadosPorAlvo`.
- `internal/enrich/musica.go`: `buscarAlbumAlvo` (repassa `alvoFaixa`).
- Fakes/mocks (§3 item 5): assinatura atualizada, sem mudança de
  comportamento nos stubs.

### Arquivos afetados
- `internal/provider/provider.go`.
- `internal/provider/musicbrainz/musicbrainz.go` (+ `musicbrainz_test.go`).
- `internal/provider/fake/musicbrainz.go` (+ `musicbrainz_test.go` se
  necessário).
- `internal/enrich/musica.go` (+ `musica_test.go`).
- `internal/scanner/musica_test.go`.
- `internal/server/external_album_test.go`,
  `internal/server/features_0047_test.go`,
  `internal/server/reconciliar_orfaos_test.go`.

## 5. Critérios de aceite

- [x] Cenário real do Bomfunk MC's (ou equivalente sintético no teste):
      release-group com uma release "Official" que NÃO tem a faixa-alvo e
      outra release do mesmo grupo que tem — `BuscarAlbumPorNome`/import
      escopado acha a faixa na segunda release.
- [x] `BuscarAlbumDaFaixa` (caminho `RecordingMBID`) também tenta a release
      real do recording, não só a "Official" do release-group.
- [x] Caminho sem faixa-alvo (`BuscarAlbumPorMbid`, e `BuscarAlbumPorNome`/
      `BuscarAlbumDaFaixa` com `AlvoFaixaAlbum{}`) preserva o número de
      chamadas HTTP de hoje (1 release só) — sem regressão de custo.
- [x] Teto de releases tentadas respeitado (não itera um release-group
      inteiro quando nenhuma release bate).
- [x] `go build ./...`, `go vet ./...`, `go test ./... -count=1` verdes;
      `make gate-local` sem regressão de cobertura (cobertura de linhas
      75.8%→75.9%, cobertura E2E de jornadas 42.1%→46.4%).
- [ ] Validado em produção (21/08/2026): deploy feito (imagem com o commit
      `6560c64`, CI verde), `saude_biblioteca` disparado manualmente várias
      vezes via `POST /jobs/saude_biblioteca/run` — mas a faixa "Freestyler"
      (entity `4838fee1-8b95-4d72-8142-e4edaa106975`) **continua na fila de
      revisão** (`GET /review`), e **não é o caso "sem dado na fonte"** que
      este briefing previa como fallback legítimo. Investigação direta na API
      pública do MusicBrainz (`release-group/1ca542d9-...?inc=releases`, sem
      tocar o banco de produção) mostra que este release-group tem **13
      releases** (bem acima do normal) e que a faixa "Freestyler" **pura
      existe sim** numa delas — `43c49ea4-b213-4a97-afe0-3e6418374f96`,
      faixa 5 —, só que essa release cai em ~10º lugar na ordem
      Official-primeiro (`ordenarReleasesPorPrioridade`), **além do teto
      `maxReleasesTentadosPorAlvo=5`** implementado neste briefing. Ou seja: o
      mecanismo funciona exatamente como projetado (tenta mais releases,
      respeita o teto, degrada sem loop infinito) — o gap real é que este
      release-group específico é um outlier grande demais para o teto atual,
      não "faixa ausente na fonte". Não foi possível re-exercitar a
      reconciliação ao vivo para ESTE item específico hoje porque
      `last_external_match_attempt_at` dele já tinha sido carimbado mais cedo
      no mesmo dia civil (durante a validação do 0120) e o throttle
      (`retry_days`, mínimo 1 dia) não permite reentrada no mesmo dia por
      nenhum valor válido de configuração — sem SQL direto em produção (não
      autorizado nesta etapa), não há caminho HTTP legítimo pra forçar o
      retry hoje. Ficou fora de escopo deste briefing subir o teto ou mudar o
      critério de escolha; ver follow-up abaixo.

## 6. Notas de implementação / decisões

- `AlvoFaixaAlbum` fica em `internal/provider`, não em `internal/enrich`,
  porque `provider` não pode depender de `enrich` (dependência é na direção
  contrária) — a duplicação de campos com `enrich.FaixaAlvo` é MÍNIMA e
  aceitável (dois estruturas com propósito ligeiramente diferente: uma é
  "alvo completo de import", a outra é só "o que ajuda a escolher a
  release").
- `releaseContemFaixaAlvo` deliberadamente NÃO reusa
  `escolherFaixaAlvo`/`melhorFaixaPorTitulo` de `internal/enrich` (import
  cycle) — é uma cópia funcional mínima (mesma fórmula/limiar), não uma
  abstração compartilhada nova; se divergirem no futuro, revisar os dois
  juntos.
- O teto (`maxReleasesTentadosPorAlvo`) existe porque um release-group real
  pode ter dezenas de releases (reedições, países, remasters) — sem teto,
  uma faixa genuinamente ausente do MusicBrainz inteiro geraria dezenas de
  chamadas HTTP por tentativa de import/reconciliação.
- Este briefing NÃO garante que a faixa "Freestyler" do Bomfunk MC's vai
  necessariamente ser encontrada — só garante que MAIS releases do
  release-group são consideradas. Se nenhuma release real do MusicBrainz
  tiver essa faixa exata, o caso continua legitimamente em revisão (dado
  ausente na fonte, não bug) — validar isso em produção antes de fechar o
  briefing.

## 7. Fora deste briefing / follow-ups

- Se, mesmo depois da rodada 2 (§8), o caso real do Bomfunk MC's ainda não
  resolver, é uma limitação de dado na fonte (MusicBrainz), não um bug —
  fica como candidato a curadoria manual do dono (edição manual do vínculo
  faixa↔álbum), sem novo briefing necessário.
- Tie-break por duração (quando dois candidatos EMPATAM no score de título e
  só um bate a duração) fica FORA desta rodada — o gate de duração (§8) só
  atua como CONFIRMAÇÃO do candidato já escolhido por título (single best
  score, sem empate), nunca resolve um empate que hoje já é rejeitado. Se
  isso se mostrar necessário na prática, vira um briefing novo e pequeno.
- Filtragem de release por metadado (`title`/`disambiguation`) pra
  deprioritizar edições de remix ANTES de buscar as faixas foi cogitada e
  DESCARTADA (ver §8): confirmado ao vivo que o release-group real do
  Bomfunk MC's tem as 13 releases tituladas igualmente "Freestyler", sem
  `disambiguation` distintiva — o dado não existe pra sustentar essa
  heurística neste caso, e não há evidência de que ajudaria em geral.

## 8. Extensão (rodada 2) — teto insuficiente + duração como sinal de match

A validação em produção (§5, 21/08/2026) achou um teto baixo demais para o
release-group real (13 releases, a certa ficando ~10ª). Antes de só
aumentar o número às cegas, o dono pediu pra investigar exatamente que
comparação o projeto já faz hoje e como ferramentas estabelecidas de gestão
de música (LMS/Lyrion, Jellyfin) resolvem esse tipo de ambiguidade.

**Achados da pesquisa:**
- O match hoje é uma cascata sequencial e SEM duração: MBID exato →
  similaridade textual de título (Jaro-Winkler, `matching.LimiarAutoMatch`)
  → `track_number`. Nenhuma combinação simultânea de sinais.
- **Jellyfin** (`MediaBrowser.Providers/Plugins/MusicBrainz/
  MusicBrainzAlbumProvider.cs`) escolhe a release do release-group pegando a
  PRIMEIRA da lista, com um `// TODO: ... Simply taking the first result is
  stupid.` literal no próprio código — pior que o que este projeto já tem
  desde a rodada 1. Não usa AcoustID/Chromaprint no core.
- **LMS/Lyrion** (`slimserver`) nem faz esse tipo de match remoto — usa
  `musicbrainz_id` só como chave de dedup LOCAL de tags já gravadas por uma
  ferramenta externa (Picard); não resolve ambiguidade textual contra o
  MusicBrainz.
- **MusicBrainz Picard** (a ferramenta de referência que resolve exatamente
  este problema) combina similaridade de título com **duração do áudio
  dentro de uma tolerância** (padrão 2s) como parte do score — e o próprio
  AcoustID já embute duração como filtro (recordings com duração muito
  diferente nunca recebem o mesmo fingerprint).
- Filtragem por metadado de RELEASE (`title`/`disambiguation`) pra
  deprioritizar edições de remix sem precisar buscar as faixas foi
  verificada ao vivo contra o release-group real e **não se sustenta**: as
  13 releases se chamam todas só "Freestyler", `disambiguation` vazio na
  maioria — não há sinal aí pra explorar.
- O dado de duração já existe DOS DOIS LADOS sem custo extra: `mediainfo`
  já extrai `Info.General.Duration` (segundos) de todo arquivo de áudio via
  ffprobe (hoje descartado antes de chegar no matching); o MusicBrainz já
  devolve `length` (ms) tanto no recording quanto no track dentro da MESMA
  chamada `GET /release/{id}?inc=recordings+...` que `buscarFaixas` já faz
  (confirmado ao vivo) — só não está mapeado nas structs hoje.

**Decisão:** ainda dentro deste briefing (0121), fazer as duas coisas:

1. Subir `maxReleasesTentadosPorAlvo` de 5 para **15** (cobre o outlier real
   de 13 releases com folga; custo continua desprezível — só entra em jogo
   quando a busca por `alvoFaixa` falha na 1ª tentativa, e MusicBrainz
   aguenta ~1 req/s).
2. Adicionar **duração como sinal de CONFIRMAÇÃO** (não de busca/ordenação —
   não reduz quantas releases são tentadas, só torna o aceite mais robusto
   uma vez que uma release já foi buscada): `provider.FaixaExterna` e
   `provider.AlvoFaixaAlbum` ganham `DuracaoMs int`; `enrich.FaixaAlvo` ganha
   `DuracaoSegundos float64`, populado pelo scanner a partir de
   `mediainfo.Info.General.Duration` (hoje descartado em `factsMusica`/
   `probarTags`). Regra (aplicada em `melhorFaixaPorTitulo` no enrich E em
   `releaseContemFaixaAlvo` no provider — mesma regra, cópias autocontidas
   pelo mesmo motivo de import cycle já documentado em §6):
   - Calcula o candidato de MELHOR score de título como hoje (sem empate no
     topo — tie-break por duração fica fora, ver §7).
   - Se o score bate o limiar E ambas as durações são conhecidas (`> 0` dos
     dois lados): só aceita se `|duração local − duração candidato| <=
     matching.ToleranciaDuracaoSegundos` (nova constante, 3.0s — um pouco
     mais folgada que o padrão de 2s do Picard, pra absorver a imprecisão
     de duração de container do ffprobe vs. decodificação exata).
   - Se qualquer uma das durações for desconhecida (0): pula o gate, mantém
     o comportamento de hoje (sem regressão pra arquivos/dados sem duração
     confiável).
   - Isso é uma trava de SEGURANÇA adicional (reduz falso-positivo de título
     parecido) — nunca torna um match mais permissivo do que hoje.

### Contrato técnico da rodada 2

- `internal/matching/matching.go`: nova constante `ToleranciaDuracaoSegundos
  = 3.0`.
- `internal/provider/provider.go`: `FaixaExterna` ganha `DuracaoMs int`
  (0 = desconhecida); `AlvoFaixaAlbum` ganha `DuracaoMs int` (0 = não
  informada pelo chamador).
- `internal/provider/musicbrainz/musicbrainz.go`:
  - `maxReleasesTentadosPorAlvo`: 5 → 15.
  - `respRelease.Media[].Tracks[]` ganha `Length int \`json:"length"\`` (nível
    do track dentro da release) e `.Recording` ganha `Length int
    \`json:"length"\`` (nível do recording) — mesmo JSON já buscado por
    `buscarFaixas`, sem chamada nova.
  - `buscarFaixas`: popula `FaixaExterna.DuracaoMs` com `tr.Length` quando
    `> 0`, senão `tr.Recording.Length`, senão 0.
  - `releaseContemFaixaAlvo`: no ramo de `FaixaTitulo`, além do score de
    título, aplica o gate de duração descrito acima (usa `alvo.DuracaoMs`/
    `fx.DuracaoMs`, em ms, contra `matching.ToleranciaDuracaoSegundos*1000`).
- `internal/enrich/musica.go`:
  - `FaixaAlvo` ganha `DuracaoSegundos float64`.
  - `melhorFaixaPorTitulo` ganha parâmetro `duracaoAlvoSegundos float64`
    (chamada por `escolherFaixaAlvo`, que repassa `alvo.DuracaoSegundos`);
    aplica o mesmo gate (convertendo para ms para comparar com
    `fx.DuracaoMs`).
  - `buscarAlbumAlvo`: o `provider.AlvoFaixaAlbum` passado a
    `BuscarAlbumPorNome` ganha `DuracaoMs: int(alvo.DuracaoSegundos *
    1000)` (0 quando `DuracaoSegundos` é 0).
- `internal/scanner/musica.go`: `factsMusica` extrai
  `info.General.Duration` (quando o unmarshal de `mi` teve sucesso) e passa
  adiante como novo parâmetro `duracaoSegundos float64` por toda a cadeia
  que hoje só carrega `tags`/`langs`: `resolverFaixa`,
  `importarEAcharPorMbid`, `matchTextual`, `(*Manager).matchTextualFaixa` —
  esta última monta `enrich.FaixaAlvo{..., DuracaoSegundos:
  duracaoSegundos}` nos dois pontos onde `FaixaAlvo` é construída (linhas
  162 e 241 antes desta mudança).
- `internal/scanner/reconciliar.go`: `probarTags` passa a devolver também a
  duração (`(mediainfo.AudioTags, float64, bool)`); `ReconciliarFaixaDraft`
  repassa pra `matchTextualFaixa`.

### Testes novos da rodada 2

- `internal/provider/musicbrainz/musicbrainz_test.go`: cenário sintético
  com DOIS candidatos de título igualmente plausíveis mas SÓ um deles
  batendo a duração (ex.: mesmo texto de título, um exatamente na duração
  do arquivo local e outro com diferença grande) confirma que o gate REJEITA
  o candidato de duração incompatível mesmo com score de título acima do
  limiar (prova a trava de segurança). Cenário complementar com duração
  desconhecida de um dos lados confirma que o gate é PULADO (sem regressão).
  Atualizar/ajustar o teste de teto (`TestBuscarFaixasComAlvoRespeitaTeto`)
  pro novo valor de `maxReleasesTentadosPorAlvo` (mais releases sintéticas
  que 15 pra continuar provando o corte).
- `internal/enrich/musica_test.go`: equivalente pro lado `melhorFaixaPorTitulo`
  /`escolherFaixaAlvo`.
- `internal/scanner/musica_test.go` / `internal/scanner/reconciliar_test.go`:
  confirma que a duração extraída de `mediainfo.Info.General.Duration`
  chega em `FaixaAlvo.DuracaoSegundos` nos dois pontos de construção (import
  por MBID e import textual) e no caminho de reconciliação.

### Critérios de aceite da rodada 2

- [x] `maxReleasesTentadosPorAlvo = 15`; teste de teto atualizado prova o
      novo valor (`TestBuscarFaixasComAlvoRespeitaTeto`: release-group
      sintético com 17 releases, exatamente 15 tentativas antes de degradar).
- [x] Duração chega ponta-a-ponta: arquivo local (`mediainfo`) → `FaixaAlvo.
      DuracaoSegundos` (scanner) → `AlvoFaixaAlbum.DuracaoMs` (enrich) →
      gate em `releaseContemFaixaAlvo` (provider) e em `melhorFaixaPorTitulo`
      (enrich). Confirmado por leitura de código (`internal/scanner/musica.go`,
      `internal/scanner/reconciliar.go`, `internal/enrich/musica.go`,
      `internal/provider/musicbrainz/musicbrainz.go`) e pelos testes novos
      (`TestScanMusicaMbidPropagaDuracaoDoArquivo`,
      `TestScanMusicaBuscaTextualPropagaDuracaoDoArquivo`,
      `TestReconciliarFaixaDraftPropagaDuracaoDoArquivo`).
- [x] Gate de duração REJEITA um candidato de título plausível mas duração
      incompatível (`TestReleaseContemFaixaAlvoGateDeDuracaoRejeitaDivergencia`,
      `TestMelhorFaixaPorTituloGateDuracaoRejeitaDivergencia`); PULA o gate sem
      duração conhecida de um dos lados, sem regressão pra dados incompletos
      (`TestReleaseContemFaixaAlvoGateDeDuracaoPuladoQuandoDesconhecida`,
      `TestMelhorFaixaPorTituloGateDuracaoPuladoQuandoAlvoDesconhecida`,
      `TestMelhorFaixaPorTituloGateDuracaoPuladoQuandoCandidatoDesconhecida`,
      `TestScanMusicaSemDuracaoNoMediainfoChegaZero`,
      `TestReconciliarFaixaDraftSemDuracaoChegaZero`).
- [x] `go build ./...`, `go vet ./...`, `go test ./... -count=1` verdes;
      `make gate-local` sem regressão de cobertura (cobertura Go subiu de
      75.8% para 75.9%, cobertura E2E de 42.1% para 46.4%; 1 aviso novo de
      golangci-lint — sugestão cosmética de staticcheck QF1012 em arquivo de
      teste — e gocyclo com mais funções >10, ambos não-bloqueantes e sem
      função de produção nova acima de 10 (pico real: 10, nas funções
      alteradas por esta rodada)).
- [ ] Validado em produção: reconciliação ou reimport manual da faixa
      "Freestyler" do Bomfunk MC's (entity `4838fee1-8b95-4d72-8142-
      e4edaa106975`) sai da fila de revisão — ou, se ainda não sair,
      documentar o motivo concreto observado (não otimista) antes de
      fechar.

      **Não validado ainda — throttle da rodada 1 continua ativo.** Deploy
      da rodada 2 feito em produção em 21/08/2026 (imagem
      `4aa982ed8a6e9d346e6aaeadac44edf0666f0db5`, logs limpos, schema=25 sem
      migration nova). Duas tentativas de forçar a reconciliação via API
      HTTP (nunca SQL direto, conforme instrução):
      1. `POST /api/v1/jobs/saude_biblioteca/run` — job rodou com sucesso
         (`3 fonte(s) escaneada(s)... 185 em revisão`), mas o número de itens
         em revisão não mudou entre a execução manual da rodada 1 (mesmo dia)
         e esta; a entity continua em `status: "draft"` na fila
         (`GET /api/v1/review`, 185 itens, Freestyler ainda listada).
      2. `POST /api/v1/entities/{id}/enrich-missing` — endpoint que existe e
         *ignora* o throttle, mas é o caminho de enriquecimento retroativo de
         `external_ids` (briefing 0056/0059), não o mesmo código de
         `ReconciliarFaixaDraft`/`ReconciliarFilhosOrfaos` (briefing 0120) que
         de fato tenta casar a faixa-rascunho contra o MusicBrainz; devolveu
         `{"outcome":"none"}` — não há evidência de que essa chamada tenha
         disparado o novo gate de duração pra este caso.

      Motivo concreto: `entity.last_external_match_attempt_at` foi carimbado
      pela tentativa da rodada 1 mais cedo no mesmo dia (21/08/2026); o job
      `saude_biblioteca` só reconsidera uma entity quando já passou
      `retry_days` (mínimo 1 dia = 24h corridas, não meia-noite) desde o
      carimbo — `internal/db/queries/review.sql` (`ListarFaixasDraftOrfas`/
      `ListarSeriesDraftComArquivo`). Não existe, hoje, nenhum endpoint HTTP
      legítimo que force `ReconciliarFaixaDraft` ignorando esse throttle por
      entity — só o `enrich-missing` (código diferente) ignora throttle.
      Próximo passo pra fechar este critério: repetir a validação depois que
      as 24h desde o carimbo da rodada 1 tiverem passado (o job diário às
      04:00 já reconsidera a entity automaticamente) ou, se o dono preferir
      validar mais cedo, um briefing futuro poderia expor um endpoint de
      "forçar reconciliação por entity" que ignore o throttle
      deliberadamente (hoje não existe por design — o throttle existe
      justamente pra não martelar a API do MusicBrainz).
