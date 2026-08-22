# 0123 — MusicBrainz reprova releases que só falharam de REDE (não de match) na busca por faixa-alvo

> Depois da rodada 2 do 0121 (teto 15 + duração como confirmação) e do 0122
> (força reconciliação manual), a validação em produção do caso real
> (Bomfunk MC's / Freestyler) continuou `linked=false` mesmo com a release
> certa dentro do teto e batendo título+duração quase perfeitos. Achado: o
> loop de releases desiste de uma release PARA SEMPRE assim que ela esgota o
> retry de rede (3 tentativas) — mesmo que a causa seja só instabilidade
> transitória do servidor público (503 "currently busy", ~1 em 3 chamadas,
> já documentado no próprio código) e não "a faixa não está aí".

| | |
|---|---|
| **ID** | 0123 |
| **Tipo** | fix |
| **Fase (roadmap)** | Transversal / correção de dado — achado ao validar o 0122 em produção (força reconciliação do caso real Bomfunk MC's/Freestyler). |
| **Docs relacionadas** | — |
| **Status** | in-progress |
| **Depende de** | [0121](0121-musicbrainz-tenta-outras-releases-do-release-group.md), [0122](0122-forcar-reconciliacao-manual-por-entity.md) |

---

## 1. Objetivo

Ao usar o `force-reconcile` novo (briefing 0122) contra a entity real do
caso que motivou 0120/0121 (faixa "Freestyler", `4838fee1-8b95-4d72-8142-e4edaa106975`),
o resultado continuou `{"linked": false}` — duas vezes seguidas, reproduzido
ao vivo. Investigação (consultas diretas ao MusicBrainz público, mesmos
parâmetros/`inc` que o client usa):

- O release-group certo (`1ca542d9-eec9-4bad-9e20-7535431f5b1c`, 13
  releases) continua sendo achado corretamente pelo `melhorReleaseGroupPorTitulo`
  — o log de erro (`enrich: faixa alvo nao encontrada no album 1ca542d9-...`)
  confirma isso (só chega a essa mensagem depois do release-group já ter
  sido resolvido).
- A release certa, `43c49ea4-b213-4a97-afe0-3e6418374f96` (Official, faixa 5
  = "Freestyler" exato), está na posição **~10ª** por prioridade
  (Official-primeiro) — dentro do teto de 15 com folga (só 13 releases no
  total, nenhuma sequer é cortada pelo teto agora).
- Título: `textsim.Similaridade("Freestyler", "Freestyler")` = 100 (exato,
  muito acima do limiar 92).
- Duração: arquivo local = 306,364s (mediainfo/ffprobe); MusicBrainz (faixa
  5 dessa release) = 306,000s. Diferença = **364ms**, bem dentro da
  tolerância de 3s (`matching.ToleranciaDuracaoSegundos`).

Ou seja: pelos DADOS, essa release deveria satisfazer `releaseContemFaixaAlvo`
sem ambiguidade. A causa não é de matching/threshold — é de **confiabilidade
de rede dentro do próprio loop de tentativas**:

`buscarFaixasComAlvo` (`internal/provider/musicbrainz/musicbrainz.go`) já
tenta várias releases (briefing 0121) e cada `getMB` individual já retenta
até 3× em cima de 502/503/504 (`tentativasMB`, comentário no código já
documenta "~1 em 3 chamadas" de 503 observado em produção). Mas se uma
release ESPECÍFICA esgota as 3 tentativas (baixa probabilidade por chamada,
mas nada desprezível ao longo de até 13 releases × até 3 tentativas cada
numa única reconciliação), `buscarFaixasComAlvo` a descarta **para sempre**
nesta chamada (`ultimoErr = err; continue`) — sem log nenhum (o loop de
erro é totalmente silencioso hoje) e sem uma segunda chance. Se justo essa
release descartada era a que continha a faixa-alvo, o resultado final é
"nenhuma bateu" mesmo a faixa genuinamente existindo dentro do teto.

Confirmado ao vivo: a chamada real ao `force-reconcile` levou **40s** na
1ª tentativa e **66s** na 2ª — bem acima do mínimo teórico de ~15-20s para
15 chamadas sequenciais a 1 req/s sem nenhum retry, indicando que várias
chamadas de fato precisaram retentar (503) durante essas execuções.

## 2. Decisão

Duas mudanças pequenas e cirúrgicas, ambas em
`internal/provider/musicbrainz/musicbrainz.go`, função `buscarFaixasComAlvo`:

1. **Log de diagnóstico por tentativa falha** (hoje inexistente): quando uma
   release individual falha de rede dentro do loop, loga
   `log.Printf` com o releaseID e o erro — para o PRÓXIMO caso parecido não
   exigir investigação manual via `curl` direto no MusicBrainz como desta
   vez.
2. **Segunda passada, só sobre as releases que falharam de REDE** (não sobre
   as que responderam mas não bateram o match): se a primeira passada
   terminar sem nenhuma release satisfazendo `releaseContemFaixaAlvo`, E
   pelo menos uma release tiver falhado por erro de rede (não por não
   bater o match), tenta de novo **só essas** (cada uma já ganha um novo
   ciclo de 3 tentativas do `getMB`, então isso não é "mais 3 tentativas em
   cima de 3" — é uma segunda chance limpa, útil justamente porque 503 é
   instabilidade **transitória**: a mesma release que deu 3× 503 agora pode
   responder normalmente segundos depois). Bounded: no máximo
   `len(ids)` releases extras no pior caso (já limitado pelo teto de 15
   existente), sem laço adicional além dessa segunda passada única (não
   vira retry infinito).

Fora do escopo: mudar `tentativasMB`/`backoffMB` (o retry POR CHAMADA já
existe e está documentado; o gap era a ausência de segunda chance NO NÍVEL
DA RELEASE, não a quantidade de tentativas por chamada individual), ou
qualquer mudança de matching/duração (já corretos, confirmados pela
investigação acima).

## 3. Contrato técnico

`internal/provider/musicbrainz/musicbrainz.go`, função `buscarFaixasComAlvo`
(a partir da versão pós-0121): extrair o corpo do loop atual para uma função
nova reusável entre a 1ª e a 2ª passada, preservando a checagem inicial
(`alvoFaixa == (provider.AlvoFaixaAlbum{})`) e o teto (`maxReleasesTentadosPorAlvo`)
tal como estão hoje.

- Extrair uma função nova `tentarReleases(ctx, ids []string, alvoFaixa
  provider.AlvoFaixaAlbum) (faixasEncontradas []provider.FaixaExterna, match
  bool, ultimaFaixasObtida []provider.FaixaExterna, obtevesAlgumaVez bool,
  falhadas []string)` (nomes de retorno ilustrativos — o agente que
  implementar tem liberdade de ajustar a assinatura interna exata contanto
  que o COMPORTAMENTO abaixo seja preservado; é função NÃO-exportada, uso
  interno único) que:
  - Itera `ids`, chama `c.buscarFaixas(ctx, releaseID)` por item.
  - Erro: `log.Printf("musicbrainz: busca de faixas da release %s falhou (retentando outra release do grupo): %v", releaseID, err)`, acumula `releaseID` em `falhadas`, `continue`.
  - Sucesso: marca `obtevesAlgumaVez = true`, guarda como a "última obtida"
    (para o fallback de degradação); se `releaseContemFaixaAlvo(fx,
    alvoFaixa)`, retorna IMEDIATAMENTE (`match=true`, com essas faixas) —
    mesmo comportamento de early-return de hoje.
  - Loop termina sem match: devolve o que tiver (última obtida,
    `obtevesAlgumaVez`, `falhadas`).
- `buscarFaixasComAlvo` chama essa função 1x com `ids`; se não deu match E
  `len(falhadas) > 0`, chama de novo SÓ com `falhadas` (2ª passada) — se essa
  segunda passada der match, retorna com ele; senão, degrada com a "última
  obtida" entre AS DUAS passadas (a mais recente sucedida, seguindo a mesma
  semântica de hoje: "nenhuma bateu dentro do teto, degrada com a última
  obtida").
- Se a 2ª passada também falhar tudo (raro — 503 persistente demais), o
  comportamento final é IDÊNTICO ao de hoje quando nada bate: degrada com a
  última obtida da 1ª passada (ou `nil`/erro se `obtevesAlgumaVez` nunca foi
  true em nenhuma das duas).
- Import novo: `"log"` (se ainda não importado no arquivo — confirme antes
  de adicionar, pode já estar).

## 4. Testes

`internal/provider/musicbrainz/musicbrainz_test.go`:

- Fixture que simula um servidor HTTP fake onde UMA release específica
  (dentre várias) devolve 503 nas primeiras N chamadas (esgotando as 3
  tentativas do `getMB`) mas responde 200 numa chamada POSTERIOR (a da 2ª
  passada) — e essa release é a que contém a faixa-alvo. Assert: o
  resultado final é a faixa-alvo encontrada (prova que a 2ª passada resgata
  o caso).
- Fixture onde a mesma release falha em TODAS as tentativas de AMBAS as
  passadas (503 sempre) — assert: comportamento de degradação idêntico ao
  atual (usa a última release que respondeu com sucesso, sem pânico/loop
  infinito).
- Assert que a 2ª passada só é feita quando `falhadas` não está vazio (não
  gasta chamada extra à toa quando tudo respondeu normal e só não bateu o
  match — nesse caso não há nada em `falhadas`, comportamento igual a hoje).
- Ajustar o teste do teto (`servidorTetoFake` ou equivalente, já existente
  desde a rodada 2 do 0121) se a contagem de chamadas HTTP esperada mudar
  por causa da 2ª passada condicional.

## 5. Critérios de aceite

- [x] Log de diagnóstico por release falhada, com releaseID e erro.
- [x] Segunda passada implementada, condicional a `falhadas` não-vazio,
      bounded (nunca mais que uma repetição extra).
- [x] Teste prova que uma release que falhou de rede na 1ª passada e
      resolveria na 2ª é resgatada.
- [x] Teste prova que falha persistente nas duas passadas degrada sem
      pânico/loop infinito, mesmo comportamento de hoje.
- [x] `go build`/`go vet`/`go test ./... -count=1`, `make gate-local` verdes.
- [ ] Validado em produção: repetir `POST /entities/4838fee1-.../force-reconcile`
      (briefing 0122) e confirmar se agora resolve o caso real — fecha (ou
      documenta honestamente, se ainda não resolver) o item pendente do
      0121/0122.
