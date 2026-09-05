package server

// midias_faltantes.go implementa a materialização "sob demanda" reusada pelo
// job midias_faltantes (briefing 0064, internal/job/midiasfaltantes): para
// UMA entity, materializa fisicamente na pasta REAL da biblioteca a arte HOJE
// VENCEDORA (mesmo agregador da galeria, montarGruposArtwork/artwork.go,
// 0048) de cada kind relevante, e o .nfo quando ausente (mesma função
// gravarNFO do 0062, localmedia_handlers.go). Nunca escolhe um candidato
// diferente do que a UI já mostraria como topo -- só aplica no disco o que já
// é a escolha vigente (briefing 0064 §3, explicitamente FORA de escopo:
// auto-selecionar quando NADA foi resolvido ainda).

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// kindsMidiasFaltantes são os kinds de arte de NÍVEL DE OBRA que o job
// materializa (briefing 0064 §4 cita "poster, fanart, banner, clearlogo,
// discart, landscape" -- os três últimos usam a NOMENCLATURA DE ARQUIVO do
// internal/localmedia (KindClearlogo/KindDiscart/KindLandscape), não o
// vocabulário de asset.kind gravado no banco e agrupado por
// montarGruposArtwork (assets.go/fanarttv.go: poster, fanart, banner, logo,
// clearart, disc, thumb, characterart -- "clearlogo"/"discart"/"landscape"
// NUNCA aparecem como chave desse agregador). Decisão de implementação (não
// 100% explícita no briefing, resolvendo essa divergência de vocabulário):
// usamos aqui o vocabulário REAL do agregador, mapeando pela intenção --
// poster, fanart, banner, logo (equivalente de "clearlogo" na pasta real),
// disc (equivalente de "discart") e thumb (arte 16:9 de nível de série/filme,
// o equivalente mais próximo de "landscape" no vocabulário existente:
// kindsTV mapeia "tvthumb"->"thumb" na fanart.tv). `still` fica de fora --
// é SEMPRE por EPISÓDIO (briefing 0044), fora do escopo deste job de nível
// de obra.
var kindsMidiasFaltantes = []string{kindPoster, kindFanart, kindBanner, kindLogo, kindDisc, kindThumb}

// ListarEntitiesComMediaFilePresente devolve os ids de TODA entity elegível
// para o job midias_faltantes (briefing 0064, estendido pelo 0081 e 0116):
// movie/tv com media_file 'present' direto (ListarEntityIDsComMediaFilePresente --
// media_file.entity_id aponta pra SÉRIE, nunca season/episódio, ver
// internal/scanner/serie.go gravarArquivoSerie), MAIS season com ao menos um
// episódio descendente presente (ListarSeasonIDsComEpisodioPresente --
// habilita o pôster de temporada) MAIS episódio com media_file presente via
// a junção media_file_entity (ListarEpisodeIDsComMediaFilePresente --
// habilita o NFO do episódio) MAIS artista/album de música com ao menos uma
// FAIXA descendente presente (ListarEntitiesMusicaComFaixaPossuida, briefing
// 0116 -- media_file.entity_id de música só aponta pra faixa, ADR-0027; sem
// isso o job nunca alcançava artista/album, então capa de álbum e foto de
// artista nunca eram materializadas mesmo com o toggle de auto-download
// ligado). Season aparecer aqui NÃO contradiz a guarda de
// gravarNFOSeFaltante (season não tem NFO próprio) -- só passa a
// materializar arte de nível de temporada.
func (s *Server) ListarEntitiesComMediaFilePresente(ctx context.Context) ([]string, error) {
	diretos, err := s.leitura().ListarEntityIDsComMediaFilePresente(ctx)
	if err != nil {
		return nil, err
	}
	seasons, err := s.leitura().ListarSeasonIDsComEpisodioPresente(ctx)
	if err != nil {
		return nil, err
	}
	episodios, err := s.leitura().ListarEpisodeIDsComMediaFilePresente(ctx)
	if err != nil {
		return nil, err
	}
	musica, err := s.leitura().ListarEntitiesMusicaComFaixaPossuida(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(diretos)+len(seasons)+len(episodios)+len(musica))
	ids = append(ids, diretos...)
	ids = append(ids, seasons...)
	ids = append(ids, episodios...)
	ids = append(ids, musica...)
	return ids, nil
}

// MaterializarMidiasFaltantes materializa, para UMA entity, a arte vencedora
// (agregador montarGruposArtwork) de cada kind de kindsMidiasFaltantes ainda
// sem local_path, e o NFO quando entity.nfo_path é nulo -- o corpo do job
// midias_faltantes (briefing 0064), também chamável isoladamente (não-HTTP,
// é isso que internal/job/midiasfaltantes.Materializador consome). Erro só
// propaga em falha real de LEITURA da entity (banco fora do ar); falhas
// pontuais de download/materialização de UM kind ou do NFO são logadas e
// puladas (best-effort -- mesmo espírito do resto do sistema de jobs,
// 0053/0054/0056), nunca abortam as demais.
func (s *Server) MaterializarMidiasFaltantes(ctx context.Context, entityID string) (artesBaixadas []string, nfoGravado bool, err error) {
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		return nil, false, err
	}

	// materializarPosterDeArtista roda ANTES do loop de kindsMidiasFaltantes de propósito:
	// ela usa o vencedor AINDA-NÃO-MATERIALIZADO do kind `thumb` (com SrcURL, pra baixar) --
	// se rodasse depois, o próprio loop abaixo (kindsMidiasFaltantes inclui "thumb") já
	// teria materializado o thumb como asset LOCAL sem SrcURL, e o alias não acharia nada
	// pra baixar.
	if ok, kerr := s.materializarPosterDeArtista(ctx, ent); kerr != nil {
		log.Printf("midias_faltantes: falha ao materializar o poster da entity artista %q (best-effort, ignorado): %v", entityID, kerr)
	} else if ok {
		artesBaixadas = append(artesBaixadas, kindPoster)
	}

	for _, kind := range kindsMidiasFaltantes {
		if kind == kindPoster && ent.Type == tipoEntArtista {
			continue // já tratado por materializarPosterDeArtista acima (alias thumb->poster) -- reprocessar aqui contaria a mesma arte duas vezes em artesBaixadas
		}
		ok, kerr := s.materializarKindVencedor(ctx, ent, kind)
		if kerr != nil {
			log.Printf("midias_faltantes: falha ao materializar o kind %q da entity %q (best-effort, ignorado): %v", kind, entityID, kerr)
			continue
		}
		if ok {
			artesBaixadas = append(artesBaixadas, kind)
		}
	}

	nfoGravado, nerr := s.gravarNFOSeFaltante(ctx, ent)
	if nerr != nil {
		log.Printf("midias_faltantes: falha ao gravar o NFO da entity %q (best-effort, ignorado): %v", entityID, nerr)
	}
	return artesBaixadas, nfoGravado, nil
}

// materializarKindVencedor pega o PRIMEIRO item do agregador
// (montarGruposArtwork, MESMA precedência da galeria/0048/0062 --
// ordenarGruposArtwork já ordena is_principal desc, is_local_in_library desc)
// para `kind` e materializa fisicamente na pasta real quando ainda faltava --
// nunca escolhe um candidato diferente do que a UI já mostraria como topo.
// Devolve ok=false sem erro quando não há candidato para o kind OU o
// vencedor já está materializado (local_path preenchido) -- ambos "nada a
// fazer", não é falha.
func (s *Server) materializarKindVencedor(ctx context.Context, ent sqlcgen.Entity, kind string) (bool, error) {
	grupos := s.montarGruposArtwork(ctx, ent.ID)
	candidatos := grupos[kind]
	if len(candidatos) == 0 {
		return false, nil
	}
	vencedor := candidatos[0]

	if vencedor.IsLocal {
		asset, err := s.leitura().ObterAsset(ctx, vencedor.AssetID)
		if err != nil {
			return false, err
		}
		if vencedor.IsLocalInLibrary {
			// briefing 0127: a coluna do banco diz "já materializado", mas o
			// arquivo pode ter sumido fisicamente (mount remoto rclone/gdrive,
			// exclusão externa) -- reconfirma via os.Stat antes de pular.
			if arquivoLocalAindaExiste(asset.LocalPath.String) {
				return false, nil // já materializado, nada a fazer
			}
			if _, err := s.regravarBinarioAssetNoLocalPathExistente(ctx, asset); err != nil {
				return false, err
			}
			return true, nil // autocura: regravado no mesmo local_path
		}
		return s.materializarAssetOuCache(ctx, ent, asset)
	}

	if vencedor.SrcURL == "" {
		return false, nil // sem URL de origem conhecida -- nada a baixar
	}
	asset, err := s.baixarEMaterializarArte(ctx, ent.ID, vencedor.SrcURL, kind)
	if err != nil {
		if errors.Is(err, errBaixarArte) {
			// Falha de DOWNLOAD (rede/status/tipo) -- best-effort, não é erro
			// de infra (mesmo tratamento de baixarMelhorArteDoKind, 0059).
			return false, nil
		}
		return false, err
	}
	return s.materializarAssetOuCache(ctx, ent, asset)
}

// materializarAssetOuCache chama materializarAsset (grava na pasta REAL da biblioteca) e
// trata errSemDestino como sucesso "só cache" (briefing 0116): álbum/artista de música
// não tem media_file DIRETO (só a faixa, descendente, liga -- ADR-0027), então
// resolverDestino nunca acha uma pasta pra eles -- gravarAssetNaPastaReal sempre devolveria
// errSemDestino. Isso não invalida o ganho do fix: o asset já foi criado no CACHE interno
// por baixarEMaterializarArte (ANTES desta chamada, ou já existia via ObterAsset), que é
// tudo que has_poster/o card precisam (entity.sql: EXISTS asset do kind, sem exigir
// local_path). Materializar capa/foto de música na pasta REAL (arquivo ao lado das faixas)
// fica FORA de escopo aqui -- exigiria resolver um "diretório canônico" de álbum/artista a
// partir dos arquivos das faixas descendentes, decisão própria não coberta pelo bug
// relatado (ausência da capa/foto no card). Para movie/tv/season/episódio (que sempre têm
// destino resolvível, únicos tipos elegíveis antes do 0116) o comportamento não muda --
// errSemDestino nunca ocorre pra eles aqui.
func (s *Server) materializarAssetOuCache(ctx context.Context, ent sqlcgen.Entity, asset sqlcgen.Asset) (bool, error) {
	if _, err := s.materializarAsset(ctx, ent, asset); err != nil {
		if errors.Is(err, errSemDestino) {
			return true, nil
		}
		return false, err
	}
	return true, nil
}

// materializarPosterDeArtista materializa o "poster" de uma entity artista a partir do
// vencedor do kind `thumb` (briefing 0116): a fanart.tv não modela um kind próprio de
// pôster para artista de música (kindsArtista, internal/provider/fanarttv/fanarttv.go, só
// tem thumb/fanart/logo/banner), mas o card/has_poster (internal/db/queries/entity.sql)
// só enxerga kind='poster' -- sem este passo, artista NUNCA teria has_poster=true mesmo
// com foto disponível no inventário. No-op (false, nil) para qualquer tipo != artista, se
// já existe algo no slot `poster` (materializado ou não -- o loop normal de
// kindsMidiasFaltantes cuida do resto) ou se não há candidato/URL de origem no kind
// `thumb`. Sempre baixa (nunca reusa um asset local de OUTRO kind): o thumb já
// materializado não pode virar poster só trocando a linha do banco, porque
// materializarAsset nomeia o arquivo na pasta real pelo kind.
func (s *Server) materializarPosterDeArtista(ctx context.Context, ent sqlcgen.Entity) (bool, error) {
	if ent.Type != tipoEntArtista {
		return false, nil
	}
	grupos := s.montarGruposArtwork(ctx, ent.ID)
	if len(grupos[kindPoster]) > 0 {
		return false, nil // ja tem poster (o loop normal de kindsMidiasFaltantes cuida do resto)
	}
	candidatos := grupos[kindThumb]
	if len(candidatos) == 0 || candidatos[0].SrcURL == "" {
		return false, nil // sem candidato de thumb, ou o vencedor e um asset local (sem SrcURL)
	}

	asset, err := s.baixarEMaterializarArte(ctx, ent.ID, candidatos[0].SrcURL, kindPoster)
	if err != nil {
		if errors.Is(err, errBaixarArte) {
			return false, nil
		}
		return false, err
	}
	return s.materializarAssetOuCache(ctx, ent, asset)
}

// MaterializarSeAutoDownloadLigado é o adaptador que satisfaz
// scanner.Materializador (briefing 0089): chamado pelo scanner logo após
// QUALQUER auto-import bem-sucedido (filme via TentarAutoMatchFilme, série
// via ImportarSeriePorNome). Toggle desligado (default) ⇒ no-op, sem tocar o
// disco -- só quando o dono ligou "materializar artes automaticamente" em
// Configurações a materialização de fato roda, reusando
// MaterializarMidiasFaltantes (0064) sem lógica nova. err só propaga em
// falha real de leitura da config/entity; falhas pontuais de download já são
// absorvidas (best-effort) dentro de MaterializarMidiasFaltantes.
func (s *Server) MaterializarSeAutoDownloadLigado(ctx context.Context, entityID string) error {
	s.dispararTrailerTemaAutomaticoSeLigado(ctx, entityID)

	cfg, err := s.obterAutoMaterializeArtworkSettings(ctx)
	if err != nil || !cfg.Enabled {
		return err
	}
	_, _, err = s.MaterializarMidiasFaltantes(ctx, entityID)
	return err
}

// dispararTrailerTemaAutomaticoSeLigado dispara, em best-effort e SEM
// bloquear o chamador, uma tentativa de baixar trailer/tema para `entityID`
// quando o toggle "baixar_trailer_tema_automaticamente" está ligado
// (briefing 0104, ADR-0026 item 3). A leitura do toggle é síncrona (settings
// já ficam em cache, custo desprezível); o download em si roda numa
// goroutine com contexto PRÓPRIO (context.Background(), NUNCA o do request
// HTTP do scan -- que morre assim que a resposta é enviada) e timeout
// generoso, já que baixar vídeo via yt-dlp é ordens de magnitude mais lento
// que uma imagem (briefing 0102 §6). Falha (rede, sem candidato, yt-dlp
// indisponível) só é logada -- o job periódico trailer_tema_faltantes cobre
// depois, se ligado (ADR-0026 item 4).
func (s *Server) dispararTrailerTemaAutomaticoSeLigado(ctx context.Context, entityID string) {
	cfg, err := s.obterAutoDownloadTrailerTemaSettings(ctx)
	if err != nil || !cfg.Enabled {
		return
	}
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if _, err := s.BaixarTrailerEntity(bgCtx, entityID); err != nil {
			log.Printf("trailer_tema_faltantes: falha ao baixar trailer automatico da entity %q (best-effort, ignorado): %v", entityID, err)
		}
		if _, err := s.BaixarTemaEntity(bgCtx, entityID); err != nil {
			log.Printf("trailer_tema_faltantes: falha ao baixar tema automatico da entity %q (best-effort, ignorado): %v", entityID, err)
		}
	}()
}

// gravarNFOSeFaltante gera o NFO da entity (mesma função gravarNFO do 0062)
// quando ainda não existe (entity.nfo_path nulo). season nunca tem NFO
// próprio (mesma guarda de writeEntityNfo) -- desde o briefing 0081, season
// PASSA a aparecer no universo de ListarEntitiesComMediaFilePresente (para o
// pôster de temporada), então esta guarda deixou de ser só defensiva: é ela
// quem impede o job de tentar gravar um NFO de season. Episódio, por outro
// lado, também passou a aparecer aqui e AGORA ganha NFO normalmente (kind já
// suportado por nomeRaizNFO/raizEpisode, só nunca era alcançado antes). Erro
// só propaga em falha real (banco/disco); "sem pasta de destino"
// (errSemDestino -- entity sem media_file present, nunca deveria acontecer
// aqui, mas defensivo) degrada para (false, nil).
func (s *Server) gravarNFOSeFaltante(ctx context.Context, ent sqlcgen.Entity) (bool, error) {
	// briefing 0127: a coluna do banco diz "já tem NFO", mas o arquivo pode
	// ter sumido fisicamente do disco (mount remoto rclone/gdrive, exclusão
	// externa) -- reconfirma via os.Stat antes de pular; se sumiu, cai para o
	// fluxo normal abaixo (gravarNFO já regrava do zero e é idempotente).
	if ent.NfoPath.Valid && ent.NfoPath.String != "" && arquivoLocalAindaExiste(ent.NfoPath.String) {
		return false, nil
	}
	if ent.Type == tipoEntTemporada {
		return false, nil
	}

	resolvida, encontrada, err := s.resolverEntity(ctx, ent.ID, "", false)
	if err != nil {
		return false, err
	}
	if !encontrada {
		return false, nil
	}
	if _, err := s.gravarNFO(ctx, ent, resolvida); err != nil {
		if errors.Is(err, errSemDestino) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
