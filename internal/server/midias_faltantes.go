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
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/localmedia"
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
// ContarMediaFilesEmEstadoMissing devolve quantos media_file estao em estado
// 'missing' no momento da chamada (briefing 0128, Caso A) -- wrapper fino
// sobre a query sqlc, consumido pelo resumo do job midias_faltantes
// (internal/job/midiasfaltantes) para tornar visivel um sinal de
// instabilidade do mount remoto que hoje nao aparece em lugar nenhum.
func (s *Server) ContarMediaFilesEmEstadoMissing(ctx context.Context) (int64, error) {
	return s.leitura().ContarMediaFilesEmEstadoMissing(ctx)
}

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
			return s.reconfirmarAssetLocalNaLibrary(ctx, ent, asset)
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

// reconfirmarAssetLocalNaLibrary trata o caso "o banco diz que o vencedor já
// está materializado na pasta real" (vencedor.IsLocalInLibrary) -- extraído
// de materializarKindVencedor para manter a complexidade ciclomática baixa.
// Quando o asset já É o principal do kind, delega a convergirNomeCanonico
// (briefing 0128, Caso B: DIRETÓRIO + nome podem ter mudado desde a
// materialização, ex.: local_path legado pré-briefing-0081 apontando pra
// dentro de "Season 01" em vez da raiz da série) -- persiste o novo
// local_path quando o destino muda. Deliberadamente NÃO reusa
// renomearParaBare (usado por PUT .../prefer): aquela função preserva o
// DIRETÓRIO atual do asset por design (só resolve bare-vs-rotacionado
// dentro da pasta já correta) -- aqui o próprio diretório pode estar errado.
// Um asset local NÃO-principal (edge case: nenhum asset do kind é principal)
// só recebe o self-heal simples do briefing 0127 (regravar in-place se o
// arquivo sumiu) -- recalcular o nome bare pra ele colidiria com o nome que
// o verdadeiro principal deveria ocupar (ver briefing 0128 §6).
func (s *Server) reconfirmarAssetLocalNaLibrary(ctx context.Context, ent sqlcgen.Entity, asset sqlcgen.Asset) (bool, error) {
	if asset.IsPrincipal != 0 {
		haviaSumido := !arquivoLocalAindaExiste(asset.LocalPath.String)
		destino, err := s.convergirNomeCanonico(ctx, ent, asset)
		if err != nil {
			return false, err
		}
		if destino == asset.LocalPath.String {
			return haviaSumido, nil // sem drift de nome; convergirNomeCanonico já cobre o self-heal do 0127 (regravado in-place se o arquivo tinha sumido)
		}
		if err := sqlcgen.New(s.db.Write).AtualizarAssetLocalPath(ctx, sqlcgen.AtualizarAssetLocalPathParams{
			LocalPath: sql.NullString{String: destino, Valid: true},
			ID:        asset.ID,
		}); err != nil {
			return false, err
		}
		return true, nil // convergência: arquivo renomeado pro destino canônico atual
	}

	// briefing 0127: a coluna do banco diz "já materializado", mas o arquivo
	// pode ter sumido fisicamente (mount remoto rclone/gdrive, exclusão
	// externa) -- reconfirma via os.Stat antes de pular.
	if arquivoLocalAindaExiste(asset.LocalPath.String) {
		return false, nil // já materializado, nada a fazer
	}
	if _, err := s.regravarBinarioAssetNoLocalPathExistente(ctx, asset); err != nil {
		return false, err
	}
	return true, nil // autocura: regravado no mesmo local_path
}

// convergirNomeCanonico recalcula o destino canônico ATUAL (DIRETÓRIO + nome
// bare) de um asset já local via resolverDestino, e RENOMEIA (rename, nunca
// recopia bytes) o arquivo físico se divergir do local_path registrado --
// briefing 0128, Caso B. Diferente de renomearParaBare
// (localmedia_handlers.go, usado por PUT .../prefer): aquela função só
// resolve bare-vs-rotacionado MANTENDO o diretório atual (`filepath.Dir`)
// por design, porque no fluxo de troca de principal o asset já está na pasta
// certa; aqui o próprio DIRETÓRIO pode estar errado (naming legado
// pré-briefing-0081, ex.: asset gravado dentro de "Season 01" quando deveria
// estar na raiz da série) -- recalcular só o nome sem recalcular o
// diretório NÃO fecharia esse gap. Quando o destino recalculado é igual ao
// atual, cai no MESMO self-heal do briefing 0127 (regrava in-place se o
// arquivo tiver sumido do disco).
func (s *Server) convergirNomeCanonico(ctx context.Context, ent sqlcgen.Entity, alvo sqlcgen.Asset) (string, error) {
	dir, nomeBaseVideo, pastaExclusiva, ok, err := s.resolverDestino(ctx, ent)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errSemDestino
	}
	ext := filepath.Ext(alvo.LocalPath.String)
	kind, nomeBase := s.kindNomeEBase(ctx, ent, alvo.Kind, nomeBaseVideo)
	destino := filepath.Join(dir, localmedia.NomeArquivoArte(kind, true, 0, pastaExclusiva, nomeBase, ext))
	if destino == alvo.LocalPath.String {
		if arquivoLocalAindaExiste(alvo.LocalPath.String) {
			return destino, nil
		}
		if _, err := s.regravarBinarioAssetNoLocalPathExistente(ctx, alvo); err != nil {
			return "", err
		}
		return destino, nil
	}
	if err := localmedia.RenomearArquivo(alvo.LocalPath.String, destino); err != nil {
		return "", err
	}
	return destino, nil
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

// nfoJaConvergenteNoDisco decide se o nfo_path já registrado (nfoPathAntigo)
// ainda é o destino canônico ATUAL e o arquivo continua presente -- usado
// por gravarNFOSeFaltante (briefing 0128, Caso B) para só pular a
// regravação quando NADA mudou (nem o destino recalculado via
// resolverDestino, nem a presença física do arquivo, briefing 0127).
// Extraída para manter a complexidade ciclomática de gravarNFOSeFaltante
// baixa. Falha ao recalcular o destino (banco/disco) degrada para o guard
// simples do 0127 (arquivo sumiu?), nunca bloqueia o self-heal já existente.
func (s *Server) nfoJaConvergenteNoDisco(ctx context.Context, ent sqlcgen.Entity, nfoPathAntigo string) bool {
	dir, nomeBaseVideo, _, ok, err := s.resolverDestino(ctx, ent)
	if err != nil || !ok {
		return arquivoLocalAindaExiste(nfoPathAntigo)
	}
	destinoAtual := filepath.Join(dir, localmedia.NomeArquivoNFO(ent.Type, nomeBaseVideo))
	return destinoAtual == nfoPathAntigo && arquivoLocalAindaExiste(nfoPathAntigo)
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
	if ent.Type == tipoEntTemporada {
		return false, nil
	}

	nfoPathAntigo := ent.NfoPath.String
	temNfoRegistrado := ent.NfoPath.Valid && nfoPathAntigo != ""

	if temNfoRegistrado && s.nfoJaConvergenteNoDisco(ctx, ent, nfoPathAntigo) {
		return false, nil
	}
	// sem NFO registrado, OU destino divergente (naming legado), OU destino
	// certo mas arquivo sumido -- cai no fluxo normal abaixo, que sempre
	// grava no destino recém-calculado (gravarNFO é idempotente por
	// conteúdo; briefings 0062/0127/0128).

	resolvida, encontrada, err := s.resolverEntity(ctx, ent.ID, "", false)
	if err != nil {
		return false, err
	}
	if !encontrada {
		return false, nil
	}
	resultado, err := s.gravarNFO(ctx, ent, resolvida)
	if err != nil {
		if errors.Is(err, errSemDestino) {
			return false, nil
		}
		return false, err
	}
	removerNfoOrfaoSeMudou(temNfoRegistrado, nfoPathAntigo, resultado.NfoPath)
	return true, nil
}

// removerNfoOrfaoSeMudou remove (best-effort) o .nfo antigo quando
// gravarNFOSeFaltante regravou num destino DIFERENTE do registrado
// (naming legado, briefing 0128 Caso B) -- sem isso, o .nfo errado fica
// órfão ao lado do novo, no lugar certo, e Kodi/Emby podem escanear os
// dois. Seguro remover: NFO é sempre exportação one-way do sistema, nunca
// editado pelo dono fora do fluxo do MyMediaDB (AGENTS.md). No-op quando
// não havia nfo_path registrado antes, ou quando o destino não mudou.
func removerNfoOrfaoSeMudou(temNfoRegistrado bool, nfoPathAntigo, novoPath string) {
	if !temNfoRegistrado || nfoPathAntigo == novoPath {
		return
	}
	if rerr := os.Remove(nfoPathAntigo); rerr != nil && !os.IsNotExist(rerr) {
		log.Printf("gravarNFOSeFaltante: falha ao remover NFO legado orfao %q (best-effort, ignorado): %v", nfoPathAntigo, rerr)
	}
}
