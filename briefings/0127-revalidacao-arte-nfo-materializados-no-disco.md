# 0127 — Revalidação de arte/NFO materializados contra o disco real (self-heal)

> Quando uma arte (`asset.local_path`) ou o `.nfo` (`entity.nfo_path`) some
> fisicamente da pasta real da biblioteca depois de materializado — apagado
> por engano, sincronização do rclone/gdrive, ou remontagem do mount remoto
> — o item fica preso PERMANENTEMENTE mostrando "salvo na pasta" na UI,
> porque nada no sistema jamais reconfirma a existência física do arquivo
> registrado no banco. Este briefing fecha esse gap com um `os.Stat`
> pontual nos dois únicos consumidores que decidem "já está materializado,
> nada a fazer" — sem tocar no scanner nem em nenhuma rota quente de leitura.

| | |
|---|---|
| **ID** | 0127 |
| **Tipo** | fix |
| **Fase (roadmap)** | Transversal — Infraestrutura operacional |
| **Status** | done |
| **Docs relacionadas** | [08-artwork-e-assets](../docs/08-artwork-e-assets.md) |
| **Depende de** | 0062, 0064, 0081 |

---

## 1. Objetivo

Caso real em produção: a entity `Mortal Kombat: Os Defensores da Terra`
(tv, `0b057574-c047-4e0d-ae5a-9cabe1a56165`) tem `asset.local_path`
preenchido para `poster`, `fanart` e `logo` — os três aparecem na galeria
com o selo "Salvo na pasta" — mas a pasta real
(`/mnt/gdrive-media/Videos/Series Infantis/Mortal Kombat - Defensores da
Terra (1996)/`) só contém `logo.png`. `poster.jpg` e `fanart.jpg` sumiram
fisicamente do disco (mount rclone/gdrive, `AGENTS.md` já documenta que
esse mount não emite evento — mas nem *pooling* existe hoje: ninguém nunca
mais olha pro arquivo depois da primeira escrita).

Investigação (ver seção 6) confirmou a causa raiz: **dois pontos do
código** decidem "já materializado, pula" olhando **só a coluna do banco**
(`asset.local_path`/`entity.nfo_path` não-nulos), sem jamais fazer
`os.Stat` no caminho registrado:

1. `materializarKindVencedor` (`internal/server/midias_faltantes.go:144-147`)
   — consumido pelo job periódico `midias_faltantes` (roda dentro de
   `saude_biblioteca`, processa TODA entity elegível a cada execução) e
   pelo botão "Baixar Artes Faltantes" do menu Gerenciar.
2. `materializarAsset` (`internal/server/localmedia_handlers.go:156-159`)
   — consumido por `POST .../assets/{id}/materialize` e `PUT
   .../assets/{id}/prefer`.
3. `gravarNFOSeFaltante` (`internal/server/midias_faltantes.go:286-288`) —
   mesmo padrão para `entity.nfo_path`.

Como o job periódico varre **toda** a biblioteca a cada rodada, corrigir
esses dois/três pontos faz o sistema se autocurar sozinho na PRÓXIMA
execução do `saude_biblioteca`, sem precisar de nenhuma ação manual nem de
um job novo.

**Fora de escopo, propositalmente:** os selos de "completude de
catalogação" (`internal/server/completeness.go`) não são afetados por
este bug — eles avaliam se existe *algum* asset servível pelo cache
interno (`escolherAssetServido`/`path_or_uri`), nunca dependeram de
`local_path`, e continuam corretos independente deste fix.

## 2. Jornada do usuário

1. O dono materializa uma arte (upload, seleção da galeria, ou
   automaticamente via `midias_faltantes`) — o arquivo é gravado na pasta
   real e `asset.local_path` é preenchido. A galeria mostra "Salvo na
   pasta".
2. Meses depois, o arquivo físico desaparece por um motivo externo ao
   MyMediaDB (sincronização do rclone, exclusão manual, glitch do mount).
   O banco não sabe disso.
3. **Hoje:** a galeria continua mostrando "Salvo na pasta" para sempre; o
   job periódico e o botão "Baixar Artes Faltantes" pulam esse kind por
   achá-lo "já resolvido"; nada nunca reescreve o arquivo.
4. **Depois deste fix:** na próxima execução do job `midias_faltantes`
   (periódica, via `saude_biblioteca`) ou na próxima vez que o dono clicar
   em "Baixar Artes Faltantes"/`PUT .../prefer`/`POST .../materialize`
   para aquele kind, o sistema detecta que o arquivo sumiu (`os.Stat` →
   `os.IsNotExist`) e **regrava o binário do cache diretamente no mesmo
   `local_path` já registrado** — sem esperar reimport, sem o dono precisar
   apagar nada manualmente no banco.

## 3. Escopo

**Dentro:**
- `os.Stat` do `asset.local_path`/`entity.nfo_path` nos três pontos
  listados no §1, tratando `os.IsNotExist` como "não materializado,
  regravar" e qualquer OUTRO erro de stat (permissão, mount
  temporariamente indisponível) como "assume que existe" (best-effort,
  não dispara reescrita em cima de um glitch transitório do mount
  remoto — ver §4, `arquivoLocalAindaExiste`).
- Para **asset**: regravação **in-place** no MESMO `local_path` já
  gravado (relê o binário do cache via `lerBinarioAssetDoCache` +
  `localmedia.EscreverArquivoAtomico` direto no path existente) — **não**
  recalcula nome/índice/rotação via `gravarAssetNaPastaReal` (ver nota de
  risco no §6: recalcular trataria erroneamente um asset que já É
  principal como "não-principal ainda", produzindo um nome rotacionado
  diferente do bare original). Não precisa reescrever `local_path` no
  banco (o valor não muda).
- Para **NFO**: cair para o fluxo normal de `gravarNFO` (já reescreve do
  zero e é idempotente por conteúdo — não tem a armadilha de rotação que
  asset tem).
- Testes cobrindo: arquivo ausente → regravado; arquivo presente → no-op
  (nenhuma escrita/leitura de cache desnecessária); erro de stat que não
  seja "não existe" → tratado como presente (nenhuma reescrita).

**Fora (explicitamente):**
- Qualquer verificação de existência no *hot path* de leitura
  (`GET /entities/{id}/artwork`, `GET /assets`, badges de completude de
  catalogação) — permanece leitura pura de coluna, sem I/O de disco a
  cada carregamento de tela.
- Cruzar o `WalkDir` do scanner com a tabela `asset`/`entity.nfo_path`
  para limpeza em lote (ideia levantada na investigação, mas o self-heal
  do job `midias_faltantes` já resolve o caso reportado com escopo bem
  menor — o scanner não precisa aprender sobre `local_path`).
- `PATCH /library/{id}` (recasqueamento de path do datasource) e sua
  cascata (`recascarAssetsDaEntidade`) — problema adjacente, mas
  orquestrado por fluxo próprio (0070); não reabrir aqui.
- Materialização de capa/foto de música (álbum/artista) na pasta real —
  já fora de escopo por design (0116, `errSemDestino` tratado como
  sucesso "só cache"); este fix não muda isso.

## 4. Contrato técnico

### Backend (Go) — único lado tocado, sem mudança de schema/API/contrato

**Novo helper** (sugestão de local: `internal/server/localmedia_handlers.go`,
perto de `materializarAsset`):

```go
// arquivoLocalAindaExiste confirma via os.Stat se o caminho fisicamente
// registrado ainda existe. Erros que NAO sejam "nao existe" (permissao,
// mount FUSE/rclone temporariamente indisponivel) sao tratados como
// "assume que existe" -- best-effort, evita reescrever em cima de um
// glitch transitorio do mount remoto (AGENTS.md ja documenta que esse
// mount nao emite evento de mudanca; aqui vale o mesmo cuidado: silencio
// do stat != certeza de que sumiu).
func arquivoLocalAindaExiste(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil || !os.IsNotExist(err)
}
```

**1. `internal/server/localmedia_handlers.go:156-159`** (`materializarAsset`)
— trocar o early-return cego por: se `arquivoLocalAindaExiste(asset.LocalPath.String)`
→ comportamento atual (no-op); senão, regravar in-place (novo helper
`regravarBinarioAssetNoLocalPathExistente(ctx, asset)`: relê do cache via
`lerBinarioAssetDoCache` + `localmedia.EscreverArquivoAtomico(asset.LocalPath.String, dados)`,
devolve `asset` inalterado — mesma assinatura de retorno de hoje).

**2. `internal/server/midias_faltantes.go:144-147`** (`materializarKindVencedor`)
— o branch `if vencedor.IsLocalInLibrary { return false, nil }` precisa
primeiro buscar o `asset` real (`s.leitura().ObterAsset(ctx, vencedor.AssetID)`,
já é feito no branch seguinte) e checar `arquivoLocalAindaExiste`. Se o
arquivo existe → `return false, nil` (inalterado). Se sumiu → chamar o
mesmo `regravarBinarioAssetNoLocalPathExistente` do item 1 (reuso do
helper, não duplicar a lógica) e devolver `(true, nil)` em caso de
sucesso — assim o resumo do job (`artesTotal`) reflete a autocura.

**3. `internal/server/midias_faltantes.go:286-288`** (`gravarNFOSeFaltante`)
— trocar `if ent.NfoPath.Valid && ent.NfoPath.String != "" { return false, nil }`
por: se válido E `arquivoLocalAindaExiste(ent.NfoPath.String)` → `return
false, nil` (inalterado); se válido mas sumiu → **não** retornar, cair
para o fluxo normal abaixo (`resolverEntity` + `gravarNFO`), que já
regrava do zero.

### Arquivos afetados
- `internal/server/localmedia_handlers.go` (novo helper + fix em
  `materializarAsset`).
- `internal/server/midias_faltantes.go` (fix em `materializarKindVencedor`
  e `gravarNFOSeFaltante`).
- Testes novos/ajustados nos `_test.go` correspondentes (procurar
  primeiro se já existe suíte para essas três funções antes de criar
  arquivo novo).

## 5. Critérios de aceite

- [ ] Asset com `local_path` preenchido cujo arquivo físico foi apagado
      manualmente do disco: ao rodar `MaterializarMidiasFaltantes` (ou o
      job `midias_faltantes` completo) para a entity, o arquivo volta a
      existir no MESMO caminho, com o mesmo nome (sem virar rotacionado).
- [ ] Mesmo cenário via `PUT .../assets/{id}/prefer` e
      `POST .../assets/{id}/materialize` — idem.
- [ ] Asset com `local_path` preenchido e arquivo presente: nenhuma
      leitura de cache/escrita em disco a mais acontece (regressão de
      performance zero no caminho feliz — cobrir com teste que falha se
      `lerBinarioAssetDoCache`/`EscreverArquivoAtomico` forem chamados
      nesse caso).
- [ ] `entity.nfo_path` preenchido cujo `.nfo` foi apagado: `gravarNFOSeFaltante`
      regrava o `.nfo` do zero.
- [ ] Erro de `os.Stat` que não seja "arquivo não existe" (simular via
      caminho cujo componente pai é um arquivo comum, produzindo
      `ENOTDIR`) NÃO dispara reescrita.
- [ ] `go build ./...`, `go vet ./...`, `go test ./internal/server/... -count=1`
      verdes.
- [ ] `make gate-local` (rodado só no agente final de validação) sem
      regressão de métrica.
- [ ] Validado em produção: `GET /api/v1/entities/0b057574-c047-4e0d-ae5a-9cabe1a56165/assets`
      continua trazendo os três `local_path`, e a pasta real volta a ter
      `poster.jpg`/`fanart.jpg`/`logo.png` depois do job rodar (ou de um
      disparo manual de `materialize-missing-media` para essa entity).

## 6. Notas de implementação / decisões

- **Por que regravar in-place em vez de recomputar via `gravarAssetNaPastaReal`:**
  `gravarAssetNaPastaReal` decide nome bare vs. rotacionado com base em
  `kindJaTemPrincipal` — mas o PRÓPRIO asset sendo regravado já é
  `is_principal=1` no banco (ele se tornou principal na primeira
  materialização). Se o fluxo normal de "novo asset" fosse reusado aqui,
  `jaTemPrincipal` daria `true` (o próprio asset conta), `tornaPrincipal`
  daria `false`, e o arquivo seria regravado com um nome ROTACIONADO
  (`poster-1.jpg`) em vez do nome bare original (`poster.jpg`) — trocando
  silenciosamente o arquivo "principal" da pasta por um nome que Kodi/Emby
  não reconhecem como capa padrão. Regravar direto no `local_path` já
  registrado evita essa armadilha por completo.
- **Por que não tocar no hot path de leitura:** `is_local_in_library`
  (badge da galeria) e `escolherAssetServido` (completude de catalogação)
  são consultados a cada carregamento de tela de entity — um `os.Stat`
  ali bateria direto no mount remoto (rclone/gdrive) a cada request,
  reintroduzindo o mesmo tipo de problema de performance que o scanner
  incremental (briefing 0064) resolveu para hash de vídeo. O self-heal
  fica só nos pontos que JÁ fazem I/O pesado (download/gravação) e já
  rodam em background ou sob ação explícita do dono.
- **Por que "outro erro de stat" assume que existe:** mount FUSE
  remoto (rclone/gdrive) pode devolver erro transitório (timeout,
  reconexão) sem o arquivo ter sumido de verdade — `AGENTS.md` já
  documenta essa fragilidade para o `fsnotify`; o mesmo cuidado se aplica
  aqui. Tratar qualquer erro que não seja `os.IsNotExist` como "assume
  presente" evita reescritas em massa disparadas por uma instabilidade
  passageira do mount.

## 7. Fora deste briefing / follow-ups

- Cruzar o `WalkDir` do `saude_biblioteca` com `asset.local_path`/
  `entity.nfo_path` para uma limpeza ativa em lote (marcar como `NULL` o
  que não bate mais) — só vale a pena se o self-heal aqui implementado se
  mostrar insuficiente em produção (ex.: itens cujo `media_file` não é
  mais "elegível" e por isso nunca mais passam pelo job `midias_faltantes`).
- Mesma revalidação para o path do `PATCH /library/{id}`
  (recasqueamento) — hoje é reescrita pura de prefixo de string sem tocar
  o disco; se o prefixo informado não bater exatamente com a reorganização
  real, o mesmo tipo de dessincronização acontece por uma via diferente.
