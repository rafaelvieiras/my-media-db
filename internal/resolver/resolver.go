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

	return Entidade{
		ID:                c.Entity.ID,
		Type:              c.Entity.Type,
		Title:             derefStr(resolvido["title"]),
		OriginalTitle:     asStrPtr(resolvido["original_title"]),
		Description:       asStrPtr(resolvido["description"]),
		ReleaseDate:       asStrPtr(resolvido["release_date"]),
		Status:            c.Entity.Status,
		Tags:              asStrSlice(resolvido["tags"]),
		ExternalIDs:       parseExternalIDs(c.Entity.ExternalIdsJson),
		Owned:             temPosse(c.Files),
		FileCount:         len(c.Files),
		PosterURL:         c.PosterURL,
		ExtensionData:     asObjMap(resolvido["extension_data"]),
		Lang:              c.LangPedido,
		OriginalLanguage:  nsPtr(c.Entity.OriginalLanguage),
		ResolvedLanguages: resolvedLangs,
	}
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
	default: // kindEscalar
		return comporEscalar(campo, orig, arquivo, externo, override)
	}
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
