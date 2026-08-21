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
| **Status** | done |
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
- [ ] Validado em produção: reconciliação (0120) ou reimport manual da faixa
      "Freestyler" do Bomfunk MC's sai da fila de revisão (ou, se a faixa
      realmente não existir em NENHUMA release do release-group real do
      MusicBrainz — a validado antes de fechar —, confirma que o teto foi
      respeitado e a faixa permanece corretamente em revisão, sem tentar
      infinitas releases).

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

- Se, após este fix, o caso real do Bomfunk MC's ainda não resolver (nenhuma
  release do release-group real tiver a faixa "Freestyler" pura), é uma
  limitação de dado na fonte (MusicBrainz), não um bug — fica como
  candidato a curadoria manual do dono (edição manual do vínculo faixa↔
  álbum), sem novo briefing necessário.
