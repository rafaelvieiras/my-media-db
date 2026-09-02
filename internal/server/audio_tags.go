package server

// audio_tags.go implementa POST /api/v1/entities/{id}/audio-tags/write
// (briefing 0125, menu Gerenciar > "Gravar metadados no arquivo"): regrava a
// PRÓPRIA tag do arquivo de áudio (ID3v2/Vorbis comment, internal/audiotag)
// com o metadado JÁ RESOLVIDO (override ⟵ externo ⟵ arquivo, o mesmo caminho
// de resolverEntity que a tela de detalhe usa) — nunca um valor bruto de
// provider. Para `faixa`, grava só o arquivo dela; para `album`, cascateia
// para TODAS as faixas POSSUÍDAS (best-effort: uma falha isolada não aborta
// as demais), respeitando artist-credit divergente (feat., ADR-0027 item 4)
// na tag de ARTISTA daquela faixa específica — álbum/ano/gênero/capa
// continuam vindo do álbum. `type=='artista'` (ou qualquer outro) é 422: sem
// granularidade de "obra" coerente para cascatear (briefing 0125 §3). Ao
// contrário do padrão "sem cascata automática" do briefing 0088, aqui álbum
// cascatear é a ÚNICA interpretação coerente da ação (álbum nunca tem
// arquivo próprio, ADR-0027) — não é uma cascata em massa arbitrária.
//
// Depois de escrever cada faixa, sincroniza media_file.hash/mtime/size
// reaproveitando recheckEntityFilesPorFonte (a MESMA função que
// refresh_files.go já usa, briefing 0088) a partir de um NOVO caller — sem
// query nova, o próximo scan incremental não vê o arquivo regravado como uma
// "mudança" de surpresa.

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/audiotag"
	"github.com/rafaelvieiras/my-media-db/internal/musicext"
	"github.com/rafaelvieiras/my-media-db/internal/resolver"
)

// audioTagsWriteResultDTO é a resposta de POST /entities/{id}/audio-tags/write
// (briefing 0125 §4): resumo informativo, mesmo espírito solto do 0088 —
// `falhas` traz "faixaID: motivo" para cada faixa que não regravou.
type audioTagsWriteResultDTO struct {
	FaixasRegravadas int      `json:"faixas_regravadas"`
	Falhas           []string `json:"falhas"`
}

// writeAudioTags trata POST /api/v1/entities/{id}/audio-tags/write. 404 se a
// entity não existe; 422 se `type` não for `faixa`/`album`; 503 se o ffmpeg
// (mecanismo de escrita, internal/audiotag) não estiver disponível no
// ambiente. Sem body.
func (s *Server) writeAudioTags(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	writer := s.audioTagWriter
	if writer == nil || !writer.Disponivel() {
		writeError(w, http.StatusServiceUnavailable, "audiotag_unavailable",
			"gravacao de tag de audio indisponivel (ffmpeg ausente no ambiente)")
		return
	}

	var faixaIDs []string
	var albumID string
	switch ent.Type {
	case tipoEntFaixa:
		faixaIDs = []string{id}
		albumID = s.albumPaiDaFaixa(ctx, id)
	case tipoEntAlbum:
		albumID = id
		faixaIDs = s.faixasPossuidasDoAlbum(ctx, id)
	default:
		writeError(w, http.StatusUnprocessableEntity, "unsupported_type",
			"gravacao de tag de audio so suporta entity type=faixa ou type=album")
		return
	}

	albumCtx, err := s.montarContextoAlbum(ctx, albumID)
	if err != nil {
		respondErroInterno(w)
		return
	}

	dto := audioTagsWriteResultDTO{Falhas: []string{}}
	for _, faixaID := range faixaIDs {
		if err := s.gravarTagDaFaixa(ctx, writer, faixaID, albumCtx); err != nil {
			log.Printf("audio-tags/write: faixa %q falhou (segue -- best-effort): %v", faixaID, err)
			dto.Falhas = append(dto.Falhas, fmt.Sprintf("%s: %v", faixaID, err))
			continue
		}
		dto.FaixasRegravadas++
	}
	writeJSON(w, http.StatusOK, dto)
}

// contextoAlbum agrupa o que é comum a TODAS as faixas de um álbum sendo
// regravadas (evita resolver álbum/artista/contagem de faixas de novo a
// cada faixa da cascata). AlbumEnt/ArtistaEnt zero-value (ID=="") quando o
// álbum não foi encontrado ou a faixa não tem álbum-pai (defensivo -- o
// modelo, ADR-0027, garante que toda faixa tem um álbum-pai).
type contextoAlbum struct {
	albumEnt   resolver.Entidade
	artistaEnt resolver.Entidade
	trackTotal int
}

// albumPaiDaFaixa sobe de uma faixa para o álbum-pai (entity_relation
// kind=music_album) — "" quando a faixa não tem (não deveria acontecer no
// modelo, mas degrada em vez de falhar a chamada inteira).
func (s *Server) albumPaiDaFaixa(ctx context.Context, faixaID string) string {
	pais, err := s.leitura().ListarRelacoesPais(ctx, faixaID)
	if err != nil {
		return ""
	}
	for _, p := range pais {
		if p.Kind == musicext.KindAlbum {
			return p.ParentID
		}
	}
	return ""
}

// faixasPossuidasDoAlbum lista os ids das faixas-filho (entity_relation
// kind=music_album, mesma ordem de sort_index=track_number que
// ListarRelacoesFilhas já devolve) que têm ao menos um media_file 'present'
// — a cascata do briefing 0125 só grava faixa POSSUÍDA (sem arquivo, não há
// o que regravar). Erro de leitura degrada para lista vazia (best-effort,
// mesmo espírito do resto do endpoint).
func (s *Server) faixasPossuidasDoAlbum(ctx context.Context, albumID string) []string {
	rels, err := s.leitura().ListarRelacoesFilhas(ctx, albumID)
	if err != nil {
		log.Printf("audio-tags/write: listar faixas do album %q falhou: %v", albumID, err)
		return nil
	}
	var ids []string
	for _, r := range rels {
		if r.Kind != musicext.KindAlbum {
			continue
		}
		files, err := s.leitura().ListarMediaFilesPorEntity(ctx, r.ChildID)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.State == "present" {
				ids = append(ids, r.ChildID)
				break
			}
		}
	}
	return ids
}

// montarContextoAlbum resolve o álbum (RESOLVIDO, mesmo caminho de
// resolverEntity) e o artista PRINCIPAL dele (entity_relation
// kind=music_artista_principal), mais o total de faixas do álbum (contagem
// de filhos kind=music_album — vira o "total de faixas" da tag, campo que o
// modelo, ADR-0027, não persiste por faixa). albumID=="" devolve zero-value
// sem erro (faixa avulsa sem álbum-pai, caso defensivo). Erro só propaga se
// a PRÓPRIA resolução do álbum falhar de verdade (não NoRows -- esse caso já
// degrada para zero-value, o álbum pode ter sumido entre a checagem do
// chamador e agora).
func (s *Server) montarContextoAlbum(ctx context.Context, albumID string) (contextoAlbum, error) {
	if albumID == "" {
		return contextoAlbum{}, nil
	}
	albumEnt, achado, err := s.resolverEntity(ctx, albumID, "", false)
	if err != nil {
		return contextoAlbum{}, err
	}
	if !achado {
		return contextoAlbum{}, nil
	}

	var artistaEnt resolver.Entidade
	if pais, err := s.leitura().ListarRelacoesPais(ctx, albumID); err == nil {
		for _, p := range pais {
			if p.Kind != musicext.KindArtistaPrincipal {
				continue
			}
			if a, ok, err := s.resolverEntity(ctx, p.ParentID, "", false); err == nil && ok {
				artistaEnt = a
			}
			break
		}
	}

	total := 0
	if rels, err := s.leitura().ListarRelacoesFilhas(ctx, albumID); err == nil {
		for _, r := range rels {
			if r.Kind == musicext.KindAlbum {
				total++
			}
		}
	}

	return contextoAlbum{albumEnt: albumEnt, artistaEnt: artistaEnt, trackTotal: total}, nil
}

// gravarTagDaFaixa resolve o arquivo present + o metadado resolvido de UMA
// faixa e chama o Writer; em caso de sucesso, sincroniza media_file via
// recheckEntityFilesPorFonte (0088, reaproveitada). Erro devolvido é só
// dessa faixa (o chamador -- writeAudioTags -- trata best-effort, não aborta
// a cascata).
func (s *Server) gravarTagDaFaixa(ctx context.Context, writer *audiotag.Writer, faixaID string, albumCtx contextoAlbum) error {
	files, err := s.leitura().ListarMediaFilesPorEntity(ctx, faixaID)
	if err != nil {
		return err
	}
	var path string
	for _, f := range files {
		if f.State == "present" {
			path = f.Path
			break
		}
	}
	if path == "" {
		return errors.New("sem arquivo present vinculado")
	}

	faixaEnt, achada, err := s.resolverEntity(ctx, faixaID, "", false)
	if err != nil {
		return err
	}
	if !achada {
		return errors.New("faixa nao encontrada")
	}

	meta := s.montarMetadadoFaixa(ctx, faixaEnt, albumCtx)
	if err := writer.Escrever(ctx, path, meta); err != nil {
		return err
	}

	// Sincroniza media_file.hash/mtime/size PÓS-escrita (briefing 0125 §6):
	// best-effort -- a tag já foi gravada com sucesso; uma falha aqui só
	// significa que o próximo scan incremental reconfere o arquivo por
	// conta própria (o comportamento de ANTES deste briefing).
	s.recheckEntityFilesPorFonte(ctx, files)
	return nil
}

// musicaNamespace extrai o namespace core.music@1 já RESOLVIDO (map[string]any,
// veio de resolver.Entidade.ExtensionData -- ao contrário de
// musicext.ExtractNamespace, que lê o extension_data_json CRU da entity;
// aqui o valor já passou pela composição override⟵externo⟵arquivo). nil
// quando a entity não tem o namespace.
func musicaNamespace(ent resolver.Entidade) map[string]any {
	if ent.ExtensionData == nil {
		return nil
	}
	ns, _ := ent.ExtensionData[musicext.Namespace].(map[string]any)
	return ns
}

// featCredenciadoPorRelacao sobe de uma faixa para o artista de COLABORAÇÃO
// (entity_relation kind=music_artista_feat, ADR-0027 item 4) quando existir
// -- devolve o nome/MBID RESOLVIDOS dele (mesmo caminho de resolverEntity).
// ("", "") quando não há relação de feat (o caso comum).
func (s *Server) featCredenciadoPorRelacao(ctx context.Context, faixaID string) (nome, mbid string) {
	pais, err := s.leitura().ListarRelacoesPais(ctx, faixaID)
	if err != nil {
		return "", ""
	}
	for _, p := range pais {
		if p.Kind != musicext.KindArtistaFeat {
			continue
		}
		ent, ok, err := s.resolverEntity(ctx, p.ParentID, "", false)
		if err != nil || !ok {
			continue
		}
		return ent.Title, ent.ExternalIDs["mbid"]
	}
	return "", ""
}

// montarMetadadoFaixa monta o audiotag.Metadado de UMA faixa a partir da
// entity RESOLVIDA dela (faixaEnt) + o contexto do álbum já resolvido
// (albumCtx, compartilhado entre todas as faixas de uma cascata). O artista
// da tag é o da faixa (respeitando divergência feat., ADR-0027 item 4: a
// entity_relation kind=music_artista_feat vence, com fallback para as
// chaves feat_artist_name/feat_artist_mbid do extension_data quando não há
// relação -- briefing 0125 §4/ADR-0029 item 4); AlbumArtist é sempre o
// artista PRINCIPAL do álbum.
func (s *Server) montarMetadadoFaixa(ctx context.Context, faixaEnt resolver.Entidade, albumCtx contextoAlbum) audiotag.Metadado {
	nsFaixa := musicaNamespace(faixaEnt)
	nsAlbum := musicaNamespace(albumCtx.albumEnt)

	trackNumber, _ := musicext.AsInt(nsFaixa["track_number"])
	discNumber, _ := musicext.AsInt(nsFaixa["disc_number"])
	discTotal, _ := musicext.AsInt(nsFaixa["disc_total"])
	isrc, _ := musicext.AsString(nsFaixa["isrc"])
	genre, _ := musicext.AsString(nsAlbum["genre"])

	featRelNome, featRelMbid := s.featCredenciadoPorRelacao(ctx, faixaEnt.ID)
	featNsNome, _ := musicext.AsString(nsFaixa[musicext.FeatArtistNameKey])
	featNsMbid, _ := musicext.AsString(nsFaixa[musicext.FeatArtistMbidKey])

	artista := albumCtx.artistaEnt.Title
	artistaMbid := albumCtx.artistaEnt.ExternalIDs["mbid"]
	if nome := primeiroNaoVazio(featRelNome, featNsNome); nome != "" {
		artista = nome
	}
	if mbid := primeiroNaoVazio(featRelMbid, featNsMbid); mbid != "" {
		artistaMbid = mbid
	}

	albumArtistaMbid := albumCtx.artistaEnt.ExternalIDs["mbid"]

	return audiotag.Metadado{
		Title:            faixaEnt.Title,
		Artist:           artista,
		Album:            albumCtx.albumEnt.Title,
		AlbumArtist:      albumCtx.artistaEnt.Title,
		Date:             strOrEmpty(albumCtx.albumEnt.ReleaseDate),
		Genre:            genre,
		TrackNumber:      trackNumber,
		TrackTotal:       albumCtx.trackTotal,
		DiscNumber:       discNumber,
		DiscTotal:        discTotal,
		Compilation:      albumArtistaMbid != "" && albumArtistaMbid == musicext.MBIDVariousArtists,
		ISRC:             isrc,
		MBRecordingID:    faixaEnt.ExternalIDs["mbid"],
		MBReleaseGroupID: albumCtx.albumEnt.ExternalIDs["mbid"],
		MBArtistID:       artistaMbid,
		MBAlbumArtistID:  albumArtistaMbid,
		CoverPath:        s.capaDoAlbum(ctx, albumCtx.albumEnt.ID),
	}
}

// capaDoAlbum resolve o path em disco da capa VENCEDORA do álbum (mesma
// precedência de serviço de arte de internal/server/assets.go,
// escolherAssetServido -- preferida→user→local→external→base), validado
// contra as raízes confiáveis (mesma guarda de path-traversal do
// getAssetFile) e confirmado no disco. "" quando o álbum não tem nenhum
// poster ou o path não passa na guarda/não existe mais (best-effort: a
// tag sai sem capa em vez de falhar a faixa inteira).
func (s *Server) capaDoAlbum(ctx context.Context, albumID string) string {
	if albumID == "" {
		return ""
	}
	assets, err := s.leitura().ListarAssetsPorEntity(ctx, albumID)
	if err != nil {
		return ""
	}
	asset, ok := escolherAssetServido(assets, kindPoster)
	if !ok {
		return ""
	}
	raizes, err := s.raizesConfiaveis(ctx)
	if err != nil || !caminhoSobRaizes(asset.PathOrUri, raizes) {
		return ""
	}
	if _, err := os.Stat(asset.PathOrUri); err != nil {
		return ""
	}
	return asset.PathOrUri
}

// primeiroNaoVazio devolve `a` quando não-vazio, senão `b`.
func primeiroNaoVazio(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// strOrEmpty deref um *string ("" quando nil) -- o release_date resolvido
// (resolver.Entidade.ReleaseDate) é *string.
func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
