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
	"path/filepath"
	"sort"
	"strings"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/naming"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/tvext"
	"github.com/rafaelvieiras/my-media-db/internal/tvmatch"
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

// arvoreDaSerie valida a capacidade do provider e busca a arvore bruta (temporadas +
// episodios) na fonte -- sem tocar nenhuma entity. Extraido de importarSerie para ser
// reusado por Enriquecer (0067): la, a entity ja existe e so a arvore precisa ser
// buscada/aplicada; aqui, POST /import busca ANTES de criar a serie (falha rapido sem
// deixar entity orfa quando a fonte nao suporta serie).
func (s *Servico) arvoreDaSerie(ctx context.Context, source, sourceID string, langs []string) (provider.SerieArvore, error) {
	prov, ok := s.provFonte(source)
	if !ok {
		return provider.SerieArvore{}, fmt.Errorf("enrich: fonte %q sem provider", source)
	}
	sp, ok := prov.(provider.SerieProvider)
	if !ok {
		return provider.SerieArvore{}, fmt.Errorf("enrich: fonte %q nao suporta serie", source)
	}
	return sp.BuscarSerieArvore(ctx, sourceID, langs)
}

// aplicarArvoreSerie grava a arvore JA BUSCADA numa serie que JA EXISTE (serieID
// conhecido): extension_data da serie, enriquecimento rico cruzado (best-effort),
// cada temporada/episodio (find-or-create com refresh, 0043) e o inventario de artes.
// So AtualizarExtensionData propaga erro (escrita de banco real); o resto e best-effort,
// exatamente como importarSerie ja fazia.
func (s *Servico) aplicarArvoreSerie(ctx context.Context, serieID, source, sourceID string, arvore provider.SerieArvore, langs []string) error {
	if err := s.escrita().AtualizarExtensionData(ctx, sqlcgen.AtualizarExtensionDataParams{
		ExtensionDataJson: nsValid(extensaoSerie(arvore)),
		ID:                serieID,
	}); err != nil {
		return err
	}
	// Enriquecimento RICO cruzado do nó série (0045, rota C; agnóstico de fonte no 0046):
	// o OUTRO provider disponível (o que não é a fonte-base da árvore) preenche os campos
	// ricos (slogan/gêneros/classificação/keywords/similar/situação) + baixa a fanart.
	// Best-effort: falha/ausência degrada para só-a-árvore (nunca aborta a árvore).
	s.enriquecerSerieRico(ctx, serieID, source, sourceID, langs)
	for _, t := range arvore.Temporadas {
		s.importarTemporada(ctx, serieID, source, arvore.DefaultSeasonType, t, langs)
	}
	// Inventário de artes (0048): DEPOIS da árvore, para que as artes de temporada (Season)
	// encontrem a entity da temporada correspondente. Best-effort (sem ArteProvider ou id,
	// degrada em silêncio).
	s.enriquecerArtes(ctx, serieID, tipoSerie, langs)
	// Recasamento de arquivos pendentes (0069): arquivos que ja estavam presos na SERIE
	// (escaneados ANTES da arvore existir, sem vinculo tv_episode) ganham o vinculo certo
	// agora que a arvore acabou de ser sincronizada -- sem esperar um novo scan. Por
	// ULTIMO, best-effort (nunca propaga erro): a arvore/identidade ja foram gravadas com
	// sucesso quando este passo roda.
	s.recasarArquivosPendentes(ctx, serieID)
	return nil
}

// recasarArquivosPendentes fecha o gap do 0069: o vinculo arquivo->episodio
// (media_file_entity, kind=tv_episode) so era tentado DENTRO de um scan, contra a
// arvore que ja existisse naquele momento (internal/scanner/serie.go). Quando o
// scanner encontra a serie ANTES dela ter identidade externa, o arquivo fica preso
// direto na entity da serie, sem vinculo, ate o PROXIMO scan completo. Este passo
// tenta o mesmo casamento por NUMERO (internal/tvmatch, regra do 0034) contra a
// arvore recem-sincronizada, no MESMO instante em que ela passa a existir -- e
// idempotente (pula arquivo ja vinculado) e nunca casa forcado (sem match, o
// arquivo continua pendente, exatamente como hoje).
func (s *Servico) recasarArquivosPendentes(ctx context.Context, serieID string) {
	arquivos, err := s.leitura().ListarMediaFilesPorEntity(ctx, serieID)
	if err != nil {
		log.Printf("enrich: listar arquivos da serie %s p/ recasamento falhou (segue): %v", serieID, err)
		return
	}
	var idx *tvmatch.Index // construido sob demanda, so' se houver candidato a recasar
	for _, mf := range arquivos {
		if s.temVinculoEpisodio(ctx, mf.ID) {
			continue // ja vinculado (scan anterior ou iteracao anterior deste loop)
		}
		ep, ok := naming.ParseEpisode(semExtensaoArquivo(filepath.Base(mf.Path)))
		if !ok {
			continue // nao reconhece padrao de episodio -- fica pendente
		}
		if idx == nil {
			idx = s.carregarIndexSerie(ctx, serieID)
		}
		ids, ok := tvmatch.Casar(idx, ep)
		if !ok {
			continue // sem match na arvore -- fica pendente, como hoje
		}
		s.ligarArquivoAosEpisodios(ctx, mf.ID, ids)
	}
}

// temVinculoEpisodio diz se o media_file ja tem algum vinculo kind=tv_episode em
// media_file_entity -- garante a idempotencia do recasamento (nunca reprocessa nem
// duplica um arquivo que um scan anterior, ou uma iteracao anterior deste mesmo
// loop, ja vinculou). Erro de leitura degrada para "sem vinculo" (o pior caso e
// tentar recasar de novo, que e idempotente por causa do upsert em
// CriarVinculoEntidade).
func (s *Servico) temVinculoEpisodio(ctx context.Context, mediaFileID string) bool {
	vinculos, err := s.leitura().ListarVinculosPorArquivo(ctx, mediaFileID)
	if err != nil {
		log.Printf("enrich: listar vinculos do arquivo %s falhou (segue): %v", mediaFileID, err)
		return false
	}
	for _, v := range vinculos {
		if v.Kind == relacaoEpisodio {
			return true
		}
	}
	return false
}

// ligarArquivoAosEpisodios cria o vinculo media_file_entity (kind=tv_episode) para
// cada episodio casado, com ordinal = posicao na lista -- mesmo layout do scanner
// (um range S01E01-E03 vira 3 linhas, ordinal 0/1/2, sob o mesmo media_file). Falha
// degrada (loga e segue, um episodio da lista nao derruba os demais).
func (s *Servico) ligarArquivoAosEpisodios(ctx context.Context, mediaFileID string, episodioIDs []string) {
	for i, epID := range episodioIDs {
		if err := s.escrita().CriarVinculoEntidade(ctx, sqlcgen.CriarVinculoEntidadeParams{
			MediaFileID: mediaFileID,
			EntityID:    epID,
			Kind:        relacaoEpisodio,
			Ordinal:     int64(i),
		}); err != nil {
			log.Printf("enrich: vincular arquivo %s ao episodio %s falhou (segue): %v", mediaFileID, epID, err)
		}
	}
}

// carregarIndexSerie monta o indice numero->episode_id da arvore de uma serie
// (espelha scanner.carregarIndex, MESMA leitura: ListarRelacoesFilhas + extension_data
// de cada episodio), agora disponivel em internal/enrich via internal/tvmatch. Falha
// de leitura degrada para um indice vazio -- Casar simplesmente nao encontra nada, o
// arquivo fica pendente (nenhum casamento forcado).
func (s *Servico) carregarIndexSerie(ctx context.Context, serieID string) *tvmatch.Index {
	idx := tvmatch.NovoIndex()
	q := s.leitura()

	serie, err := q.ObterEntity(ctx, serieID)
	if err != nil {
		log.Printf("enrich: serie %s ilegivel ao montar indice de recasamento: %v", serieID, err)
		return idx
	}
	idx.PreencherOrdenacoes(tvext.Namespace(serie.ExtensionDataJson, nsTV))

	temps, err := q.ListarRelacoesFilhas(ctx, serieID)
	if err != nil {
		log.Printf("enrich: temporadas da serie %s ilegiveis ao montar indice de recasamento: %v", serieID, err)
		return idx
	}
	for _, t := range temps {
		if t.Kind != relacaoTemporada {
			continue
		}
		eps, err := q.ListarRelacoesFilhas(ctx, t.ChildID)
		if err != nil {
			continue // temporada ilegivel: degrada sem derrubar o resto do indice
		}
		for _, e := range eps {
			if e.Kind != relacaoEpisodio {
				continue
			}
			ep, err := q.ObterEntity(ctx, e.ChildID)
			if err != nil {
				continue
			}
			idx.IndexarEpisodio(ep.ID, tvext.Namespace(ep.ExtensionDataJson, nsTV))
		}
	}
	return idx
}

// semExtensaoArquivo remove a extensao do nome-base de um arquivo antes do
// naming.ParseEpisode (que espera o nome sem extensao) -- espelha
// scanner.semExtensao, sem depender do pacote scanner (internal/enrich nao pode
// importa-lo, so' o contrario).
func semExtensaoArquivo(nome string) string {
	return strings.TrimSuffix(nome, filepath.Ext(nome))
}

// sincronizarArvoreSerie combina os dois passos acima para um chamador que so tem
// (serieID, source, sourceID) -- o caso de Enriquecer (0067), que nao tem uma arvore
// pre-buscada como importarSerie tem. Erro cobre tanto capacidade/rede (arvoreDaSerie)
// quanto a escrita do extension_data (aplicarArvoreSerie); o chamador (Enriquecer)
// trata como best-effort.
func (s *Servico) sincronizarArvoreSerie(ctx context.Context, serieID, source, sourceID string, langs []string) error {
	arvore, err := s.arvoreDaSerie(ctx, source, sourceID, langs)
	if err != nil {
		return err
	}
	return s.aplicarArvoreSerie(ctx, serieID, source, sourceID, arvore, langs)
}

// importarSerie importa a ÁRVORE de uma série. Requer a capacidade opcional
// SerieProvider (senão erro → 502, como a coleção). A série (type='tv') nasce/atualiza
// pelo molde de item (detalhe/pôster/curarTitulo) e recebe o namespace core.tv@1; em
// seguida cada temporada da ordenação default e cada episódio fino são importados. A
// falha de UMA temporada/episódio degrada (loga e segue) — a árvore parcial é válida.
//
// Capacidade/busca da árvore roda ANTES de resolver a entity da série (mesma ordem de
// sempre: falha rápido sem deixar entity órfã quando a fonte não suporta série).
func (s *Servico) importarSerie(ctx context.Context, source, sourceID string, langs []string) (string, bool, error) {
	arvore, err := s.arvoreDaSerie(ctx, source, sourceID, langs)
	if err != nil {
		return "", false, fmt.Errorf("enrich: buscar arvore da serie: %w", err)
	}
	serieID, criada, err := s.importarItemSemArvore(ctx, source, sourceID, tipoSerie, langs)
	if err != nil {
		return serieID, criada, err
	}
	if err := s.aplicarArvoreSerie(ctx, serieID, source, sourceID, arvore, langs); err != nil {
		return serieID, criada, err
	}
	return serieID, criada, nil
}

// enriquecerSerieRico ENRIQUECE o nó série com os campos ricos que a fonte-base da árvore
// não modela (0045, rota C; AGNÓSTICO DE FONTE no 0046): escolhe o PRIMEIRO OUTRO provider
// registrado com a capacidade SerieRicoProvider, cruza o id externo da série (o sourceID
// da fonte-base) para a fonte rica, busca o canônico rico (só as chaves ricas) e o
// PERSISTE como uma camada media_external da fonte rica no nó série — que a composição de
// camadas (montarExterno) já compõe de graça sobre a base, reusando a maquinaria do filme
// SEM clobbar título/idioma. Em seguida cacheia a FANART (backdrop_url). Idempotente (o
// reuse REESCREVE por (entity_id, source), casando com o 0043). Tudo best-effort: sem
// outro provider rico, sem match ou erro de rede degrada em silêncio para só-a-árvore —
// nunca aborta. Sem hardcode de "tmdb"/"tvdb": a fonte-base e a rica são resolvidas por
// disponibilidade/capacidade no registry.
func (s *Servico) enriquecerSerieRico(ctx context.Context, serieID, source, sourceID string, langs []string) {
	rico, ricoSource, ok := s.enriquecedorRico(source)
	if !ok {
		return // nenhum OUTRO provider expõe a capacidade rica: só a árvore da fonte-base
	}
	idExterno := idExternoDaSerie(source, sourceID)
	if idExterno == "" {
		return // sem id externo para cruzar
	}
	campos, ok, err := rico.BuscarSerieDetalheRico(ctx, idExterno, langs)
	if err != nil {
		log.Printf("enrich: serie-rico %s da serie %s (fonte-base %s id %s) falhou (segue so-arvore): %v", ricoSource, serieID, source, idExterno, err)
		return
	}
	if !ok || len(campos) == 0 {
		return // sem match na fonte rica: graca (so-a-arvore)
	}
	if err := s.persistirSerieRico(ctx, serieID, ricoSource, campos); err != nil {
		log.Printf("enrich: persistir serie-rico da serie %s falhou (segue): %v", serieID, err)
		return
	}
	// backdrop_url agora existe no canônico da série: baixa a fanart (poster_url não vem
	// no canônico rico — o pôster segue da fonte-base, já cacheado em importarItem).
	s.cachearArtes(ctx, serieID, campos)
	// Elenco/equipe/produtoras (MD6, 0029) do no serie vem da FONTE RICA (o aggregate do
	// TMDB), sob a source rica -- idempotente por (serie, ricoSource). No caso TMDB-base
	// (sem outro provider) este ramo nao roda; os creditos entao vem do persistir do no
	// serie (source-base), que ja mapeou o aggregate_credits do /tv.
	s.persistirCreditos(ctx, serieID, ricoSource, campos)
}

// enriquecedorRico escolhe o provider de ENRIQUECIMENTO rico do nó série: o PRIMEIRO
// provider registrado, DIFERENTE da fonte-base `base`, que implemente SerieRicoProvider
// (respeita a ordem = prioridade do registry). Substitui o antigo Get("tmdb") fixo — a
// árvore pode vir de qualquer fonte e o enriquecedor é o outro provider capaz. (nil,"",
// false) quando nenhum outro tem a capacidade: a série fica só com a árvore (graça).
func (s *Servico) enriquecedorRico(base string) (provider.SerieRicoProvider, string, bool) {
	for _, prov := range s.providers.All() {
		if prov.Nome() == base {
			continue // a fonte-base não enriquece a si mesma
		}
		if rico, ok := prov.(provider.SerieRicoProvider); ok {
			return rico, prov.Nome(), true
		}
	}
	return nil, "", false
}

// idExternoDaSerie devolve o id externo da série na FONTE-BASE da árvore (o sourceID do
// import), que o provider de enriquecimento rico cruza para a sua própria fonte. Genérico
// (antes idTvdbDaSerie assumia source=="tvdb"): não presume qual é a fonte-base — qualquer
// SerieProvider que ancore a árvore expõe seu id aqui, e o enriquecedor rico o resolve.
func idExternoDaSerie(source, sourceID string) string {
	_ = source // a fonte-base é genérica; o id a cruzar é sempre o sourceID do import
	return sourceID
}

// persistirSerieRico grava (ou reescreve) o canônico RICO da série como uma camada
// media_external da fonte rica `ricoSource`, embrulhado no envelope ChaveEnvelopeSerieRico
// que o MapearDetalhe daquela fonte reconhece (passthrough). SourceID fica NULL: é uma
// camada de ENRIQUECIMENTO cruzada, não um import primário da fonte rica (o nó série não é
// deduplicado por id dela — a âncora é a fonte-base da árvore).
func (s *Servico) persistirSerieRico(ctx context.Context, serieID, ricoSource string, campos map[string]any) error {
	payload, err := json.Marshal(map[string]any{provider.ChaveEnvelopeSerieRico: campos})
	if err != nil {
		return err
	}
	_, err = s.escrita().UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
		ID:          s.novoID(),
		EntityID:    serieID,
		Source:      ricoSource,
		SourceID:    sql.NullString{},
		PayloadJson: nsValid(string(payload)),
	})
	return err
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
	s.cachearPosterTemporadaSeAusente(ctx, tempID, t.PosterURL)
	s.ligarFilho(ctx, serieID, tempID, relacaoTemporada, t.SeasonNumber)
	for _, ep := range t.Episodios {
		s.importarEpisodio(ctx, tempID, source, defaultType, ep, langs)
	}
}

// cachearPosterTemporadaSeAusente baixa o pôster da temporada APENAS quando ainda não
// há asset externo (briefing 0043): no re-import a árvore inteira refresca, mas re-baixar
// arte já presente a cada import martelaria o CDN sem ganho (o pôster raramente muda). Só-
// se-ausente dá idempotência barata; refrescar arte que MUDOU é follow-up (hash/force).
// Erro de leitura degrada (loga e segue); erro de download idem — a árvore parcial vale.
func (s *Servico) cachearPosterTemporadaSeAusente(ctx context.Context, tempID, posterURL string) {
	if posterURL == "" {
		return
	}
	_, err := s.leitura().ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{
		EntityID: tempID,
		Kind:     kindPoster,
		Origin:   originExternal,
	})
	if err == nil {
		return // ja existe: nao re-baixa (so-se-ausente)
	}
	if !isNoRows(err) {
		log.Printf("enrich: checar poster da temporada %s falhou (segue): %v", tempID, err)
		return
	}
	if err := s.cachearArte(ctx, tempID, posterURL, kindPoster, "poster"); err != nil {
		log.Printf("enrich: poster da temporada %s falhou (segue): %v", tempID, err)
	}
}

// acharOuCriarTemporada faz o find-or-create IDEMPOTENTE da temporada por
// (série-pai, season_number): não há id de fonte no struct, então o dedup é pelo
// número — reusa um filho tv_season já ligado com o mesmo sort_index. No reuse REESCREVE
// a base (título/ano/extension_data) com os dados atuais do provider (briefing 0043 — o
// override do dono continua vencendo na resolução); senão cria uma entity type='season'.
// Os campos vêm de camposTemporada, compartilhado entre create e update (sem divergir).
func (s *Servico) acharOuCriarTemporada(ctx context.Context, serieID string, t provider.TemporadaExterna, langs []string) (string, error) {
	title, releaseDate, extData := camposTemporada(t, langs)
	if rels, err := s.leitura().ListarRelacoesFilhas(ctx, serieID); err == nil {
		for _, r := range rels {
			if r.Kind == relacaoTemporada && r.SortIndex.Valid && int(r.SortIndex.Int64) == t.SeasonNumber {
				if err := s.escrita().AtualizarTemporadaBase(ctx, sqlcgen.AtualizarTemporadaBaseParams{
					Title:             title,
					ReleaseDate:       releaseDate,
					ExtensionDataJson: extData,
					ID:                r.ChildID,
				}); err != nil {
					return "", err
				}
				return r.ChildID, nil
			}
		}
	}
	novo := s.novoID()
	if _, err := s.escrita().CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:                novo,
		Type:              tipoTemporada,
		Title:             title,
		ReleaseDate:       releaseDate,
		ExtensionDataJson: extData,
	}); err != nil {
		return "", err
	}
	return novo, nil
}

// camposTemporada monta os campos-base da temporada compartilhados entre create e o
// refresh de reuse (briefing 0043): título (Name[default] ou "Temporada N"), data (ano) e
// o extension_data core.tv@1. Centralizar evita que o refresh esqueça um campo no futuro.
func camposTemporada(t provider.TemporadaExterna, langs []string) (title string, releaseDate, extData sql.NullString) {
	return tituloTemporada(t, langs), nsOrNull(t.Year), nsValid(extensaoTemporada(t))
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
	campos, err := montarCamposEpisodio(source, ep, langs)
	if err != nil {
		return "", err
	}
	if existente, ok, err := s.acharPorIDExterno(ctx, source, ep.SourceID); err != nil {
		return "", err
	} else if ok {
		// Reuse: REESCREVE a base com os dados atuais do provider (briefing 0043) — em
		// especial still_url/numbering (extension_data), que so o create gravava. Nao toca
		// status/tags/original_title/media_override: o override do dono segue vencendo.
		if err := s.escrita().AtualizarEpisodioBase(ctx, sqlcgen.AtualizarEpisodioBaseParams{
			Title:             campos.title,
			Description:       campos.description,
			ReleaseDate:       campos.releaseDate,
			ExternalIdsJson:   campos.externalIDs,
			ExtensionDataJson: campos.extensionData,
			ID:                existente,
		}); err != nil {
			return "", err
		}
		return existente, nil
	}
	novo := s.novoID()
	if _, err := s.escrita().CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:                novo,
		Type:              tipoEpisodio,
		Title:             campos.title,
		Description:       campos.description,
		ReleaseDate:       campos.releaseDate,
		ExternalIdsJson:   campos.externalIDs,
		ExtensionDataJson: campos.extensionData,
	}); err != nil {
		return "", err
	}
	return novo, nil
}

// camposEpisodio agrega os campos-base do episódio (escalares da linha) compartilhados
// entre o create e o refresh de reuse (briefing 0043): título, sinopse (idioma default),
// data de exibição, external_ids ({fonte: episode.id} + os da fonte) e o extension_data
// core.tv@1 (numbering + absolute_number + still_url). Um só ponto de montagem evita que
// o refresh divirja do create (esquecer um campo novo no futuro).
type camposEpisodio struct {
	title         string
	description   sql.NullString
	releaseDate   sql.NullString
	externalIDs   sql.NullString
	extensionData sql.NullString
}

// montarCamposEpisodio computa os camposEpisodio; erra só no marshal dos external_ids.
func montarCamposEpisodio(source string, ep provider.EpisodioExterno, langs []string) (camposEpisodio, error) {
	extIDs, err := json.Marshal(idsDoEpisodio(source, ep))
	if err != nil {
		return camposEpisodio{}, err
	}
	return camposEpisodio{
		title:         tituloEpisodio(ep, langs),
		description:   nsOrNull(valorPorIdioma(ep.Overview, langs)),
		releaseDate:   nsOrNull(ep.AirDate),
		externalIDs:   nsValid(string(extIDs)),
		extensionData: nsValid(extensaoEpisodio(ep)),
	}, nil
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
