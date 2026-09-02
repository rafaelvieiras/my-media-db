# 0126 — Fingerprint acústico (Chromaprint/AcoustID) como último fallback do matching de faixa

> Quando um arquivo de áudio não tem MBID embutido nem metadado textual
> confiável (rip antigo, CD sem tag, download de fonte obscura), o pipeline
> de matching hoje cai direto para `faixa` `status=draft` — permanentemente,
> mesmo que o ÁUDIO em si seja perfeitamente identificável. Este briefing
> adiciona um passo 4.5 novo — fingerprint acústico via Chromaprint,
> consultado contra o AcoustID — entre a busca textual (passo 4) e o
> fallback para draft (passo 5), fechando o único caso de borda que nenhuma
> abordagem baseada em tag/nome resolve.

| | |
|---|---|
| **ID** | 0126 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Música (artista/álbum/faixa) |
| **Docs relacionadas** | [ADR-0027](../docs/adr/0027-modelo-de-musica-hierarquia-artista-album-faixa.md), [ADR-0029](../docs/adr/0029-import-de-musica-local-first-por-faixa-possuida.md), [03-arquitetura](../docs/03-arquitetura.md) |
| **Status** | done |
| **Depende de** | [0105](0105-fundacao-schema-musica.md), [0106](0106-provider-musicbrainz-cover-art-archive.md), [0107](0107-scanner-matching-arquivo-faixa.md), [0115](0115-musica-import-local-first.md) |

---

## 1. Objetivo

O pipeline de matching de música (`internal/scanner/musica.go`, comentário de
topo do arquivo) depende inteiramente de dois sinais: **MBID já embutido** na
tag, ou **busca textual** por nome (artista/álbum/faixa via Jaro-Winkler,
`matching.LimiarAutoMatch`). Quando nenhum dos dois existe — tag ausente,
tag genérica ("Track01", sem nome confiável) ou sem MBID — o passo 5 do
pipeline (`criarFaixaDraft`) cria a `faixa` como `status=draft` **sem
nenhuma `entity_relation`** e o arquivo fica órfão para sempre: nenhum
mecanismo hoje tenta se identificar sozinho de novo a partir do ÁUDIO em si
(o job de reconciliação do briefing 0120 retenta o mesmo matching textual
periodicamente, mas contra os MESMOS dados de tag ausentes — nunca resolve
um arquivo genuinamente sem metadado).

Pesquisa de mercado sobre como as ferramentas mais respeitadas do nicho de
tagging/organização de música resolvem esse cenário específico (arquivo sem
metadado textual confiável) confirma um único mecanismo capaz disso, usado
tanto por **beets** quanto por **MusicBrainz Picard** (referência do nicho):
**fingerprinting acústico via Chromaprint** — biblioteca que gera uma
impressão digital do CONTEÚDO DO ÁUDIO, não do metadado — **consultado
contra o serviço público AcoustID** (acoustid.org, API gratuita para uso
não-comercial, mapeia fingerprint → MBIDs de recording no MusicBrainz).
Nenhuma das outras ferramentas pesquisadas para o mesmo cenário (Jellyfin,
Kodi, LMS/Lyrion, Navidrome, Lidarr) tem equivalente no core — Jellyfin e
LMS, por exemplo, dependem só de tag/nome (o próprio 0121 §8 já documentou
que o provider de música do Jellyfin escolhe a release "estupidamente", sem
nenhum sinal acústico).

Ordem de precedência de mercado (documentada explicitamente pelo Picard, que
este briefing adota): fingerprint acústico é **sempre o último fallback**,
nunca antes de MBID/tag explícita — tag explícita vence sempre quando
existe, e fingerprint é caro computacionalmente (roda o arquivo inteiro por
um algoritmo de DSP), então só compensa entrar quando os sinais baratos já
falharam. É exatamente o buraco que este briefing insere: **entre** o passo
4 (textual) e o passo 5 (draft) do pipeline já existente.

## 2. Jornada do usuário

1. O dono tem um arquivo de áudio na biblioteca de música sem tag MBID, sem
   `Title`/`Album`/`Artist` confiáveis (ou com tags claramente inúteis, tipo
   "Track01" — cenário comum em rips antigos ou downloads de fonte obscura).
2. **Hoje:** o scanner cria uma `faixa` `status=draft` sem nenhuma relação de
   artista/álbum — o arquivo fica na fila de revisão/órfãos para sempre,
   dependente de correção manual.
3. **Depois deste briefing:** quando os passos 1–4 do pipeline falham (sem
   MBID, sem match textual plausível), o scanner calcula o fingerprint
   acústico do arquivo (via `fpcalc`, subprocesso), consulta o AcoustID, e —
   se a resposta trouxer um `recording` MBID com confiança suficiente —
   segue o MESMO caminho de import ESCOPADO já usado pelos passos 2/3
   (`enrich.ImportarFaixaPossuida`, ADR-0029): importa só o artista +
   álbum daquela faixa + a própria faixa, **nunca a discografia inteira**.
4. Se o fingerprint não bater em nenhum candidato plausível (áudio
   genuinamente desconhecido do AcoustID, ou `fpcalc` indisponível no
   container): o arquivo continua caindo no passo 5 (draft) — comportamento
   atual preservado, agora como fallback de ÚLTIMA instância de verdade, não
   mais o único fallback possível.
5. O dono pode acompanhar que o passo de fingerprint rodou pelo log do
   scan/job (nenhuma UI nova é necessária — a faixa recém-identificada
   aparece nas telas existentes de biblioteca/detalhe, igual qualquer outro
   match automático).

## 3. Escopo

**Dentro:**

1. Cálculo de fingerprint acústico via `fpcalc` (binário do projeto
   Chromaprint) como **subprocesso** — mesmo padrão arquitetural que
   `internal/mediainfo` já usa para `ffprobe` (subprocess, não CGO):
   dependência de RUNTIME, checada/injetável, nunca de build. Ausência do
   binário DEGRADA em silêncio (loga um aviso, pula o passo), nunca derruba
   o scan — mesmo contrato que `mediainfo` já tem para `ffprobe` ausente.
2. Client HTTP do AcoustID (novo pacote) que consulta o endpoint de lookup
   público (`https://api.acoustid.org/v2/lookup`) com o fingerprint + a
   duração do áudio (o AcoustID exige os dois — a duração já é extraída
   pelo `mediainfo`/`ffprobe` hoje, ver briefing 0121 §8) e devolve
   candidato(s) de `recording` (MBID) com escore de confiança.
3. Nova capacidade opcional de provider, `FingerprintProvider`
   (`internal/provider/provider.go`), no mesmo molde de
   `FaixaResolverProvider`/`MusicaAlbumProvider`: descoberta via
   type-assert, degrada em silêncio quando o provider registrado não a
   implementa (ex.: se o dono nunca configurou a chave do AcoustID, ou o
   `fpcalc` não está disponível no container).
4. Novo passo 4.5 no pipeline de `internal/scanner/musica.go`
   (`resolverFaixa`), disparado SÓ quando o passo 4 (busca textual) já
   falhou — nunca antes, nunca em paralelo. Ao achar um `recording` MBID
   plausível via fingerprint, reaproveita o MESMO caminho de import
   escopado dos passos 2/3 (`resolverArtistaMBID` + `ImportarFaixaPossuida`
   via `MusicaAlbumProvider.BuscarAlbumDaFaixa`, já implementado pelo
   briefing 0121) — **zero lógica de import nova**, o fingerprint só produz
   um `RecordingMBID` que entra no mesmo cano que o passo 2 já usa.
5. Config de credencial da API key ("client key") do AcoustID via
   `internal/integration` (mesmo mecanismo do briefing 0095/0096 já usado
   por TMDB/TVDB/fanart.tv): registrável pela UI de Integrações, com
   fallback de variável de ambiente de bootstrap — nunca hardcoded.
6. Log/observabilidade mínima: o scan/job registra quando o passo de
   fingerprint rodou e o resultado (achou/não achou candidato) — reusa o
   mecanismo de log já existente no pipeline (`log.Printf`, mesmo padrão dos
   outros passos), sem novo endpoint ou tela.

**Fora (explicitamente):**

- **Contribuir fingerprints de volta ao AcoustID** (submissão da impressão
  digital calculada para a base pública). O AcoustID distingue duas chaves:
  uma "client key" (aplicação, usada para CONSULTAR/lookup — é a única que
  este briefing precisa) e uma "user key" (pessoal, usada para SUBMETER
  fingerprints novos). Submeter é prática do Picard só depois de o dono
  confirmar manualmente que o match está certo — contribuição é opt-in de
  mercado, sem relação com a capacidade de consulta, e fica fora de escopo
  aqui.
- **Rodar fingerprint em toda faixa, sempre.** Cálculo de DSP sobre o
  arquivo inteiro é caro (ordens de magnitude mais lento que abrir tags);
  só entra quando os sinais baratos (MBID, texto) já falharam — nunca como
  primeira tentativa nem em paralelo com eles.
- **UI nova.** Nenhuma tela nova é necessária — o resultado (faixa
  identificada) já se reflete nos fluxos existentes de biblioteca/detalhe/
  fila de revisão; no máximo o log do job existente expõe que o passo de
  fingerprint rodou.
- **Ação "reconciliar via fingerprint" avulsa/sob demanda numa tela.** O
  gatilho é sempre o próprio pipeline do scanner (e, por extensão, a
  reconciliação periódica de faixas-rascunho do briefing 0120, que já
  chama o mesmo `matchTextualFaixa`/pipeline equivalente) — não um botão
  novo na UI de Revisão/Órfãos. Se o dono quiser forçar isso fora do ciclo
  normal, o mecanismo já existente de `force-reconcile` por entity
  (briefing 0122) é reaproveitado sem mudança, desde que o passo de
  fingerprint esteja embutido no mesmo caminho de reconciliação.
- **Detecção de duplicata via fingerprint** (ex.: dois arquivos = a mesma
  gravação, mesmo com tags diferentes). Fora do escopo — este briefing só
  cobre IDENTIFICAÇÃO de faixa sem match, não deduplicação.
- **Suporte a outros algoritmos de fingerprint** (ex.: `MusicBrainz Picard`
  também aceita AcoustID via `pyacoustid`, mas há outros serviços de
  fingerprint no mercado) — Chromaprint/AcoustID é o único par escolhido,
  por ser o padrão de fato do nicho e não exigir infraestrutura própria.

## 4. Contrato técnico

### Dados / modelo

- Nenhuma migração de schema nova. O resultado do fingerprint só produz um
  `RecordingMBID` que entra no MESMO caminho de import escopado que os
  passos 2/3 do pipeline já gravam (`entity type=faixa`, `entity_relation`
  artista/álbum, `extension_data core.music@1`) — nenhum campo novo em
  `entity`/`media_file` é necessário para este briefing. (Se, na
  implementação, fizer sentido persistir o próprio fingerprint calculado
  para evitar recalcular em re-scans sem mudança de arquivo, isso é uma
  decisão de implementação a avaliar contra o custo de armazenamento —
  ver §6; não é pré-requisito do briefing.)

### Config / credencial

- Nova integração `internal/integration.Descriptor` para AcoustID (slug
  proposto: `acoustid`), com um único `Campo` do tipo `TipoSegredo` (a
  "client key"/API key da aplicação, obtida registrando o app em
  acoustid.org — gratuita, uso não-comercial). `EnvVar` de bootstrap (ex.:
  `MYMEDIADB_ACOUSTID_CLIENT_KEY`), seguindo o mesmo padrão TMDB/TVDB/
  fanart.tv já registrado em `cmd/mymediadb/main.go`.
- `Configurada(efetivos)`: exige a client key preenchida (efetivo, banco
  com precedência sobre env var, mesmo resolver de sempre).
- Sem a integração configurada E/OU sem `fpcalc` disponível no container:
  o provider AcoustID simplesmente não implementa `FingerprintProvider` (ou
  implementa mas retorna "indisponível" de forma consistente) — o scanner
  faz type-assert, não encontra a capacidade, pula o passo 4.5 em silêncio
  (mesmo contrato de degradação de qualquer outra capacidade opcional do
  projeto).

### Backend (Go)

- **`internal/fingerprint`** (nome de pacote sugerido — a critério de quem
  implementar; alternativa: `internal/acoustid`): wrapper do `fpcalc` como
  subprocesso, mesmo padrão de `internal/mediainfo` (`os/exec`, caminho do
  binário injetável para teste, checagem de presença no boot que DEGRADA
  em vez de falhar). Função de assinatura próxima a
  `CalcularFingerprint(ctx, path string) (fingerprint string, duracaoSegundos float64, err error)`.
- Novo client HTTP AcoustID dentro do mesmo pacote ou em subpacote irmão
  (ex.: `internal/fingerprint/acoustid` ou junto de outros providers em
  `internal/provider/acoustid`), seguindo o MESMO padrão arquitetural que
  `internal/provider/musicbrainz` já usa: client injetável (`http.Client`,
  `baseURL` para teste), rate limit CLIENT-SIDE (AcoustID documenta **no
  máximo 3 requisições/segundo**, mesmo padrão de mutex+timestamp já usado
  em `internal/provider/musicbrainz/ratelimit.go` — reaproveitar a mesma
  ideia, não necessariamente o mesmo código, já que o intervalo é
  diferente), timeout padrão, e resolução da client key via
  `internal/integration.Resolver` (mesmo mecanismo de TMDB/TVDB).
- `internal/provider/provider.go`: nova interface `FingerprintProvider`,
  CAPACIDADE OPCIONAL no molde das existentes (comentário explicando o
  papel, mesmo padrão de `FaixaResolverProvider`/`MusicaAlbumProvider`).
  Assinatura proposta:
  ```go
  // FingerprintProvider é uma CAPACIDADE OPCIONAL de um Provider (briefing
  // 0126): identificar uma faixa a partir do FINGERPRINT ACÚSTICO do áudio
  // (não de metadado/nome) -- último fallback do pipeline de matching de
  // música, usado só quando MBID embutido e busca textual já falharam. Nem
  // toda fonte tem essa capacidade (hoje: um client AcoustID dedicado,
  // nunca o MusicaProvider/MusicaAlbumProvider do MusicBrainz -- lookup por
  // fingerprint é um serviço/endpoint DISTINTO da API de metadado); o
  // scanner faz type-assert do Provider para esta interface e degrada em
  // silêncio (cai para o passo de draft) quando ausente.
  type FingerprintProvider interface {
      IdentificarPorFingerprint(ctx context.Context, fingerprint string, duracaoSegundos float64) (RecordingCandidato, bool, error)
  }

  // RecordingCandidato é o resultado de UM lookup por fingerprint: o MBID
  // de recording mais provável e o escore de confiança devolvido pelo
  // AcoustID (0-1), usado como limiar mínimo de aceite (ver matching.go).
  type RecordingCandidato struct {
      RecordingMBID string
      Confianca     float64
  }
  ```
- `internal/matching`: nova constante de limiar mínimo de confiança do
  AcoustID para aceitar um candidato (ex.:
  `LimiarConfiancaFingerprint`), a calibrar durante a implementação —
  AcoustID já filtra por similaridade de duração no próprio serviço, mas o
  escore de confiança pode variar; o projeto decide seu próprio corte,
  igual já fez para `LimiarAutoMatch` (textual) e
  `ToleranciaDuracaoSegundos` (briefing 0121).
- `internal/scanner/musica.go`: novo passo entre `matchTextual` (passo 4) e
  `criarFaixaDraft` (passo 5) dentro de `resolverFaixa`. Pseudocódigo do
  encaixe (não prescreve implementação exata, só a ordem/contrato):
  ```go
  if id, ok := r.matchTextual(ctx, tags, duracaoSegundos, langs); ok {
      return id, confMusicaTextual
  }

  if id, ok := r.matchPorFingerprint(ctx, path, duracaoSegundos, langs); ok {
      return id, confFingerprint // nova confiança, entre confMusicaTextual e confDraft
  }

  return r.criarFaixaDraft(ctx, tags, rawNome), confDraft
  ```
  `matchPorFingerprint` (novo método de `scanRun`, mesmo molde de
  `matchTextual`): calcula o fingerprint (degrada se `fpcalc` ausente ou
  erro), type-assert do provider para `FingerprintProvider` (degrada se
  ausente), consulta, e — achando um `RecordingMBID` com confiança acima do
  limiar — segue o MESMO caminho já usado por `importarEAcharPorMbid`
  (resolve o artista via `resolverArtistaMBID`/`FaixaResolverProvider`
  reverso, ou — quando o fingerprint só traz o recording, sem artista —
  reusa a resolução reversa igual ao passo 3 do pipeline atual) e dispara
  `enrich.ImportarFaixaPossuida` com `RecordingMBID` preenchido.
- Atualizar o comentário de topo de `internal/scanner/musica.go` (a
  numeração 1–5 do pipeline vira 1–4, 4.5, 5, refletindo o novo passo —
  mesmo padrão de documentação rigorosa que os demais briefings desta
  vertical mantêm).
- Boot/wiring (`cmd/mymediadb/main.go`): registrar o novo Descriptor de
  integração AcoustID (mesmo padrão de TMDB/TVDB/fanart.tv) e o novo
  provider/capacidade, condicionado à presença da client key configurada
  (mesmo padrão condicional que outros providers com credencial já usam) e
  à checagem de disponibilidade do `fpcalc` no ambiente (boot, não
  build) — se `fpcalc` ausente, a capacidade não é registrada mesmo com a
  chave configurada (as duas dependências — chave E binário — são
  necessárias).

### Frontend (Vite/React)

- Nenhuma tela nova é pré-requisito (ver §3, Fora). Único ponto de UI
  potencialmente tocado: a página de Integrações (`/dashboard/integrations`,
  briefing 0096) ganha uma entrada nova ("AcoustID") na listagem — mesmo
  padrão genérico já existente para toda integração registrada no
  `Registry` (a UI já é dirigida pelo `Descriptor`/`Campos()`, então uma
  nova integração some "de graça" sem código de tela dedicado, igual TMDB/
  TVDB/fanart.tv hoje).

### Arquivos afetados (previsão)

- `internal/fingerprint/fingerprint.go` (novo — wrapper `fpcalc`).
- `internal/fingerprint/fingerprint_test.go` (novo).
- `internal/provider/acoustid/acoustid.go` (novo — client HTTP + rate
  limit + `FingerprintProvider`).
- `internal/provider/acoustid/acoustid_test.go` (novo).
- `internal/provider/acoustid/descriptor.go` (novo — `integration.Descriptor`
  do AcoustID, mesmo padrão de `internal/provider/tmdb`).
- `internal/provider/provider.go` (interface `FingerprintProvider` +
  `RecordingCandidato`).
- `internal/matching/matching.go` (novo limiar de confiança).
- `internal/scanner/musica.go` (novo passo 4.5, atualização do comentário
  de topo).
- `internal/scanner/musica_test.go` (testes do novo passo).
- `internal/scanner/reconciliar.go`/`reconciliar_test.go` (se o passo de
  fingerprint também entrar no caminho de reconciliação de faixa-rascunho
  do briefing 0120 — ver §6, decisão a confirmar na implementação).
- `internal/provider/fake/` (fake de `FingerprintProvider` para testes de
  scanner sem rede real, mesmo padrão dos demais fakes do pacote).
- `cmd/mymediadb/main.go` (wiring do provider/descriptor novos).
- `Dockerfile` (adicionar `chromaprint` ao `apk add` da imagem final —
  ver §6, pacote confirmado disponível no Alpine 3.22).

## 5. Critérios de aceite

- [x] `fpcalc` ausente no container: o boot loga um aviso (mesmo padrão de
      `ffprobe` ausente), a capacidade `FingerprintProvider` não é
      registrada, e o scan continua funcionando normalmente — arquivos sem
      match caem no passo 5 (draft) como hoje, sem panic/erro fatal.
      Confirmado por leitura de código (`cmd/mymediadb/main.go` só faz
      `provs = append(provs, acoustid.New(...))` no `default` do `switch`
      quando `fingerprint.NewCalculador().Disponivel()` é true) e por
      `TestScanMusicaFingerprintIndisponivelViraDraft`/
      `TestScanMusicaSemProviderFingerprintViraDraft`
      (`internal/scanner/musica_test.go`).
- [x] Integração AcoustID não configurada (sem client key, banco nem env
      var): mesma degradação silenciosa acima, independente do `fpcalc`
      estar ou não presente. Confirmado pelo mesmo `switch` em
      `cmd/mymediadb/main.go` (`!credAcoustid.Configurada()` loga e não
      registra) e por `TestIdentificarPorFingerprint_ClientKeyAusenteDegradaSemRede`
      (`internal/provider/acoustid/acoustid_test.go`).
- [x] `fpcalc` presente E client key configurada: um arquivo sintético (teste)
      sem MBID embutido e sem match textual plausível (tag ausente/genérica)
      aciona o passo de fingerprint; um `RecordingCandidato` com confiança
      acima do limiar dispara o MESMO import escopado do passo 2/3
      (`ImportarFaixaPossuida`) — nunca a discografia inteira do artista
      (confirma ADR-0029: só a faixa+álbum+artista da faixa identificada são
      materializados). Confirmado por
      `TestScanMusicaFingerprintAcimaDoLimiarImportaEscopado`.
- [x] Arquivo sem match textual E sem match de fingerprint (candidato abaixo
      do limiar de confiança, ou AcoustID sem resultado nenhum) continua
      caindo em `status=draft` — comportamento atual preservado como
      fallback de última instância. Confirmado por
      `TestScanMusicaFingerprintAbaixoDoLimiarViraDraft`.
- [x] O passo de fingerprint só é chamado DEPOIS que o passo textual (passo
      4) já falhou — nunca antes, nunca em paralelo (teste que conta
      chamadas/mocka o `FingerprintProvider` e confirma zero chamadas
      quando o match textual já resolveu). Confirmado por
      `TestScanMusicaFingerprintNaoChamadoQuandoTextualResolve`.
- [x] Rate limit client-side do AcoustID (máx. 3 req/s) respeitado por
      construção (mesmo padrão de teste do limitador do MusicBrainz —
      `internal/provider/musicbrainz/ratelimit.go` — adaptado para este
      client). Confirmado por `internal/provider/acoustid/ratelimit_test.go`
      (`TestLimitador_TresChamadasRespeitamTresPorSegundo` etc.).
- [x] Nova integração AcoustID aparece em `/dashboard/integrations` sem
      código de tela dedicado (herda o formulário dinâmico genérico do
      briefing 0096). Confirmado por leitura de código:
      `internal/provider/acoustid/descriptor.go` implementa
      `integration.Descriptor` (mesmo molde do TMDB) e é registrado via
      `server.WithIntegrations` em `cmd/mymediadb/main.go` — a UI genérica
      de Integrações consome qualquer `Descriptor` do Registry sem mudança
      de tela.
- [x] `go build ./...`, `go vet ./...`, `go test ./... -count=1` verdes;
      `make gate-local` sem regressão de cobertura (cobertura Go/Web
      subiram; a única métrica que piorou foi gocyclo >10 funções, aviso
      não-bloqueante por AGENTS.md — ver §6/§7 abaixo sobre a extensão ao
      job de reconciliação, que ficou de fora desta entrega por decisão
      documentada, não por pendência).

## 6. Notas de implementação / decisões

- **Por que subprocess e não CGO/binding Go do Chromaprint.** O mesmo
  motivo já documentado no AGENTS.md para `yt-dlp` (briefing 0101/
  ADR-0025): a imagem final é `alpine:3.22` (musl), e evitar CGO mantém o
  binário do MyMediaDB `CGO_ENABLED=0`/cross-compile simples — chamar
  `fpcalc` como processo externo (igual `ffprobe`) é consistente com o
  padrão arquitetural já estabelecido pelo `internal/mediainfo`, em vez de
  introduzir uma segunda estratégia de integração de binário de terceiro no
  mesmo projeto.
- **Pacote Alpine confirmado.** `chromaprint` existe no repositório
  `community` do Alpine 3.22 (`pkgs.alpinelinux.org`, branch v3.22,
  arquiteturas `x86_64`/`aarch64`/`armhf`/`armv7` — cobre o alvo ARM do
  projeto) e o binário `fpcalc` é instalado em `/usr/bin/fpcalc` só com
  `apk add chromaprint` (sem pacote `-tools` separado, diferente do que se
  poderia supor por analogia com outros projetos C). Não há, portanto,
  pesquisa pendente sobre disponibilidade do pacote — confirmado durante a
  escrita deste briefing; só falta o `apk add chromaprint` no `Dockerfile`.
- **Duas chaves distintas do AcoustID — só uma é necessária aqui.** A API
  do AcoustID tem uma "client key" (por aplicação, registrada uma vez,
  usada em toda consulta via o parâmetro `client`) e uma "user key" (por
  usuário final, exigida SÓ ao **submeter** fingerprints novos para a base
  pública). Este briefing só precisa da client key — consultar (lookup)
  nunca exige a user key. Isso é o que torna "contribuir de volta" (fora de
  escopo, §3) uma decisão totalmente independente de "consultar" (dentro de
  escopo): não há acoplamento técnico entre as duas, só uma distinção de
  uso que a própria API já modela.
- **Uso não-comercial / rate limit.** O serviço gratuito do AcoustID é para
  uso não-comercial e limita a 3 requisições/segundo — folgado o bastante
  para o padrão de uso deste projeto (fingerprint só roda no fallback final
  de um scan, nunca em massa/paralelo) mas o rate limit client-side é
  obrigatório pelo mesmo motivo que o MusicBrainz já tem um (evitar
  bloqueio por abuso).
- **Confiança como sinal, não como certeza.** O escore de confiança
  devolvido pelo AcoustID por si só não é uma prova de identidade tão forte
  quanto um MBID explicitamente embutido na tag pelo dono — por isso o
  novo passo entra ESTRITAMENTE depois da busca textual (nunca antes) e a
  confiança resultante da entity (`confFingerprint`) deveria, por
  princípio, ficar no MESMO patamar ou abaixo de `confMusicaTextual`
  (nunca acima) — a implementação decide o valor exato, mas a ORDEM de
  precedência (embutido > textual > fingerprint > draft) é a decisão fixada
  por este briefing, refletindo a mesma prática documentada do Picard (ver
  §1).
- **Reconciliação periódica (briefing 0120) — decisão tomada: FICOU DE
  FORA desta entrega.** O job de reconciliação de faixas-rascunho
  (`internal/scanner/reconciliar.go`) continua reusando só `matchTextualFaixa`
  (o mesmo método do passo 4) — o passo 4.5 de fingerprint NÃO foi
  cabeado nesse caminho (confirmado por leitura de código:
  `reconciliar.go` não referencia `matchPorFingerprint`/`fingerprintCalculador`/
  `fingerprintProvider`). Motivo: fingerprint exige acesso ao ARQUIVO físico
  (não só às tags já extraídas), custo de I/O potencialmente alto rodando
  sobre milhares de faixas-rascunho acumuladas num job periódico — este
  briefing cobriu só o caminho do SCAN (arquivo sendo processado, já com
  acesso ao path). Isso é uma limitação aceita, documentada em §7 como
  follow-up, não uma pendência desta entrega — inclusive `force-reconcile`
  (briefing 0122, `POST /entities/{id}/force-reconcile`) NÃO ganha
  fingerprint de graça: ele chama `scanner.ReconciliarFaixaDraft`, a MESMA
  função de `reconciliar.go` que só tenta `matchTextualFaixa` (confirmado
  por leitura de código, `internal/server/reconciliar_orfaos.go`). Uma
  faixa-rascunho já existente só ganha uma segunda chance via fingerprint
  se o arquivo passar de novo pelo SCAN (ex.: rescan da pasta) — não existe
  hoje nenhum caminho de reconciliação (automático ou sob demanda) que
  rode o passo 4.5 isoladamente. Cabear fingerprint em
  `reconciliar.go`/`force-reconcile` fica para um follow-up separado (nota
  que corrige a menção em §3 acima, escrita antes desta decisão).
- **Persistir o fingerprint calculado, ou recalcular a cada tentativa?**
  Levantado mas não decidido aqui: se um arquivo passa pelo passo 4.5 e não
  encontra candidato (fica em draft), tentativas futuras (reconciliação
  periódica) recalculariam o fingerprint do zero, a menos que ele seja
  persistido (ex.: em `extension_data` do `media_file` ou faixa-rascunho).
  Como o pipeline já tem uma checagem de incrementalidade por
  `mtime`/tamanho (`mediaFileInalteradoPorPath`, ver comentário de
  `factsMusica`) que evita retrabalho quando o arquivo não mudou, este
  briefing assume — sem fechar como regra rígida — que o CÁLCULO do
  fingerprint só roda quando o arquivo é efetivamente reprocessado (não
  em todo re-scan de um arquivo já resolvido), deixando cache explícito do
  valor calculado como otimização opcional de implementação, não
  pré-requisito de aceite.

## 7. Fora deste briefing / follow-ups

- Rodar o passo de fingerprint também no job periódico de reconciliação de
  faixas-rascunho (briefing 0120) — decisão de custo/benefício a avaliar na
  implementação (ver §6); se ficar de fora, mencionar explicitamente no
  fechamento deste briefing e considerar como candidato a briefing
  separado.
- Contribuir fingerprints de volta ao AcoustID (opt-in de mercado, exige a
  "user key" pessoal — nunca a "client key" da aplicação).
- Detecção de duplicata entre arquivos via fingerprint (mesma gravação,
  tags diferentes) — usa o mesmo dado calculado aqui, mas é um problema
  diferente (dedup, não identificação), fora de escopo.
- UI dedicada de diagnóstico ("por que esta faixa não identificou nem por
  fingerprint") — se necessário no futuro, cabe na mesma superfície
  genérica de fila de revisão/órfãos já existente, sem tela nova.
