# 0095 — Fundação de Integrações: schema genérico + registry Go + API mínima + migra TMDB

> Substitui o hardcode de credencial de provider (env var lida só no boot) por
> um registro de config genérico, gravável em banco, com precedência sobre a
> variável de ambiente — a base que a UI do 0096 vai consumir.

| | |
|---|---|
| **ID** | 0095 |
| **Tipo** | feature |
| **Fase (roadmap)** | Fase 5+ — Integrações configuráveis via UI |
| **Status** | done |
| **Docs relacionadas** | [ADR-0023](../docs/adr/0023-integracoes-configuraveis-via-ui-precedencia-sobre-env.md), [04-modelo-de-dados](../docs/04-modelo-de-dados.md), [07-enriquecimento-providers](../docs/07-enriquecimento-providers.md), [03-arquitetura](../docs/03-arquitetura.md) |
| **Depende de** | — |

---

## 1. Objetivo

Hoje `MYMEDIADB_TMDB_TOKEN`/`_API_KEY` (e as demais credenciais de provider)
só existem como variável de ambiente lida uma vez em
`cmd/mymediadb/main.go`. Trocar uma chave exige editar `.env`/compose e
reiniciar; não há UI, não há como ver "o que está configurado" sem entrar no
container. Este briefing entrega a **fundação de backend**: um registro
genérico de config de integração (schema Go + tabela + API), com o TMDB como
primeiro consumidor migrado — sem nenhuma tela ainda (isso é o 0096). Fecha
[ADR-0023](../docs/adr/0023-integracoes-configuraveis-via-ui-precedencia-sobre-env.md).

## 2. Jornada do usuário

Este briefing não tem UI própria; o "usuário" aqui é a API (consumida pelo
0096) e, transitoriamente, um admin via `curl`/Postman:

1. `GET /api/v1/integrations` lista as integrações registradas (hoje: só
   `tmdb`) com `configurada: bool` — hoje `false` se nenhuma env var estiver
   setada, sem precisar entrar no container pra saber.
2. `GET /api/v1/integrations/tmdb` devolve o schema de campos (`token`,
   `api_key`) e o valor efetivo de cada um: se só a env var estiver setada,
   `origem: "env"`; sem nada, `origem: "nenhum"`.
3. `PUT /api/v1/integrations/tmdb` com `{"valores":{"token":"..."}}` grava no
   banco. Um novo `GET` no mesmo campo agora mostra `origem: "banco"` — a env
   var continua lá, mas não é mais o valor efetivo.
4. Reiniciar o processo: o provider TMDB passa a usar o valor do banco (sem
   hot-reload nesta entrega — ver §6).
5. `PUT` com `{"valores":{"token":null}}` remove o override daquele campo —
   volta a herdar da env var (ou "nenhum", se ela também estiver vazia).

## 3. Escopo

**Dentro:**
- Pacote `internal/integration`: interfaces `Campo`/`Descriptor`/`Testador`
  (capacidade opcional, não implementada nesta entrega) + `Registry` (mesmo
  molde do `provider.Registry`) + `Resolver` (calcula o valor efetivo de um
  campo: banco → env var → ausente).
- Migration `integration_config` (slug PK, `values_json` esparso,
  `updated_at`) + queries sqlc (ASCII).
- Adapter `integration.Descriptor` para o TMDB (`internal/provider/tmdb`),
  delegando a `Configurada()` já existente em `tmdb.Credencial`.
- `cmd/mymediadb/main.go` passa a resolver a credencial TMDB via
  `integration.Resolver` (banco → env var), em vez de `os.Getenv` direto.
- API: `GET /integrations`, `GET /integrations/{slug}`,
  `PUT /integrations/{slug}` — contrato OpenAPI + tipos gerados do front.
- Testes Go (registry, resolver, handlers) cobrindo os critérios de aceite.

**Fora (explicitamente):**
- Qualquer tela (`/dashboard/integrations` é o 0096).
- Migrar TVDB/fanart.tv para o registro (só o TMDB nesta entrega; os outros
  seguem via env var direto, sem regressão — ficam de follow-up).
- Migrar o token do webhook Jellyfin (0093, hoje em `app_settings`) para este
  mecanismo — follow-up, não regressão (endpoint atual continua igual).
- Hot-reload sem restart e endpoint de "testar conexão" (capacidade
  `Testador`) — ambos follow-up explícito (ADR-0023 §Consequências).
- Criptografia em repouso de `values_json` (mesmo nível de proteção que
  `app_settings` já tem hoje).

## 4. Contrato técnico

### Dados / modelo

Nova migration (`0021_integration_config.sql`, próximo número livre —
confirmar contra `internal/db/migrations/` no momento da implementação):

```sql
CREATE TABLE integration_config (
    slug        TEXT PRIMARY KEY,
    values_json TEXT NOT NULL DEFAULT '{}',
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);
```

`values_json` é um objeto raso `{campo: valor_string}` — só os campos que o
dono setou pela UI/API entram (blob esparso, mesmo espírito do R4/R6:
presença da chave = override ativo; ausência = herda da env var). Bump do
schema version de teste (`db_test.go`/`schema_nucleo_test.go`).

### Backend (Go)

`internal/integration/integration.go` (novo pacote):

```go
type TipoCampo string
const (
    TipoTexto   TipoCampo = "text"
    TipoSegredo TipoCampo = "secret"
    TipoBool    TipoCampo = "bool"
    TipoNumero  TipoCampo = "number"
    TipoSelect  TipoCampo = "select"
)

type Opcao struct{ Valor, Label string }

type Campo struct {
    Nome        string
    Label       string
    Tipo        TipoCampo
    Obrigatorio bool   // hint de UI (asterisco); NÃO decide "configurada"
    Ajuda       string
    Placeholder string
    EnvVar      string  // env var de bootstrap/fallback deste campo; "" = nenhuma
    Opcoes      []Opcao // só p/ TipoSelect
}

type Descriptor interface {
    Slug() string
    Nome() string
    Descricao() string
    Campos() []Campo
    // Configurada decide, a partir dos valores EFETIVOS (banco-ou-env por
    // campo), se a integração está operante. Cada Descriptor decide sua
    // própria regra (ex.: TMDB aceita token OU api_key).
    Configurada(efetivos map[string]string) bool
}

// Capacidade OPCIONAL — não implementada nesta entrega, só a interface
// (para não quebrar type-assert quando o 0096/follow-up a usar).
type Testador interface {
    Testar(ctx context.Context, valores map[string]string) error
}
```

`internal/integration/registry.go`: `Registry` idêntico em forma a
`provider.Registry` (`Registrar/Get/All/Nomes`, zero-value utilizável, último
a registrar sob o mesmo `Slug()` vence).

`internal/integration/resolver.go`: `Resolver` recebe o `*sql.DB`/querier;
método `Efetivo(ctx, slug string, campo Campo) (valor, origem string, ok bool)`
— lê `integration_config` pelo slug, checa a chave `campo.Nome` no
`values_json`; se ausente, cai em `os.Getenv(campo.EnvVar)` quando `EnvVar !=
""`; se nenhum dos dois, `ok=false`, `origem="nenhum"`. `Valores(ctx, slug,
campos)` agrega isso pra todos os campos de um Descriptor de uma vez (usado
pelo `Configurada()`).

`internal/provider/tmdb/integration.go` (novo): `tmdb.NovoDescriptorIntegracao()`
implementando `integration.Descriptor` — `Slug()="tmdb"`, dois `Campo`s
(`token` secreto/`MYMEDIADB_TMDB_TOKEN`, `api_key` secreto/
`MYMEDIADB_TMDB_API_KEY`), `Configurada` delega a
`tmdb.Credencial{Token: efetivos["token"], APIKey: efetivos["api_key"]}.Configurada()`.

`cmd/mymediadb/main.go`: `provedoresReais()` passa a montar `credTmdb` via
`integration.Resolver.Valores(ctx, "tmdb", ...)` em vez de `os.Getenv` puro —
mesmo resultado hoje (nada no banco → cai na env var), mas já lendo pela
camada nova. TVDB/fanart.tv **não mudam** nesta entrega (fora de escopo).

### API

Namespace `/api/v1`, tag `integrations`:

- `GET /integrations` → `200 {"integrations":[{slug,nome,descricao,configurada}]}`
  — itera `integration.Registry.All()`, resolve efetivos de cada e chama
  `Descriptor.Configurada()`.
- `GET /integrations/{slug}` → `200 {slug,nome,descricao,campos:[...]}`, cada
  campo com `{nome,label,tipo,obrigatorio,ajuda,placeholder,opcoes?,definido,origem}`
  e, **só quando `tipo != "secret"`**, `valor` com o efetivo. `404` se o slug
  não está no Registry.
- `PUT /integrations/{slug}` → body `{"valores":{"campo":"valor"|null}}`.
  Parcial: só os campos presentes no body mudam; `null` remove a chave do
  `values_json` (volta a herdar da env var). `200` com o mesmo shape do GET
  após gravar. `404` se slug desconhecido; `422` se `valores` vier vazio ou
  com chave que não é um `Campo` do Descriptor. **Não bloqueia** por
  obrigatoriedade ausente (o dono pode salvar parcialmente) — reflete só em
  `configurada:false` na listagem.

### Frontend (Vite/React)

Só `npm run gen:types` para os novos tipos ficarem disponíveis a
`web/src/lib/api-types.ts` (consumidos pelo 0096) — nenhuma tela nesta
entrega.

### Arquivos afetados

- `internal/db/migrations/00NN_integration_config.sql` (novo)
- `internal/db/queries/integration_config.sql` (novo, ASCII)
- `internal/db/sqlcgen/*` (gerado)
- `internal/integration/{integration,registry,resolver}.go` (+ testes) (novo pacote)
- `internal/provider/tmdb/integration.go` (+ teste) (novo)
- `internal/server/integrations.go` (+ teste) (novo)
- `internal/server/server.go` (rotas + campo `integrations integration.Registry`)
- `cmd/mymediadb/main.go` (migra bootstrap do TMDB)
- `openapi.yaml` (tag `integrations`)
- `web/src/lib/api-types.ts` (gerado)
- `docs/04-modelo-de-dados.md` (já feito neste PR de documentação — conferir se segue batendo)

## 5. Critérios de aceite

- [x] `GET /integrations` lista `tmdb` com `configurada:false` quando nem
      banco nem env var têm valor.
- [x] Setar `MYMEDIADB_TMDB_TOKEN` no ambiente (sem tocar o banco) faz
      `GET /integrations` devolver `configurada:true` e
      `GET /integrations/tmdb` devolver o campo `token` com
      `origem:"env"`, `definido:true`, sem `valor` (é secreto).
- [x] `PUT /integrations/tmdb {"valores":{"token":"abc"}}` grava no banco;
      `GET` subsequente devolve `origem:"banco"` pro campo `token` (a env var
      segue setada, mas não é mais a fonte efetiva).
- [x] `PUT` com `{"valores":{"token":null}}` remove o override — `GET`
      volta a mostrar `origem:"env"` (ou `"nenhum"` se a env var também
      estiver vazia).
- [x] Reiniciar o processo com um valor gravado no banco faz o provider TMDB
      efetivamente usar esse valor nas chamadas reais (teste de integração
      construindo `main`'s `provedoresReais()`-equivalente contra um banco
      com `integration_config` populado).
- [x] `GET /integrations/inexistente` → `404`.
- [x] `PUT /integrations/tmdb {"valores":{}}` → `422`.
- [x] `PUT /integrations/tmdb {"valores":{"campo_que_nao_existe":"x"}}` → `422`.
- [x] Campo `tipo:"secret"` NUNCA aparece com `valor` no JSON de resposta
      (nem em `GET` nem no `PUT` de eco) — só `definido`/`origem`.
- [x] `go build ./...`, `go vet ./...`, `go test ./... -count=1` verdes;
      `tsc -b` verde após `gen:types`.

## 6. Notas de implementação / decisões

- **Sem hot-reload.** `provedoresReais()` só lê o efetivo UMA VEZ no boot,
  igual hoje — só a FONTE do valor muda (banco-ou-env em vez de só env).
  Trocar a credencial pela UI (0096) exige restart do processo pra valer;
  isso é aceitável pro MVP (ADR-0023) e é um ganho sobre hoje mesmo assim
  (não precisa editar `.env`/rebuildar imagem).
- **`Configurada()` é do Descriptor, não do framework.** Evita inventar uma
  DSL de obrigatoriedade condicional (TMDB aceita token OU api_key; outra
  fonte pode exigir os dois) — cada integração já sabe essa regra hoje
  (`Credencial.Configurada()`), só reusamos.
- **TVDB/fanart.tv ficam de fora desta entrega** de propósito — o objetivo é
  provar o mecanismo com UM provider real antes de replicar; migrá-los é
  follow-up mecânico (mesmo adapter, campo a campo).
- **Slug = chave de URL e de registro.** Escolher `slug` estável desde já
  (`"tmdb"`, não `"TMDB"`/`"tmdb-provider"`) evita migration de dado depois.

## 7. Fora deste briefing / follow-ups

- UI `/dashboard/integrations` → **briefing 0096**.
- Hot-reload do `provider.Registry` sem restart.
- Capacidade `Testador` ("testar conexão") — endpoint
  `POST /integrations/{slug}/test`.
- Migrar TVDB e fanart.tv para `internal/integration`.
- Migrar o token do webhook Jellyfin (0093) para este mecanismo genérico
  (hoje em `app_settings`, chave própria).
