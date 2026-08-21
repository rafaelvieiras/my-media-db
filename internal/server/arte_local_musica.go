package server

// arte_local_musica.go implementa a DESCOBERTA DE ARTE LOCAL para artista/album de
// música (briefing 0119): o mesmo mecanismo que filme/série já têm (poster.jpg/
// folder.jpg na pasta da obra, scanner.descobrirPoster/RefreshLocalAsset,
// asset origin='local') nunca tinha sido ligado à vertical de música -- artista/album
// NUNCA têm media_file DIRETO (ADR-0027, só a faixa liga), então a pasta "da obra"
// precisa ser DERIVADA do arquivo de uma faixa descendente: a pasta do ÁLBUM é o
// diretório do arquivo da faixa; a pasta do ARTISTA é um nível acima (o pai da pasta
// do álbum). Cada requisição de fonte de arte agora tem TRÊS fontes possíveis, todas
// se complementando (nenhuma delas exclusiva) -- local (esta), MusicBrainz/Cover Art
// Archive e fanart.tv (enriquecerArtes, 0048/0106/0117) -- e origin='local' VENCE
// origin='external' na precedência de serviço (escolherAssetServido, assets.go): uma
// imagem que o dono já tem na pasta é tratada como decisão dele, não clobrada por
// nenhum provider.

import (
	"context"
	"log"
	"path/filepath"
)

// DescobrirArteLocalMusica varre TODA entity artista/album com ao menos uma faixa
// descendente possuída (ListarEntitiesMusicaComFaixaPossuida, mesmo universo do job
// midias_faltantes/0116) e reconfere a pasta correspondente em busca de um pôster
// local (scanner.RefreshLocalAsset, mesmo mecanismo de filme/série -- 0088).
// Idempotente (RefreshLocalAsset faz UPSERT por (entity,kind,origin)) e best-effort:
// falha pontual numa entity é logada e pulada, nunca aborta o lote. err só propaga em
// falha real de LISTAGEM (banco fora do ar).
func (s *Server) DescobrirArteLocalMusica(ctx context.Context) (encontrados int, err error) {
	if s.scanner == nil {
		return 0, nil
	}
	ids, err := s.leitura().ListarEntitiesMusicaComFaixaPossuida(ctx)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		ok, err := s.descobrirArteLocalMusicaDe(ctx, id)
		if err != nil {
			log.Printf("arte local musica: falha ao verificar a entity %q (best-effort, ignorado): %v", id, err)
			continue
		}
		if ok {
			encontrados++
		}
	}
	return encontrados, nil
}

// descobrirArteLocalMusicaDe resolve a pasta canônica de UMA entity (artista/album)
// a partir do path de uma faixa descendente qualquer (ObterCaminhoDescendentePresente)
// e delega a scanner.RefreshLocalAsset -- que já sabe procurar poster.jpg/folder.jpg
// na pasta e fazer o UPSERT do asset origin='local'. `p` é construído para que
// filepath.Dir(p) resolva exatamente à pasta certa: para álbum, o path DA FAIXA (seu
// dir já É a pasta do álbum); para artista, o path da PASTA DO ÁLBUM (um nível acima
// dela é a pasta do artista) -- RefreshLocalAsset sempre aplica filepath.Dir(p)
// internamente, então "p" aqui é só um vetor pra chegar na pasta certa, nunca um
// arquivo real que precise existir.
func (s *Server) descobrirArteLocalMusicaDe(ctx context.Context, entityID string) (bool, error) {
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		return false, err
	}
	faixaPath, err := s.leitura().ObterCaminhoDescendentePresente(ctx, entityID)
	if err != nil {
		return false, nil // sem descendente possuido (defensivo -- nao deveria acontecer aqui)
	}

	var p string
	switch ent.Type {
	case tipoEntAlbum:
		p = faixaPath // filepath.Dir(faixaPath) = pasta do album
	case tipoEntArtista:
		p = filepath.Dir(faixaPath) // filepath.Dir(pasta do album) = pasta do artista
	default:
		return false, nil
	}

	return s.scanner.RefreshLocalAsset(ctx, entityID, []string{p})
}
