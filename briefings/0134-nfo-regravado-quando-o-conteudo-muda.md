# 0134 — NFO regravado quando o conteúdo muda

> O `.nfo` deixa de ser "grava uma vez e nunca mais": se o banco ganhou dado
> novo, o arquivo em disco é reescrito; se está igual, nada é tocado.

| | |
|---|---|
| **ID** | 0134 |
| **Tipo** | fix |
| **Fase (roadmap)** | Fase 2 — Biblioteca e scan |
| **Status** | ready |
| **Docs relacionadas** | [08-artwork-e-assets](../docs/08-artwork-e-assets.md), [0127](0127-revalidacao-arte-nfo-materializados-no-disco.md), [0128](0128-convergencia-local-path-nfo-e-job-midias-faltantes-honesto.md) |
| **Depende de** | — |

---

## 1. Objetivo

`gravarNFOSeFaltante` (job `midias_faltantes`) pula a gravação quando o
`nfo_path` registrado ainda aponta para o destino canônico e o arquivo existe —
`nfoJaConvergenteNoDisco` olha **só o caminho e a presença**, nunca o conteúdo.
Como `entity.nfo_path` nunca volta a nulo, o `.nfo` gravado uma vez fica
congelado para sempre.

Consequência medida no catálogo da sala em 06/nov/2026, durante o mutirão de
metadados: séries e episódios que ganharam sinopse e arte **depois** do primeiro
`materialize` continuavam com `.nfo` vazio ou desatualizado no disco. O caminho
usado para corrigir foi operacional e caro — backup do `.nfo`, `rm` do arquivo e
`materialize-missing-media` de novo. Um `.nfo` de 0 byte é o caso extremo: o
arquivo existe, então o job o considera pronto, e a biblioteca consumidora lê um
episódio sem nada dentro.

## 2. Jornada do usuário

1. O dono cataloga a biblioteca. O job `midias_faltantes` grava os `.nfo`.
2. Semanas depois ele completa o enriquecimento (sinopse, elenco, nota) ou troca
   o pôster.
3. **Antes:** o job roda de novo, vê `nfo_path` preenchido e o arquivo presente,
   e não faz nada — o `.nfo` continua contando a história antiga (ou nenhuma).
4. **Depois:** o job monta o XML que o banco produciria agora, compara com o do
   disco e regrava só quando difere. `.nfo` vazio, truncado ou desatualizado se
   corrige sozinho na próxima passada.
5. Se nada mudou no banco, o arquivo não é tocado (nem `nfo_written_at` muda),
   então o mtime do `.nfo` continua sendo sinal de mudança real.

## 3. Escopo

**Dentro:**
- `nfoJaConvergenteNoDisco` passa a comparar **conteúdo**: monta o XML esperado
  (resolver + `localmedia.MontarNFO`, com os campos locais do Kodi/Emby
  preservados a partir do arquivo em disco) e compara byte a byte com o do disco.
- Igual → pula (comportamento de hoje). Diferente → regrava pelo caminho normal.
- O mesmo critério vale para `writeEntityNfo` (ação manual "Salvar informações
  (NFO)")? Não: a ação manual sempre regrava, como hoje. Ela é explícita.

**Fora (explicitamente):**
- Reescrever `.nfo` cujo *destino* mudou: isso já é tratado pelo 0128 (self-heal
  de naming legado, com remoção do órfão).
- Preservar campos locais além do que `ExtrairCamposUsuario` já cobre.
- Tocar em `.nfo` de season (não existe) ou em NFO de música.

## 4. Contrato técnico

### Backend (Go)
- `internal/server/midias_faltantes.go`:
  - `nfoJaConvergenteNoDisco(ctx, ent, nfoPathAntigo)` deixa de ser um teste de
    caminho e passa a devolver `(convergente bool, err error)` calculando:
    1. destino atual por `resolverDestino` (como hoje);
    2. se o destino difere do registrado ou o arquivo não existe → não
       convergente;
    3. lê o arquivo, extrai os campos locais (`localmedia.ExtrairCamposUsuario`),
       resolve a entity (`resolverEntity(ctx, ent.ID, "", false)`) e monta o XML
       (`localmedia.MontarNFO`);
    4. convergente quando os bytes são iguais.
  - `gravarNFOSeFaltante` usa o resultado; quando não converge, segue no fluxo
    atual (que já é idempotente por conteúdo e preserva os campos locais).
- Custo: uma leitura de arquivo e uma resolução de entity por entity elegível,
  por passada do job. Sem rede, sem download: as materializações de arte em
  `gravarNFO` só rodam quando há regravação.
- Nenhuma migration, nenhuma mudança de API, nenhuma mudança de schema.

### Arquivos afetados
- `internal/server/midias_faltantes.go`
- `internal/server/midias_faltantes_test.go`

## 5. Critérios de aceite

- [ ] Entity com `nfo_path` preenchido e `.nfo` **vazio** no disco: a próxima
      passada do job grava o conteúdo e devolve `nfo_gravado=true`.
- [ ] Entity cujo banco ganhou sinopse depois da primeira gravação: o `.nfo` é
      reescrito com a sinopse nova.
- [ ] Entity cujo banco não mudou: o `.nfo` **não** é reescrito (conteúdo e
      mtime iguais; `nfo_gravado=false`).
- [ ] Os campos locais do Kodi/Emby (`playcount`, `watched`, `resume`,
      `userrating`) continuam preservados na regravação.
- [ ] `.nfo` de destino divergente continua sendo regravado e o órfão removido
      (comportamento do 0128 preservado).
- [ ] `go build ./... && go vet ./... && go test ./... -count=1` verdes.

## 6. Notas de implementação / decisões

- **Por que não comparar `nfo_written_at` com `updated_at`**: funciona por acaso
  e falha nos dois sentidos (edição de override não muda `updated_at` da entity
  resolvida; mudança em `media_external` de idioma alternativo não muda nada da
  entity). Comparar o XML gerado é a única medida fiel do que sairia.
- **Por que a comparação é byte a byte e não semântica**: o XML é gerado pelo
  mesmo `MontarNFO` nos dois lados do tempo; se o gerador mudar de formato, o
  arquivo *deve* ser reescrito. Ordem de elementos e indentação são estáveis
  porque saem do mesmo `encoding/xml`.
- **Por que preservar os campos locais antes de comparar**: sem isso, todo NFO
  com `playcount` do Kodi pareceria divergente e seria reescrito a cada passada
  (o que também apagaria o dado local, já que `MontarNFO` os recebe do usuário).
- **Degradação**: falha ao resolver a entity ou ao ler o arquivo não pode
  derrubar o job — cai no comportamento antigo (arquivo existe ⇒ pula) e loga.

## 7. Fora deste briefing / follow-ups

- Um job que audite todos os `.nfo` do disco contra o banco de uma vez (hoje a
  convergência é verificada por entity, na passada do `midias_faltantes`).
