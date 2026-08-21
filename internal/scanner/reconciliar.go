package scanner

// reconciliar.go implementa a RECONCILIAÇÃO de filhos órfãos (briefing 0120):
// uma faixa/série que ficou presa em rascunho (draft) porque o match falhou
// na hora do scan (ou a árvore pai ainda não existia) nunca é retentada pelo
// scan em si -- o pipeline de match roda uma ÚNICA vez, por arquivo, no
// momento do scan (ver o comentário de pacote de musica.go/serie.go). Os dois
// métodos exportados aqui são chamados de FORA do pacote scanner (camada
// server/job, análogo a RefreshLocalAsset em recheck.go) para retentar essa
// resolução mais tarde, sem re-varrer o datasource inteiro: reusam a MESMA
// lógica de matching do scan (Manager.matchTextualFaixa, Manager.
// tentarImportarSerie) fora do contexto de um scanRun.
//
// Nenhum dos dois apaga a entity-rascunho (princípio 5, não-destrutivo): ela
// só fica sem media_file (posse derivada cai a zero) e some da fila de
// revisão pelo mecanismo já existente.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"path/filepath"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
	"github.com/rafaelvieiras/my-media-db/internal/naming"
	"github.com/rafaelvieiras/my-media-db/internal/tvmatch"
)

// ReconciliarFaixaDraft tenta religar uma faixa DRAFT órfã (sem
// entity_relation como filha de nenhum álbum) contra a árvore de identidade
// já resolvida, re-extraindo as tags do arquivo (o media_file dela) e
// rodando o MESMO matchTextualFaixa do scan. Sucesso: repontea
// media_file.entity_id da faixa draft pra faixa recém-resolvida. linked=false
// sem erro é degradação normal (sem provider/importador, sem media_file,
// tags não extraíveis, ou sem match -- nunca um erro de negócio, só erro real
// de banco/io sobe em err).
func (m *Manager) ReconciliarFaixaDraft(ctx context.Context, entityID string, langs []string) (linked bool, err error) {
	mf, ok, err := m.mediaFilePresentePorEntity(ctx, entityID)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	tags, ok := m.probarTags(ctx, mf.Path)
	if !ok {
		return false, nil
	}

	faixaID, ok := m.matchTextualFaixa(ctx, tags, langs)
	if !ok {
		return false, nil
	}

	if err := sqlcgen.New(m.db.Write).AtualizarMediaFileEntityID(ctx, sqlcgen.AtualizarMediaFileEntityIDParams{
		EntityID:        faixaID,
		MatchConfidence: nfFloat(confMusicaTextual),
		ID:              mf.ID,
	}); err != nil {
		return false, err
	}
	return true, nil
}

// ReconciliarSerieDraft tenta reimportar uma série DRAFT com o título da
// entity via tentarImportarSerie; sucesso: para cada media_file hoje
// apontando pra essa série-rascunho, re-deriva o número do episódio
// (naming.ParseEpisode no basename do path) e casa via tvmatch.Casar contra o
// índice da série recém-importada, repontando media_file.entity_id pra série
// real e criando os vínculos media_file_entity (mesmo padrão de
// gravarArquivoSerie: RemoverVinculosDoArquivoPorKind + CriarVinculoEntidade,
// kind=kindVinculoEpisodio). Media_file sem episódio casável fica apontando
// pra série real mesmo assim (só sem vínculo de episódio -- cai na fila de
// revisão dela, não mais na fila da série inteira). linked=true só quando
// pelo menos 1 media_file foi religado. preferredLang é sempre "" aqui (sem
// preferência de fonte específica -- a reconciliação não tem uma
// library_source de origem clara por arquivo; o importer cai pro idioma
// global, mesmo comportamento de preferredLangDaFonte quando não há
// preferência).
func (m *Manager) ReconciliarSerieDraft(ctx context.Context, entityID string, langs []string) (linked bool, err error) {
	entity, err := sqlcgen.New(m.db.Read).ObterEntity(ctx, entityID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	res, ok := m.tentarImportarSerie(ctx, entity.Title, langs, "")
	if !ok {
		return false, nil
	}

	arquivos, err := sqlcgen.New(m.db.Read).ListarMediaFilesPorEntity(ctx, entityID)
	if err != nil {
		return false, err
	}

	religados := 0
	for _, mf := range arquivos {
		if mf.State != "present" {
			continue
		}
		if err := m.religarArquivoSerie(ctx, mf, res); err != nil {
			return religados > 0, err
		}
		religados++
	}
	return religados > 0, nil
}

// religarArquivoSerie repontea UM media_file da série-rascunho pra série
// real (res.serieID), casando o episódio quando possível (mesmo padrão de
// gravarArquivoSerie em serie.go): re-deriva o número do episódio do
// basename do path e casa contra o índice já carregado em `res`. Sem
// episódio casável, o arquivo ainda assim é repontado pra série real, só sem
// vínculo (cai na fila de revisão da série real, não mais na da rascunho).
func (m *Manager) religarArquivoSerie(ctx context.Context, mf sqlcgen.MediaFile, res serieResolvida) error {
	confianca := float64(confRevisaoSerie)
	var episodeIDs []string
	if res.index != nil {
		base := semExtensao(filepath.Base(mf.Path))
		if ep, ok := naming.ParseEpisode(base); ok {
			if ids, matched := tvmatch.Casar(res.index, ep); matched {
				episodeIDs = ids
				confianca = res.conf
			}
		}
	}

	escrita := sqlcgen.New(m.db.Write)
	if err := escrita.AtualizarMediaFileEntityID(ctx, sqlcgen.AtualizarMediaFileEntityIDParams{
		EntityID:        res.serieID,
		MatchConfidence: nfFloat(confianca),
		ID:              mf.ID,
	}); err != nil {
		return err
	}

	if len(episodeIDs) == 0 {
		return nil
	}

	if err := escrita.RemoverVinculosDoArquivoPorKind(ctx, sqlcgen.RemoverVinculosDoArquivoPorKindParams{
		MediaFileID: mf.ID,
		Kind:        kindVinculoEpisodio,
	}); err != nil {
		return err
	}
	for i, epID := range episodeIDs {
		if err := escrita.CriarVinculoEntidade(ctx, sqlcgen.CriarVinculoEntidadeParams{
			MediaFileID: mf.ID,
			EntityID:    epID,
			Kind:        kindVinculoEpisodio,
			Ordinal:     int64(i),
		}); err != nil {
			return err
		}
	}
	return nil
}

// mediaFilePresentePorEntity devolve o PRIMEIRO media_file 'present' de uma
// entity (ADR-0027: 1:1 faixa-arquivo, mas defensivo contra mais de um).
// ok=false sem erro quando a entity não tem nenhum media_file present.
func (m *Manager) mediaFilePresentePorEntity(ctx context.Context, entityID string) (sqlcgen.MediaFile, bool, error) {
	arquivos, err := sqlcgen.New(m.db.Read).ListarMediaFilesPorEntity(ctx, entityID)
	if err != nil {
		return sqlcgen.MediaFile{}, false, err
	}
	for _, mf := range arquivos {
		if mf.State == "present" {
			return mf, true, nil
		}
	}
	return sqlcgen.MediaFile{}, false, nil
}

// probarTags re-extrai as tags de áudio de um path via o Prober já injetado
// no Manager (mesmo caminho de factsMusica, musica.go). ok=false sem erro
// quando o probe falha, devolve vazio, ou o JSON não desserializa -- nunca um
// erro de negócio (a reconciliação apenas não consegue tentar de novo agora).
func (m *Manager) probarTags(ctx context.Context, path string) (mediainfo.AudioTags, bool) {
	mi, err := m.prober.Probe(ctx, path)
	if err != nil || len(mi) == 0 {
		if err != nil {
			log.Printf("scanner: reconciliação: probe de %q falhou: %v", path, err)
		}
		return mediainfo.AudioTags{}, false
	}
	var info mediainfo.Info
	if err := json.Unmarshal(mi, &info); err != nil || info.Tags == nil {
		return mediainfo.AudioTags{}, false
	}
	return *info.Tags, true
}
