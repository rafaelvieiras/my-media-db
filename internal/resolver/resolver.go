// Package resolver compõe a visão RESOLVIDA de uma entity a partir das camadas
// disponíveis. A precedência é `override ⟵ externo ⟵ arquivo`: o override do dono
// vence tudo, a camada externa (enriquecimento) vence o arquivo, e o arquivo/núcleo
// é a base sempre presente. A composição é campo a campo, guiada por um REGISTRO
// declarativo de origem (mapa `campo → {base, kind}`) que o motor ITERA — nada de
// `switch` por nome de campo (ADR-0001). Adicionar uma vertical/campo é trocar o
// registro (dado), não o código.
//
// A forma da resposta (Entidade) é a DEFINITIVA (doc 04/03) e a assinatura pública
// `Resolver(Camadas) Entidade` NÃO muda quando camadas chegam — só o conteúdo. Por
// isso a UI (briefing 0011) não sente a Fase 3.
package resolver

import (
	"bytes"
	"database/sql"
	"encoding/json"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/richfields"
)

// CamadaExterna é UMA fonte externa já na forma CANÔNICA: `Campos` usa OS NOSSOS
// nomes de campo (ex.: "title", "description", "tags", "extension_data"), não o
// shape cru do provider. O mapeamento raw→canônico é do provider (0014/0015); o
// resolver NÃO conhece o TMDB. As camadas chegam já ORDENADAS pela lista de
// prioridade (R2): a primeira com valor para um campo forma a base externa dele.
type CamadaExterna struct {
	// Fonte identifica o provedor (ex.: "tmdb"); hoje só informativo.
	Fonte string
	// Campos são os valores canônicos daquela fonte (nomes dos nossos campos).
	Campos map[string]any
}

// Camadas agrega as CAMADAS que o resolver compõe: o arquivo/núcleo (base, sempre
// presente), as fontes externas e o patch de override do dono. Quem chama passa o
// que tiver; a assinatura de Resolver não muda.
type Camadas struct {
	// Entity é a linha de núcleo (camada base ARQUIVO, sempre presente).
	Entity sqlcgen.Entity
	// Files são os media_file da entity (posse derivada e contagem saem daqui).
	Files []sqlcgen.MediaFile
	// PosterURL é a referência (relativa) do endpoint que serve o pôster local,
	// ou nil quando a entity não tem pôster. O chamador a monta (é um link de
	// API, não um dado de domínio), o resolver apenas a coloca no shape final.
	PosterURL *string
	// BackdropURL é a referência do endpoint que serve o FUNDO (fanart/backdrop)
	// local, quando cacheado (0015). Quando nil, o resolver cai na URL direta da
	// fonte (backdrop_url do canônico externo) — o mesmo campo, servido do disco
	// quando disponível.
	BackdropURL *string

	// Externo são as camadas EXTERNAS canônicas, ordenadas por prioridade (R2).
	// Vazio = sem enriquecimento (comportamento do 0010 preservado).
	Externo []CamadaExterna
	// Override é o patch esparso CRU de media_override.override_json: presença da
	// chave = override ativo, mesmo com valor `null` (R6/ADR-0006). Nil = sem
	// override.
	Override map[string]json.RawMessage

	// LangPedido é o idioma resolvido do pedido (default = principal das settings).
	// Ecoado no shape resolvido; vazio quando não há contexto de idioma (o motor
	// então preserva o comportamento pré-multi-idioma do 0013).
	LangPedido string
	// Cadeia é a ordem de fallback já derivada das settings (ADR-0019):
	// `[pedido, fallback, original_language?]` deduplicada, terminando
	// IMPLICITAMENTE na base neutra (arquivo) — o motor cai no arquivo quando
	// nenhum idioma da cadeia tem valor. VAZIA = sem contexto de idioma: campos
	// traduzíveis compõem como no 0013 (sem consultar a cadeia).
	Cadeia []string
	// IdiomaPrincipal é o idioma PRINCIPAL das settings. Serve para interpretar o
	// externo/override LEGADO (formato do 0015, um valor escalar/lista sem chave de
	// idioma): esse valor é tratado como pertencente ao principal (ADR-0018, nota
	// de tolerância do 0019). Vazio ⇒ usa o primeiro idioma da cadeia.
	IdiomaPrincipal string
}

// Entidade é a visão RESOLVIDA — o shape final servido em
// GET /api/v1/entities/{id}. Os ponteiros saem como `null` no JSON quando
// ausentes; `tags`/`external_ids`/`extension_data` saem sempre como
// lista/objeto (nunca null) para simplificar o consumo no front.
type Entidade struct {
	ID            string            `json:"id"`
	Type          string            `json:"type"`
	Title         string            `json:"title"`
	OriginalTitle *string           `json:"original_title"`
	Description   *string           `json:"description"`
	ReleaseDate   *string           `json:"release_date"`
	Status        string            `json:"status"`
	Tags          []string          `json:"tags"`
	ExternalIDs   map[string]string `json:"external_ids"`
	Owned         bool              `json:"owned"`
	FileCount     int               `json:"file_count"`
	PosterURL     *string           `json:"poster_url"`
	ExtensionData map[string]any    `json:"extension_data"`

	// ── Campos RICOS (paridade com o detalhe do externo, briefings 0022/0023) ──
	// São READ-ONLY: vêm só da camada EXTERNA (o dono não os edita por override
	// nesta fase). Os traduzíveis (Tagline/Keywords) compõem por idioma como
	// title/description; os neutros/estruturados são extraídos direto do canônico
	// externo via internal/richfields (o MESMO shape do detalhe do externo).
	// Genres NÃO tem campo próprio: são os `tags` (que já carregam os gêneros).
	Tagline  *string  `json:"tagline"`
	Keywords []string `json:"keywords"`
	// AiringStatus é a SITUAÇÃO de exibição de uma SÉRIE (0045): string CRUA da fonte,
	// exposta VERBATIM (TMDB: "Returning Series"|"Ended"|"Canceled"|"In Production"|
	// "Planned"; TVDB: status.name) — a tradução p/ pt-BR é na UI. É NEUTRA (não
	// traduzível) e READ-ONLY (o dono não a edita). NÃO confundir com o `Status` de
	// CATÁLOGO acima (owned/cataloged, intenção do dono): são campos distintos. Ausente
	// (omitempty) quando a fonte não a informa (filme, ou série só-TVDB sem match).
	AiringStatus  *string  `json:"airing_status,omitempty"`
	Runtime       *int     `json:"runtime"`
	Certification *string  `json:"certification"`
	VoteAverage   *float64 `json:"vote_average"`
	// LocalRating é a NOTA LOCAL do dono (0–10, ADR-0012): a avaliação PRÓPRIA,
	// gravada na camada de OVERRIDE, que COMPLEMENTA/SOBREPÕE a nota da fonte
	// (VoteAverage). null quando o dono não avaliou — a UI então mostra a nota da
	// fonte (limpar a nota local é NÃO-DESTRUTIVO). Diferente dos demais campos ricos
	// (read-only), esta é EDITÁVEL por override: entra no registro como kindNumero.
	LocalRating   *float64               `json:"local_rating"`
	BackdropURL   *string                `json:"backdrop_url"`
	Videos        []richfields.Video     `json:"videos"`
	Collection    *richfields.Collection `json:"collection"`
	Similar       []richfields.Similar   `json:"similar"`
	ExternalLinks map[string]string      `json:"external_links"`

	// Cast/Crew/Companies são os CRÉDITOS (elenco, equipe, produtoras — briefing
	// 0029 Fase B): paridade com o detalhe do externo, mesmo shape (via richfields,
	// fonte única). READ-ONLY nesta rodada (sem editor — Fase C). Sempre `[]` (nunca
	// null) quando ausentes. ProfileURL/LogoURL saem com a URL DIRETA do provider
	// (richfields é puro); o server os reescreve para o proxy local pós-resolução
	// (mesma fronteira de Ancestors/Ownership).
	Cast      []richfields.Cast    `json:"cast"`
	Crew      []richfields.Crew    `json:"crew"`
	Companies []richfields.Company `json:"companies"`

	// Lang é o eco do idioma pedido (ADR-0019). Vazio quando não há contexto de
	// idioma (comportamento do 0013).
	Lang string `json:"lang"`
	// OriginalLanguage é o código NEUTRO do idioma original da obra (âncora do
	// fallback), ou null quando a fonte não o informou.
	OriginalLanguage *string `json:"original_language"`
	// ResolvedLanguages é a PROVENIÊNCIA DE IDIOMA (ADR-0019): campo traduzível →
	// idioma efetivo que serviu o valor (`""` = base neutra/arquivo). Mapa lateral
	// que NÃO muda os campos (o shape segue plano). Sempre não-nil (vazio quando
	// não há contexto de idioma).
	ResolvedLanguages map[string]string `json:"resolved_languages"`

	// Ancestors é a trilha RAIZ→PAI (breadcrumb, 0032) da entity na árvore de
	// séries: episódio → [série, temporada]; temporada → [série]; obra de topo
	// (movie/tv/collection) → []. Sempre não-nil (`[]`, nunca null). NÃO vem do
	// resolver puro — é anotada no server pós-resolução (subindo por
	// ListarRelacoesPais), mesma fronteira dos internal_id (0026/0031).
	Ancestors []AncestorRef `json:"ancestors"`

	// Ownership é a POSSE AGREGADA de uma entity-pai da árvore de séries (0034):
	// quantos episódios são possuídos de quantos no total. Presente (não-null) só
	// para `tv` (soma across temporadas) e `season` (episódios diretos); null para
	// movie/episódio/coleção (posse simples via `owned`). NÃO vem do resolver puro —
	// é anotada no server pós-resolução (mesma fronteira de Ancestors), porque a
	// agregação percorre entity_relation.
	Ownership *OwnershipInfo `json:"ownership"`

	// Orderings são as ordens de exibição de episódios disponíveis de uma SÉRIE
	// (0040): a `default` (o default_season_type gravado no import) e a lista
	// `available` (união das chaves de `numbering` dos episódios + `absolute` quando
	// há absolute_number), com o default primeiro. Presente (não-null) só para `tv`
	// com ao menos uma ordem; `omitempty` (ausente) para os demais tipos e para série
	// sem numbering — a UI então esconde o seletor. Anotada no server pós-resolução
	// (mesma fronteira de Ancestors/Ownership), porque varre a árvore de episódios.
	Orderings *OrderingsInfo `json:"orderings,omitempty"`

	// StillURL é a URL CRUA do still (frame 16:9) do EPISÓDIO (0042): artwork NEUTRO de
	// idioma, lido do extension_data core.tv@1 (still_url). Presente (não-null) só para
	// `episode` com still; `omitempty` (ausente) para os demais tipos e para episódio sem
	// still — a UI cai no placeholder. Anotada no server pós-resolução (o resolver é puro,
	// não conhece core.tv), e servida pelo image proxy (busca+cacheia sob demanda) no
	// front. Não é baixada no import: só a URL é persistida.
	StillURL *string `json:"still_url,omitempty"`

	// CatalogCompleteness é a completude de CATALOGAÇÃO do item (briefing 0058)
	// contra a régua configurada em /settings (0057), calculada AO VIVO a cada
	// request pelo server (anotarCompletudeCatalogacao, entities.go) —
	// reaproveita os dados que este mesmo resolverEntity já monta, sem query
	// extra. NÃO vem do resolver puro (mesma fronteira de Ancestors/Ownership/
	// StillURL). `null` somente quando o item é draft (nunca casado) — não faz
	// sentido avaliar metadados que ainda não existem; para item não-draft
	// nunca é nil aqui (diferente de EntityCard.catalog_completeness na
	// listagem, que lê de um cache em lote recalculado pelo job e pode ficar
	// nil até a primeira execução). Distinto de completude de POSSE
	// (web/src/lib/entity-badges.ts, briefings 0036/0037).
	CatalogCompleteness *CatalogCompleteness `json:"catalog_completeness"`
}

// CatalogCompleteness é o resultado da avaliação de completude de
// CATALOGAÇÃO (briefing 0058) de um item contra a régua por tipo configurada
// em /settings (0057, internal/server/catalog_completeness.go). Vive no
// pacote resolver (não em internal/server) para poder ser um CAMPO do shape
// final (Entidade), serializado direto por GET /entities/{id} — o pacote
// server usa este MESMO tipo (via alias `resultadoCompletude`,
// internal/server/completeness.go) para não duplicar a definição nem exigir
// conversão ao preencher o campo.
type CatalogCompleteness struct {
	// Complete é true quando o item atende a régua inteira, ou quando o tipo
	// não tem entrada na config (tipo sem régua nunca é incompleto).
	Complete bool `json:"complete"`
	// Missing são as chaves ESTÁVEIS do que falta, em ordem fixa (description,
	// runtime, certification, keywords, cast, media_file, artwork:<kind>...).
	// `[]` (nunca nil) quando Complete é true.
	Missing []string `json:"missing"`
}

// OrderingsInfo descreve as ordens de exibição de episódios de uma série (0040):
// `Default` é a ordem inicial (o default_season_type da série) e `Available` a lista
// de ordens oferecidas ao seletor (o default primeiro, `absolute` ao fim quando há
// número absoluto). A UI só mostra o seletor quando `Available` tem mais de um item.
type OrderingsInfo struct {
	Default   string   `json:"default"`
	Available []string `json:"available"`
}

// OwnershipInfo é a posse agregada de uma temporada/série (0034): episódios
// possuídos (`owned_count`) sobre o total de episódios (`total_count`). A UI
// renderiza o badge "possui 12/13". Contagens derivadas dos vínculos
// media_file_entity (kind='tv_episode') + a árvore entity_relation.
type OwnershipInfo struct {
	OwnedCount int `json:"owned_count"`
	TotalCount int `json:"total_count"`
}

// AncestorRef é um nó ancestral na trilha do breadcrumb (0032): o mínimo para
// renderizar e NAVEGAR a um nível-pai (id + título + type). Montado no server.
type AncestorRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

// baseOrigem diz de onde vem a BASE de um campo antes do override: `externo`
// (enriquecimento sobrepõe o arquivo), `arquivo` (arquivo é autoritativo; externo
// não é consultado) ou `nenhuma` (só o override fala). O motor lê a etiqueta, não
// o nome do campo.
type baseOrigem int

const (
	baseArquivo baseOrigem = iota
	baseExterno
	baseNenhuma
)

// kindCampo classifica a forma de composição do campo (escalar, lista por
// operações, ou extensão por namespace/chave). O motor despacha por kind — uma
// classe de comportamento —, jamais por nome de campo.
type kindCampo int

const (
	kindEscalar kindCampo = iota
	kindLista
	kindExtensao
	// kindNumero é um escalar NUMÉRICO (*float64) — ex.: a nota local do dono. Compõe
	// como o escalar (override vence; `null` apaga), mas o valor é um número, não uma
	// string. É uma classe própria para o motor despachar sem `switch` por nome.
	kindNumero
	// kindCreditos é o crédito (elenco/equipe/produtoras, briefing 0029 Fase C):
	// base SEMPRE externa (mesclada por prioridade, sem base-arquivo), override é um
	// PATCH segregado por sub-kind (cast/crew/companies) com operações
	// reorder/remove/edit/add — não um escalar/lista/extensão comum. Ver
	// comporCreditos.
	kindCreditos
)

// origemCampo é a etiqueta declarativa de um campo editorial. `traduzivel` marca
// os campos que compõem POR IDIOMA (ADR-0018): quando há contexto de idioma
// (cadeia não-vazia), o motor os resolve na dimensão idioma × camada; os NEUTROS
// (traduzivel=false) nunca consultam a cadeia. É etiqueta no registro, não `switch`
// por nome de campo (R1/ADR-0001).
type origemCampo struct {
	base       baseOrigem
	kind       kindCampo
	traduzivel bool
	// soLeitura marca campos que o motor RESOLVE (compõe da camada externa) mas que
	// NÃO são editáveis pelo dono via override (0016): ficam de fora de
	// CamposEditaveis. São os campos RICOS de paridade com o externo (tagline,
	// keywords) — display, não edição. A composição os trata como qualquer campo;
	// a etiqueta só governa a superfície de EDIÇÃO.
	soLeitura bool
}

// registroOrigem é o METADADO de origem por campo (ADR-0001): o motor itera este
// mapa para compor. Só campos EDITORIAIS entram aqui. Ficam de FORA (não passam
// pela composição de camadas): id/type/created_at/updated_at (sistema), status
// (intenção do dono), owned/file_count (derivados), poster_url (link de API) e
// external_ids (identidade autoritativa, MD4 — segue do núcleo). Trocar este mapa
// (dado) adiciona/muda campos sem tocar o motor.
var registroOrigem = map[string]origemCampo{
	"title":          {base: baseExterno, kind: kindEscalar, traduzivel: true},
	"original_title": {base: baseExterno, kind: kindEscalar},
	"description":    {base: baseExterno, kind: kindEscalar, traduzivel: true},
	"release_date":   {base: baseExterno, kind: kindEscalar},
	"tags":           {base: baseExterno, kind: kindLista, traduzivel: true},
	"extension_data": {base: baseExterno, kind: kindExtensao},
	// Campos RICOS traduzíveis, READ-ONLY (paridade com o externo, 0022/0023): o
	// motor compõe por idioma (como title/tags), mas ficam FORA da edição por
	// override — soLeitura os exclui de CamposEditaveis. Os neutros ricos (runtime,
	// vote_average, backdrop_url, certification, videos, collection, similar) não
	// entram no registro: são extraídos direto do externo em Resolver (via richfields).
	"tagline":  {base: baseExterno, kind: kindEscalar, traduzivel: true, soLeitura: true},
	"keywords": {base: baseExterno, kind: kindLista, traduzivel: true, soLeitura: true},
	// Situação de exibição da SÉRIE (0045): escalar NEUTRO (verbatim, não traduzível) e
	// READ-ONLY. Base externa (vem do canônico do provider); soLeitura o exclui de
	// CamposEditaveis (display, não edição), como tagline/keywords.
	"airing_status": {base: baseExterno, kind: kindEscalar, soLeitura: true},
	// Nota LOCAL do dono (ADR-0012): número NEUTRO, base NENHUMA (só o override fala —
	// arquivo e externo não a têm; a nota da fonte é o vote_average, campo distinto).
	// EDITÁVEL (não soLeitura): entra em CamposEditaveis para o editor de override.
	"local_rating": {base: baseNenhuma, kind: kindNumero},
	// Créditos (elenco/equipe/produtoras, briefing 0029 Fase C): base externa
	// (mesclada por prioridade, igual aos campos ricos), NEUTRO (não traduzível —
	// character/job/department não compõem por idioma nesta fase, decisão travada
	// no §6.5) e EDITÁVEL (o dono reordena/remove/edita/adiciona créditos sem
	// destruir a base do provider).
	"credits": {base: baseExterno, kind: kindCreditos},
}

// ── Superfície pública do registro (R1, briefing 0016) ───────────────────────
//
// O editor de override (0016) precisa saber quais campos são EDITÁVEIS e quais
// compõem POR IDIOMA para montar/validar o patch — SEM redigitar a lista (que
// viraria uma segunda fonte de verdade). Estes helpers só LEEM o registroOrigem;
// não mudam a composição nem a assinatura de Resolver.

// Kind é a CLASSE de composição de um campo editorial, exposta para o editor de
// override validar a FORMA do patch (escalar aceita string|null; lista aceita
// array|{add,remove}|null; extensão aceita objeto|null).
type Kind string

const (
	// KindEscalar é um campo de valor único (title/original_title/description/
	// release_date).
	KindEscalar Kind = "escalar"
	// KindLista é um campo-lista por operações (tags).
	KindLista Kind = "lista"
	// KindExtensao é o mapa de verticais por namespace/chave (extension_data).
	KindExtensao Kind = "extensao"
	// KindNumero é um campo escalar NUMÉRICO (nota local): aceita número|null.
	KindNumero Kind = "numero"
	// KindCreditos é o crédito (elenco/equipe/produtoras): aceita `null` (apaga
	// tudo) ou um objeto `{cast?,crew?,companies?}` com as operações
	// reorder/remove/edit/add por sub-kind (briefing 0029 Fase C).
	KindCreditos Kind = "creditos"
)

// CampoInfo é a etiqueta PÚBLICA de um campo editorial, derivada do registroOrigem:
// se compõe por idioma (Traduzivel) e sua classe de composição (Kind).
type CampoInfo struct {
	Traduzivel bool
	Kind       Kind
}

// kindPublico traduz a etiqueta interna de kind na pública.
func kindPublico(k kindCampo) Kind {
	switch k {
	case kindLista:
		return KindLista
	case kindExtensao:
		return KindExtensao
	case kindNumero:
		return KindNumero
	case kindCreditos:
		return KindCreditos
	default:
		return KindEscalar
	}
}

// CamposEditaveis devolve o mapa campo→CampoInfo dos campos EDITORIAIS (os que
// passam pela composição de camadas), derivado do registroOrigem. Uma cópia nova a
// cada chamada — o registro interno permanece imutável de fora. É a fonte única que
// o server consulta para saber quais chaves de override são válidas (R1/ADR-0001).
func CamposEditaveis() map[string]CampoInfo {
	out := make(map[string]CampoInfo, len(registroOrigem))
	for campo, orig := range registroOrigem {
		if orig.soLeitura {
			continue // campo rico read-only: resolve, mas não é editável (0016).
		}
		out[campo] = CampoInfo{Traduzivel: orig.traduzivel, Kind: kindPublico(orig.kind)}
	}
	return out
}

// MapaIdiomaDoOverride normaliza o override CRU de um campo TRADUZÍVEL num mapa
// idioma→valor pronto para o MERGE DE 2 NÍVEIS do editor (0016). Se o valor já está
// no formato POR IDIOMA, devolve uma cópia; se está no formato LEGADO (0015, sem
// chave de idioma), embrulha-o sob `principal` (mesma tolerância da leitura). Usa a
// MESMA classificação que a composição (parseIdiomaMap), preservando a fonte única.
// Para um campo não traduzível ou inexistente devolve nil.
func MapaIdiomaDoOverride(campo string, raw json.RawMessage, principal string) map[string]json.RawMessage {
	orig, ok := registroOrigem[campo]
	if !ok || !orig.traduzivel {
		return nil
	}
	m, ehMap := parseIdiomaMap(raw, true, orig.kind)
	out := make(map[string]json.RawMessage, len(m)+1)
	if ehMap {
		for idioma, v := range m {
			out[idioma] = v
		}
		return out
	}
	out[principal] = raw
	return out
}

// Resolver compõe a Entidade resolvida a partir das camadas. É o PONTO ÚNICO onde
// a precedência `override ⟵ externo ⟵ arquivo` é aplicada. O motor monta a base
// ARQUIVO como valores canônicos, itera o registro compondo campo a campo, e
// assembla o shape final. Campos fora do registro vêm diretos do núcleo.
func Resolver(c Camadas) Entidade {
	// Base ARQUIVO em valores canônicos (mesmos nomes/tipos que a composição usa).
	baseArquivoVals := map[string]any{
		"title":          strPtr(c.Entity.Title),
		"original_title": nsPtr(c.Entity.OriginalTitle),
		"description":    nsPtr(c.Entity.Description),
		"release_date":   nsPtr(c.Entity.ReleaseDate),
		"tags":           parseTags(c.Entity.TagsJson),
		"extension_data": parseExtensionData(c.Entity.ExtensionDataJson),
		// Nota local não tem base de arquivo (só o override a define): a base é nil.
		"local_rating": (*float64)(nil),
		// Créditos não têm base de arquivo (só externo+override falam) — igual ao
		// padrão do extension_data, cuja base-arquivo é sempre um mapa vazio.
		"credits": map[string]any{},
	}

	// Modo IDIOMA: só quando há cadeia derivada das settings. Sem cadeia, o motor
	// preserva integralmente o comportamento do 0013 (composição sem idioma).
	langMode := len(c.Cadeia) > 0
	principal := c.IdiomaPrincipal
	if principal == "" && langMode {
		principal = c.Cadeia[0]
	}

	// Composição genérica: itera o registro, despacha por kind, compõe. Para campos
	// TRADUZÍVEIS em modo idioma, compõe na dimensão idioma × camada e registra a
	// proveniência de idioma; os demais seguem a composição neutra.
	resolvido := make(map[string]any, len(registroOrigem))
	resolvedLangs := map[string]string{}
	for campo, orig := range registroOrigem {
		if langMode && orig.traduzivel {
			val, prov := comporTraduzivel(campo, orig, baseArquivoVals[campo], c.Cadeia, principal, c.Externo, c.Override)
			resolvido[campo] = val
			resolvedLangs[campo] = prov
			continue
		}
		resolvido[campo] = comporCampo(campo, orig, baseArquivoVals[campo], c.Externo, c.Override)
	}

	// Campos RICOS neutros/estruturados: NÃO passam pela composição de camadas (não
	// têm base arquivo nem override) — vêm direto do canônico externo, na prioridade
	// R2 (primeira fonte com valor). Reusa richfields (compartilhado com o /external),
	// garantindo o MESMO shape do detalhe do externo.
	rico := camposExternosMesclados(c.Externo)
	externalIDs := parseExternalIDs(c.Entity.ExternalIdsJson)

	// Créditos (0029 Fase C): a ENTRADA de richfields.CastDe/CrewDe/CompaniesDe deixa
	// de ser o `rico` cru e passa a ser o resultado da COMPOSIÇÃO (resolvido["credits"],
	// já com override aplicado) — os extratores em si não mudam (reaproveita 100% do
	// parsing existente).
	creditos := asObjMap(resolvido["credits"])

	// Backdrop: prefere o FUNDO cacheado localmente (rota do chamador); sem cache,
	// cai na URL direta da fonte (canônico externo). Mesmo campo, origem diferente.
	backdrop := c.BackdropURL
	if backdrop == nil {
		backdrop = richfields.StrPtrDe(rico, "backdrop_url")
	}

	return Entidade{
		ID:                c.Entity.ID,
		Type:              c.Entity.Type,
		Title:             derefStr(resolvido["title"]),
		OriginalTitle:     asStrPtr(resolvido["original_title"]),
		Description:       asStrPtr(resolvido["description"]),
		ReleaseDate:       asStrPtr(resolvido["release_date"]),
		Status:            c.Entity.Status,
		Tags:              asStrSlice(resolvido["tags"]),
		ExternalIDs:       externalIDs,
		Owned:             temPosse(c.Files),
		FileCount:         len(c.Files),
		PosterURL:         c.PosterURL,
		ExtensionData:     asObjMap(resolvido["extension_data"]),
		Tagline:           asStrPtr(resolvido["tagline"]),
		Keywords:          asStrSlice(resolvido["keywords"]),
		AiringStatus:      asStrPtr(resolvido["airing_status"]),
		Runtime:           richfields.IntPtrDe(rico, "runtime"),
		Certification:     richfields.CertificacaoDaRegiao(rico, c.LangPedido),
		VoteAverage:       richfields.FloatPtrDe(rico, "vote_average"),
		LocalRating:       asFloatPtr(resolvido["local_rating"]),
		BackdropURL:       backdrop,
		Videos:            richfields.VideosDe(rico),
		Collection:        richfields.ColecaoDe(rico),
		Similar:           richfields.SimilaresDe(rico),
		Cast:              richfields.CastDe(creditos),
		Crew:              richfields.CrewDe(creditos),
		Companies:         richfields.CompaniesDe(creditos),
		ExternalLinks:     richfields.LinksExternos(rico, c.Entity.Type, externalIDs["tmdb"]),
		Lang:              c.LangPedido,
		OriginalLanguage:  nsPtr(c.Entity.OriginalLanguage),
		ResolvedLanguages: resolvedLangs,
	}
}

// camposExternosMesclados achata as camadas externas numa visão canônica única
// para os campos ricos NEUTROS: para cada chave, o valor da fonte de MAIOR
// prioridade que a tem (índice 0 = mais alta, R2). Itera do fim para o começo para
// a de índice 0 prevalecer. Só valores não-nil entram.
func camposExternosMesclados(externo []CamadaExterna) map[string]any {
	out := map[string]any{}
	for i := len(externo) - 1; i >= 0; i-- {
		for k, v := range externo[i].Campos {
			if v != nil {
				out[k] = v
			}
		}
	}
	return out
}

// comporCampo despacha a composição de um campo pela sua CLASSE (kind) — nunca
// pelo nome. É aqui que o motor permanece genérico: novos campos só precisam de
// uma entrada no registro com um dos kinds já suportados.
func comporCampo(campo string, orig origemCampo, arquivo any, externo []CamadaExterna, override map[string]json.RawMessage) any {
	switch orig.kind {
	case kindLista:
		return comporLista(campo, orig, arquivo, externo, override)
	case kindExtensao:
		return comporExtensao(campo, arquivo, externo, override)
	case kindNumero:
		return comporNumero(campo, orig, arquivo, externo, override)
	case kindCreditos:
		return comporCreditos(campo, externo, override)
	default: // kindEscalar
		return comporEscalar(campo, orig, arquivo, externo, override)
	}
}

// comporNumero aplica a precedência a um campo escalar NUMÉRICO (nota local): o
// override vence (chave presente, mesmo `null` → apaga com nil); senão, se a base é
// externa, o primeiro valor numérico das fontes; senão a base arquivo. Devolve
// sempre um *float64. Espelha comporEscalar, trocando string por número.
func comporNumero(campo string, orig origemCampo, arquivo any, externo []CamadaExterna, override map[string]json.RawMessage) *float64 {
	if raw, ok := override[campo]; ok {
		// Presença da chave decide o override; `null` → nil (apaga); número → &n;
		// qualquer outra forma degrada a nil.
		var v *float64
		_ = json.Unmarshal(raw, &v)
		return v
	}
	if orig.base == baseExterno {
		for _, cam := range externo {
			if v := richfields.FloatPtrDe(cam.Campos, campo); v != nil {
				return v
			}
		}
	}
	return asFloatPtr(arquivo)
}

// ── Créditos (elenco/equipe/produtoras, briefing 0029 Fase C) ───────────────

// creditosKinds são os 3 sub-kinds de crédito, na ordem em que a forma canônica do
// patch os declara (briefing 0029 §6.5 / openapi.yaml OverridePatch).
var creditosKinds = [...]string{"cast", "crew", "companies"}

// comporCreditos compõe o campo `credits`: a BASE é sempre a camada externa
// mesclada por prioridade (as mesmas 3 chaves que os campos ricos read-only da
// Fase B já usavam), SEM base-arquivo. Sem override, devolve a base tal qual
// (chaves ausentes quando a fonte não trouxe). `override["credits"] == null`
// apaga TUDO (mapa vazio). Um patch presente decodifica por sub-kind
// (cast/crew/companies) e aplica remove→edit→add→reorder — sub-chave ausente no
// patch herda a base daquele sub-kind intacta. Devolve sempre um mapa não-nil.
func comporCreditos(campo string, externo []CamadaExterna, override map[string]json.RawMessage) map[string]any {
	rico := camposExternosMesclados(externo)
	base := map[string]any{}
	for _, kind := range creditosKinds {
		if v, ok := rico[kind]; ok {
			base[kind] = v
		}
	}

	raw, temOverride := override[campo]
	if !temOverride {
		return base
	}
	if ehNull(raw) {
		return map[string]any{}
	}

	var patch map[string]json.RawMessage
	if json.Unmarshal(raw, &patch) != nil {
		// Forma inválida (já barrada na escrita pelo server): degrada para a base
		// intacta em vez de propagar um erro de um resolver que é puro.
		return base
	}

	out := make(map[string]any, len(base))
	for k, v := range base {
		out[k] = v
	}
	for _, kind := range creditosKinds {
		rawKind, ok := patch[kind]
		if !ok {
			continue // sub-chave ausente: herda a base daquele sub-kind intacta.
		}
		if ehNull(rawKind) {
			out[kind] = []any{}
			continue
		}
		itens := clonarListaCreditos(base[kind])
		itens = aplicarPatchCredito(kind, itens, rawKind)
		out[kind] = creditosParaAny(itens)
	}
	return out
}

// patchCredito é a forma canônica de UM sub-bloco do patch de créditos (briefing
// 0029 §6.5): as 4 operações, todas opcionais. Edit é chave→patch de campos
// (map[string]any tolera qualquer valor; a validação de forma já rodou no
// server).
type patchCredito struct {
	Reorder []string                  `json:"reorder"`
	Remove  []string                  `json:"remove"`
	Edit    map[string]map[string]any `json:"edit"`
	Add     []map[string]any          `json:"add"`
}

// aplicarPatchCredito aplica as 4 operações de UM sub-kind NA ORDEM travada:
// remove → edit → add → reorder. Para `cast`, reescreve o campo `order` de cada
// item ao final (índice 0-based), mantendo richfields.Cast.Order coerente com a
// lista resultante (crew/companies não têm campo `order`).
func aplicarPatchCredito(kind string, itens []map[string]any, raw json.RawMessage) []map[string]any {
	var ops patchCredito
	if json.Unmarshal(raw, &ops) != nil {
		return itens // forma inválida: degrada para os itens intactos.
	}

	if len(ops.Remove) > 0 {
		removerSet := make(map[string]struct{}, len(ops.Remove))
		for _, k := range ops.Remove {
			removerSet[k] = struct{}{}
		}
		filtrados := make([]map[string]any, 0, len(itens))
		for _, it := range itens {
			if _, off := removerSet[chaveCredito(kind, it)]; !off {
				filtrados = append(filtrados, it)
			}
		}
		itens = filtrados
	}

	// Chaves ANTES do edit: para crew, a chave inclui `job` — um campo que o
	// próprio `edit` pode mudar. Se o `reorder` (montado pelo front com a chave
	// ANTIGA) casasse com a chave PÓS-edit, o item editado "sumiria" do reorder e
	// seria empurrado ao fim da lista só por ter sido editado. Fixar as chaves
	// aqui, antes da mutação, mantém `edit` e `reorder` na MESMA chave.
	chaves := make([]string, len(itens))
	for i, it := range itens {
		chaves[i] = chaveCredito(kind, it)
	}

	for i, it := range itens {
		if patchCampos, ok := ops.Edit[chaves[i]]; ok {
			for k, v := range patchCampos {
				it[k] = v
			}
		}
	}

	for _, novo := range ops.Add {
		itens = append(itens, novo)
		chaves = append(chaves, chaveCredito(kind, novo))
	}

	if len(ops.Reorder) > 0 {
		itens = reordenarCreditos(itens, chaves, ops.Reorder)
	}

	if kind == "cast" {
		for i := range itens {
			itens[i]["order"] = i
		}
	}

	return itens
}

// reordenarCreditos move os itens cujas chaves (pré-edit, `chaves[i]` casa com
// `itens[i]`) aparecem em `reorder` para a posição correspondente, NA ORDEM
// dada; os itens restantes (não mencionados) vêm depois, na ordem relativa
// original. Chaves em `reorder` que não existem (mais) são ignoradas
// silenciosamente.
func reordenarCreditos(itens []map[string]any, chaves []string, reorder []string) []map[string]any {
	idx := make(map[string]int, len(itens))
	for i, k := range chaves {
		idx[k] = i
	}
	usado := make(map[string]bool, len(itens))
	out := make([]map[string]any, 0, len(itens))
	for _, k := range reorder {
		if i, ok := idx[k]; ok && !usado[k] {
			out = append(out, itens[i])
			usado[k] = true
		}
	}
	for i, it := range itens {
		k := chaves[i]
		if !usado[k] {
			out = append(out, it)
			usado[k] = true
		}
	}
	return out
}

// chaveCredito é a CHAVE do crédito para reorder/remove/edit (briefing 0029
// §6.5): `source_id` para cast/companies; `source_id + "|" + job` para crew — a
// MESMA chave que equipeSimples/equipeAgregada (tmdb_creditos.go) já usam para
// dedup, porque um membro de equipe pode ter dois cargos com o mesmo source_id.
// `job` ausente (ex.: crédito custom incompleto) trata como "".
func chaveCredito(kind string, item map[string]any) string {
	sourceID, _ := item["source_id"].(string)
	if kind != "crew" {
		return sourceID
	}
	job, _ := item["job"].(string)
	return sourceID + "|" + job
}

// clonarListaCreditos converte a base de um sub-kind (uma []any de
// map[string]any, o mesmo shape que os itens de rico["cast"] etc já têm) numa
// []map[string]any de CÓPIAS RASAS — para o patch (edit/reorder de `order`) não
// mutar os mapas da camada externa original.
func clonarListaCreditos(v any) []map[string]any {
	itensAny, _ := v.([]any)
	out := make([]map[string]any, 0, len(itensAny))
	for _, it := range itensAny {
		if m, ok := it.(map[string]any); ok {
			out = append(out, clonarCredito(m))
		}
	}
	return out
}

// clonarCredito faz uma cópia rasa de um item de crédito.
func clonarCredito(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// creditosParaAny converte a []map[string]any de volta para []any — o shape que
// richfields.CastDe/CrewDe/CompaniesDe (via comoListaAny) esperam.
func creditosParaAny(itens []map[string]any) []any {
	out := make([]any, len(itens))
	for i, it := range itens {
		out[i] = it
	}
	return out
}

// ── Composição por idioma (traduzíveis) ─────────────────────────────────────

// comporTraduzivel compõe um campo TRADUZÍVEL na dimensão idioma × camada
// (ADR-0019). Percorre a `cadeia` (`pedido → fallback → original → …`); para cada
// idioma tenta `override[campo][idioma]` (presença decide; `null` apaga E
// INTERROMPE o fallback — R6 por idioma) e senão `externo[campo][idioma]` (primeira
// fonte com valor); a PRIMEIRA etapa com presença define o valor e a PROVENIÊNCIA
// (idioma efetivo). Nenhum idioma da cadeia com valor cai na base ARQUIVO (neutra),
// proveniência `""`. Devolve (valor, idioma efetivo).
func comporTraduzivel(campo string, orig origemCampo, arquivo any, cadeia []string, principal string, externo []CamadaExterna, override map[string]json.RawMessage) (any, string) {
	if orig.kind == kindLista {
		return comporListaIdioma(campo, arquivo, cadeia, principal, externo, override)
	}
	return comporEscalarIdioma(campo, arquivo, cadeia, principal, externo, override)
}

// comporEscalarIdioma resolve title/description por idioma. Ver comporTraduzivel.
func comporEscalarIdioma(campo string, arquivo any, cadeia []string, principal string, externo []CamadaExterna, override map[string]json.RawMessage) (*string, string) {
	rawOv, temOv := override[campo]
	idiomaMap, ehMap := parseIdiomaMap(rawOv, temOv, kindEscalar)
	for _, idioma := range cadeia {
		// OVERRIDE do idioma: presença decide (null apaga e interrompe).
		if temOv {
			if ehMap {
				if r, ok := idiomaMap[idioma]; ok {
					return escalarDeRaw(r), idioma
				}
			} else if idioma == principal {
				// Legado (0015): override sem chave de idioma vale sob o principal.
				return escalarDeRaw(rawOv), idioma
			}
		}
		// EXTERNO do idioma: primeira fonte com valor.
		if v, ok := externoEscalarIdioma(externo, campo, idioma, principal); ok {
			return v, idioma
		}
	}
	// Base ARQUIVO (neutra): último recurso da cadeia.
	return asStrPtr(arquivo), ""
}

// comporListaIdioma resolve tags por idioma: dentro do idioma escolhido, o override
// `{add,remove}`/array/null faz o MERGE sobre a base externa daquele idioma (ou a
// base arquivo quando a fonte não tem valor para ele) — R3 por idioma.
func comporListaIdioma(campo string, arquivo any, cadeia []string, principal string, externo []CamadaExterna, override map[string]json.RawMessage) ([]string, string) {
	arquivoTags := asStrSlice(arquivo)
	rawOv, temOv := override[campo]
	idiomaMap, ehMap := parseIdiomaMap(rawOv, temOv, kindLista)
	for _, idioma := range cadeia {
		base := arquivoTags
		extList, extOk := externoListaIdioma(externo, campo, idioma, principal)
		if extOk {
			base = extList
		}
		if temOv {
			if ehMap {
				if r, ok := idiomaMap[idioma]; ok {
					return aplicarOverrideLista(base, r), idioma
				}
			} else if idioma == principal {
				return aplicarOverrideLista(base, rawOv), idioma
			}
		}
		if extOk {
			return dedup(base), idioma
		}
	}
	return dedup(arquivoTags), ""
}

// parseIdiomaMap decide se o override CRU de um campo traduzível está no formato
// POR IDIOMA (`{"pt-BR": …, "en": …}`) ou no formato LEGADO (escalar/array/objeto de
// operações do 0015, sem chave de idioma). Só um OBJETO JSON pode ser mapa por
// idioma; para listas, um objeto de operações `{add,remove}` é legado, não idioma.
// Devolve (mapa idioma→raw, ehMap). Quando ehMap=false, o chamador trata o override
// inteiro como valor do idioma principal.
func parseIdiomaMap(raw json.RawMessage, temOv bool, kind kindCampo) (map[string]json.RawMessage, bool) {
	if !temOv {
		return nil, false
	}
	t := bytes.TrimSpace(raw)
	if len(t) == 0 || t[0] != '{' {
		return nil, false // escalar, array ou null → legado
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil, false
	}
	if kind == kindLista && apenasChavesDeOperacao(m) {
		return nil, false // {add,remove} → operação legada, não mapa por idioma
	}
	return m, true
}

// apenasChavesDeOperacao diz se todas as chaves do objeto são de OPERAÇÃO de lista
// (`add`/`remove`) — o que caracteriza o override-lista legado (não um mapa por
// idioma). Objeto vazio conta como operação (merge vazio sobre a base).
func apenasChavesDeOperacao(m map[string]json.RawMessage) bool {
	for k := range m {
		if k != "add" && k != "remove" {
			return false
		}
	}
	return true
}

// escalarDeRaw decodifica um valor escalar de override cru num *string: `null` → nil
// (apaga), `"s"` → &s, outra forma degrada a nil. Espelha comporEscalar.
func escalarDeRaw(raw json.RawMessage) *string {
	var v *string
	_ = json.Unmarshal(raw, &v)
	return v
}

// externoEscalarIdioma busca, na ordem de prioridade das fontes, o primeiro valor
// escalar do campo para o idioma pedido. O externo do campo pode vir como
// `map[idioma]valor` (formato do 0020) OU como escalar simples (formato LEGADO do
// 0015) — o legado só casa quando o idioma pedido é o principal (ADR-0018).
func externoEscalarIdioma(externo []CamadaExterna, campo, idioma, principal string) (*string, bool) {
	for _, cam := range externo {
		v, ok := cam.Campos[campo]
		if !ok || v == nil {
			continue
		}
		if m, isMap := v.(map[string]any); isMap {
			if val, has := m[idioma]; has {
				if s, ok := val.(string); ok {
					return &s, true
				}
			}
			continue
		}
		if idioma == principal {
			if s, ok := v.(string); ok {
				return &s, true
			}
		}
	}
	return nil, false
}

// externoListaIdioma busca a lista do campo para o idioma pedido, com a mesma
// tolerância ao legado de externoEscalarIdioma (lista simples = principal).
func externoListaIdioma(externo []CamadaExterna, campo, idioma, principal string) ([]string, bool) {
	for _, cam := range externo {
		v, ok := cam.Campos[campo]
		if !ok || v == nil {
			continue
		}
		if m, isMap := v.(map[string]any); isMap {
			if val, has := m[idioma]; has && val != nil {
				return asStrSlice(val), true
			}
			continue
		}
		if idioma == principal {
			return asStrSlice(v), true
		}
	}
	return nil, false
}

// comporEscalar aplica a precedência a um campo escalar: override (chave presente,
// mesmo `null` → vence, apagando com nil) ⟶ senão externo (primeira fonte com
// valor, se a base for externo) ⟶ senão arquivo. Devolve sempre um *string.
func comporEscalar(campo string, orig origemCampo, arquivo any, externo []CamadaExterna, override map[string]json.RawMessage) *string {
	if raw, ok := override[campo]; ok {
		// Presença da chave decide o override; o valor decide o conteúdo:
		// `null` → nil (apaga); "s" → &s; qualquer outra forma degrada a nil.
		var v *string
		_ = json.Unmarshal(raw, &v)
		return v
	}
	if orig.base == baseExterno {
		if v, ok := primeiroExternoEscalar(externo, campo); ok {
			return v
		}
	}
	return asStrPtr(arquivo)
}

// primeiroExternoEscalar devolve o valor da PRIMEIRA fonte (na ordem de
// prioridade) cujo Campos[campo] existe e é uma string — a base externa do campo.
func primeiroExternoEscalar(externo []CamadaExterna, campo string) (*string, bool) {
	for _, cam := range externo {
		if v, ok := cam.Campos[campo]; ok && v != nil {
			if s, ok := v.(string); ok {
				return &s, true
			}
		}
	}
	return nil, false
}

// comporLista compõe um campo-lista por MERGE DE OPERAÇÕES (R3/ADR-0003). A base é
// a lista externa (primeira fonte com valor) ou a do arquivo; o override
// `{add, remove}` reescreve sobre ela. Ordem: base, remoções por valor, adições ao
// fim, dedup. Um override array puro é substituição total; `null` apaga a base.
func comporLista(campo string, orig origemCampo, arquivo any, externo []CamadaExterna, override map[string]json.RawMessage) []string {
	base := asStrSlice(arquivo)
	if orig.base == baseExterno {
		if v, ok := primeiroExternoLista(externo, campo); ok {
			base = v
		}
	}
	if raw, ok := override[campo]; ok {
		return aplicarOverrideLista(base, raw)
	}
	return dedup(base)
}

// primeiroExternoLista devolve a lista da PRIMEIRA fonte com valor para o campo.
func primeiroExternoLista(externo []CamadaExterna, campo string) ([]string, bool) {
	for _, cam := range externo {
		if v, ok := cam.Campos[campo]; ok && v != nil {
			return asStrSlice(v), true
		}
	}
	return nil, false
}

// aplicarOverrideLista interpreta o override de um campo-lista: `null` apaga a
// base; um array puro substitui a base; um objeto `{add, remove}` faz o merge de
// operações. Forma canônica = `{add, remove}` (ADR-0003); array é atalho de
// substituição total. Qualquer outra forma degrada para a base intacta.
func aplicarOverrideLista(base []string, raw json.RawMessage) []string {
	if ehNull(raw) {
		return []string{}
	}
	// Substituição total (array puro).
	var arr []string
	if err := json.Unmarshal(raw, &arr); err == nil {
		return dedup(arr)
	}
	// Merge por operações (forma canônica).
	var ops struct {
		Add    []string `json:"add"`
		Remove []string `json:"remove"`
	}
	if err := json.Unmarshal(raw, &ops); err == nil {
		return mesclarLista(base, ops.Add, ops.Remove)
	}
	return dedup(base)
}

// mesclarLista aplica as operações sobre a base: remove por valor, acrescenta as
// adições ao fim, e dedupa preservando a primeira ocorrência (ordem estável).
func mesclarLista(base, add, remove []string) []string {
	remover := make(map[string]struct{}, len(remove))
	for _, r := range remove {
		remover[r] = struct{}{}
	}
	out := make([]string, 0, len(base)+len(add))
	for _, v := range base {
		if _, off := remover[v]; !off {
			out = append(out, v)
		}
	}
	out = append(out, add...)
	return dedup(out)
}

// comporExtensao compõe extension_data por NAMESPACE e por CHAVE interna com a
// mesma precedência (R5/ADR-0005): arquivo é a base, cada fonte externa sobrepõe
// (a de maior prioridade vence), e o override reescreve por cima — com `null`
// apagando o namespace inteiro ou uma chave. Composição recursiva rasa
// (namespace → objeto de campos). Sempre devolve um mapa não-nil.
func comporExtensao(campo string, arquivo any, externo []CamadaExterna, override map[string]json.RawMessage) map[string]any {
	merged := clonarExt(asObjMap(arquivo))

	// Externo: index 0 é a prioridade mais ALTA. Aplica do fim para o começo para
	// a fonte de maior prioridade prevalecer por namespace/chave.
	for i := len(externo) - 1; i >= 0; i-- {
		mesclarExt(merged, asObjMap(externo[i].Campos[campo]))
	}

	if raw, ok := override[campo]; ok {
		aplicarOverrideExt(merged, raw)
	}
	return merged
}

// mesclarExt sobrepõe `src` em `dst` por namespace/chave: cada chave de src vence
// a de dst; namespaces novos entram inteiros.
func mesclarExt(dst, src map[string]any) {
	for ns, obj := range src {
		alvo := asObjMap(dst[ns])
		for k, v := range asObjMap(obj) {
			alvo[k] = v
		}
		dst[ns] = alvo
	}
}

// aplicarOverrideExt reescreve o extension_data composto com o patch do dono. Um
// `null` no topo apaga tudo; `null` num namespace apaga o namespace; `null` numa
// chave apaga a chave; qualquer outro valor sobrepõe. Chave/namespace ausente
// herda a base.
func aplicarOverrideExt(merged map[string]any, raw json.RawMessage) {
	if ehNull(raw) {
		for ns := range merged {
			delete(merged, ns)
		}
		return
	}
	var namespaces map[string]json.RawMessage
	if err := json.Unmarshal(raw, &namespaces); err != nil {
		return
	}
	for ns, rawNs := range namespaces {
		if ehNull(rawNs) {
			delete(merged, ns)
			continue
		}
		var chaves map[string]json.RawMessage
		if err := json.Unmarshal(rawNs, &chaves); err != nil {
			continue
		}
		alvo := asObjMap(merged[ns])
		for k, rawV := range chaves {
			if ehNull(rawV) {
				delete(alvo, k)
				continue
			}
			var val any
			if err := json.Unmarshal(rawV, &val); err == nil {
				alvo[k] = val
			}
		}
		merged[ns] = alvo
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// temPosse aplica a POSSE DERIVADA (ADR-0009) sobre os arquivos já carregados:
// verdadeiro se, e só se, existe ao menos um media_file com state='present'.
func temPosse(files []sqlcgen.MediaFile) bool {
	for _, f := range files {
		if f.State == "present" {
			return true
		}
	}
	return false
}

// parseTags decodifica tags_json (um array JSON de strings) numa fatia. Sempre
// devolve uma fatia não-nil (vazia quando ausente/inválido) para o JSON sair
// como `[]` em vez de `null`.
func parseTags(raw sql.NullString) []string {
	out := []string{}
	if !raw.Valid || raw.String == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw.String), &out)
	if out == nil {
		return []string{}
	}
	return out
}

// parseExtensionData decodifica extension_data_json (objeto JSON namespace→objeto)
// num mapa. Sempre devolve um mapa não-nil (vazio quando ausente/inválido) para o
// JSON sair como `{}` em vez de `null`.
func parseExtensionData(raw sql.NullString) map[string]any {
	out := map[string]any{}
	if !raw.Valid || raw.String == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw.String), &out)
	if out == nil {
		return map[string]any{}
	}
	return out
}

// parseExternalIDs decodifica external_ids_json (objeto JSON string->string).
// Valores não-string são ignorados. Sempre devolve um mapa não-nil.
func parseExternalIDs(raw sql.NullString) map[string]string {
	out := map[string]string{}
	if !raw.Valid || raw.String == "" {
		return out
	}
	bruto := map[string]any{}
	if err := json.Unmarshal([]byte(raw.String), &bruto); err != nil {
		return out
	}
	for k, v := range bruto {
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// nsPtr converte um sql.NullString em *string (nil quando NULL) para o campo
// sair como `null` no JSON.
func nsPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	v := ns.String
	return &v
}

// strPtr embrulha uma string num *string (a base arquivo dos escalares é sempre
// um ponteiro, para uniformizar a composição).
func strPtr(s string) *string { return &s }

// asStrPtr extrai um *string de um valor dinâmico da composição (nil quando o
// valor não é *string).
func asStrPtr(v any) *string {
	if p, ok := v.(*string); ok {
		return p
	}
	return nil
}

// asFloatPtr extrai um *float64 de um valor dinâmico da composição (nil quando o
// valor não é *float64) — usado pelo campo numérico (nota local).
func asFloatPtr(v any) *float64 {
	if p, ok := v.(*float64); ok {
		return p
	}
	return nil
}

// derefStr deref um *string dinâmico para string ("" quando nil) — usado no campo
// title, que é não-nulável no shape.
func derefStr(v any) string {
	if p, ok := v.(*string); ok && p != nil {
		return *p
	}
	return ""
}

// asStrSlice converte um valor dinâmico numa fatia de strings não-nil. Aceita
// []string e []any (com elementos string), ignorando o que não for string.
func asStrSlice(v any) []string {
	switch s := v.(type) {
	case []string:
		if s == nil {
			return []string{}
		}
		return s
	case []any:
		out := make([]string, 0, len(s))
		for _, e := range s {
			if str, ok := e.(string); ok {
				out = append(out, str)
			}
		}
		return out
	default:
		return []string{}
	}
}

// asObjMap converte um valor dinâmico num objeto (map[string]any) não-nil.
func asObjMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok && m != nil {
		return m
	}
	return map[string]any{}
}

// clonarExt faz uma cópia rasa-por-namespace do extension_data (nível namespace e
// nível chave), para a composição não mutar a base arquivo original.
func clonarExt(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for ns, obj := range src {
		copia := make(map[string]any)
		for k, v := range asObjMap(obj) {
			copia[k] = v
		}
		out[ns] = copia
	}
	return out
}

// dedup remove duplicatas preservando a primeira ocorrência; devolve fatia
// não-nil.
func dedup(in []string) []string {
	visto := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := visto[v]; ok {
			continue
		}
		visto[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// ehNull diz se o JSON cru é o literal `null` (após aparar espaços) — o sinal de
// APAGAR na presença-da-chave (R6).
func ehNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}
