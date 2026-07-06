# 02 — Glossário

Vocabulário canônico do projeto. Termos aqui têm significado fixo nas demais docs
e nos briefings.

| Termo | Definição |
|---|---|
| **Entity (entidade)** | Unidade central do modelo: qualquer mídia catalogada. Tem um `type` (`movie`, `tv`, `episode`, `collection`, …). Ver [04](04-modelo-de-dados.md). |
| **Camada de override** | Registro esparso das edições do dono. Vence sempre sobre o dado externo e o do arquivo. É o que dá autoridade final ao usuário. |
| **Dado externo** | Payload puxado de uma fonte (TMDB etc.), guardado cru e identificado pela fonte, como *base* editorial. Cacheado localmente. |
| **Dado do arquivo (mediainfo)** | Fatos técnicos extraídos do arquivo de mídia (codec, HDR, faixas, duração). Não editável. |
| **Resolver** | Componente que compõe o valor final de uma entidade: `override ⟵ externo ⟵ arquivo`. Toda API serve do resolver, não do dado bruto. |
| **`extension_data`** | Blob JSON *namespaced* com campos específicos de uma vertical. Não altera o schema do núcleo. Validado por JSON Schema. |
| **Namespace (de extensão)** | Chave no formato `<owner>.<slug>@<major>` (ex.: `core.tv@1`, futuramente `plugin.games@1`). |
| **Vertical** | Uma categoria de mídia com campos próprios (filmes, séries, games, livros…). No MVP as verticais são internas (`core.*`). |
| **Provider** | Cliente de uma fonte externa de metadados (TMDB, TVDB, IMVDB, MusicBrainz…). Usado sob demanda. |
| **Datasource / Library source** | Pasta do servidor apontada pelo dono para varredura. |
| **Scan** | Varredura não-destrutiva de um datasource: descobre arquivos e extrai mediainfo. |
| **Matching** | Associação de um arquivo/entrada a uma entidade (e, opcionalmente, a um id externo). |
| **`content_hash`** | Identidade estável de um arquivo por conteúdo, para não duplicar registros ao renomear. |
| **Asset** | Mídia auxiliar de uma entidade (poster, fanart, banner, logo…). Origem `external` ou `user`. Binário em disco; metadado no banco. |
| **`status`** | Estado da entidade no catálogo (`owned`, `wanted`, `draft`, `unreleased`…). Sustenta "o banco é maior que a biblioteca". |
| **`/api/tmdb`** | Camada da API **compatível com o TMDB**, para apontar clientes existentes (Jellyfin/Kodi). |
| **`/api/v1`** | API **nativa**, para tudo que o TMDB não modela (override, biblioteca, mediainfo, usuários). |
| **`VITE_API_BASE`** | Variável que define a base da API para o frontend: vazia = mesmo origin (binário Go); preenchida = API externa/remota. |
| **Plugin** | Extensão de terceiros que adiciona verticais/integrações. **Alvo do design, fora do MVP** — ver [11](11-decisoes-abertas.md). |
| **Briefing** | Ordem de serviço autocontida para uma unidade de trabalho. Ver [`briefings/`](../briefings/README.md). |
