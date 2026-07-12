package enrich

// serie.go é o ramo de import de SÉRIES do enrich (briefing 0031 / ADR-0021):
// materializa a ÁRVORE série→temporada→episódio a partir de um provider.SerieProvider.
// A série é um item comum (reusa importarItem para detalhe/pôster/curarTitulo) mais o
// namespace core.tv@1 (season types); as temporadas (só as da ordenação default) e os
// episódios (grão fino, ancorados no episode.id) nascem com o extension_data próprio e
// ligam-se por entity_relation com sort_index. É idempotente: re-importar reusa
// entities e relações (dedup de temporada por número, de episódio por id externo).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Erros sentinela de ImportarSeriePorNome: o scanner de series (0034) os distingue
// para rotear o arquivo para a fila de revisao SEM adivinhar (nunca importa a serie
// errada nem fabrica episodio).
var (
	// ErrSerieNaoEncontrada: nenhum provider retornou candidato para o nome. -> revisao.
	ErrSerieNaoEncontrada = errors.New("enrich: serie nao encontrada no provider")
	// ErrSerieAmbigua: havia candidatos, mas nenhum match de titulo EXATO e UNICO
	// (0 exatos com candidatos presentes, ou 2+ exatos). -> revisao.
	ErrSerieAmbigua = errors.New("enrich: serie ambigua (sem match de titulo exato e unico)")
)

const (
	// tipoSerie/tipoTemporada/tipoEpisodio são os entity.type da árvore (ADR-0021).
	tipoSerie     = "tv"
	tipoTemporada = "season"
	tipoEpisodio  = "episode"
	// relacaoTemporada/relacaoEpisodio são os entity_relation.kind da árvore.
	relacaoTemporada = "tv_season"
	relacaoEpisodio  = "tv_episode"
	// nsTV é o namespace do extension_data da vertical de séries (core.tv@1).
	nsTV = "core.tv@1"
)

// importarSerie importa a ÁRVORE de uma série. Requer a capacidade opcional
// SerieProvider (senão erro → 502, como a coleção). A série (type='tv') nasce/atualiza
// pelo molde de item (detalhe/pôster/curarTitulo) e recebe o namespace core.tv@1; em
// seguida cada temporada da ordenação default e cada episódio fino são importados. A
// falha de UMA temporada/episódio degrada (loga e segue) — a árvore parcial é válida.
func (s *Servico) importarSerie(ctx context.Context, source, sourceID string, langs []string) (string, bool, error) {
	prov, ok := s.provFonte(source)
	if !ok {
		return "", false, fmt.Errorf("enrich: fonte %q sem provider", source)
	}
	sp, ok := prov.(provider.SerieProvider)
	if !ok {
		return "", false, fmt.Errorf("enrich: fonte %q nao suporta serie", source)
	}
	arvore, err := sp.BuscarSerieArvore(ctx, sourceID, langs)
	if err != nil {
		return "", false, fmt.Errorf("enrich: buscar arvore da serie: %w", err)
	}

	serieID, criada, err := s.importarItem(ctx, source, sourceID, tipoSerie, langs)
	if err != nil {
		return serieID, criada, err
	}
	if err := s.escrita().AtualizarExtensionData(ctx, sqlcgen.AtualizarExtensionDataParams{
		ExtensionDataJson: nsValid(extensaoSerie(arvore)),
		ID:                serieID,
	}); err != nil {
		return serieID, criada, err
	}
	for _, t := range arvore.Temporadas {
		s.importarTemporada(ctx, serieID, source, arvore.DefaultSeasonType, t, langs)
	}
	return serieID, criada, nil
}

// ImportarSeriePorNome busca a serie pelo NOME nos providers que suportam series,
// escolhe o candidato de TITULO EXATO e UNICO (case-insensitive) e importa a arvore
// dele (reusando importarSerie, idempotente). E o ponto reusavel que o scanner de
// series (0034) chama quando a serie ainda nao esta no catalogo.
//
// Escolha (respeita a prioridade de fontes = ordem do Registry, ex.: [tmdb, tvdb]):
// percorre os providers na ordem; o PRIMEIRO que devolver exatamente UM match de
// titulo exato vence e e importado. Um provider que devolva 2+ exatos e ambiguo. Se
// nenhum provider deu exato-unico mas houve candidatos, e ErrSerieAmbigua; se nao
// houve candidato algum, e ErrSerieNaoEncontrada. NUNCA adivinha por titulo parcial.
func (s *Servico) ImportarSeriePorNome(ctx context.Context, nome string, langs []string) (string, error) {
	alvo := strings.TrimSpace(nome)
	if alvo == "" {
		return "", ErrSerieNaoEncontrada
	}
	lang := langPrincipal(langs)
	houveCandidato := false
	for _, prov := range s.providers.All() {
		if _, ok := prov.(provider.SerieProvider); !ok {
			continue
		}
		res, err := prov.Buscar(ctx, alvo, tipoSerie, lang)
		if err != nil {
			log.Printf("enrich: busca de serie %q na fonte %q falhou (segue): %v", alvo, prov.Nome(), err)
			continue
		}
		if len(res) > 0 {
			houveCandidato = true
		}
		exatos := exatosPorTitulo(res, alvo)
		switch len(exatos) {
		case 0:
			continue // esta fonte nao tem exato; tenta a proxima (prioridade)
		case 1:
			return s.importarSeriePorFonte(ctx, prov.Nome(), exatos[0].SourceID, langs)
		default:
			return "", ErrSerieAmbigua // 2+ exatos na mesma fonte: nao adivinha
		}
	}
	if houveCandidato {
		return "", ErrSerieAmbigua // candidatos existiam, nenhum exato-unico
	}
	return "", ErrSerieNaoEncontrada
}

// importarSeriePorFonte importa a arvore da serie escolhida (source, sourceID),
// reusando o importarSerie da Fase 1. Devolve so o id (o `criada` nao interessa ao
// scanner: idempotente reusa a serie existente).
func (s *Servico) importarSeriePorFonte(ctx context.Context, source, sourceID string, langs []string) (string, error) {
	id, _, err := s.importarSerie(ctx, source, sourceID, langs)
	return id, err
}

// exatosPorTitulo filtra os resultados cujo titulo casa EXATAMENTE (case-insensitive)
// o nome alvo, dedup por SourceID (a mesma serie repetida na fonte conta uma vez).
func exatosPorTitulo(res []provider.ResultadoExterno, alvo string) []provider.ResultadoExterno {
	vistos := map[string]bool{}
	var out []provider.ResultadoExterno
	for _, r := range res {
		if !strings.EqualFold(strings.TrimSpace(r.Title), alvo) {
			continue
		}
		if vistos[r.SourceID] {
			continue
		}
		vistos[r.SourceID] = true
		out = append(out, r)
	}
	return out
}

// importarTemporada cria (ou reusa) a temporada, cacheia seu pôster, liga-a à série e
// importa seus episódios. Isolada de importarSerie para manter o gocyclo baixo. Falhas
// degradam (loga e segue).
func (s *Servico) importarTemporada(ctx context.Context, serieID, source, defaultType string, t provider.TemporadaExterna, langs []string) {
	tempID, err := s.acharOuCriarTemporada(ctx, serieID, t, langs)
	if err != nil {
		log.Printf("enrich: temporada %d da serie %s falhou (segue): %v", t.SeasonNumber, serieID, err)
		return
	}
	if t.PosterURL != "" {
		if err := s.cachearArte(ctx, tempID, t.PosterURL, kindPoster, "poster"); err != nil {
			log.Printf("enrich: poster da temporada %s falhou (segue): %v", tempID, err)
		}
	}
	s.ligarFilho(ctx, serieID, tempID, relacaoTemporada, t.SeasonNumber)
	for _, ep := range t.Episodios {
		s.importarEpisodio(ctx, tempID, source, defaultType, ep, langs)
	}
}

// acharOuCriarTemporada faz o find-or-create IDEMPOTENTE da temporada por
// (série-pai, season_number): não há id de fonte no struct, então o dedup é pelo
// número — reusa um filho tv_season já ligado com o mesmo sort_index. Senão cria uma
// entity type='season' com título (Name[default] ou "Temporada N"), ano e o namespace
// core.tv@1 (ordering + season_number + year).
func (s *Servico) acharOuCriarTemporada(ctx context.Context, serieID string, t provider.TemporadaExterna, langs []string) (string, error) {
	if rels, err := s.leitura().ListarRelacoesFilhas(ctx, serieID); err == nil {
		for _, r := range rels {
			if r.Kind == relacaoTemporada && r.SortIndex.Valid && int(r.SortIndex.Int64) == t.SeasonNumber {
				return r.ChildID, nil
			}
		}
	}
	novo := s.novoID()
	if _, err := s.escrita().CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:                novo,
		Type:              tipoTemporada,
		Title:             tituloTemporada(t, langs),
		ReleaseDate:       nsOrNull(t.Year),
		ExtensionDataJson: nsValid(extensaoTemporada(t)),
	}); err != nil {
		return "", err
	}
	return novo, nil
}

// importarEpisodio cria (ou reusa) o episódio e liga-o à temporada com sort_index =
// número do episódio NA ORDENAÇÃO DEFAULT. Isolada para o gocyclo.
//
// O still do episódio NÃO é baixado no import: uma série tem centenas/milhares de
// episódios (Looney Tunes ~1195), e baixar um still por episódio síncrono no
// POST /import é lento e hostil ao CDN. O artwork de episódio (still) fica para a
// Fase 3 (proxy/cache sob demanda), junto com o artwork de temporada — ver briefing
// 0031 §7. Aqui só persistimos o episódio e a relação ordenada.
func (s *Servico) importarEpisodio(ctx context.Context, tempID, source, defaultType string, ep provider.EpisodioExterno, langs []string) {
	epID, err := s.acharOuCriarEpisodio(ctx, source, ep, langs)
	if err != nil {
		log.Printf("enrich: episodio %s falhou (segue): %v", ep.SourceID, err)
		return
	}
	s.ligarFilho(ctx, tempID, epID, relacaoEpisodio, ep.Numbering[defaultType].Number)
}

// acharOuCriarEpisodio faz o find-or-create do episódio por id externo (dedup por
// external_id_<fonte>, a ÂNCORA do ADR-0021 — nunca o número) e, EM SEGUIDA, grava a
// base externa (media_external) com os títulos/sinopses POR IDIOMA — a mesma i18n do
// filme aplicada ao episódio (briefing 0041). É idempotente: no reuse a linha-base é
// reaproveitada e o media_external é REESCRITO (upsert por (entity_id, source)).
func (s *Servico) acharOuCriarEpisodio(ctx context.Context, source string, ep provider.EpisodioExterno, langs []string) (string, error) {
	epID, err := s.idDoEpisodio(ctx, source, ep, langs)
	if err != nil {
		return "", err
	}
	if err := s.persistirEpisodioExterno(ctx, epID, source, ep); err != nil {
		return "", err
	}
	return epID, nil
}

// idDoEpisodio devolve o id da entity do episódio: reusa a existente (dedup por id
// externo) ou cria uma nova. Campos ESCALARES na LINHA (base/fallback curado, idioma
// default apenas; o per-idioma vive no media_external — 0041): título, descrição, data
// de exibição, external_ids ({fonte: episode.id} + os que a fonte trouxe) e o namespace
// core.tv@1 (absolute_number + numbering multi-ordem).
func (s *Servico) idDoEpisodio(ctx context.Context, source string, ep provider.EpisodioExterno, langs []string) (string, error) {
	if existente, ok, err := s.acharPorIDExterno(ctx, source, ep.SourceID); err != nil {
		return "", err
	} else if ok {
		return existente, nil
	}
	extIDs, err := json.Marshal(idsDoEpisodio(source, ep))
	if err != nil {
		return "", err
	}
	novo := s.novoID()
	if _, err := s.escrita().CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:                novo,
		Type:              tipoEpisodio,
		Title:             tituloEpisodio(ep, langs),
		Description:       nsOrNull(valorPorIdioma(ep.Overview, langs)),
		ReleaseDate:       nsOrNull(ep.AirDate),
		ExternalIdsJson:   nsValid(string(extIDs)),
		ExtensionDataJson: nsValid(extensaoEpisodio(ep)),
	}); err != nil {
		return "", err
	}
	return novo, nil
}

// persistirEpisodioExterno grava (ou reescreve) a base externa do episódio: um
// media_external per-fonte cujo payload carrega os mapas title/overview POR IDIOMA crus
// (sem descartar idiomas, ao contrário do escalar da linha). Espelha o import de filme
// (enrich.persistir → UpsertMediaExternal), mas com o envelope de episódio (discriminador
// entity_kind) — é o que faz o detalhe do episódio traduzir pelo resolver. Idempotente
// por (entity_id, source): o reuse REESCREVE. original_language não é denormalizado (a
// árvore não o propaga por episódio; a cadeia funciona como [lang, fallback] — 0041 §4).
func (s *Servico) persistirEpisodioExterno(ctx context.Context, epID, source string, ep provider.EpisodioExterno) error {
	payload, err := provider.MontarEnvelopeEpisodio(ep, "")
	if err != nil {
		return err
	}
	_, err = s.escrita().UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
		ID:          s.novoID(),
		EntityID:    epID,
		Source:      source,
		SourceID:    nsValid(ep.SourceID),
		PayloadJson: nsValid(string(payload)),
	})
	return err
}

// ligarFilho cria a relação pai→filho de forma IDEMPOTENTE: carrega os já-ligados e
// pula quando o par (child, kind) já existe (o UNIQUE(parent,child,kind) ainda protege
// de corrida). sort_index carrega o número estável do filho (temporada na série,
// episódio na temporada) para ListarRelacoesFilhas ordenar. Falha degrada (loga).
func (s *Servico) ligarFilho(ctx context.Context, parentID, childID, kind string, sortIndex int) {
	if rels, err := s.leitura().ListarRelacoesFilhas(ctx, parentID); err == nil {
		for _, r := range rels {
			if r.Kind == kind && r.ChildID == childID {
				return
			}
		}
	}
	if _, err := s.escrita().CriarEntityRelation(ctx, sqlcgen.CriarEntityRelationParams{
		ID:        s.novoID(),
		ParentID:  parentID,
		ChildID:   childID,
		Kind:      kind,
		SortIndex: sql.NullInt64{Int64: int64(sortIndex), Valid: true},
	}); err != nil {
		log.Printf("enrich: ligar %s->%s (%s) falhou (segue): %v", parentID, childID, kind, err)
	}
}

// --- montagem do extension_data (namespace core.tv@1) ---

// extensaoSerie monta o extension_data da série: default_season_type + season_types.
func extensaoSerie(a provider.SerieArvore) string {
	return envelopeTV(map[string]any{
		"default_season_type": a.DefaultSeasonType,
		"season_types":        a.SeasonTypes,
	})
}

// extensaoTemporada monta o extension_data da temporada: ordering + season_number
// (+ year quando houver).
func extensaoTemporada(t provider.TemporadaExterna) string {
	payload := map[string]any{
		"ordering":      t.Ordering,
		"season_number": t.SeasonNumber,
	}
	if t.Year != "" {
		payload["year"] = t.Year
	}
	return envelopeTV(payload)
}

// extensaoEpisodio monta o extension_data do episódio: numbering (TODAS as ordens que
// a série usa) + absolute_number quando a fonte informou + still_url (0042) quando o
// provider trouxe um frame. O still é artwork NEUTRO de idioma (como o numbering): só
// a URL crua é persistida — a imagem é buscada/cacheada SOB DEMANDA pelo image proxy na
// 1ª visualização, jamais baixada no import (uma série tem centenas/milhares de eps).
func extensaoEpisodio(ep provider.EpisodioExterno) string {
	payload := map[string]any{
		"numbering": numberingMap(ep.Numbering),
	}
	if ep.AbsoluteNumber != nil {
		payload["absolute_number"] = *ep.AbsoluteNumber
	}
	if ep.StillURL != "" {
		payload["still_url"] = ep.StillURL
	}
	return envelopeTV(payload)
}

// numberingMap converte o Numbering canônico (ordem→Numero) no objeto JSON
// ordem→{season,number} que o extension_data guarda.
func numberingMap(n map[string]provider.Numero) map[string]any {
	out := make(map[string]any, len(n))
	for ordem, num := range n {
		out[ordem] = map[string]any{"season": num.Season, "number": num.Number}
	}
	return out
}

// envelopeTV embrulha o payload no namespace core.tv@1 e serializa. Erro de marshal
// (inatingível para estes mapas simples) degrada para "{}".
func envelopeTV(payload map[string]any) string {
	b, err := json.Marshal(map[string]any{nsTV: payload})
	if err != nil {
		return "{}"
	}
	return string(b)
}

// --- helpers puros de campo ---

// tituloTemporada é Name[idioma default] ou o fallback "Temporada N" (temporadas
// raramente têm nome). N é o season_number (mesmo quando é ano civil, "Temporada N"
// ainda serve como rótulo).
func tituloTemporada(t provider.TemporadaExterna, langs []string) string {
	if nome := valorPorIdioma(t.Name, langs); nome != "" {
		return nome
	}
	return fmt.Sprintf("Temporada %d", t.SeasonNumber)
}

// tituloEpisodio é Title[idioma default]; cai para o SourceID quando não há título
// (title é NOT NULL — nunca vazio), espelhando o tituloInicial de item.
func tituloEpisodio(ep provider.EpisodioExterno, langs []string) string {
	if t := valorPorIdioma(ep.Title, langs); t != "" {
		return t
	}
	return ep.SourceID
}

// idsDoEpisodio monta os external_ids do episódio: {fonte: episode.id} (a âncora de
// dedup por external_id_<fonte>) MAIS os ids que a fonte trouxe (ex.: tvdb). A fonte
// vence se colidir (é o id autoritativo do import).
func idsDoEpisodio(source string, ep provider.EpisodioExterno) map[string]string {
	ids := map[string]string{}
	for k, v := range ep.ExternalIDs {
		if v != "" {
			ids[k] = v
		}
	}
	ids[source] = ep.SourceID
	return ids
}

// valorPorIdioma escolhe o valor do "idioma default": o primeiro de `langs` com valor,
// senão qualquer valor presente (em ordem alfabética de idioma, para determinismo). ""
// quando o mapa não tem nenhum.
func valorPorIdioma(m map[string]string, langs []string) string {
	for _, l := range langs {
		if v := m[l]; v != "" {
			return v
		}
	}
	chaves := make([]string, 0, len(m))
	for k := range m {
		chaves = append(chaves, k)
	}
	sort.Strings(chaves)
	for _, k := range chaves {
		if m[k] != "" {
			return m[k]
		}
	}
	return ""
}

// nsOrNull embrulha uma string num sql.NullString válido, ou NULL quando vazia (para
// colunas opcionais: description/release_date de temporada/episódio).
func nsOrNull(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
