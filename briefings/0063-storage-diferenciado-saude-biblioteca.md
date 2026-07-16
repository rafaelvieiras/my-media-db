# 0063 — Storage diferenciado na saúde da biblioteca

> `/health/system` passa a separar quanto de disco é banco, quanto é mídia
> GERENCIADA (vídeo + NFO/arte escritos pelo MyMediaDB dentro da pasta
> rastreada), quanto é cache interno da app (`assetsDir`) e quanto é
> arquivo NÃO reconhecido dentro das pastas rastreadas.

| | |
|---|---|
| **ID** | 0063 |
| **Tipo** | feature |
| **Fase (roadmap)** | Transversal / infraestrutura operacional — mesma família do [0052](0052-fundacao-sistema-de-jobs.md)/[0053](0053-job-saude-biblioteca.md)/[0055](0055-telemetria-sistema-saude.md) |
| **Status** | done |
| **Docs relacionadas** | [03-arquitetura](../docs/03-arquitetura.md), [08-artwork-e-assets](../docs/08-artwork-e-assets.md) |
| **Depende de** | 0053, 0055, 0062 |

---

## 1. Objetivo

`GET /health/system` (briefing 0055) já soma o tamanho dos vídeos rastreados
(`media_file.size`) por `library_source` e o tamanho do `.sqlite`. Isso NÃO é
suficiente depois do [briefing 0062](0062-midias-locais-nfo-e-artes-na-pasta.md):
a partir dele, a aplicação também escreve arte/NFO **dentro** da pasta
rastreada (antes só existia no cache interno `assetsDir`) — e pastas de filme
frequentemente têm arquivos que **não pertencem ao sistema** (samples, legendas
soltas, `.nfo` de outra ferramenta, lixo). Sem diferenciar essas três coisas,
o dono não sabe realmente quanto espaço a aplicação está "cobrando" dele.

## 2. Jornada do usuário

1. Usuário abre a tela de saúde da biblioteca / admin (onde `/health/system`
   já é exibido, briefing 0055).
2. Em vez de um único "espaço usado" por fonte, vê quatro números somando o
   total: **banco de dados**, **mídia gerenciada** (vídeos + NFO/arte que o
   MyMediaDB reconhece como seus, escritos por convenção do 0062 ou já
   rastreados pelo scanner), **cache interno da aplicação** (`assetsDir`), e
   **não identificado** (arquivos dentro de pastas rastreadas que não casam
   com nenhum padrão conhecido — nem vídeo indexado, nem NFO/arte com nome
   canônico do 0062).
3. Desvio: fonte offline (`library_source.state = 'offline'`) — mantém o
   último valor calculado (não recalcula, mesma política do 0055 para
   `Statfs`), com um selo indicando que o número pode estar desatualizado.

## 3. Escopo

**Dentro:**
- Extensão do payload de `GET /health/system` com os quatro buckets acima.
- Classificação de arquivo "gerenciado" vs. "não identificado" reaproveitando
  a MESMA varredura completa que o job `saude_biblioteca` (0053) já faz a
  cada execução (`scanner.Manager.Start(source)` por fonte) — **sem** nova
  varredura de disco dedicada a este briefing.
- Medição do tamanho de `assetsDir` (cache interno da aplicação).

**Fora (explicitamente):**
- Qualquer ação sobre os arquivos "não identificados" (apagar, mover,
  quarentena) — este briefing só MEDE e EXIBE. Ação fica para um briefing
  futuro, se o dono pedir.
- Nova varredura de disco independente do scan já existente — depende do
  [briefing 0064](0064-job-midias-faltantes-watch-scan-incremental.md) só na
  medida em que reaproveita a MESMA passada de scan; não introduz outra.
- Diferenciar por `kind` de arte dentro do bucket "mídia gerenciada" (fica
  como granularidade única: vídeo + NFO + arte juntos).

## 4. Contrato técnico

### Dados / modelo

Nenhuma tabela nova. O cálculo é feito em memória a partir do que o scan
(0053, estendido abaixo) já enumera — **não** persiste o detalhe por
arquivo, só os agregados que `/health/system` expõe (mesmo padrão do 0055:
resposta calculada sob demanda, sem cache de longa duração no banco).

### API

`GET /api/v1/health/system` (existente, 0055) — o objeto `Storage` ganha:

```jsonc
{
  "storage": {
    "database": { "size_bytes": 0 },        // já existe
    "sources": [ /* já existe, por library_source */ ],
    "managed_media_bytes": 0,               // NOVO: soma de vídeos indexados
                                             // + NFO/arte com nome canônico
                                             // (0062) dentro de pastas rastreadas
    "app_cache_bytes": 0,                   // NOVO: tamanho de assetsDir
    "unmanaged_bytes": 0                    // NOVO: resto encontrado nas pastas
                                             // rastreadas que não casa com nada
                                             // acima
  }
}
```

`openapi.yaml` ganha os três campos novos em `HealthSystemResponse` (ou
schema equivalente já usado pelo 0055).

### Backend (Go)

- `internal/job/libraryhealth/libraryhealth.go` (0053): a rotina que já
  percorre cada `library_source` durante o scan passa a, além do que já faz
  (matching de vídeo), classificar CADA arquivo não-vídeo encontrado na
  varredura em duas categorias: **conhecido** (nome casa com algum padrão de
  `internal/localmedia/naming.go`, do 0062 — ex. `poster.jpg`, `*-fanart.jpg`,
  `*.nfo`) ou **não identificado** (todo o resto). Acumula bytes por
  categoria e persiste o resultado agregado (por execução) num lugar que
  `/health/system` possa ler — reaproveitando a mesma estrutura de resultado
  que o job de saúde já grava ao final de cada rodada (`job_execution` ou
  equivalente já existente; não criar tabela nova só para isso).
- Novo helper `internal/telemetry/` (mesmo pacote de `UsoDisco`, já citado na
  pesquisa arquitetural) para somar o tamanho de `assetsDir` via
  `filepath.WalkDir` simples (esse diretório é só da aplicação, pequeno
  comparado à biblioteca — não precisa de otimização incremental).
- `internal/server/health_system.go`: monta os três campos novos a partir do
  resultado mais recente do job de saúde + o helper de `assetsDir`.

### Frontend (Vite/React)

- Tela de saúde/admin que já exibe o storage do 0055: adiciona os três
  blocos novos (mídia gerenciada / cache da app / não identificado) ao lado
  do que já existe (banco + por fonte).

### Arquivos afetados (previsão)

- `internal/job/libraryhealth/libraryhealth.go` (classificação de arquivo
  durante o scan já existente)
- `internal/telemetry/*.go` (tamanho de `assetsDir`)
- `internal/server/health_system.go`
- `openapi.yaml`
- `web/src/**` (tela de saúde/admin)

## 5. Critérios de aceite

- [ ] `GET /health/system` retorna `managed_media_bytes`, `app_cache_bytes` e
      `unmanaged_bytes` somando (aproximadamente, dado que fontes offline
      usam valor cacheado) o total de disco ocupado pelas pastas rastreadas.
- [ ] Um arquivo estranho colocado manualmente numa pasta de filme rastreada
      (ex. `sample.txt`) aparece computado em `unmanaged_bytes` após a
      próxima execução do job de saúde, e NÃO em `managed_media_bytes`.
- [ ] Um `poster.jpg` escrito pelo mecanismo do 0062 é computado em
      `managed_media_bytes`, não em `unmanaged_bytes`.
- [ ] `app_cache_bytes` reflete o tamanho real de `assetsDir` (teste com
      arquivo de tamanho conhecido).
- [ ] Nenhuma varredura de disco NOVA é introduzida — teste/verificação de
      que a classificação acontece dentro da mesma passada de
      `scanner.Manager.Start` que o job de saúde já dispara (ex. contando
      chamadas de `filepath.WalkDir` antes/depois deste briefing).

## 6. Notas de implementação / decisões

- Reaproveitar a varredura existente do job de saúde é a decisão central
  deste briefing — evita duplicar o custo de I/O que motivou o
  [briefing 0064](0064-job-midias-faltantes-watch-scan-incremental.md) a
  existir (não varrer pastas "muito lento").
- O termo "órfão" já é usado no projeto para OUTRO conceito (entities/itens
  sem match, 0053/0018) — este briefing usa deliberadamente "não
  identificado"/`unmanaged` para não colidir.

## 7. Fora deste briefing / follow-ups

- Ação sobre arquivos não identificados (limpeza assistida).
- Granularidade por `kind` de arte dentro de "mídia gerenciada".
