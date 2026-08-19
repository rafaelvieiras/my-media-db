package enrich

// musica.go é o ramo de import da VERTICAL DE MÚSICA do enrich (briefing 0106,
// ADR-0027): materializa a hierarquia artista→álbum→faixa a partir de um
// provider.MusicaProvider (MusicBrainz). Molde de serie.go, porém mais raso (sem
// ordenações concorrentes): artista/álbum/faixa são find-or-create IDEMPOTENTES por
// MBID (ObterEntityPorMbid), ligados por entity_relation com os kinds de
// internal/musicext. Reusa ligarFilho (serie.go) para a ligação idempotente
// pai→filho — mesmo mecanismo de dedup (checa ListarRelacoesFilhas antes de inserir).
// "Various Artists" (ADR-0027 item 5) não tem nenhum caso especial: cai no mesmo
// find-or-create por MBID que qualquer outro artista.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/musicext"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	// tipoArtista/tipoAlbum/tipoFaixa são os entity.type da árvore de música
	// (ADR-0027, briefing 0105 — sem CHECK, valores por convenção).
	tipoArtista = "artista"
	tipoAlbum   = "album"
	tipoFaixa   = "faixa"
)

// importarArtista importa a ÁRVORE de um artista (discografia inteira): busca a árvore
// na fonte (capacidade opcional MusicaProvider — erro → 502 quando ausente, mesmo padrão
// de importarColecao/importarSerie), find-or-create do artista por MBID e, para cada
// álbum, find-or-create + faixas + arte de capa. A falha de UM álbum/faixa degrada (loga
// e segue) — a discografia parcial é válida, mesmo espírito de importarSerie.
func (s *Servico) importarArtista(ctx context.Context, source, sourceID string, langs []string) (string, bool, error) {
	arvore, err := s.arvoreDoArtista(ctx, source, sourceID, langs)
	if err != nil {
		return "", false, fmt.Errorf("enrich: buscar arvore do artista: %w", err)
	}
	artistaID, criada, err := s.acharOuCriarArtista(ctx, arvore.MBID, arvore.Name, arvore.Bio)
	if err != nil {
		return "", false, err
	}
	for i, alb := range arvore.Albuns {
		s.importarAlbum(ctx, artistaID, arvore.MBID, alb, i, langs)
	}
	return artistaID, criada, nil
}

// arvoreDoArtista valida a capacidade do provider e busca a árvore bruta (álbuns +
// faixas) na fonte — sem tocar nenhuma entity. Espelha arvoreDaSerie (serie.go).
func (s *Servico) arvoreDoArtista(ctx context.Context, source, sourceID string, langs []string) (provider.ArtistaArvore, error) {
	prov, ok := s.provFonte(source)
	if !ok {
		return provider.ArtistaArvore{}, fmt.Errorf("enrich: fonte %q sem provider", source)
	}
	mp, ok := prov.(provider.MusicaProvider)
	if !ok {
		return provider.ArtistaArvore{}, fmt.Errorf("enrich: fonte %q nao suporta musica", source)
	}
	return mp.BuscarArtistaArvore(ctx, sourceID, langs)
}

// importarAlbum cria (ou reusa) o álbum, liga-o ao artista principal
// (kind=music_artista_principal, sort_index=posição na discografia), cacheia a capa
// (Cover Art Archive, via o mesmo pipeline de artes do filme/série — internal/enrich/
// artes.go) e importa cada faixa. Isolada para manter o gocyclo baixo; falhas degradam
// (loga e segue), a discografia parcial é válida.
func (s *Servico) importarAlbum(ctx context.Context, artistaID, artistaMBID string, alb provider.AlbumExterno, indice int, langs []string) {
	albumID, _, err := s.acharOuCriarAlbum(ctx, alb)
	if err != nil {
		log.Printf("enrich: album %s do artista %s falhou (segue): %v", alb.MBID, artistaID, err)
		return
	}
	s.ligarFilho(ctx, artistaID, albumID, musicext.KindArtistaPrincipal, indice)
	// Inventário de artes (Cover Art Archive): mesmo gatilho best-effort de
	// enriquecerArtes já usado por filme/série (0048) — cruza pelo external_id_mbid
	// que acharOuCriarAlbum acabou de gravar.
	s.enriquecerArtes(ctx, albumID, tipoAlbum, langs)
	for _, fx := range alb.Faixas {
		s.importarFaixa(ctx, albumID, artistaMBID, fx)
	}
}

// importarFaixa cria (ou reusa) a faixa, liga-a ao álbum (kind=music_album,
// sort_index=track_number) e, quando o artista REAL da faixa (artist-credit do
// recording) diverge do artista principal do álbum (feat./colaboração — ADR-0027 item
// 4), cria/reusa essa outra entity artista e liga ADICIONALMENTE por
// kind=music_artista_feat (não substitui o vínculo music_artista_principal do álbum).
func (s *Servico) importarFaixa(ctx context.Context, albumID, artistaPrincipalMBID string, fx provider.FaixaExterna) {
	faixaID, _, err := s.acharOuCriarFaixa(ctx, fx)
	if err != nil {
		log.Printf("enrich: faixa %s do album %s falhou (segue): %v", fx.MBID, albumID, err)
		return
	}
	s.ligarFilho(ctx, albumID, faixaID, musicext.KindAlbum, fx.TrackNumber)

	if fx.ArtistaCreditoMBID == "" || fx.ArtistaCreditoMBID == artistaPrincipalMBID {
		return // artista da faixa == artista do album (o caso comum): sem feat
	}
	featID, _, err := s.acharOuCriarArtista(ctx, fx.ArtistaCreditoMBID, fx.ArtistaCreditoNome, "")
	if err != nil {
		log.Printf("enrich: artista feat %s da faixa %s falhou (segue): %v", fx.ArtistaCreditoMBID, faixaID, err)
		return
	}
	s.ligarFilho(ctx, featID, faixaID, musicext.KindArtistaFeat, 0)
}

// acharOuCriarArtista faz o find-or-create IDEMPOTENTE de uma entity `artista` por MBID
// (ObterEntityPorMbid): reusa a existente ou cria uma nova com o nome/bio atuais. Não
// reescreve a base no reuse (ao contrário de acharOuCriarTemporada em serie.go) — o
// escopo deste briefing exige só a idempotência (nunca duplicar), não o refresh de
// metadados no reimport. "Various Artists" (MBID fixo, ADR-0027 item 5) entra por aqui
// como qualquer outro artista, sem caso especial.
func (s *Servico) acharOuCriarArtista(ctx context.Context, mbid, nome, bio string) (string, bool, error) {
	if existente, err := s.leitura().ObterEntityPorMbid(ctx, nsValid(mbid)); err == nil {
		return existente.ID, false, nil
	} else if !isNoRows(err) {
		return "", false, err
	}
	titulo := nome
	if titulo == "" {
		titulo = mbid
	}
	extIDs, err := json.Marshal(map[string]string{"mbid": mbid})
	if err != nil {
		return "", false, err
	}
	novo := s.novoID()
	if _, err := s.escrita().CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:              novo,
		Type:            tipoArtista,
		Title:           titulo,
		Description:     nsOrNull(bio),
		ExternalIdsJson: nsValid(string(extIDs)),
	}); err != nil {
		return "", false, err
	}
	return novo, true, nil
}

// acharOuCriarAlbum faz o find-or-create IDEMPOTENTE de uma entity `album` por MBID
// (o MBID de RELEASE-GROUP — mesmo id que o Cover Art Archive consome). Grava o ano
// (release_date) e o extension_data core.music@1 (genre, ADR-0027 item 7 — gênero vive
// no nível de álbum, não por faixa). Sem refresh no reuse, mesma nota de
// acharOuCriarArtista.
func (s *Servico) acharOuCriarAlbum(ctx context.Context, alb provider.AlbumExterno) (string, bool, error) {
	if existente, err := s.leitura().ObterEntityPorMbid(ctx, nsValid(alb.MBID)); err == nil {
		return existente.ID, false, nil
	} else if !isNoRows(err) {
		return "", false, err
	}
	titulo := alb.Name
	if titulo == "" {
		titulo = alb.MBID
	}
	extIDs, err := json.Marshal(map[string]string{"mbid": alb.MBID})
	if err != nil {
		return "", false, err
	}
	novo := s.novoID()
	if _, err := s.escrita().CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:                novo,
		Type:              tipoAlbum,
		Title:             titulo,
		ReleaseDate:       nsOrNull(alb.Year),
		ExtensionDataJson: nsValid(extensaoAlbum(alb)),
		ExternalIdsJson:   nsValid(string(extIDs)),
	}); err != nil {
		return "", false, err
	}
	return novo, true, nil
}

// acharOuCriarFaixa faz o find-or-create IDEMPOTENTE de uma entity `faixa` por MBID (o
// MBID de RECORDING — nunca por número/posição, ADR-0027 item 1). Grava o
// extension_data core.music@1 (track_number/disc_number/disc_total/isrc). Sem refresh
// no reuse, mesma nota de acharOuCriarArtista.
func (s *Servico) acharOuCriarFaixa(ctx context.Context, fx provider.FaixaExterna) (string, bool, error) {
	if existente, err := s.leitura().ObterEntityPorMbid(ctx, nsValid(fx.MBID)); err == nil {
		return existente.ID, false, nil
	} else if !isNoRows(err) {
		return "", false, err
	}
	titulo := fx.Title
	if titulo == "" {
		titulo = fx.MBID
	}
	extIDs, err := json.Marshal(map[string]string{"mbid": fx.MBID})
	if err != nil {
		return "", false, err
	}
	novo := s.novoID()
	if _, err := s.escrita().CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:                novo,
		Type:              tipoFaixa,
		Title:             titulo,
		ExtensionDataJson: nsValid(extensaoFaixa(fx)),
		ExternalIdsJson:   nsValid(string(extIDs)),
	}); err != nil {
		return "", false, err
	}
	return novo, true, nil
}

// extensaoAlbum monta o extension_data do álbum: genre, só quando a fonte trouxe
// (ADR-0027 item 7 — gênero é do álbum/artista, nunca duplicado por faixa).
func extensaoAlbum(alb provider.AlbumExterno) string {
	payload := map[string]any{}
	if alb.Genre != "" {
		payload["genre"] = alb.Genre
	}
	return envelopeMusica(payload)
}

// extensaoFaixa monta o extension_data da faixa: numeração (track_number/disc_number/
// disc_total, ADR-0027 item 1) + isrc quando a fonte trouxe (item 6 — identificador
// auxiliar, não vira coluna).
func extensaoFaixa(fx provider.FaixaExterna) string {
	payload := map[string]any{
		"track_number": fx.TrackNumber,
		"disc_number":  fx.DiscNumber,
		"disc_total":   fx.DiscTotal,
	}
	if fx.ISRC != "" {
		payload["isrc"] = fx.ISRC
	}
	return envelopeMusica(payload)
}

// envelopeMusica embrulha o payload no namespace core.music@1 (internal/musicext) e
// serializa. Erro de marshal (inatingível para estes mapas simples) degrada para "{}".
func envelopeMusica(payload map[string]any) string {
	b, err := json.Marshal(map[string]any{musicext.Namespace: payload})
	if err != nil {
		return "{}"
	}
	return string(b)
}
