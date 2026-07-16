// Package localmedia materializa metadado/arte na pasta REAL da biblioteca
// (briefing 0062), no padrão Kodi/Emby: nomes canônicos de arquivo de arte
// (poster.jpg, fanartN.jpg, seasonNN-poster.jpg, <vídeo>-thumb.jpg), a
// rotação numerada sem reindexação ao trocar a arte "principal", e a escrita
// atômica de arquivo (temp + rename). É um pacote PURO — sem I/O de disco nem
// SQL — exceto EscreverArquivoAtomico (a única função que toca o disco,
// isolada para o chamador testar/mocar as demais sem ela). Os tipos Entity/
// MediaFile aqui são projeções MÍNIMAS de sqlcgen.Entity/sqlcgen.MediaFile
// (id/type e path/state/created_at/entity_id): o pacote não importa
// internal/db/sqlcgen para não acoplar a decisão de nomenclatura ao schema —
// o chamador (internal/server) converte.
package localmedia

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Tipos de entity relevantes para DiretorioDestino/NomeArquivoArte — espelham
// as constantes homônimas de internal/server/entities.go (tipoEntSerie/
// tipoEntTemporada/tipoEntEpisodio), duplicadas aqui porque este pacote não
// importa internal/server (evita ciclo — server importa localmedia).
const (
	TipoSerie     = "tv"
	TipoTemporada = "season"
	TipoEpisodio  = "episode"
)

// Kinds de arte que a tabela kind->nome-base do briefing 0062 §4 conhece.
// Espelham (sem importar) as constantes kindX de internal/server/assets.go.
const (
	KindPoster    = "poster"
	KindFanart    = "fanart"
	KindBackdrop  = "backdrop" // sinônimo de ENTRADA de KindFanart (fanart.tv/TMDB usam os dois termos)
	KindBanner    = "banner"
	KindClearlogo = "clearlogo"
	KindDiscart   = "discart"
	KindLandscape = "landscape"
	KindThumb     = "thumb" // still/thumb de EPISÓDIO
	// KindSeasonPoster é um kind SINTÉTICO — não existe em asset.kind (que
	// continua 'poster' para uma temporada): sinaliza a NomeArquivoArte/
	// IndiceRotacaoLivre que o alvo é o PÔSTER DE UMA TEMPORADA (convenção
	// Kodi season{NN}-poster.jpg, sempre na raiz da série, nunca prefixado
	// por vídeo). O chamador (handler) usa este kind SÓ quando a entity é
	// season E o kind do asset é poster; para os demais casos, o kind vem
	// direto de asset.Kind.
	KindSeasonPoster = "season_poster"
)

// baseDeKind é a tabela FIXA kind->nome-base do briefing 0062 §4. Os kinds
// especiais (thumb de episódio, season poster) são tratados à parte em
// baseComPrefixo — não entram aqui.
var baseDeKind = map[string]string{
	KindPoster:    "poster",
	KindFanart:    "fanart",
	KindBackdrop:  "fanart", // sinônimo de entrada
	KindBanner:    "banner",
	KindClearlogo: "clearlogo",
	KindDiscart:   "discart",
	KindLandscape: "landscape",
}

// extPadraoDeKind são as exceções à extensão default .jpg (briefing 0062 §4).
var extPadraoDeKind = map[string]string{
	KindClearlogo: ".png",
	KindDiscart:   ".png",
}

// ExtensaoPadrao devolve a extensão DEFAULT do kind (.png para clearlogo/
// discart, .jpg para os demais) — o chamador usa quando ainda não conhece a
// extensão real do binário (ex.: antes de baixar).
func ExtensaoPadrao(kind string) string {
	if e, ok := extPadraoDeKind[kind]; ok {
		return e
	}
	return ".jpg"
}

// baseComPrefixo monta o nome-base (sem sufixo de rotação, sem extensão) de
// um arquivo de arte, aplicando as regras dos kinds especiais:
//   - thumb de episódio: SEMPRE prefixado por nomeBaseVideo (sem variante
//     bare — episódio sempre nomeia pelo vídeo), ignorando pastaExclusiva.
//   - season poster: SEMPRE "season{NN}-poster" (nomeBaseVideo aqui é o
//     "season{NN}" pré-formatado pelo chamador — NomeSeasonBase), NUNCA
//     prefixado por vídeo, ignorando pastaExclusiva.
//   - demais kinds: pastaExclusiva=true -> nome bare (<base>); senão
//     prefixado por vídeo (<nomeBaseVideo>-<base>).
func baseComPrefixo(kind string, pastaExclusiva bool, nomeBaseVideo string) string {
	switch kind {
	case KindThumb:
		return nomeBaseVideo + "-thumb"
	case KindSeasonPoster:
		return nomeBaseVideo + "-poster"
	}
	base, ok := baseDeKind[kind]
	if !ok {
		base = kind // kind desconhecido (extensível, ex.: futuro "theme"): usa o próprio nome
	}
	if pastaExclusiva {
		return base
	}
	return nomeBaseVideo + "-" + base
}

// sufixoRotacao devolve o sufixo numérico da rotação: "" quando principal
// (nome bare), ou o índice quando não-principal (indiceRotacao <= 0 vira 1,
// defensivo — nunca gera sufixo "0" ou negativo).
func sufixoRotacao(principal bool, indiceRotacao int) string {
	if principal {
		return ""
	}
	if indiceRotacao <= 0 {
		indiceRotacao = 1
	}
	return strconv.Itoa(indiceRotacao)
}

// NomeArquivoArte monta o NOME (sem diretório) do arquivo de arte canônico
// (briefing 0062 §4). kind é um dos Kind* acima (aceita "backdrop" como
// sinônimo de KindFanart); ext vazio cai na ExtensaoPadrao do kind.
//
// Regras: thumb de episódio nunca tem variante bare (sempre prefixado pelo
// vídeo); season poster nunca é prefixado (usa nomeBaseVideo="season{NN}",
// ver NomeSeasonBase); os demais kinds usam nome bare quando pastaExclusiva
// E principal, prefixados por nomeBaseVideo quando a pasta não é exclusiva,
// e sempre com o sufixo de rotação quando não-principal.
func NomeArquivoArte(kind string, principal bool, indiceRotacao int, pastaExclusiva bool, nomeBaseVideo, ext string) string {
	if ext == "" {
		ext = ExtensaoPadrao(kind)
	}
	return baseComPrefixo(kind, pastaExclusiva, nomeBaseVideo) + sufixoRotacao(principal, indiceRotacao) + ext
}

// NomeSeasonBase formata o "season{NN}" (NN = season_number zero-padded a 2
// dígitos) que NomeArquivoArte espera em nomeBaseVideo quando kind==
// KindSeasonPoster — fonte única desse formato.
func NomeSeasonBase(seasonNumber int) string {
	return "season" + pad2(seasonNumber)
}

// pad2 zero-preenche um inteiro não-negativo a 2 dígitos (season_number costuma
// ser 0-99; acima disso, transborda sem quebrar — só perde o zero-padding).
func pad2(n int) string {
	s := strconv.Itoa(n)
	if len(s) < 2 {
		return strings.Repeat("0", 2-len(s)) + s
	}
	return s
}

// IndiceRotacaoLivre calcula o PRÓXIMO sufixo numérico LIVRE (maior índice já
// usado entre `caminhosExistentes` mais 1 — NUNCA reindexa/compacta buracos,
// briefing 0062 §6) para o nome-base+ext do kind dado. `caminhosExistentes`
// são os `local_path` (ou só o nome do arquivo) dos OUTROS assets já
// materializados do mesmo (entity,kind) — o chamador os lista do banco, este
// pacote não acessa o disco nem o banco. Começa em 1 quando nenhum já tem
// sufixo numérico (bare não conta como índice 0 para este cálculo).
func IndiceRotacaoLivre(caminhosExistentes []string, kind string, pastaExclusiva bool, nomeBaseVideo, ext string) int {
	if ext == "" {
		ext = ExtensaoPadrao(kind)
	}
	prefixo := baseComPrefixo(kind, pastaExclusiva, nomeBaseVideo)
	maior := 0
	for _, caminho := range caminhosExistentes {
		nome := strings.TrimSuffix(filepath.Base(caminho), ext)
		sufixo := strings.TrimPrefix(nome, prefixo)
		if sufixo == "" || sufixo == nome {
			continue // nome bare (indice 0, nao conta) ou nao casa o prefixo
		}
		n, err := strconv.Atoi(sufixo)
		if err != nil || n <= 0 {
			continue
		}
		if n > maior {
			maior = n
		}
	}
	return maior + 1
}

// Entity é a projeção MÍNIMA de sqlcgen.Entity que DiretorioDestino precisa.
type Entity struct {
	ID   string
	Type string
}

// MediaFile é a projeção MÍNIMA de sqlcgen.MediaFile que DiretorioDestino/
// EhPastaExclusiva precisam. CreatedAt é o timestamp ISO8601
// (strftime('%Y-%m-%dT%H:%M:%fZ')) usado pelo resto do banco — comparável
// lexicograficamente sem parse.
type MediaFile struct {
	EntityID  string
	Path      string
	State     string
	CreatedAt string
}

const estadoPresente = "present"

// DiretorioDestino calcula o DIRETÓRIO de destino (pasta real da biblioteca)
// de uma entity, seguindo o briefing 0062 §4:
//
//   - movie/episode (e qualquer outro tipo "de folha"): o diretório do
//     media_file mais antigo (created_at asc, state=present) da PRÓPRIA
//     entity — filtra mediaFiles por EntityID==entity.ID.
//   - tv (série): idem, filtrando por EntityID==entity.ID — no banco, todo
//     media_file de episódio já aponta entity_id para a SÉRIE (vínculo N:M
//     via media_file_entity, ADR-0021), então "a própria entity" da série JÁ
//     é "qualquer descendente" — sem precisar de travessia extra aqui.
//   - season (temporada): a pasta é SEMPRE a raiz da SÉRIE, nunca uma
//     subpasta de temporada — acha o ancestral mais próximo do tipo "tv" na
//     trilha `ancestors` (a mesma trilha RAIZ->PAI que
//     resolver.AncestorRef/montarAncestrais já produzem) e filtra mediaFiles
//     por esse id.
//
// ok=false quando não há nenhum media_file present elegível (sem pasta de
// destino — 404 no handler).
func DiretorioDestino(entity Entity, mediaFiles []MediaFile, ancestors []Entity) (string, bool) {
	mf, ok := ArquivoMaisAntigo(entity, mediaFiles, ancestors)
	if !ok {
		return "", false
	}
	return filepath.Dir(mf.Path), true
}

// ArquivoMaisAntigo devolve o media_file 'present' mais antigo (created_at
// asc) elegível para a entity (mesma resolução de alvo que DiretorioDestino
// usa — própria entity, ou o ancestral "tv" para season). ok=false quando
// nenhum é elegível. Exposta separadamente de DiretorioDestino porque o
// chamador do NFO também precisa do nome-base do vídeo (não só o diretório).
func ArquivoMaisAntigo(entity Entity, mediaFiles []MediaFile, ancestors []Entity) (MediaFile, bool) {
	alvo := entity.ID
	if entity.Type == TipoTemporada {
		if serieID, ok := serieAncestral(ancestors); ok {
			alvo = serieID
		}
	}
	var maisAntigo MediaFile
	achou := false
	for _, mf := range mediaFiles {
		if mf.EntityID != alvo || mf.State != estadoPresente {
			continue
		}
		if !achou || mf.CreatedAt < maisAntigo.CreatedAt {
			maisAntigo, achou = mf, true
		}
	}
	return maisAntigo, achou
}

// serieAncestral acha o id do ancestral mais próximo do tipo "tv" (a série)
// na trilha RAIZ->PAI (ancestors). false quando a trilha não tem série
// (defensivo — season sem série no meio de uma migração incompleta, etc.).
func serieAncestral(ancestors []Entity) (string, bool) {
	for i := len(ancestors) - 1; i >= 0; i-- {
		if ancestors[i].Type == TipoSerie {
			return ancestors[i].ID, true
		}
	}
	return "", false
}

// NomeBaseVideo devolve o nome do arquivo de vídeo SEM extensão (para
// prefixar artes/NFO): filepath.Base(path) menos filepath.Ext(path).
func NomeBaseVideo(path string) string {
	nome := filepath.Base(path)
	return strings.TrimSuffix(nome, filepath.Ext(nome))
}

// EhPastaExclusiva diz se `dir` é a pasta de UM ÚNICO vídeo (media_file.path)
// de QUALQUER entity — pasta com 1 filme/episódio isolado usa nome bare;
// pasta com 2+ vídeos (múltiplas edições da mesma entity, ou entities
// distintas na mesma pasta) usa o nome prefixado pelo vídeo. Conta só os
// media_files state=present (arquivos "fantasma"/missing não ocupam a pasta
// de verdade). `mediaFiles` é TODA a base relevante (de qualquer entity) que
// o chamador já carregou — este pacote não consulta o banco.
func EhPastaExclusiva(dir string, mediaFiles []MediaFile) bool {
	n := 0
	for _, mf := range mediaFiles {
		if mf.State != estadoPresente {
			continue
		}
		if filepath.Dir(mf.Path) == dir {
			n++
			if n > 1 {
				return false
			}
		}
	}
	return n == 1
}

// EscreverArquivoAtomico grava `dados` em `destino` via arquivo temporário no
// MESMO diretório (sufixo ".tmp-<random>") + os.Rename — o único ponto de
// I/O deste pacote. Nunca deixa um leitor ver escrita parcial nem um
// `.tmp-*` órfão em falha (o temporário é removido em qualquer erro antes do
// rename bem-sucedido, briefing 0062 §5). Cria o diretório de destino se
// ainda não existir (mesmo padrão de gravarBinarioAsset/escreverAtomico de
// internal/server, duplicado aqui para o pacote não depender de
// internal/server).
func EscreverArquivoAtomico(destino string, dados []byte) error {
	dir := filepath.Dir(destino)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(destino)+".tmp-*")
	if err != nil {
		return err
	}
	nome := tmp.Name()
	if _, err := tmp.Write(dados); err != nil {
		_ = tmp.Close()
		_ = os.Remove(nome)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(nome)
		return err
	}
	if err := os.Rename(nome, destino); err != nil {
		_ = os.Remove(nome)
		return err
	}
	return nil
}

// RenomearArquivo renomeia `origem` para `destino` (mesmo diretório, rotação
// de arte): sempre os.Rename, nunca copia bytes de novo (briefing 0062 §4).
// Exposta como função nomeada (em vez de o chamador usar os.Rename direto)
// só para o pacote concentrar toda a manipulação de arquivo de arte num só
// lugar.
func RenomearArquivo(origem, destino string) error {
	return os.Rename(origem, destino)
}

// NomeArquivoNFO decide o NOME do .nfo (briefing 0062 §4): "tvshow.nfo" fixo
// para série (na raiz da pasta da série); "<nomeBaseVideo>.nfo" para os
// demais tipos com vídeo próprio (filme/episódio).
func NomeArquivoNFO(entityType, nomeBaseVideo string) string {
	if entityType == TipoSerie {
		return "tvshow.nfo"
	}
	return nomeBaseVideo + ".nfo"
}
