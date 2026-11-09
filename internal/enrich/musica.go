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
	"github.com/rafaelvieiras/my-media-db/internal/matching"
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
	// Inventário de artes do ARTISTA (fanart.tv, briefing 0116): mesmo gatilho
	// best-effort já usado pelo álbum (Cover Art Archive) — cruza pelo
	// external_id_mbid que acharOuCriarArtista acabou de gravar. Cover Art
	// Archive não modela foto/fundo de artista (só capa de release), por isso
	// só a fanart.tv (quando configurada) tem chance de responder aqui.
	s.enriquecerArtes(ctx, artistaID, tipoArtista, langs)
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
	faixaID, _, err := s.acharOuCriarFaixa(ctx, artistaPrincipalMBID, fx)
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
// extension_data core.music@1 (track_number/disc_number/disc_total/isrc + o metadado
// de feat quando `artistaPrincipalMBID` divergir do artist-credit da faixa, ver
// extensaoFaixa). Sem refresh no reuse, mesma nota de acharOuCriarArtista.
func (s *Servico) acharOuCriarFaixa(ctx context.Context, artistaPrincipalMBID string, fx provider.FaixaExterna) (string, bool, error) {
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
		ExtensionDataJson: nsValid(extensaoFaixa(artistaPrincipalMBID, fx)),
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
// auxiliar, não vira coluna). Quando o artist-credit de `fx` DIVERGE do artista
// principal `artistaPrincipalMBID` (feat./colaboração, ADR-0027 item 4), grava também
// o nome/MBID BRUTO da colaboração (musicext.FeatArtistNameKey/FeatArtistMbidKey,
// ADR-0029 item 4, briefing 0115) — METADADO de leitura só, independente de existir ou
// não uma entity/entity_relation music_artista_feat (que só o import de árvore
// completa cria, importarFaixa acima; o import escopado de UMA faixa,
// ImportarFaixaPossuida, NUNCA cria essa entity/relação, só este metadado).
func extensaoFaixa(artistaPrincipalMBID string, fx provider.FaixaExterna) string {
	payload := map[string]any{
		"track_number": fx.TrackNumber,
		"disc_number":  fx.DiscNumber,
		"disc_total":   fx.DiscTotal,
	}
	if fx.ISRC != "" {
		payload["isrc"] = fx.ISRC
	}
	if fx.ArtistaCreditoMBID != "" && fx.ArtistaCreditoMBID != artistaPrincipalMBID {
		if fx.ArtistaCreditoNome != "" {
			payload[musicext.FeatArtistNameKey] = fx.ArtistaCreditoNome
		}
		payload[musicext.FeatArtistMbidKey] = fx.ArtistaCreditoMBID
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

// FaixaAlvo descreve UMA faixa específica já identificada pelo scanner (ADR-0029,
// briefing 0115) para o import ESCOPADO de ImportarFaixaPossuida: ou o MBID EXATO do
// recording (RecordingMBID, o caminho preciso — arquivo com MBID de faixa embutido),
// ou uma identificação TEXTUAL — AlbumNome (obrigatório neste caminho) + FaixaTitulo/
// FaixaNumero para localizar a faixa certa dentro do álbum (fallback sem MBID embutido,
// mesma tag que o scanner já lia para o match textual). RecordingMBID tem prioridade
// quando presente; exatamente um dos dois caminhos é usado por ImportarFaixaPossuida.
type FaixaAlvo struct {
	// RecordingMBID é o MBID de RECORDING conhecido (tag do arquivo). Quando presente,
	// resolve o álbum via MusicaAlbumProvider.BuscarAlbumDaFaixa e localiza a faixa por
	// MBID EXATO — nenhuma outra faixa é considerada.
	RecordingMBID string
	// AlbumNome é o nome do álbum (tag), usado só quando RecordingMBID está vazio —
	// resolve o álbum via MusicaAlbumProvider.BuscarAlbumPorNome.
	AlbumNome string
	// FaixaTitulo é o título da faixa (tag), para localizar a faixa certa dentro do
	// álbum já resolvido por AlbumNome (match por similaridade, mesmo limiar do
	// scanner). Ignorado quando RecordingMBID está presente.
	FaixaTitulo string
	// FaixaNumero é o track_number (tag), desempate quando FaixaTitulo está vazio.
	FaixaNumero int
	// FaixaDisco é o disc_number (tag) da faixa no arquivo; 0 = desconhecido. Só
	// desempata título repetido entre discos de uma reedição (briefing 0138).
	FaixaDisco int
	// ArtistaNome é o artista do álbum (album_artist da tag, com fallback para
	// artist) -- escopa a busca de álbum por título no caminho de compilação/trilha.
	ArtistaNome string
	// DuracaoSegundos é a duração conhecida do arquivo local, em segundos (tag/
	// probe), quando disponível (briefing 0121 §8); 0 = desconhecida. Usada só
	// como sinal de CONFIRMAÇÃO adicional ao título já escolhido por
	// melhorFaixaPorTitulo, nunca de busca/desempate.
	DuracaoSegundos float64
}

// ImportarFaixaPossuida é o import ESCOPADO de UMA faixa que o scanner casou contra um
// arquivo REAL (ADR-0029, briefing 0115): acha-ou-cria só o artista (IDENTIDADE, sem
// loop de álbuns — acharOuCriarArtistaIdentidade), só o ÁLBUM daquela faixa (via a
// capacidade seletiva provider.MusicaAlbumProvider, nunca a árvore inteira) e só a
// PRÓPRIA faixa dentro dele. Ao contrário de importarArtista (árvore inteira, pull
// EXPLÍCITO do dono via busca manual — PERMANECE intocado, continua sendo o único
// caminho que o dispatcher genérico Importar(tipo="artista") usa), nenhum outro
// release do artista é tocado, e nenhuma entity/entity_relation de artista feat é
// criada automaticamente (o metadado da colaboração fica em extension_data, ver
// extensaoFaixa) — é o gatilho AUTOMÁTICO do scan, que tem que ser conservador.
func (s *Servico) ImportarFaixaPossuida(ctx context.Context, source, artistaMBID string, alvo FaixaAlvo, langs []string) (string, bool, error) {
	prov, ok := s.provFonte(source)
	if !ok {
		return "", false, fmt.Errorf("enrich: fonte %q sem provider", source)
	}
	ap, ok := prov.(provider.MusicaAlbumProvider)
	if !ok {
		return "", false, fmt.Errorf("enrich: fonte %q nao suporta album seletivo de musica", source)
	}

	alb, err := s.buscarAlbumAlvo(ctx, ap, artistaMBID, alvo, langs)
	if err != nil {
		return "", false, err
	}
	return s.importarFaixaComAlbum(ctx, prov, artistaMBID, alb, alvo, langs)
}

// ImportarFaixaPorTituloDeAlbum é o caminho de COMPILAÇÃO/TRILHA do import
// escopado (briefing 0136): resolve o álbum pelo TÍTULO (capacidade opcional
// provider.MusicaAlbumPorTituloProvider) e usa o artist-credit que a FONTE
// atribui a ele como artista principal -- em compilação é "Various Artists", que
// a busca de artista do caminho normal não resolve. Depois disso, o resto é o
// MESMO import escopado do ADR-0029 (artista + álbum + a faixa possuída; nunca a
// discografia inteira).
//
// Degradação SILENCIOSA (ok=false, err=nil) quando: o alvo não tem nome de álbum,
// a fonte não implementa a capacidade, nenhum release-group passa do limiar, ou o
// release-group não trouxe artist-credit. O scanner então segue o pipeline (passo
// 4.5/fingerprint ou rascunho) sem log de erro -- mesma semântica de qualquer
// capacidade opcional do projeto.
func (s *Servico) ImportarFaixaPorTituloDeAlbum(ctx context.Context, source string, alvo FaixaAlvo, langs []string) (string, bool, error) {
	if alvo.AlbumNome == "" {
		return "", false, nil
	}
	prov, ok := s.provFonte(source)
	if !ok {
		return "", false, nil
	}
	tp, ok := prov.(provider.MusicaAlbumPorTituloProvider)
	if !ok {
		return "", false, nil
	}
	ac, achou, err := tp.BuscarAlbumPorTitulo(ctx, alvo.AlbumNome, provider.AlvoFaixaAlbum{
		RecordingMBID: alvo.RecordingMBID,
		FaixaTitulo:   alvo.FaixaTitulo,
		FaixaNumero:   alvo.FaixaNumero,
		FaixaDisco:    alvo.FaixaDisco,
		ArtistaNome:   alvo.ArtistaNome,
		DuracaoMs:     int(alvo.DuracaoSegundos * 1000),
	}, langs)
	if err != nil {
		return "", false, err
	}
	if !achou || ac.ArtistaMBID == "" {
		return "", false, nil
	}
	return s.importarFaixaComAlbum(ctx, prov, ac.ArtistaMBID, ac.Album, alvo, langs)
}

// importarFaixaComAlbum é o corpo comum do import escopado (ADR-0029), extraído
// no briefing 0136 para ser reusado pelos DOIS caminhos de resolução do álbum
// (por artista + nome do álbum, e por título do álbum em compilações): garante o
// artista (identidade + inventário de artes), o álbum (find-or-create + artes +
// vínculo artista→álbum) e a faixa possuída (find-or-create + vínculo
// álbum→faixa). Nenhuma lógica nova: é exatamente a sequência que
// ImportarFaixaPossuida já executava, agora sem o "qual álbum" no meio.
func (s *Servico) importarFaixaComAlbum(ctx context.Context, prov provider.Provider, artistaMBID string, alb provider.AlbumExterno, alvo FaixaAlvo, langs []string) (string, bool, error) {
	artistaID, _, err := s.acharOuCriarArtistaIdentidade(ctx, prov, artistaMBID, langs)
	if err != nil {
		return "", false, err
	}
	// Inventário de artes do ARTISTA (fanart.tv, briefing 0116) — mesmo gatilho
	// best-effort do álbum abaixo, ver nota longa em importarArtista.
	s.enriquecerArtes(ctx, artistaID, tipoArtista, langs)

	albumID, _, err := s.acharOuCriarAlbum(ctx, alb)
	if err != nil {
		return "", false, err
	}
	// sort_index=0: import escopado não conhece a posição do álbum na discografia
	// completa (só a árvore inteira, importarArtista, tem essa ordem) — ligarFilho é
	// idempotente por (parent,child,kind), reimportar não duplica o vínculo.
	s.ligarFilho(ctx, artistaID, albumID, musicext.KindArtistaPrincipal, 0)
	s.enriquecerArtes(ctx, albumID, tipoAlbum, langs)

	fx, ok := escolherFaixaAlvo(alb.Faixas, alvo)
	if !ok {
		return "", false, fmt.Errorf("enrich: faixa alvo nao encontrada no album %s", alb.MBID)
	}
	faixaID, criada, err := s.acharOuCriarFaixa(ctx, artistaMBID, fx)
	if err != nil {
		return "", false, err
	}
	s.ligarFilho(ctx, albumID, faixaID, musicext.KindAlbum, fx.TrackNumber)
	return faixaID, criada, nil
}

// acharOuCriarArtistaIdentidade faz o find-or-create do artista SEM tocar em nenhum
// álbum (diferente de importarArtista, que exige a árvore inteira via MusicaProvider):
// reusa a entity existente por MBID (checagem local, sem rede); sem ela, puxa só a
// IDENTIDADE via o core do Provider (BuscarDetalhe/MapearDetalhe — o mesmo par que
// qualquer import genérico usa para o título inicial, 1 chamada barata, SEM
// release-groups) e delega a criação para acharOuCriarArtista.
func (s *Servico) acharOuCriarArtistaIdentidade(ctx context.Context, prov provider.Provider, mbid string, langs []string) (string, bool, error) {
	if existente, err := s.leitura().ObterEntityPorMbid(ctx, nsValid(mbid)); err == nil {
		return existente.ID, false, nil
	} else if !isNoRows(err) {
		return "", false, err
	}
	raw, err := prov.BuscarDetalhe(ctx, mbid, tipoArtista, langs)
	if err != nil {
		return "", false, fmt.Errorf("enrich: buscar detalhe do artista %s: %w", mbid, err)
	}
	campos, err := prov.MapearDetalhe(raw)
	if err != nil {
		return "", false, fmt.Errorf("enrich: mapear detalhe do artista %s: %w", mbid, err)
	}
	nome, _ := campos["title"].(string)
	return s.acharOuCriarArtista(ctx, mbid, nome, "")
}

// buscarAlbumAlvo escolhe entre BuscarAlbumDaFaixa (preferencial, quando o scanner já
// sabe o MBID de recording) e BuscarAlbumPorNome (fallback textual, quando só há o
// nome do álbum da tag) — os dois caminhos que FaixaAlvo modela. No caminho textual,
// repassa FaixaTitulo/FaixaNumero/DuracaoMs (a partir de alvo.DuracaoSegundos) como
// provider.AlvoFaixaAlbum (briefing 0121 §8) — o provider pode então tentar releases
// alternativas do mesmo release-group quando a release padrão não contém a faixa que
// este import está atrás.
func (s *Servico) buscarAlbumAlvo(ctx context.Context, ap provider.MusicaAlbumProvider, artistaMBID string, alvo FaixaAlvo, langs []string) (provider.AlbumExterno, error) {
	if alvo.RecordingMBID != "" {
		alb, achou, err := ap.BuscarAlbumDaFaixa(ctx, alvo.RecordingMBID, langs)
		if err != nil {
			return provider.AlbumExterno{}, err
		}
		if !achou {
			return provider.AlbumExterno{}, fmt.Errorf("enrich: album da faixa %s nao encontrado na fonte", alvo.RecordingMBID)
		}
		return alb, nil
	}
	if alvo.AlbumNome == "" {
		return provider.AlbumExterno{}, fmt.Errorf("enrich: alvo de faixa sem mbid de recording e sem nome de album")
	}
	alb, achou, err := ap.BuscarAlbumPorNome(ctx, artistaMBID, alvo.AlbumNome, provider.AlvoFaixaAlbum{
		FaixaTitulo: alvo.FaixaTitulo,
		FaixaNumero: alvo.FaixaNumero,
		DuracaoMs:   int(alvo.DuracaoSegundos * 1000),
	}, langs)
	if err != nil {
		return provider.AlbumExterno{}, err
	}
	if !achou {
		return provider.AlbumExterno{}, fmt.Errorf("enrich: album %q do artista %s nao encontrado na fonte", alvo.AlbumNome, artistaMBID)
	}
	return alb, nil
}

// desempatePorDiscoEFaixa devolve a ÚNICA faixa empatada cuja posição (disco,
// faixa) casa com a do arquivo; ok=false quando a posição é desconhecida ou
// ambígua dos dois lados (briefing 0138).
func desempatePorDiscoEFaixa(faixas []provider.FaixaExterna, disco, numero int) (provider.FaixaExterna, bool) {
	// 1a tentativa: posição EXATA (disco, faixa). Sem tag de disco, disco 1 é o
	// default seguro (release de disco único também informa 1).
	if numero > 0 {
		alvoDisco := disco
		if alvoDisco <= 0 {
			alvoDisco = 1
		}
		var porPosicao []provider.FaixaExterna
		for _, fx := range faixas {
			if fx.TrackNumber == numero && discoDe(fx) == alvoDisco {
				porPosicao = append(porPosicao, fx)
			}
		}
		if len(porPosicao) == 1 {
			return porPosicao[0], true
		}
	}
	// 2a tentativa: só o DISCO, e só quando o ARQUIVO declarou qual é. A numeração
	// do arquivo vem de tags/pasta e pode não casar com a ordem do release, mas o
	// disco costuma casar -- e é o que separa a faixa do disco 1 da MESMA faixa no
	// disco 2 de uma reedição.
	if disco <= 0 {
		return provider.FaixaExterna{}, false
	}
	var porDisco []provider.FaixaExterna
	for _, fx := range faixas {
		if discoDe(fx) == disco {
			porDisco = append(porDisco, fx)
		}
	}
	if len(porDisco) == 1 {
		return porDisco[0], true
	}
	return provider.FaixaExterna{}, false
}

// discoDe é o disco da faixa na fonte, com 0 tratado como disco 1 (arquivo/release
// de disco único).
func discoDe(fx provider.FaixaExterna) int {
	if fx.DiscNumber <= 0 {
		return 1
	}
	return fx.DiscNumber
}

// duracoesCompativeis diz se a duração do arquivo bate com a da fonte dentro da
// tolerância de confirmação (briefing 0121 §8). Duração desconhecida de qualquer
// dos lados não reprova: o sinal simplesmente não existe.
func duracoesCompativeis(alvoSegundos float64, fonteMs int) bool {
	if alvoSegundos <= 0 || fonteMs <= 0 {
		return true
	}
	diff := alvoSegundos*1000 - float64(fonteMs)
	if diff < 0 {
		diff = -diff
	}
	return diff <= matching.ToleranciaDuracaoSegundos*1000
}

// escolherFaixaAlvo localiza, dentro de `faixas` (as do ÚNICO álbum já resolvido), a
// faixa que corresponde a `alvo`: por MBID EXATO quando alvo.RecordingMBID está
// presente (o caminho preciso, sem ambiguidade possível); por título mais próximo
// (mesmo motor/limiar de matching.LimiarAutoMatch do scanner) quando há FaixaTitulo;
// por track_number como último desempate. ok=false sem nenhum critério aplicável ou
// sem match plausível — nunca aplica um "parecido" (mesmo racional do scanner).
func escolherFaixaAlvo(faixas []provider.FaixaExterna, alvo FaixaAlvo) (provider.FaixaExterna, bool) {
	if alvo.RecordingMBID != "" {
		for _, fx := range faixas {
			if fx.MBID == alvo.RecordingMBID {
				return fx, true
			}
		}
		return provider.FaixaExterna{}, false
	}
	if alvo.FaixaTitulo != "" {
		return melhorFaixaPorTitulo(alvo, faixas)
	}
	if alvo.FaixaNumero > 0 {
		for _, fx := range faixas {
			if fx.TrackNumber == alvo.FaixaNumero {
				return fx, true
			}
		}
	}
	return provider.FaixaExterna{}, false
}

// ImportarFaixaAvulsa importa UMA faixa FALTANTE de um álbum JÁ INTERNO (briefing
// 0115 Parte 2, tela externa "faixas faltando" — GET /external/{source}/album/{id}
// + POST /import type=faixa com parent_id): dado o MBID de RECORDING `recordingMBID`
// (source_id do item na tela) e o id INTERNO do álbum pai `albumID` (a entity
// `album` já catalogada, com external_ids["mbid"] gravado), resolve o álbum de novo
// na fonte via MusicaAlbumProvider.BuscarAlbumPorMbid (reaproveita o mbid do álbum
// pai — nunca enumera a discografia do artista), localiza a faixa por MBID EXATO
// entre as faixas do release, e faz find-or-create + liga ao pai (kind=music_album,
// sort_index=track_number) — mesmo padrão de acharOuCriarFaixa/ligarFilho usado por
// importarFaixa/ImportarFaixaPossuida. O artista PRINCIPAL do álbum (para a detecção
// de feat em extensaoFaixa) é resolvido a partir do pai do álbum na árvore
// (artistaPrincipalDoAlbum) — "" quando ausente degrada em silêncio (a faixa é
// criada sem o metadado de feat, nunca falha só por isso).
func (s *Servico) ImportarFaixaAvulsa(ctx context.Context, source, recordingMBID, albumID string, langs []string) (string, bool, error) {
	prov, ok := s.provFonte(source)
	if !ok {
		return "", false, fmt.Errorf("enrich: fonte %q sem provider", source)
	}
	ap, ok := prov.(provider.MusicaAlbumProvider)
	if !ok {
		return "", false, fmt.Errorf("enrich: fonte %q nao suporta album seletivo de musica", source)
	}

	ids, ok := s.idsExternosDe(ctx, albumID)
	if !ok || ids.Mbid == "" {
		return "", false, fmt.Errorf("enrich: album pai %s sem mbid externo", albumID)
	}

	alb, err := ap.BuscarAlbumPorMbid(ctx, ids.Mbid, langs)
	if err != nil {
		return "", false, err
	}

	fx, achou := escolherFaixaAlvo(alb.Faixas, FaixaAlvo{RecordingMBID: recordingMBID})
	if !achou {
		return "", false, fmt.Errorf("enrich: faixa %s nao encontrada no album %s", recordingMBID, ids.Mbid)
	}

	faixaID, criada, err := s.acharOuCriarFaixa(ctx, s.artistaPrincipalDoAlbum(ctx, albumID), fx)
	if err != nil {
		return "", false, err
	}
	s.ligarFilho(ctx, albumID, faixaID, musicext.KindAlbum, fx.TrackNumber)
	return faixaID, criada, nil
}

// artistaPrincipalDoAlbum resolve o MBID do artista PRINCIPAL do álbum `albumID`
// (via entity_relation kind=music_artista_principal, pai do álbum) — usado só por
// ImportarFaixaAvulsa para detectar feat na faixa avulsa importada (extensaoFaixa).
// "" quando o álbum não tem pai amarrado (não deveria acontecer para um álbum já
// catalogado, mas degrada em silêncio — nunca falha por isso).
func (s *Servico) artistaPrincipalDoAlbum(ctx context.Context, albumID string) string {
	pais, err := s.leitura().ListarRelacoesPais(ctx, albumID)
	if err != nil {
		return ""
	}
	for _, r := range pais {
		if r.Kind != musicext.KindArtistaPrincipal {
			continue
		}
		if ids, ok := s.idsExternosDe(ctx, r.ParentID); ok {
			return ids.Mbid
		}
	}
	return ""
}

// melhorFaixaPorTitulo escolhe, entre `faixas`, a de título mais próximo de `alvo`
// (Jaro-Winkler via internal/textsim, limiar matching.LimiarAutoMatch — mesmo motor/
// número do scanner). Abaixo do limiar: (zero,false), nunca um match "parecido".
// Depois de escolhido o melhor candidato por título, aplica um gate de CONFIRMAÇÃO
// por duração (briefing 0121 §8, matching.ToleranciaDuracaoSegundos): quando
// `duracaoAlvoSegundos` e a duração do candidato (FaixaExterna.DuracaoMs) são AMBAS
// conhecidas e divergem além da tolerância, rejeita mesmo com título batendo. Qualquer
// um dos dois lados desconhecido (0) pula o gate, preservando o comportamento
// anterior.
//
// Briefing 0124: releases reais podem ter 2+ faixas com o MESMO título (ex.:
// remix/edit que ficou com o título "limpo" no MusicBrainz) -- todas empatam no
// maior score. Nesse caso a duração vira critério de DESEMPATE (não só
// confirmação): entre os empatados, só aceita (e devolve) quando exatamente 1 tem
// duração conhecida E dentro da tolerância; sem duração do alvo, ou com 0/2+
// candidatos plausíveis, continua ambíguo -- (zero,false), nunca escolhe
// arbitrariamente pela ordem de iteração.
func melhorFaixaPorTitulo(alvo FaixaAlvo, faixas []provider.FaixaExterna) (provider.FaixaExterna, bool) {
	melhorScore := -1.0
	var empatados []provider.FaixaExterna
	for _, fx := range faixas {
		score := matching.SimilaridadeNormalizada(alvo.FaixaTitulo, fx.Title)
		switch {
		case score > melhorScore:
			melhorScore = score
			empatados = []provider.FaixaExterna{fx}
		case score == melhorScore:
			empatados = append(empatados, fx)
		}
	}
	if melhorScore < matching.LimiarAutoMatch {
		return provider.FaixaExterna{}, false
	}
	if len(empatados) == 1 {
		melhor := empatados[0]
		if alvo.DuracaoSegundos > 0 && melhor.DuracaoMs > 0 {
			diffMs := alvo.DuracaoSegundos*1000 - float64(melhor.DuracaoMs)
			if diffMs < 0 {
				diffMs = -diffMs
			}
			if diffMs > matching.ToleranciaDuracaoSegundos*1000 {
				return provider.FaixaExterna{}, false
			}
		}
		return melhor, true
	}
	// 2+ empatados no topo. (disco, faixa) do ARQUIVO desempata primeiro: numa
	// reedição a mesma faixa aparece em mais de um disco e a posição é o único
	// sinal que diz qual delas o arquivo é (briefing 0138, caso real "The Hurting
	// (Deluxe Edition)": disco 2 não existe no release simples).
	if unica, ok := desempatePorDiscoEFaixa(empatados, alvo.FaixaDisco, alvo.FaixaNumero); ok {
		return unica, duracoesCompativeis(alvo.DuracaoSegundos, unica.DuracaoMs)
	}
	// Sem posição utilizável: a duração desempata, não confirma. Sem duração do
	// alvo não há sinal para decidir -- rejeita (mesmo comportamento seguro de
	// quando não há duração).
	if alvo.DuracaoSegundos <= 0 {
		return provider.FaixaExterna{}, false
	}
	duracaoAlvoMs := alvo.DuracaoSegundos * 1000
	var unico provider.FaixaExterna
	compativeis := 0
	for _, fx := range empatados {
		if fx.DuracaoMs <= 0 {
			continue
		}
		diffMs := duracaoAlvoMs - float64(fx.DuracaoMs)
		if diffMs < 0 {
			diffMs = -diffMs
		}
		if diffMs <= matching.ToleranciaDuracaoSegundos*1000 {
			compativeis++
			unico = fx
		}
	}
	if compativeis == 1 {
		return unico, true
	}
	return provider.FaixaExterna{}, false
}
