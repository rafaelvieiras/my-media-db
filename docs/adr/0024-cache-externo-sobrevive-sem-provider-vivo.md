# ADR-0024 — Cache externo sobrevive sem provider vivo (parse puro ≠ busca ao vivo)

- **Status:** Aceita
- **Data:** 2026-08-12
- **Agenda:** novo item PR1 (ver [00](../00-agenda-de-decisoes.md)) · **Spec:** [03](../03-arquitetura.md), [04](../04-modelo-de-dados.md), [07](../07-enriquecimento-providers.md)

## Contexto

Na migração de produção de 12/08/2026, o ambiente novo subiu com as 4 credenciais
de provider (TMDB/fanart.tv/TVDB) vazias. Resultado observado: **todo item já
catalogado perdeu sinopse, elenco, equipe, tags e similares/sugestões** — não só
para item novo (esperado, sem chave não dá pra buscar), mas para item que **já
tinha esse payload cacheado em `media_external`/`external_detail` havia meses**,
copiado intacto na migração (confirmado por hash idêntico do arquivo `.sqlite`).

Investigação no código (não especulação) achou a causa exata:

- `cmd/mymediadb/main.go` só constrói e registra um `provider.Provider` (`tmdb.New`,
  `tvdb.New`, `fanarttv.New`) quando a credencial correspondente não está vazia. Sem
  credencial, o provider **nunca entra no `provider.Registry`** — não é uma
  degradação parcial, é ausência total do objeto.
- `internal/server/entities.go:montarExterno` devolve `nil` inteiro quando
  `s.providers.Vazio()`; e `camadasDaFonte` devolve `nil` **por fonte** quando
  `s.providers.Get(fonte)` falha — **mesmo que `media_external` tenha linhas
  daquela fonte com `payload_json` presente e válido**. O motivo do `Get` falhar
  não importa para o código: chave nunca configurada, chave revogada, provedor
  descontinuado — todos colapsam no mesmo "sem provider = sem dado", inclusive
  para dado que já está gravado localmente.
- O mapeamento raw→canônico (`prov.MapearDetalhe`, `prov.(SimilarProvider).Similares`,
  os extratores de `internal/richfields`) é **função pura**: recebe o
  `payload_json` já salvo e devolve campos estruturados, **sem nenhuma chamada de
  rede nem credencial**. Hoje essa função só é alcançável através do mesmo
  `provider.Registry` que também guarda a capacidade de busca ao vivo
  (`BuscarDetalhe`, que sim precisa de credencial) — as duas capacidades,
  semanticamente independentes, compartilham um único gate binário.
- Um segundo ponto com o mesmo defeito, achado na mesma investigação:
  `internal/externaldetail/externaldetail.go:PayloadFresco` — quando o cache
  (`external_detail`) expira (`TTLPadrao = 24h`) e a re-busca ao provider falha
  (rede fora, chave revogada, rate limit), a função devolve o **erro cru** e
  descarta o payload velho ainda sentado na mesma linha da tabela, em vez de
  servir o stale-mas-presente e só sinalizar que não conseguiu atualizar.

Isso contraria dois princípios já firmados no projeto: `AGENTS.md` §6
("independência de provider: a mídia sobrevive a QUALQUER provider registrado") e
[ADR-0011](0011-external-ids-autoritativo-media-external-cache.md) (`media_external`
já é declarado "cache de payload" — implicitamente duradouro, não condicionado à
sessão de rede atual). E é o oposto do objetivo de fundação do projeto: um item
catalogado deve ficar **legível para sempre em modo 100% offline**, mesmo que o
provider que o enriqueceu um dia deixe de existir, mude de API, ou o dono decida
não manter mais aquela chave.

## Decisão

**Separar as duas capacidades hoje fundidas no `provider.Registry`:**

1. **Capacidade de busca ao vivo** (`BuscarDetalhe`/`BuscarSerieArvore`/etc.) —
   continua exigindo credencial. Sem ela, o provider não entra no registro de
   busca; comportamento atual preservado (não dá pra buscar o que nunca foi
   buscado, e está tudo bem).
2. **Capacidade de leitura/parse de payload já cacheado** (`MapearDetalhe`,
   `Similares`, extratores de `internal/richfields`, `PessoaProvider` sobre
   payload salvo) — **passa a estar disponível para QUALQUER `source` que o
   binário sabe interpretar, independente de credencial configurada agora**. Um
   provider "parser-only" nasce registrado sempre (`tmdb`, `tvdb`, `fanarttv` são
   codecs conhecidos do binário, não segredos) — só a busca ao vivo depende de
   chave.
3. `montarExterno`/`camadasDaFonte` (`internal/server/entities.go`) passam a
   consultar a capacidade de parse (sempre presente), não mais o registro de
   busca ao vivo — uma linha de `media_external` com payload válido **sempre**
   compõe a camada externa do resolver, chave configurada ou não.
4. `externaldetail.PayloadFresco` (`internal/externaldetail/externaldetail.go`):
   quando a re-busca falha (TTL expirado + erro do provider), **serve o payload
   stale existente** em vez de propagar o erro — best-effort explícito, mesmo
   espírito de `doCacheFresco` ("cache é otimização, não barreira") aplicado
   também ao caminho de erro, não só ao caminho feliz. O chamador pode
   opcionalmente saber que o dado é stale (campo extra na resposta, fora de
   escopo desta ADR fixar o formato exato), mas **nunca perde o dado por causa
   de indisponibilidade do provider**.
5. Consequência operacional direta: `s.providers.Vazio()` deixa de ser sinônimo de
   "sem dado externo nenhum" — passa a significar só "sem busca nova possível".
   Uma instância sem NENHUMA credencial configurada, restaurada de um backup de
   uma instância que teve credenciais no passado, deve renderizar 100% do que já
   foi catalogado.

## Consequências

- Um item catalogado sobrevive à saída de circulação do provider que o
  enriqueceu (TMDB descontinuar a API, chave expirar, dono não renovar) — condição
  central do princípio de sobrevivencialismo digital do projeto, não só um detalhe
  de UX.
- Restaurar um backup em ambiente novo (como a migração que expôs isto) nunca
  mais perde elenco/sinopse/similares por falta de reconfigurar credenciais no
  primeiro boot — a UI de Integrações (ADR-0023) continua sendo o único jeito de
  habilitar busca NOVA, mas para de ser pré-requisito de leitura do que já existe.
- Custo: o `provider.Registry` (ou um registro irmão) precisa distinguir
  explicitamente as duas capacidades por tipo/interface, em vez de um único mapa
  `nome→Provider`. Providers "parser-only" (sem credencial) precisam continuar
  falhando de forma clara em `BuscarDetalhe` (não silenciar a ausência de busca —
  só a ausência de LEITURA do que já existe é o bug).
- Fora de escopo desta ADR: decidir o formato exato de "sinalizar dado stale" na
  API (item 4) — fica como detalhe de implementação do briefing que materializar
  esta decisão.
- Decisão irmã: [ADR-0011](0011-external-ids-autoritativo-media-external-cache.md)
  (o que `media_external` é) e [ADR-0023](0023-integracoes-configuraveis-via-ui-precedencia-sobre-env.md)
  (como a credencial chega até o provider) continuam valendo sem mudança — esta
  ADR só corrige QUEM PODE LER o cache que as duas já descrevem.
