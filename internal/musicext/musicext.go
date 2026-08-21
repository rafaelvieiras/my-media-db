// Package musicext concentra a LEITURA do extension_data da vertical de
// música (namespace core.music@1, ADR-0027, briefing 0105): extrair o objeto
// do namespace e coagir os números de JSON — mesmo molde do internal/tvext
// (série). Também guarda os `kind` de entity_relation e o MBID canônico de
// "Various Artists" usados para amarrar a hierarquia artista→álbum→faixa.
package musicext

import (
	"database/sql"
	"encoding/json"
)

// Namespace do extension_data desta vertical.
const Namespace = "core.music@1"

// Kinds de entity_relation que amarram a hierarquia (ADR-0027 §2/§4).
const (
	// KindAlbum liga álbum (parent) → faixa (child); sort_index = track_number.
	KindAlbum = "music_album"
	// KindArtistaPrincipal liga artista (parent) → álbum ou faixa (child);
	// sort_index = ordem de billing.
	KindArtistaPrincipal = "music_artista_principal"
	// KindArtistaFeat liga artista (parent) → faixa (child): colaboração
	// pontual, quando o artista da faixa diverge do artista principal do álbum.
	KindArtistaFeat = "music_artista_feat"
)

// Chaves do extension_data core.music@1 que guardam o nome/MBID BRUTO da
// colaboração (feat.) de uma faixa, quando o artist-credit dela diverge do
// artista PRINCIPAL do álbum (ADR-0027 item 4, ADR-0029 item 4, briefing 0115).
// São METADADO de leitura só -- gravadas pelo import ESCOPADO (local-first, uma
// faixa possuída por vez, internal/enrich/musica.go ImportarFaixaPossuida) SEM
// criar entity/entity_relation(kind=music_artista_feat) automaticamente; o
// import de ÁRVORE completa (importarArtista, pull explícito do dono) continua
// criando a entity+relação como antes (ADR-0029 item 5) e também grava estas
// mesmas chaves (redundante com a relação, mas sem custo e sem contradição).
const (
	// FeatArtistNameKey é o nome exibível do artista colaborador, quando a fonte
	// o trouxe. Ausente do payload quando não há divergência de artist-credit.
	FeatArtistNameKey = "feat_artist_name"
	// FeatArtistMbidKey é o MBID do artista colaborador na fonte. Sempre presente
	// junto de FeatArtistNameKey (a divergência só é detectável com o MBID).
	FeatArtistMbidKey = "feat_artist_mbid"
)

// MBIDVariousArtists é o MBID canônico que a própria MusicBrainz usa para o
// artista de agrupamento de compilações/trilhas sonoras. Usar este valor ao
// ligar um álbum de compilação a "Various Artists" — não é caso especial de
// código, é o id certo (ADR-0027 §3/MU4).
const MBIDVariousArtists = "89ad4ac3-39f7-470e-963a-56509c546377"

// FonteMusicBrainz/ChaveExternalIDMbid documentam a ÚNICA exceção à convenção
// "chave do external_ids == Provider.Nome()" que tmdb/imdb/tvdb seguem em todo
// o resto do sistema: o id de MusicBrainz sempre vive sob a chave "mbid"
// (ADR-0027 item 6, coluna gerada external_id_mbid + índice), nunca sob o nome
// literal da fonte "musicbrainz" — Nome() só identifica a FONTE (media_external
// .source, rastreio de crédito), não a chave de identidade. acharOuCriarArtista/
// Album/Faixa (musica.go) já gravam direto sob "mbid"; o gap fechado no
// briefing 0119 é o caminho GENÉRICO de enriquecimento (mesclarExternalIds,
// acharColisaoExterna) que, sem este mapeamento, gravaria/consultaria sob
// "musicbrainz" — chave que idsExternosDe (enrich/artes.go) e a coluna gerada
// nunca leem.
const (
	FonteMusicBrainz    = "musicbrainz"
	ChaveExternalIDMbid = "mbid"
)

// ExtractNamespace extrai o objeto do namespace (ex.: "core.music@1") de um
// extension_data_json (envelope namespace→objeto). Devolve nil quando o raw é
// NULL/vazio, ilegível, ou não contém o namespace — o chamador degrada (a
// entity sem aquele namespace simplesmente não tem os dados).
func ExtractNamespace(raw sql.NullString, ns string) map[string]any {
	if !raw.Valid || raw.String == "" {
		return nil
	}
	var env map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw.String), &env); err != nil {
		return nil
	}
	bruto, ok := env[ns]
	if !ok {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(bruto, &m); err != nil {
		return nil
	}
	return m
}

// AsInt coage um valor decodificado de JSON para int (números de JSON viram
// float64 no unmarshal genérico). (0,false) quando não é número.
func AsInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}

// AsString coage um valor decodificado de JSON para string. ("",false) quando
// não é string (ex.: isrc).
func AsString(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}
