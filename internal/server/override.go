package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// Este arquivo implementa o EDITOR DE OVERRIDE por idioma (briefing 0016): a
// superfície pela qual o dono ESCREVE o patch esparso que vence tudo.
//
//   PATCH  /entities/{id}/override?lang=<code>        — merge esparso de 2 níveis
//   DELETE /entities/{id}/override/{campo}?lang=<code> — limpar override (por idioma)
//   DELETE /entities/{id}/override                     — reset (apaga o override)
//   GET    /entities/{id}/override                     — devolve o blob CRU
//
// O endpoint NÃO inventa semântica: só mescla o patch no blob (preservando o
// RawMessage cru) e devolve a entity RESOLVIDA — o resolver (0019) interpreta os
// valores na LEITURA. Quais campos são editáveis/traduzíveis vem do resolver
// (resolver.CamposEditaveis — R1, fonte única). As escritas passam pelo pool de
// ESCRITA (ADR-0010).

// patchOverride trata PATCH /api/v1/entities/{id}/override?lang=<code>: funde as
// chaves do corpo no override_json. Traduzível → override[campo][lang] (deep-merge
// preservando os outros idiomas; migra escalar legado → {principal: legado});
// neutro → override[campo] escalar (lang ignorado). `null` no idioma é valor válido
// (definir vazio). 404 entity inexistente; 400 lang fora dos permitidos; 422 patch
// malformado (chave não-editorial ou forma inválida). 200 com a entity resolvida no
// lang.
func (s *Server) patchOverride(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}

	cfg, lang, ok := s.cfgELang(w, ctx, r.URL.Query().Get("lang"))
	if !ok {
		return
	}

	var patch map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body", "patch de override inválido")
		return
	}

	campos := resolver.CamposEditaveis()
	if !validarPatch(w, campos, patch) {
		return
	}

	blob, err := s.carregarBlobBruto(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}

	for campo, raw := range patch {
		novo, err := mesclarCampo(campos[campo], blob, campo, raw, lang, cfg.Primary)
		if err != nil {
			respondErroInterno(w)
			return
		}
		blob[campo] = novo
	}

	if err := s.gravarBlob(ctx, id, blob); err != nil {
		respondErroInterno(w)
		return
	}
	s.responderOverride(w, ctx, id, lang)
}

// deleteOverrideCampo trata DELETE /api/v1/entities/{id}/override/{campo}?lang=:
// LIMPAR override. Traduzível COM lang remove só override[campo][lang] (e a chave
// campo se o mapa esvaziar); traduzível SEM lang ou campo NEUTRO remove a chave
// campo inteira. Remover chave ausente é idempotente (200). 422 se {campo} não é
// editorial; 400 se lang presente fora dos permitidos; 404 entity inexistente. Se o
// blob esvaziar, a linha de override é removida.
func (s *Server) deleteOverrideCampo(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	campo := chi.URLParam(r, "campo")
	ctx := r.Context()

	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}

	info, editorial := resolver.CamposEditaveis()[campo]
	if !editorial {
		writeError(w, http.StatusUnprocessableEntity, "invalid_body",
			"campo nao editorial: "+campo)
		return
	}

	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return
	}
	// A PRESENÇA do ?lang= decide o escopo (por idioma × campo inteiro), então lemos
	// o parâmetro cru; se presente, validamos contra os permitidos (400 quando fora).
	langRaw := r.URL.Query().Get("lang")
	if !s.validarLangOpcional(w, cfg, langRaw) {
		return
	}

	blob, err := s.carregarBlobBruto(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}

	if err := removerCampo(info, blob, campo, langRaw, cfg.Primary); err != nil {
		respondErroInterno(w)
		return
	}

	if err := s.gravarBlob(ctx, id, blob); err != nil {
		respondErroInterno(w)
		return
	}

	s.responderOverride(w, ctx, id, ouPrincipal(langRaw, cfg.Primary))
}

// deleteOverride trata DELETE /api/v1/entities/{id}/override: RESET — apaga o
// override inteiro. 404 entity inexistente; 200 com a entity resolvida (na base).
func (s *Server) deleteOverride(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}

	_, lang, ok := s.cfgELang(w, ctx, r.URL.Query().Get("lang"))
	if !ok {
		return
	}

	if err := sqlcgen.New(s.db.Write).ApagarMediaOverride(ctx, id); err != nil {
		respondErroInterno(w)
		return
	}
	s.responderOverride(w, ctx, id, lang)
}

// getOverride trata GET /api/v1/entities/{id}/override: devolve o BLOB CRU guardado
// ({} se nenhum), para a UI distinguir "overridado" de "herdado" por campo e por
// idioma. 200 (entity existente); 404 se a entity nao existe.
func (s *Server) getOverride(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	if _, ok := s.exigirEntity(w, ctx, id); !ok {
		return
	}

	ov, err := s.leitura().ObterMediaOverride(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusOK, json.RawMessage("{}"))
			return
		}
		respondErroInterno(w)
		return
	}
	if !ov.OverrideJson.Valid || ov.OverrideJson.String == "" {
		writeJSON(w, http.StatusOK, json.RawMessage("{}"))
		return
	}
	// Blob CRU, tal como guardado — sem reinterpretar (o resolver interpreta na
	// leitura da entity).
	writeJSON(w, http.StatusOK, json.RawMessage(ov.OverrideJson.String))
}

// ── Helpers do editor de override ────────────────────────────────────────────

// exigirEntity carrega a entity (pool de leitura) exigindo que ela EXISTA: 404 no
// envelope padrão quando ausente, 500 em erro real de banco. Guarda comum dos
// handlers de override (a linha de override tem FK para entity — escrever para uma
// entity inexistente violaria a integridade).
func (s *Server) exigirEntity(w http.ResponseWriter, ctx context.Context, id string) (sqlcgen.Entity, bool) {
	ent, err := s.leitura().ObterEntity(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
			return sqlcgen.Entity{}, false
		}
		respondErroInterno(w)
		return sqlcgen.Entity{}, false
	}
	return ent, true
}

// cfgELang carrega as settings e resolve o ?lang= com a semântica default (ausente
// = principal; fora dos permitidos → 400). Já escreve o erro (500/400) e devolve
// ok=false quando falha. Guarda comum de PATCH/DELETE-reset.
func (s *Server) cfgELang(w http.ResponseWriter, ctx context.Context, langParam string) (languageSettings, string, bool) {
	cfg, err := s.obterLanguageSettings(ctx)
	if err != nil {
		respondErroInterno(w)
		return languageSettings{}, "", false
	}
	lang, ok := s.parseLang(w, cfg, langParam)
	if !ok {
		return languageSettings{}, "", false
	}
	return cfg, lang, true
}

// validarLangOpcional valida um ?lang= cuja PRESENÇA é significativa (DELETE de
// campo): ausente = ok (escopo campo inteiro); presente valida contra os permitidos
// (400 quando fora, já escrito).
func (s *Server) validarLangOpcional(w http.ResponseWriter, cfg languageSettings, langRaw string) bool {
	if langRaw == "" {
		return true
	}
	_, ok := s.parseLang(w, cfg, langRaw)
	return ok
}

// ouPrincipal devolve langRaw quando não vazio, senão o principal — o idioma em que
// a resposta resolvida é servida quando o DELETE não foi escopado por idioma.
func ouPrincipal(langRaw, principal string) string {
	if langRaw == "" {
		return principal
	}
	return langRaw
}

// carregarBlobBruto lê o override_json atual como o mapa CRU esparso (sempre
// não-nil, mutável) que o merge do editor manipula. Reusa carregarOverride
// (ausência/malformado → mapa vazio).
func (s *Server) carregarBlobBruto(ctx context.Context, id string) (map[string]json.RawMessage, error) {
	m, err := s.carregarOverride(ctx, id)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return map[string]json.RawMessage{}, nil
	}
	return m, nil
}

// gravarBlob persiste o blob de override pelo pool de ESCRITA. Blob vazio ({}) ⇒
// remove a linha (ApagarMediaOverride) em vez de guardar um patch inócuo — o
// resolver volta a herdar a base.
func (s *Server) gravarBlob(ctx context.Context, id string, blob map[string]json.RawMessage) error {
	esc := sqlcgen.New(s.db.Write)
	if len(blob) == 0 {
		return esc.ApagarMediaOverride(ctx, id)
	}
	b, err := json.Marshal(blob)
	if err != nil {
		return err
	}
	_, err = esc.UpsertMediaOverride(ctx, sqlcgen.UpsertMediaOverrideParams{
		EntityID:     id,
		OverrideJson: sql.NullString{String: string(b), Valid: true},
	})
	return err
}

// mesclarCampo devolve o novo valor CRU de um campo após o merge do patch: para um
// TRADUZÍVEL, funde `raw` em `blob[campo][lang]` (deep-merge de 2 níveis, migrando
// escalar legado → {principal:…}); para um NEUTRO, devolve `raw` direto (lang
// ignorado). Preserva os RawMessage crus — não reinterpreta os valores.
func mesclarCampo(info resolver.CampoInfo, blob map[string]json.RawMessage, campo string, raw json.RawMessage, lang, principal string) (json.RawMessage, error) {
	if !info.Traduzivel {
		return raw, nil
	}
	mapa := map[string]json.RawMessage{}
	if existente, ok := blob[campo]; ok {
		mapa = resolver.MapaIdiomaDoOverride(campo, existente, principal)
	}
	mapa[lang] = raw
	return json.Marshal(mapa)
}

// removerCampo aplica a remoção de LIMPAR sobre o blob (in-place): traduzível COM
// lang remove só `blob[campo][langRaw]` (e a chave campo se o mapa esvaziar);
// traduzível SEM lang ou campo NEUTRO remove a chave campo inteira. Remover o que
// não existe é no-op (idempotente).
func removerCampo(info resolver.CampoInfo, blob map[string]json.RawMessage, campo, langRaw, principal string) error {
	if !info.Traduzivel || langRaw == "" {
		delete(blob, campo)
		return nil
	}
	existente, ok := blob[campo]
	if !ok {
		return nil
	}
	mapa := resolver.MapaIdiomaDoOverride(campo, existente, principal)
	delete(mapa, langRaw)
	if len(mapa) == 0 {
		delete(blob, campo)
		return nil
	}
	reser, err := json.Marshal(mapa)
	if err != nil {
		return err
	}
	blob[campo] = reser
	return nil
}

// responderOverride resolve a entity no lang informado e responde 200 (a UI
// reflete o valor resolvido na hora). 404 se a entity sumiu no meio; 500 em erro
// de banco.
func (s *Server) responderOverride(w http.ResponseWriter, ctx context.Context, id, lang string) {
	resolvida, encontrada, err := s.resolverEntity(ctx, id, lang, true)
	if err != nil {
		respondErroInterno(w)
		return
	}
	if !encontrada {
		writeError(w, http.StatusNotFound, "not_found", "entidade não encontrada")
		return
	}
	writeJSON(w, http.StatusOK, resolvida)
}

// validarPatch rejeita (422) qualquer chave não-editorial ou com forma inválida
// para o kind do campo. Devolve false já tendo escrito o erro.
func validarPatch(w http.ResponseWriter, campos map[string]resolver.CampoInfo, patch map[string]json.RawMessage) bool {
	for campo, raw := range patch {
		info, editorial := campos[campo]
		if !editorial {
			writeError(w, http.StatusUnprocessableEntity, "invalid_body",
				"campo nao editorial: "+campo)
			return false
		}
		if !formaValida(info, raw) {
			writeError(w, http.StatusUnprocessableEntity, "invalid_body",
				"forma invalida para o campo "+campo)
			return false
		}
	}
	return true
}

// formaValida valida a FORMA do valor de patch pela classe do campo: escalar
// aceita string|null; lista aceita array de strings, {add?,remove?} ou null;
// extensão aceita objeto ou null.
func formaValida(info resolver.CampoInfo, raw json.RawMessage) bool {
	switch info.Kind {
	case resolver.KindLista:
		return formaLista(raw)
	case resolver.KindExtensao:
		return ehNullRaw(raw) || ehObjeto(raw)
	case resolver.KindNumero:
		return formaNumero(raw)
	case resolver.KindCreditos:
		return formaCreditos(raw)
	default: // KindEscalar
		return ehNullRaw(raw) || ehString(raw)
	}
}

// formaCreditos valida a forma do patch de créditos (briefing 0029 §6.5): `null`
// (apaga tudo) ou um objeto cujas chaves ⊆ {cast,crew,companies}; cada sub-bloco
// presente é `null` ou um objeto cujas chaves ⊆ {reorder,remove,edit,add}, com
// `reorder`/`remove` como array de string, `edit` como objeto raso
// string→(string→string), e `add` como array de objeto exigindo `source_id`+
// `name` string (demais chaves, se presentes, também string). Qualquer forma
// fora disso é 422.
func formaCreditos(raw json.RawMessage) bool {
	if ehNullRaw(raw) {
		return true
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return false
	}
	for kind, rawBloco := range top {
		if kind != "cast" && kind != "crew" && kind != "companies" {
			return false
		}
		if !formaBlocoCredito(rawBloco) {
			return false
		}
	}
	return true
}

// formaBlocoCredito valida UM sub-bloco (cast/crew/companies) do patch de
// créditos: `null` ou objeto com chaves ⊆ {reorder,remove,edit,add}.
func formaBlocoCredito(raw json.RawMessage) bool {
	if ehNullRaw(raw) {
		return true
	}
	var ops map[string]json.RawMessage
	if json.Unmarshal(raw, &ops) != nil {
		return false
	}
	for op, rawOp := range ops {
		switch op {
		case "reorder", "remove":
			var arr []string
			if json.Unmarshal(rawOp, &arr) != nil {
				return false
			}
		case "edit":
			if !formaEditCredito(rawOp) {
				return false
			}
		case "add":
			if !formaAddCredito(rawOp) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// formaEditCredito valida `edit`: objeto RASO chave→(campo→valor), com os
// valores dos campos sempre string (character/job/department/name são NEUTROS,
// sem sub-mapa por idioma nesta fase).
func formaEditCredito(raw json.RawMessage) bool {
	var m map[string]map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	for _, campos := range m {
		for _, v := range campos {
			var s string
			if json.Unmarshal(v, &s) != nil {
				return false
			}
		}
	}
	return true
}

// formaAddCredito valida `add`: array de objeto exigindo `source_id`+`name`
// como string (escolhidos pelo DONO — o resolver é puro, não gera id); demais
// chaves, se presentes, também devem ser string.
func formaAddCredito(raw json.RawMessage) bool {
	var arr []map[string]json.RawMessage
	if json.Unmarshal(raw, &arr) != nil {
		return false
	}
	for _, item := range arr {
		if !presenteComoString(item, "source_id") || !presenteComoString(item, "name") {
			return false
		}
		for _, v := range item {
			var s string
			if json.Unmarshal(v, &s) != nil {
				return false
			}
		}
	}
	return true
}

// presenteComoString diz se `chave` está presente no objeto e é uma string.
func presenteComoString(item map[string]json.RawMessage, chave string) bool {
	raw, ok := item[chave]
	if !ok {
		return false
	}
	var s string
	return json.Unmarshal(raw, &s) == nil
}

// formaNumero aceita `null` (definir/limpar a nota) ou um número na escala 0–10 (a
// nota local casa com o vote_average da fonte). Fora da faixa ou não-número: inválido.
func formaNumero(raw json.RawMessage) bool {
	if ehNullRaw(raw) {
		return true
	}
	var n float64
	if json.Unmarshal(raw, &n) != nil {
		return false
	}
	return n >= 0 && n <= 10
}

// formaLista aceita `null`, um array de strings (substituição), ou um objeto de
// operações `{add?,remove?}` cujos valores são arrays de strings.
func formaLista(raw json.RawMessage) bool {
	if ehNullRaw(raw) {
		return true
	}
	var arr []string
	if json.Unmarshal(raw, &arr) == nil {
		return true
	}
	var ops map[string]json.RawMessage
	if json.Unmarshal(raw, &ops) != nil {
		return false
	}
	for k, v := range ops {
		if k != "add" && k != "remove" {
			return false
		}
		var a []string
		if json.Unmarshal(v, &a) != nil {
			return false
		}
	}
	return true
}

// ehNullRaw diz se o JSON cru é o literal `null` (valor válido = definir vazio).
func ehNullRaw(raw json.RawMessage) bool {
	var v any
	return json.Unmarshal(raw, &v) == nil && v == nil
}

// ehString diz se o JSON cru é uma string.
func ehString(raw json.RawMessage) bool {
	var s string
	return json.Unmarshal(raw, &s) == nil
}

// ehObjeto diz se o JSON cru é um objeto (não array, não escalar, não null).
func ehObjeto(raw json.RawMessage) bool {
	var m map[string]json.RawMessage
	return json.Unmarshal(raw, &m) == nil && m != nil
}
