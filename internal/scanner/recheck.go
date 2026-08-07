package scanner

// recheck.go implementa dois caminhos de entrada NOVOS e ISOLADOS do scan de
// datasource inteiro (briefing 0088, menu Gerenciar > "Verificar Atualizações
// de Arquivos"): (1) RecheckEntityFiles força re-hash/mediainfo de uma LISTA
// EXPLÍCITA de paths (os media_file de UMA entity), ignorando de propósito o
// short-circuit de mediaFileInalteradoPorPath (scan incremental, briefing
// 0064) — ação pontual do dono, não o job periódico; (2) RefreshLocalAsset
// reconfere a pasta de cada media_file de uma entity JÁ CATALOGADA em busca
// de um pôster local novo/atualizado, cobrindo o gap de descobrirPoster só
// rodar dentro de criarEntity (match.go) na 1ª varredura daquela pasta.
// Nenhum dos dois faz WalkDir nem toca no ciclo de vida (missing/last_scan)
// do datasource — Start/executarScan continuam 100% incrementais, sem
// nenhuma mudança de comportamento.

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/library"
)

// RecheckEntityFiles força o re-hash/mediainfo de cada path em `paths` —
// media_file JÁ CONHECIDOS (por path) — mesmo quando mtime/size não mudaram
// desde a última vez (ignora DE PROPÓSITO o short-circuit do scan
// incremental). `source` é a library_source de origem desses arquivos
// (resolvida pelo CHAMADOR, ver internal/server/refresh_files.go — mesmo
// padrão de resolução usado em internal/server/retroativo.go), usada só para
// saber se o ffprobe está habilitado e para carimbar library_source_id/
// last_seen igual ao scan normal. Path que sumiu do disco OU cujo media_file
// já não existe (sumiu entre a listagem do chamador e esta chamada) é
// ignorado — best-effort, não é erro. Devolve quantos paths foram
// efetivamente re-hasheados.
func (m *Manager) RecheckEntityFiles(ctx context.Context, source library.Source, paths []string) (int, error) {
	r := &scanRun{
		m:          m,
		source:     source,
		scanTS:     m.agora().Format(carimboFormato),
		probeAtivo: m.prober.Disponivel(),
	}
	rechecked := 0
	for _, p := range paths {
		ok, err := r.recheckPath(ctx, p)
		if err != nil {
			return rechecked, err
		}
		if ok {
			rechecked++
		}
	}
	return rechecked, nil
}

// recheckPath re-hasheia UM path já conhecido (media_file existente),
// atualizando a linha via AtualizarMediaFileConteudoMudou — a MESMA query que
// o scan normal usa quando o conteúdo de um path já-conhecido muda
// (atualizarConteudoMudou, match.go), reaproveitada aqui sem duplicar a
// gravação. Preserva edition/part_no/match_confidence já gravados — só o
// recheck de hash/mediainfo foi pedido, não uma nova resolução de match.
// ok=false sem erro quando o path não tem media_file conhecido ou sumiu do
// disco (best-effort).
func (r *scanRun) recheckPath(ctx context.Context, path string) (bool, error) {
	existente, err := sqlcgen.New(r.m.db.Read).ObterMediaFilePorPath(ctx, path)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}

	info, statErr := os.Stat(path)
	if statErr != nil {
		log.Printf("scanner: recheck ignorando %q (sumiu do disco): %v", path, statErr)
		return false, nil
	}
	mtime := info.ModTime().UTC().Format(time.RFC3339Nano)

	hash, mediainfo, err := r.computarHashEMediainfo(ctx, path, info.Size())
	if err != nil {
		return false, err
	}

	_, err = sqlcgen.New(r.m.db.Write).AtualizarMediaFileConteudoMudou(ctx, sqlcgen.AtualizarMediaFileConteudoMudouParams{
		ContentHash:     nsOpt(hash),
		Size:            niInt(info.Size()),
		Mtime:           nsOpt(mtime),
		Edition:         existente.Edition,
		PartNo:          existente.PartNo,
		MatchConfidence: existente.MatchConfidence,
		MediainfoJson:   nsBytes(mediainfo),
		LastSeen:        nsOpt(r.scanTS),
		LibrarySourceID: existente.LibrarySourceID,
		ID:              existente.ID,
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

// RefreshLocalAsset reconfere a pasta de cada path em `paths` (os media_file
// de uma entity JÁ CATALOGADA) em busca de um pôster local — MESMO critério/
// nomes de arquivo de descobrirPoster (poster.jpg/folder.jpg no diretório da
// obra + <nome-do-arquivo>-poster.jpg/.png) — e faz UPSERT do asset
// kind=poster origin=local: cria se a entity ainda não tem um; atualiza
// path_or_uri se o arquivo local encontrado mudou. NÃO mexe em
// is_principal/precedência — só garante que a linha origin='local' reflita o
// que está no disco (briefing 0088). Para o PRIMEIRO path cuja pasta tem um
// pôster local — mesma semântica "primeiro vence" que criarEntity (match.go)
// já usa na 1ª varredura, aqui só reaplicada a uma entity que já existia.
// Devolve found=true quando um asset foi criado OU teve o path_or_uri
// atualizado; found=false quando não achou nada novo (sem pôster local em
// nenhuma pasta, ou o asset já existente já reflete o disco).
func (m *Manager) RefreshLocalAsset(ctx context.Context, entityID string, paths []string) (bool, error) {
	var encontrado string
	for _, p := range paths {
		dir := filepath.Dir(p)
		base := semExtensao(filepath.Base(p))
		if poster := descobrirPoster(dir, base); poster != "" {
			encontrado = poster
			break
		}
	}
	if encontrado == "" {
		return false, nil
	}

	leitura := sqlcgen.New(m.db.Read)
	existente, err := leitura.ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{
		EntityID: entityID,
		Kind:     "poster",
		Origin:   "local",
	})
	escrita := sqlcgen.New(m.db.Write)
	if err == nil {
		if existente.PathOrUri == encontrado {
			return false, nil // já reflete o disco, nada a fazer
		}
		if err := escrita.AtualizarAssetPath(ctx, sqlcgen.AtualizarAssetPathParams{
			PathOrUri: encontrado,
			MetaJson:  existente.MetaJson, // preserva a flag `preferred` já gravada
			ID:        existente.ID,
		}); err != nil {
			return false, err
		}
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}

	if _, err := escrita.CriarAsset(ctx, sqlcgen.CriarAssetParams{
		ID:        m.novoID(),
		EntityID:  entityID,
		Kind:      "poster",
		Origin:    "local",
		PathOrUri: encontrado,
	}); err != nil {
		return false, err
	}
	return true, nil
}
