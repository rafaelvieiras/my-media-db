# 0124 — Desempate de título por duração quando 2+ faixas da mesma release têm o mesmo nome

> A rodada 2 do 0121 adicionou duração como sinal de CONFIRMAÇÃO do candidato
> de MELHOR score de título — mas nunca tratou o caso de duas faixas da MESMA
> release empatarem nesse score (comum: remix/edit/mix diferentes às vezes
> compartilham o título "limpo" com a faixa original). Reproduzindo a chamada
> real contra o MusicBrainz de produção, achamos exatamente esse caso: a
> release do Bomfunk MC's/Freestyler tem duas faixas tituladas
> `"Freestyler"` — a duração desempata errado (ou nem desempata) e a release
> inteira é rejeitada, mesmo contendo a faixa certa.

| | |
|---|---|
| **ID** | 0124 |
| **Tipo** | fix |
| **Fase (roadmap)** | Transversal / correção de dado — achado ao validar o 0123 em produção (força reconciliação real do Bomfunk MC's/Freestyler, cenário 100% livre de falha de rede). |
| **Docs relacionadas** | — |
| **Status** | in-progress |
| **Depende de** | [0121](0121-musicbrainz-tenta-outras-releases-do-release-group.md), [0123](0123-musicbrainz-reprova-releases-que-falharam-de-rede.md) |

---

## 1. Objetivo

O briefing 0123 corrigiu a resiliência de rede do loop de releases, mas a
validação em produção mostrou 2 chamadas 100% limpas (zero falha de rede) e
o resultado continuou `linked=false`. Investigação: reproduzi a chamada REAL
(`Client.BuscarAlbumPorNome`, mesmo artista/álbum/alvo) direto contra o
MusicBrainz de produção, fora do container (script Go descartável usando o
próprio pacote — apagado depois, não versionado), e capturei a release
efetivamente escolhida quando o resultado degrada:

```
achou album=true mbid=1ca542d9-... nome="Freestyler" ano=1999 genero="breakbeat"
num faixas retornadas: 5
  title="Freestyler"                       track=1 duracaoMs=247000 isrc=FISME9900023
  title="Freestyler (Skillsters remix)"    track=2 duracaoMs=183000
  title="Freestyler (Happy Mickey Mouse remix)" track=3 duracaoMs=288000
  title="Freestyler (Missing Link remix)"  track=4 duracaoMs=340000
  title="Freestyler"                       track=5 duracaoMs=306000 isrc=AUXN21104289
```

**Duas faixas** desta release (track 1 e track 5) têm o título EXATO
`"Freestyler"` — mesmo score de similaridade (100) contra o alvo. A faixa 5
é a certa (duração 306000ms, o arquivo local tem 306364ms — diff de 364ms,
dentro da tolerância de 3s). A faixa 1 é uma OUTRA versão (provavelmemte um
edit/mix diferente cujo título ficou "limpo" no MusicBrainz), com duração
247000ms — nada a ver com o arquivo local.

O bug: tanto `releaseContemFaixaPorTitulo`
(`internal/provider/musicbrainz/musicbrainz.go`) quanto `melhorFaixaPorTitulo`
(`internal/enrich/musica.go`) computam o "melhor" candidato de título com um
laço que só atualiza a variável de "melhor" quando o score é **estritamente
maior** (`score > melhorScore`) — em caso de EMPATE, ficam com o PRIMEIRO
candidato encontrado (a ordem de iteração é a ordem das faixas na release, ou
seja, track 1 antes de track 5). Isso:

- No **provider** (`releaseContemFaixaPorTitulo`): fica com a faixa 1
  (errada) como `melhorFx`, aplica o gate de duração contra ELA (247000 vs.
  306364 → diferença de 59s, muito além da tolerância) → `false` → a release
  INTEIRA é rejeitada, mesmo contendo a faixa certa.
- No **enrich** (`melhorFaixaPorTitulo`): tem uma checagem de empate
  (`empate = true` quando `score == melhorScore`), mas ela SEMPRE rejeita em
  empate (`if empate || melhorScore < limiar { return zero, false }`) — nunca
  chega a checar se a duração poderia desempatar as duas.

Antes da rodada 2 do 0121 (sem gate de duração), esse empate era inofensivo:
o provider só precisava saber "existe uma faixa com score >= limiar nesta
release" (nunca importava QUAL das empatadas). A rodada 2 introduziu a
necessidade de rastrear uma faixa ESPECÍFICA (para aplicar o gate de
duração) — e ali o empate virou um bug real, silencioso: a rodada 2 é quem
introduziu esta regressão para releases com títulos duplicados dentro de si
mesmas (comum em compilações/singles com múltiplas versões).

## 2. Decisão

Usar a duração para **desempatar** quando 2+ faixas empatam no score de
título — não só para confirmar um candidato já único. Regra, igual nos dois
lugares (provider e enrich, mesma duplicação intencional já documentada no
0121 §6):

1. Encontrar o(s) candidato(s) de MAIOR score de título (pode ser 1 ou mais).
2. Se score < limiar: rejeita (sem mudança).
3. Se só 1 candidato no topo: aplica o gate de duração como hoje (confirma
   ou pula se desconhecida) — comportamento INALTERADO para o caso comum
   (sem título duplicado).
4. Se 2+ candidatos empatados no topo:
   - Se a duração do alvo é desconhecida: rejeita (empate sem sinal para
     decidir — mesmo comportamento SEGURO de hoje, sem regressão quando não
     há duração).
   - Se a duração do alvo é conhecida: filtra os empatados cuja duração
     TAMBÉM é conhecida e está dentro da tolerância
     (`matching.ToleranciaDuracaoSegundos`). Se **exatamente 1** sobrar,
     desempate resolvido: usa essa. Se **0 ou 2+** sobrarem, continua
     ambíguo — rejeita (nunca adivinha entre candidatos igualmente
     plausíveis).

Isso é uma extensão natural do gate já existente (usa o MESMO sinal, duração,
só que agora também para desempate, não só confirmação) — não introduz
nenhum sinal novo nem muda a tolerância.

## 3. Contrato técnico

### `internal/provider/musicbrainz/musicbrainz.go`

`releaseContemFaixaPorTitulo` — reescrever para: (a) coletar TODOS os
candidatos empatados no maior score (não só o primeiro), (b) se score <
limiar, `false`; (c) se só 1 no topo, gate de duração como hoje contra ele;
(d) se 2+ no topo, tentar desempatar por duração (regra §2.4) — só `true`
quando exatamente 1 sobra dentro da tolerância E o alvo tem duração
conhecida.

### `internal/enrich/musica.go`

`melhorFaixaPorTitulo` — mesma lógica, mas devolvendo `(provider.FaixaExterna,
bool)`: ao desempatar com sucesso (exatamente 1 sobra), devolve ESSA faixa
(não mais um "zero, false" automático só por existir empate). Ao continuar
ambíguo (0 ou 2+, ou duração do alvo desconhecida), mantém o `(zero, false)`
de hoje.

Ambas as funções continuam com a MESMA assinatura pública (nenhum chamador
muda) — só o corpo interno do critério de desempate.

## 4. Testes

`internal/provider/musicbrainz/musicbrainz_test.go` e
`internal/enrich/musica_test.go`:

- Release/lista de faixas com DUAS faixas de título idêntico (uma com
  duração compatível com o alvo, outra não) → resolve para a faixa de
  duração compatível (prova o desempate).
- Mesmo cenário mas com a duração do alvo DESCONHECIDA (0) → rejeita (sem
  regressão do comportamento seguro de hoje).
- Duas faixas de título idêntico e AMBAS com duração compatível (empate
  também na duração) → rejeita (ainda ambíguo, nunca adivinha).
- Caso sem nenhum título duplicado (o caminho comum) → comportamento
  IDÊNTICO ao de antes deste briefing (sem regressão).

## 5. Critérios de aceite

- [x] Desempate por duração implementado nos dois lugares (provider +
      enrich), mesma regra.
- [x] Os 4 cenários de teste acima cobertos nos dois pacotes.
- [x] `go build`/`go vet`/`go test ./... -count=1`, `make gate-local` verdes.
- [ ] Validado em produção: repetir `POST /entities/4838fee1-.../force-reconcile`
      (briefing 0122) e confirmar `linked=true` — fecha finalmente a cadeia
      0120→0121→0122→0123→0124 no caso real do Bomfunk MC's/Freestyler.
