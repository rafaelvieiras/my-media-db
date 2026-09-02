// Package audiotag REGRAVA a tag de áudio de um arquivo (.mp3/.flac/.ogg) com
// o metadado já RESOLVIDO de uma faixa (briefing 0125) — título, artista,
// álbum, ano, faixa/disco, gênero, MBIDs, ISRC e capa embutida — para que
// QUALQUER player/servidor que leia tag de áudio (LMS, Navidrome, gonic,
// Supysonic, clientes Subsonic/OpenSubsonic, Kodi em modo tag-scan, Jellyfin)
// veja o metadado do MyMediaDB sem depender de NFO.
//
// # Decisão de mecanismo (briefing 0125 §6)
//
// O projeto hoje só LÊ tag via ffprobe (subprocesso, internal/mediainfo); não
// havia lib de ESCRITA em uso. O briefing levantou duas opções: (a) libs Go
// dedicadas de ID3v2 + Vorbis comment; (b) subprocesso ffmpeg. Optou-se por
// (b): `apk add ffmpeg` no Dockerfile FINAL já instala o binário `ffmpeg`
// (junto do `ffprobe` que o mediainfo já usa) — ESCREVER via ffmpeg não
// introduz NENHUMA dependência binária nova, ao contrário de (a), que exigiria
// duas libs Go separadas (ID3v2 e Vorbis comment/FLAC) para cobrir MP3 e
// FLAC/OGG. `ffmpeg -map_metadata -1 -c:a copy -metadata k=v ...` reescreve o
// container SEM recodificar o áudio (lossless, rápido) e resolve os 3
// formatos com o MESMO mecanismo — só a convenção de NOME de campo muda por
// formato (ver escreverID3/escreverVorbis).
//
// Capa embutida: para MP3, a capa entra como um segundo input mapeado como
// stream de vídeo MJPEG com `-disposition:v attached_pic` (vira o frame APIC
// do ID3v2 — validado empiricamente: `ffprobe` decodifica de volta um stream
// video/mjpeg com disposition attached_pic=1). Para FLAC/OGG, o container não
// aceita um stream de vídeo de verdade (ffmpeg recusa mux de mjpeg em ogg); a
// capa entra como o valor de UM comentário Vorbis, `METADATA_BLOCK_PICTURE`,
// contendo o bloco binário da spec do FLAC (tipo+mime+descrição+dimensões
// [zeradas, a spec permite "desconhecido"]+bytes da imagem) codificado em
// base64 — montado à mão (montarBlocoImagem) porque o ffmpeg não gera esse
// bloco sozinho a partir de um input de imagem comum; validado lendo de volta
// com `ffprobe -show_streams`, que decodifica o bloco corretamente tanto em
// `.flac` quanto em `.ogg`.
//
// Pure em relação ao resolver: este pacote só recebe o metadado JÁ resolvido
// (Metadado) — não decide precedência override/externo/arquivo, não conhece
// entity/resolver. Quem monta o Metadado é o chamador (internal/server).
package audiotag

// Metadado é o valor JÁ RESOLVIDO (override ⟵ externo ⟵ arquivo, AGENTS.md
// §1) a gravar na tag de UMA faixa. Todo campo é opcional: string vazia ou
// número <= 0 simplesmente não é escrito naquele frame/comentário (nunca gera
// um valor "0"/"" no arquivo). CoverPath vazio = nenhuma capa é embutida (e
// uma capa preexistente no arquivo é removida na regravação — a escrita é
// sempre uma substituição TOTAL da tag antiga, nunca um merge, ver
// Writer.Escrever).
type Metadado struct {
	// Title/Artist/Album são o título da FAIXA, o artista CREDITADO nesta
	// faixa (pode divergir do AlbumArtist em faixas feat./colaboração,
	// ADR-0027 item 4 — quem resolve essa divergência é o CHAMADOR) e o
	// título do ÁLBUM.
	Title  string
	Artist string
	Album  string
	// AlbumArtist é o artista PRINCIPAL do álbum (billing principal),
	// sempre o mesmo em toda faixa do álbum — distinto de Artist quando há
	// divergência de artist-credit.
	AlbumArtist string
	// Date é o ano/data de lançamento do ÁLBUM (não da faixa — a faixa não
	// tem data própria no modelo, ADR-0027).
	Date string
	// Genre é o gênero do ÁLBUM (ADR-0027 item 7: gênero não é por faixa).
	Genre string
	// TrackNumber/TrackTotal/DiscNumber/DiscTotal são a numeração da faixa
	// dentro do álbum. TrackTotal é o total de faixas do álbum (derivado
	// pelo chamador, não faz parte do extension_data da faixa); DiscTotal
	// vem do extension_data da própria faixa (ADR-0027 item 1). <= 0 = não
	// escreve aquele campo.
	TrackNumber int
	TrackTotal  int
	DiscNumber  int
	DiscTotal   int
	// Compilation marca a flag de compilação (o álbum está ligado ao
	// artista "Various Artists", musicext.MBIDVariousArtists) — escrito
	// como um comentário/frame de texto best-effort (ver comentário em
	// metadataID3; ffmpeg não mapeia para o frame binário TCMP do iTunes).
	Compilation bool
	// ISRC é o identificador auxiliar da FAIXA (ADR-0027 item 6).
	ISRC string
	// MBRecordingID/MBReleaseGroupID/MBArtistID/MBAlbumArtistID são os
	// MusicBrainz IDs conhecidos pelo MyMediaDB (ADR-0027: só rastreamos
	// recording/release-group/artist — não há um MBID de RELEASE distinto
	// persistido; ver nota em metadataID3/metadataVorbis sobre reusar o de
	// release-group também como "Album Id"). MBArtistID é o MBID do
	// artista CREDITADO nesta faixa (mesma divergência feat. de Artist);
	// MBAlbumArtistID é sempre o do artista principal do álbum.
	MBRecordingID    string
	MBReleaseGroupID string
	MBArtistID       string
	MBAlbumArtistID  string
	// CoverPath é o caminho em disco (já resolvido pelo chamador — a
	// mesma precedência de arte servida do álbum, escolherAssetServido em
	// internal/server/assets.go) de uma imagem JPEG/PNG a embutir como
	// capa. "" = nenhuma capa embutida.
	CoverPath string
}
