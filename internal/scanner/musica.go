package scanner

// musica.go é o núcleo do scanner de MÚSICA (briefing 0107, ADR-0027): quando a
// biblioteca é `content_type='music'`, cada arquivo de áudio é UMA faixa. O fluxo,
// por arquivo, na ordem em que tenta resolver (para na primeira que resolver):
//
//  1. MBID de faixa (recording) embutido na tag -> ObterEntityPorMbid direto (o
//     arquivo já foi catalogado antes, ou outra faixa do mesmo álbum já trouxe a
//     árvore inteira nesta mesma execução ou numa anterior).
//  2. MBID de faixa SEM match ainda, mas a tag TAMBÉM tem o MBID do artista ->
//     importa a árvore inteira do artista (idempotente) e tenta o lookup de novo.
//  3. MBID de faixa SEM MBID de artista na tag -> resolução REVERSA
//     (provider.FaixaResolverProvider) faixa->artista, depois cai no mesmo caminho
//     do passo 2.
//  4. SEM NENHUM MBID embutido (caso DOMINANTE nos arquivos reais do dono -- os
//     scripts pessoais dele não gravam esse tag): busca textual do artista
//     (album_artist, com fallback para artist) no provider; candidato único e forte
//     o bastante importa a árvore, e a faixa certa é localizada dentro dela
//     comparando álbum/título/track_number da tag contra os filhos.
//  5. SEM MATCH PLAUSÍVEL nenhum: cria uma entity `faixa` RASCUNHO (status=draft),
//     só com os dados da tag (ou o nome do arquivo) -- SEM entity_relation de
//     artista/álbum, fica solta até correção manual (mesmo espírito de
//     acharOuCriarDraftSerie, mas sem find-or-create por título: cada arquivo sem
//     match vira seu próprio draft).
//
// TUDO isto roda em factsMusica (fase de FATOS, chamada por computarFacts em
// scan.go), FORA da transação de escrita do lote -- o import do enrich usa a
// própria conexão de escrita, então nada de transação aninhada (mesmo motivo do
// caminho de série, ver serie.go). O vínculo arquivo<->faixa é 1:1
// (media_file.entity_id aponta DIRETO pra faixa, ADR-0027 item 5) -- NUNCA a
// junção N:M de série (media_file_entity); gravarArquivoMusica (abaixo) espelha
// gravarArquivo de filme (match.go), não gravarArquivoSerie.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"path/filepath"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/matching"
	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
	"github.com/rafaelvieiras/my-media-db/internal/musicext"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/textsim"
)

const (
	// contentTypeMusic é o valor de library_source.content_type que liga o fluxo
	// de música (espelha contentTypeSeries em serie.go).
	contentTypeMusic = "music"
	// tipoArtista/tipoFaixa são os entity.type da vertical de música que este
	// arquivo cria/consulta diretamente (ADR-0027; espelham as constantes
	// privadas equivalentes de internal/enrich/musica.go, que não são
	// exportadas -- ver AGENTS.md/briefing 0107, "não duplique lógica de
	// criação", mas os LITERAIS de tipo em si não são lógica).
	tipoArtista = "artista"
	tipoFaixa   = "faixa"
	// sourceMusicBrainz é o Nome() da fonte usada para o parâmetro `source` de
	// MusicaImportador.Importar (musicbrainz.go: nomeFonte, mesmo valor).
	sourceMusicBrainz = "musicbrainz"
	// confMusicaTextual é a confiança do match por busca textual (passo 4) --
	// mesmo grau de "palpite alto, não uma prova de identidade" que
	// confTituloAno tem para filme (match.go): um nome de artista batendo
	// exato contra a fonte é um bom sinal, mas não é um id embutido.
	confMusicaTextual = confTituloAno
)

// factsMusica monta os fatos de um arquivo de áudio: extrai as tags (via o
// mediainfo_json já calculado por hashEMediainfo) e resolve a entity FAIXA
// através do pipeline de matching acima. Não escreve o media_file (isso é da
// transação do lote, ver gravarArquivoMusica) -- só resolve.
//
// INCREMENTALIDADE (briefing 0107 §5): quando já existe um media_file para
// este MESMO path com mtime/size EXATAMENTE iguais aos atuais (o mesmo
// short-circuit de mediaFileInalteradoPorPath que hashEMediainfo já usa para
// não re-hashear/re-probar), reusa o entity_id e a confiança JÁ GRAVADOS sem
// rodar o pipeline de matching de novo -- nunca rechama Importar (rede) nem
// a busca textual num re-scan sem mudança nenhuma no arquivo.
func (r *scanRun) factsMusica(ctx context.Context, path string, mi []byte, size int64, mtime string) fileFacts {
	f := fileFacts{path: path, isMusica: true, folderDir: filepath.Dir(path), rawNome: semExtensao(filepath.Base(path))}

	if existente, ok := r.mediaFileInalteradoPorPath(ctx, path, size, mtime); ok {
		f.faixaID = existente.EntityID
		f.faixaConf = existente.MatchConfidence.Float64
		return f
	}

	var tags mediainfo.AudioTags
	if len(mi) > 0 {
		var info mediainfo.Info
		if err := json.Unmarshal(mi, &info); err == nil && info.Tags != nil {
			tags = *info.Tags
		}
	}

	f.faixaID, f.faixaConf = r.resolverFaixa(ctx, tags, f.rawNome)
	return f
}

// resolverFaixa executa o pipeline de matching descrito no comentário do
// pacote, na ordem: MBID de faixa direto -> MBID de faixa + import (com ou sem
// MBID de artista embutido) -> busca textual -> draft. Sempre devolve um
// faixaID utilizável (o passo 5 nunca falha, exceto erro de banco -- nesse
// caso "" e o chamador loga e pula o arquivo, ver gravarArquivoMusica).
func (r *scanRun) resolverFaixa(ctx context.Context, tags mediainfo.AudioTags, rawNome string) (string, float64) {
	langs := r.langsPermitidos(ctx)

	if tags.MusicBrainzTrackID != "" {
		if id, ok := r.acharFaixaPorMbid(ctx, tags.MusicBrainzTrackID); ok {
			return id, confIDExterno
		}
		if id, ok := r.importarEAcharPorMbid(ctx, tags, langs); ok {
			return id, confIDExterno
		}
	}

	if id, ok := r.matchTextual(ctx, tags, langs); ok {
		return id, confMusicaTextual
	}

	return r.criarFaixaDraft(ctx, tags, rawNome), confDraft
}

// acharFaixaPorMbid é o lookup PURO (sem rede) por MBID de recording -- o
// caminho mais barato: o arquivo já foi catalogado antes, ou outra faixa do
// mesmo álbum já disparou o import da árvore inteira nesta execução.
func (r *scanRun) acharFaixaPorMbid(ctx context.Context, trackMBID string) (string, bool) {
	e, err := sqlcgen.New(r.m.db.Read).ObterEntityPorMbid(ctx, nsOpt(trackMBID))
	if err != nil {
		return "", false
	}
	return e.ID, true
}

// importarEAcharPorMbid resolve o MBID do ARTISTA (embutido na tag, ou via
// resolução reversa faixa->artista quando só o MBID de faixa está presente),
// importa a árvore inteira (idempotente) e refaz o lookup por MBID de faixa
// para pegar a entity recém-criada. ok=false em qualquer degradação (sem
// importador configurado, sem MBID de artista resolvível, import falhou, ou a
// faixa buscada não apareceu na árvore importada -- dado divergente entre a
// tag do arquivo e a fonte, raro mas possível).
func (r *scanRun) importarEAcharPorMbid(ctx context.Context, tags mediainfo.AudioTags, langs []string) (string, bool) {
	if r.m.musicaImportador == nil {
		return "", false
	}
	artistaMBID, ok := r.resolverArtistaMBID(ctx, tags)
	if !ok {
		return "", false
	}
	if _, _, err := r.m.musicaImportador.Importar(ctx, sourceMusicBrainz, artistaMBID, tipoArtista, langs); err != nil {
		log.Printf("scanner: import da arvore do artista %s (faixa %s) falhou: %v", artistaMBID, tags.MusicBrainzTrackID, err)
		return "", false
	}
	return r.acharFaixaPorMbid(ctx, tags.MusicBrainzTrackID)
}

// resolverArtistaMBID devolve o MBID do artista: direto da tag quando
// presente, ou via a capacidade opcional provider.FaixaResolverProvider
// (resolução reversa a partir do MBID de faixa) quando ausente -- o caso do
// passo 3 do pipeline (tagueador só grava o recording MBID).
func (r *scanRun) resolverArtistaMBID(ctx context.Context, tags mediainfo.AudioTags) (string, bool) {
	if tags.MusicBrainzArtistID != "" {
		return tags.MusicBrainzArtistID, true
	}
	if r.m.musicaProvider == nil {
		return "", false
	}
	resolver, ok := r.m.musicaProvider.(provider.FaixaResolverProvider)
	if !ok {
		return "", false
	}
	mbid, err := resolver.ResolverArtistaDaFaixa(ctx, tags.MusicBrainzTrackID)
	if err != nil {
		log.Printf("scanner: resolucao reversa artista<-faixa %s falhou: %v", tags.MusicBrainzTrackID, err)
		return "", false
	}
	return mbid, mbid != ""
}

// matchTextual é o FALLBACK sem MBID embutido (caso dominante nos arquivos
// reais do dono, ver AGENTS.md/briefing 0107): busca o artista por nome
// (album_artist, com fallback para artist) no provider, importa a discografia
// quando existe UM ÚNICO candidato cujo título bate com o limiar de
// matching.LimiarAutoMatch (92%, o MESMO limiar já usado pelo auto-match de
// filme -- reaproveitado de propósito para não inventar um segundo número
// mágico: o raciocínio é idêntico, o custo de um falso-positivo aqui é
// contaminar a árvore inteira do artista errado) contra o nome retornado pela
// fonte, e localiza a faixa certa dentro da árvore recém-importada. Ambíguo
// (>1 candidato empatado no topo) ou nenhum candidato forte o bastante:
// ok=false (cai para o draft, passo 5) -- nunca aplica um match "parecido".
func (r *scanRun) matchTextual(ctx context.Context, tags mediainfo.AudioTags, langs []string) (string, bool) {
	if r.m.musicaProvider == nil || r.m.musicaImportador == nil {
		return "", false
	}
	nomeArtista := tags.AlbumArtist
	if nomeArtista == "" {
		nomeArtista = tags.Artist
	}
	if nomeArtista == "" {
		return "", false
	}

	resultados, err := r.m.musicaProvider.Buscar(ctx, nomeArtista, "artista", "")
	if err != nil {
		log.Printf("scanner: busca textual do artista %q falhou: %v", nomeArtista, err)
		return "", false
	}
	var candidatos []candidatoTexto
	for _, res := range resultados {
		candidatos = append(candidatos, candidatoTexto{id: res.SourceID, titulo: res.Title})
	}
	artistaMBID, ok := melhorCandidatoTexto(nomeArtista, candidatos)
	if !ok {
		return "", false
	}

	artistaID, _, err := r.m.musicaImportador.Importar(ctx, sourceMusicBrainz, artistaMBID, tipoArtista, langs)
	if err != nil {
		log.Printf("scanner: import do artista %s (busca textual %q) falhou: %v", artistaMBID, nomeArtista, err)
		return "", false
	}
	return r.acharFaixaNaArvore(ctx, artistaID, tags)
}

// candidatoTexto é um par (id,título) candidato a match textual -- usado
// tanto para o artista (resultado de Provider.Buscar) quanto para um filho de
// entity_relation (álbum ou faixa), ver melhorFilhoPorTitulo.
type candidatoTexto struct {
	id     string
	titulo string
}

// melhorCandidatoTexto escolhe, entre `candidatos`, o de título mais próximo
// de `alvo` (Jaro-Winkler via internal/textsim -- o MESMO motor de
// similaridade que o auto-match de filme/série já usa, internal/matching),
// exigindo o mesmo limiar de matching.LimiarAutoMatch (92%, escala 0-100) e
// NENHUM empate no topo (>1 candidato igualmente forte é ambíguo, não um
// match). Limiar deliberadamente estrito e sem fuzzy adicional: o objetivo é
// nunca criar um vínculo errado em silêncio -- um nome ambíguo ou
// não-identificado cai para o draft (correção manual), o que é seguro; um
// match "parecido o bastante" aplicado errado contamina a árvore inteira do
// artista, o que não é.
func melhorCandidatoTexto(alvo string, candidatos []candidatoTexto) (string, bool) {
	melhorScore := -1.0
	melhorID := ""
	empate := false
	for _, c := range candidatos {
		score := textsim.Similaridade(alvo, c.titulo) * 100
		switch {
		case score > melhorScore:
			melhorScore, melhorID, empate = score, c.id, false
		case score == melhorScore:
			empate = true
		}
	}
	if empate || melhorScore < matching.LimiarAutoMatch {
		return "", false
	}
	return melhorID, true
}

// acharFaixaNaArvore localiza, dentro da árvore JÁ IMPORTADA do artista
// `artistaID`, o álbum cujo título mais se aproxima de tags.Album e, dentro
// dele, a faixa cujo título mais se aproxima de tags.Title (ou, sem título na
// tag, a faixa cujo track_number bate) -- reaproveita entity_relation
// GENÉRICA (kind=music_artista_principal artista->álbum, kind=music_album
// álbum->faixa, internal/musicext), nenhuma query nova.
func (r *scanRun) acharFaixaNaArvore(ctx context.Context, artistaID string, tags mediainfo.AudioTags) (string, bool) {
	if tags.Album == "" {
		return "", false
	}
	q := sqlcgen.New(r.m.db.Read)
	rels, err := q.ListarRelacoesFilhas(ctx, artistaID)
	if err != nil {
		return "", false
	}
	albumID, ok := melhorFilhoPorTitulo(ctx, q, rels, musicext.KindArtistaPrincipal, tags.Album)
	if !ok {
		return "", false
	}
	faixaRels, err := q.ListarRelacoesFilhas(ctx, albumID)
	if err != nil {
		return "", false
	}
	if tags.Title != "" {
		return melhorFilhoPorTitulo(ctx, q, faixaRels, musicext.KindAlbum, tags.Title)
	}
	return faixaPorNumero(ctx, q, faixaRels, tags.TrackNumber)
}

// melhorFilhoPorTitulo escolhe, entre os filhos de `rels` do `kind` pedido, o
// de título mais próximo de `titulo` (mesmo critério/limiar de
// melhorCandidatoTexto) -- usado tanto para achar o álbum quanto, dentro
// dele, a faixa por título.
func melhorFilhoPorTitulo(ctx context.Context, q *sqlcgen.Queries, rels []sqlcgen.EntityRelation, kind, titulo string) (string, bool) {
	var candidatos []candidatoTexto
	for _, rel := range rels {
		if rel.Kind != kind {
			continue
		}
		ent, err := q.ObterEntity(ctx, rel.ChildID)
		if err != nil {
			continue
		}
		candidatos = append(candidatos, candidatoTexto{id: ent.ID, titulo: ent.Title})
	}
	return melhorCandidatoTexto(titulo, candidatos)
}

// faixaPorNumero é o desempate quando a tag não trouxe título de faixa (só
// track number): compara contra o track_number gravado em extension_data
// core.music@1 de cada faixa filha do álbum. numero<=0 (tag também sem
// número): ok=false, sem heurística.
func faixaPorNumero(ctx context.Context, q *sqlcgen.Queries, rels []sqlcgen.EntityRelation, numero int) (string, bool) {
	if numero <= 0 {
		return "", false
	}
	for _, rel := range rels {
		if rel.Kind != musicext.KindAlbum {
			continue
		}
		ent, err := q.ObterEntity(ctx, rel.ChildID)
		if err != nil {
			continue
		}
		ns := musicext.ExtractNamespace(ent.ExtensionDataJson, musicext.Namespace)
		if ns == nil {
			continue
		}
		if tn, ok := musicext.AsInt(ns["track_number"]); ok && tn == numero {
			return ent.ID, true
		}
	}
	return "", false
}

// criarFaixaDraft é o FALLBACK final (passo 5): cria a entity `faixa`
// RASCUNHO (status=draft) só com os dados da tag disponíveis (ou o nome do
// arquivo, quando a tag de título vier vazia) -- SEM entity_relation de
// artista/álbum, fica solta até correção manual (0018/0050, a fila de
// revisão/órfãos genérica já cobre isso, sem tela nova -- ver §3 do
// briefing). Diferente de acharOuCriarDraftSerie (serie.go), NÃO faz
// find-or-create por título: uma "faixa" sem match plausível não tem um
// título único o bastante para dedup seguro (duas faixas de artistas
// diferentes podem se chamar "Intro") -- cada arquivo sem match vira seu
// próprio draft. Escreve pela conexão de ESCRITA diretamente (fora da
// transação do lote), mesmo padrão de acharOuCriarDraftSerie -- roda na fase
// de fatos, não na de gravação.
func (r *scanRun) criarFaixaDraft(ctx context.Context, tags mediainfo.AudioTags, rawNome string) string {
	titulo := tags.Title
	if titulo == "" {
		titulo = rawNome
	}
	q := sqlcgen.New(r.m.db.Write)
	id := r.m.novoID()
	if _, err := q.CriarEntity(ctx, sqlcgen.CriarEntityParams{ID: id, Type: tipoFaixa, Title: titulo}); err != nil {
		log.Printf("scanner: falha ao criar faixa rascunho %q: %v", titulo, err)
		return ""
	}
	if _, err := q.AtualizarEntity(ctx, sqlcgen.AtualizarEntityParams{
		ID: id, Type: tipoFaixa, Title: titulo, Status: "draft",
	}); err != nil {
		log.Printf("scanner: falha ao rebaixar faixa rascunho %q para draft: %v", titulo, err)
	}
	return id
}

// gravarArquivoMusica persiste o arquivo de áudio: vínculo 1:1 com a FAIXA já
// resolvida por factsMusica (media_file.entity_id aponta DIRETO pra ela --
// ADR-0027 item 5, mesmo padrão de filme, NUNCA a junção N:M de série).
// Dedup por content_hash (rename/move) e por path (conteúdo trocado no
// lugar) -- reaproveita reencontrar/atualizarConteudoMudou de match.go, o
// MESMO mecanismo de filme (nenhuma lógica nova de dedup).
func (r *scanRun) gravarArquivoMusica(ctx context.Context, q *sqlcgen.Queries, f fileFacts) error {
	if f.faixaID == "" {
		log.Printf("scanner: pulando %q: faixa nao resolvivel (nem rascunho -- provavel falha de escrita)", f.path)
		return nil
	}

	existente, err := q.ObterMediaFilePorContentHash(ctx, nsOpt(f.hash))
	if err == nil {
		return r.reencontrar(ctx, q, existente, f)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	porPath, err := q.ObterMediaFilePorPath(ctx, f.path)
	if err == nil {
		return r.atualizarConteudoMudou(ctx, q, porPath, f)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	_, err = q.CriarMediaFile(ctx, sqlcgen.CriarMediaFileParams{
		ID:              r.m.novoID(),
		EntityID:        f.faixaID,
		Path:            f.path,
		ContentHash:     nsOpt(f.hash),
		Size:            niInt(f.size),
		Mtime:           nsOpt(f.mtime),
		MatchConfidence: nfFloat(f.faixaConf),
		MediainfoJson:   nsBytes(f.mediainfo),
		State:           "present",
		LastSeen:        nsOpt(r.scanTS),
		LibrarySourceID: nsOpt(r.source.ID),
	})
	return err
}
