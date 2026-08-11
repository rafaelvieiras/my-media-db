# ADR-0023 — Integrações configuráveis via UI: registro dinâmico de credenciais, banco com precedência sobre variável de ambiente

- **Status:** Aceita
- **Data:** 2026-08-10
- **Agenda:** [I1–I4](../00-agenda-de-decisoes.md) · **Spec:** [04-modelo-de-dados](../04-modelo-de-dados.md), [07-enriquecimento-providers](../07-enriquecimento-providers.md), [03-arquitetura](../03-arquitetura.md)

## Contexto

Hoje toda credencial de fonte externa (TMDB, TheTVDB, fanart.tv) só existe
como variável de ambiente lida uma vez em `cmd/mymediadb/main.go`
(`MYMEDIADB_TMDB_TOKEN`/`_API_KEY`, `MYMEDIADB_TTDB_API_KEY`,
`MYMEDIADB_FANARTTV_API_KEY`) e usada para construir o `provider.Registry` no
boot. Trocar uma chave exige editar o `.env`/compose e **reiniciar o
processo** — não há UI, não há validação amigável, e o dono não tem como ver
"o que está configurado" sem ler variáveis de ambiente do container.

O dono pediu uma tela `/dashboard/integrations`, com subpágina por integração
e formulário dinâmico, para substituir esse hardcode — começando pelo TMDB —
e desenhada para que **plugins futuros** (docs/11, maquinário ainda fora do
MVP) possam registrar suas próprias integrações sem tocar no núcleo.

Isso esbarra na letra de uma regra hoje fixada em [AGENTS.md](../../AGENTS.md)
("segredos nunca no código — chaves de API só por variável de ambiente"). Essa
regra nasceu para proibir chave **commitada no repositório** (o incidente do
IMVDB hardcoded, ver [11](../11-decisoes-abertas.md)), não para proibir
armazenamento em banco por trás de uma UI administrativa — e o projeto **já
tem precedente** de mover configuração de env para banco+UI: o L1
([ADR-0018](0018-modelo-multi-idioma-dimensao-nas-camadas.md)) fez exatamente
isso para idiomas ("config em settings no banco, não em env"), e o briefing
0093 já grava um segredo por-instância (token do webhook Jellyfin) em
`app_settings`. Falta generalizar o padrão para credenciais de provider e
abrir o ponto de extensão para outras integrações.

## Decisão

**Banco (via UI) é a camada de override; variável de ambiente vira só o
bootstrap/fallback de deploy zero-touch — o mesmo formato override-vence do
resolver (`override ⟵ externo ⟵ arquivo`, R1–R7), aplicado a config de
integração:**

```
valor efetivo de um campo  =  valor no banco, SE PRESENTE
                            ⟵  variável de ambiente registrada pro campo
                            ⟵  ausente (integração "não configurada")
```

1. **Novo pacote `internal/integration`** (paralelo a `internal/provider`,
   mesmo espírito de capacidade-opcional já usado lá): define `Descriptor`
   (o contrato que QUALQUER integração — núcleo hoje, plugin amanhã —
   implementa para se registrar) e `Registry` (mesmo shape do
   `provider.Registry`: `Registrar/Get/All/Nomes`, ordem de registro
   preservada, zero-value utilizável).

   ```go
   type Campo struct {
       Nome, Label, Tipo string // Tipo: "text"|"secret"|"bool"|"number"|"select"
       Obrigatorio        bool   // hint de UI (asterisco); NÃO decide "configurada"
       Ajuda, Placeholder string
       EnvVar             string   // env var de bootstrap/fallback deste campo ("" = nenhuma)
       Opcoes             []Opcao  // só p/ Tipo="select"
   }

   type Descriptor interface {
       Slug() string        // ex. "tmdb" — chave de registro e de URL
       Nome() string         // nome exibível ("TMDB")
       Descricao() string
       Campos() []Campo
       // Configurada decide, a partir dos valores EFETIVOS (banco⟵env), se a
       // integração está operante. É do Descriptor, não do framework, porque a
       // regra varia (TMDB: token OU api_key; outra fonte pode exigir os dois).
       Configurada(efetivos map[string]string) bool
   }

   // Capacidade OPCIONAL (mesmo padrão de provider.ColecaoProvider etc.):
   // um Descriptor pode implementar Testador para a UI oferecer "testar conexão".
   type Testador interface {
       Testar(ctx context.Context, valores map[string]string) error
   }
   ```

   Cada provider dono de credencial (`internal/provider/tmdb`, depois
   `tvdb`/`fanarttv`) ganha um adapter que implementa `integration.Descriptor`
   delegando pra sua própria `Credencial.Configurada()` já existente — sem
   duplicar a regra que já vive lá.

2. **Nova tabela `integration_config`** (migration), UMA linha por slug,
   espelhando o blob esparso do R4 (`media_override`): só os campos que o
   dono efetivamente setou pela UI entram no JSON.

   ```sql
   CREATE TABLE integration_config (
       slug        TEXT PRIMARY KEY,
       values_json TEXT NOT NULL DEFAULT '{}',
       updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
   );
   ```

   Igual ao R6 (presença da chave = override ativo): gravar `null` num campo
   pelo PUT **remove a chave** do JSON (volta a herdar do env var); a
   ausência da tabela/linha nunca é erro, só "sem override, cai no env".

3. **Sem hot-reload no MVP.** Trocar uma credencial pela UI grava no banco
   imediatamente, mas o provider em memória só relê no próximo **restart do
   processo** (`docker restart`, sem rebuild de imagem — já um ganho grande
   sobre hoje, que exige editar `.env`/compose). A UI avisa isso
   explicitamente ("salvo — reinicie o servidor para aplicar"). Hot-reload
   (rebuild do `provider.Registry` in-process ao salvar) fica como
   **follow-up explícito**, não bloqueia a primeira entrega.

4. **Segredos nunca voltam crus pela API.** Um campo `Tipo="secret"` no GET
   devolve só `definido: bool` e `origem: "banco"|"env"|"nenhum"`, nunca o
   valor. Campos não-secretos devolvem o valor efetivo normalmente. Sem
   endpoint de "testar conexão" na primeira entrega (fica de follow-up); o
   dono valida salvando e reiniciando.

5. **AGENTS.md/CONTRIBUTING.md são atualizados** (texto exato nos briefings
   0095/0096): a regra de segredos passa de "só por variável de ambiente"
   para "banco (via UI de Integrações) tem precedência; variável de ambiente
   é só o bootstrap/fallback — nunca commitado, nunca hardcoded no código de
   nenhum dos dois jeitos".

## Consequências

- O dono configura/troca token do TMDB (e das próximas fontes) sem editar
  `.env` nem redeployar imagem — só reiniciar o container.
- Abre o **primeiro ponto de extensão concreto** para o futuro sistema de
  plugins (docs/11): um plugin de terceiros que precise de credencial própria
  implementa `integration.Descriptor` e se registra no mesmo `Registry` — sem
  o maquinário completo de plugins (manifest/sandbox/ciclo de vida) existir
  ainda. Não é o épico de plugins; é a fundação que ele vai reusar.
- `internal/integration` não conhece TMDB/TVDB/fanart.tv — cada provider
  contribui seu próprio `Descriptor` (mesma filosofia de capacidade-opcional
  do `internal/provider`).
- Fica de fora desta entrega (follow-ups): hot-reload sem restart, "testar
  conexão" (capacidade `Testador`), migrar o token do webhook Jellyfin (0093)
  para este mecanismo genérico, criptografia em repouso do `values_json`
  (hoje: mesmo nível de proteção que `app_settings` já tem — arquivo SQLite
  local, fora do controle de versão).
- Decisões relacionadas: [ADR-0001](0001-resolver-particionamento-por-origem.md)/
  [ADR-0004](0004-override-como-blob-json-esparso.md) (R1/R4, o padrão
  override-vence e blob esparso que esta decisão reaplica);
  [ADR-0018](0018-modelo-multi-idioma-dimensao-nas-camadas.md) (L1, precedente
  de settings em banco+UI); briefings **0095** (fundação: schema + registry +
  API + migração do TMDB) e **0096** (UI `/dashboard/integrations`).
