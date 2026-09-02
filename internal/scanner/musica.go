package scanner

// musica.go é o núcleo do scanner de MÚSICA (briefing 0107/0115, ADR-0027/ADR-0029):
// quando a biblioteca é `content_type='music'`, cada arquivo de áudio é UMA faixa. O
// fluxo, por arquivo, na ordem em que tenta resolver (para na primeira que resolver):
//
//  1. MBID de faixa (recording) embutido na tag -> ObterEntityPorMbid direto (o
//     arquivo já foi catalogado antes, ou outro arquivo do mesmo álbum já trouxe essa
//     faixa nesta mesma execução ou numa anterior).
//  2. MBID de faixa SEM match ainda, mas a tag TAMBÉM tem o MBID do artista ->
//     import ESCOPADO (ADR-0029, enrich.ImportarFaixaPossuida): SÓ o artista
//     (identidade) + o ÁLBUM daquela faixa + a própria faixa, idempotente -- NUNCA a
//     discografia inteira -- e tenta o lookup de novo.
//  3. MBID de faixa SEM MBID de artista na tag -> resolução REVERSA
//     (provider.FaixaResolverProvider) faixa->artista, depois cai no mesmo caminho
//     do passo 2.
//  4. SEM NENHUM MBID embutido (caso DOMINANTE nos arquivos reais do dono -- os
//     scripts pessoais dele não gravam esse tag): busca textual do artista
//     (album_artist, com fallback para artist) no provider; candidato único e forte
//     o bastante dispara o MESMO import escopado do passo 2, mas pelo caminho
//     TEXTUAL (álbum por nome via MusicaAlbumProvider.BuscarAlbumPorNome, faixa por
//     título/track_number dentro dele) -- nunca a árvore inteira.
//  4.5. SEM match textual plausível (briefing 0126): ÚLTIMO fallback antes do
//     draft. Calcula o fingerprint acústico do ARQUIVO (fpcalc/Chromaprint,
//     subprocesso -- internal/fingerprint) e consulta a capacidade opcional
//     provider.FingerprintProvider (hoje só internal/provider/acoustid) --
//     achando um `recording` MBID com confiança >= matching.
//     LimiarConfiancaFingerprint, reaproveita o MESMO caminho de import
//     escopado do passo 2/3 (importarEAcharPorMbid), NUNCA a discografia
//     inteira. fpcalc ausente, provider sem a capacidade, ou candidato fraco
//     demais: degrada em silêncio, cai para o passo 5.
//  5. SEM MATCH PLAUSÍVEL nenhum: cria uma entity `faixa` RASCUNHO (status=draft),
//     só com os dados da tag (ou o nome do arquivo) -- SEM entity_relation de
//     artista/álbum, fica solta até correção manual (mesmo espírito de
//     acharOuCriarDraftSerie, mas sem find-or-create por título: cada arquivo sem
//     match vira seu próprio draft).
//
// O import de árvore INTEIRA (discografia completa) continua existindo
// (enrich.Importar, tipo="artista") mas só é disparado pelo import MANUAL
// deliberado do dono (busca -> importar artista) -- nunca por este scanner.
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
	"github.com/rafaelvieiras/my-media-db/internal/enrich"
	"github.com/rafaelvieiras/my-media-db/internal/matching"
	"github.com/rafaelvieiras/my-media-db/internal/mediainfo"
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
	// confFingerprint é a confiança do match por fingerprint acústico (passo
	// 4.5, briefing 0126) -- por princípio, ESTRITAMENTE abaixo de
	// confMusicaTextual (um escore de similaridade acústica não é uma prova
	// de identidade tão forte quanto um MBID embutido pelo dono NEM quanto
	// um nome batendo exato na busca textual) e ACIMA de confDraft (ainda é
	// um sinal utilizável, não um chute). Reaproveita confTituloSoNom (match
	// de filme só por nome, sem confirmação de ano) em vez de inventar um
	// terceiro número mágico -- mesmo espírito de confMusicaTextual acima.
	confFingerprint = confTituloSoNom
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
	var duracaoSegundos float64
	if len(mi) > 0 {
		var info mediainfo.Info
		if err := json.Unmarshal(mi, &info); err == nil {
			if info.Tags != nil {
				tags = *info.Tags
			}
			if info.General.Duration > 0 {
				duracaoSegundos = info.General.Duration
			}
		}
	}

	f.faixaID, f.faixaConf = r.resolverFaixa(ctx, path, tags, duracaoSegundos, f.rawNome)
	return f
}

// resolverFaixa executa o pipeline de matching descrito no comentário do
// pacote, na ordem: MBID de faixa direto -> MBID de faixa + import (com ou sem
// MBID de artista embutido) -> busca textual -> fingerprint acústico ->
// draft. Sempre devolve um faixaID utilizável (o passo 5 nunca falha, exceto
// erro de banco -- nesse caso "" e o chamador loga e pula o arquivo, ver
// gravarArquivoMusica). path é o caminho do arquivo no disco, necessário só
// pelo passo 4.5 (fpcalc lê o áudio bruto, não as tags já extraídas).
// duracaoSegundos é a duração do arquivo local extraída pelo probe
// (info.General.Duration, factsMusica acima), 0 quando desconhecida --
// repassada como sinal ADICIONAL de CONFIRMAÇÃO por duração (briefing 0121
// §8, matching.ToleranciaDuracaoSegundos) até o enrich.FaixaAlvo construído
// nos pontos de import escopado abaixo (importarEAcharPorMbid/
// matchTextualFaixa/matchPorFingerprint); nunca usada aqui para decidir nada
// por si só.
func (r *scanRun) resolverFaixa(ctx context.Context, path string, tags mediainfo.AudioTags, duracaoSegundos float64, rawNome string) (string, float64) {
	langs := r.langsPermitidos(ctx)

	if tags.MusicBrainzTrackID != "" {
		if id, ok := r.acharFaixaPorMbid(ctx, tags.MusicBrainzTrackID); ok {
			return id, confIDExterno
		}
		if id, ok := r.importarEAcharPorMbid(ctx, tags, duracaoSegundos, langs); ok {
			return id, confIDExterno
		}
	}

	if id, ok := r.matchTextual(ctx, tags, duracaoSegundos, langs); ok {
		return id, confMusicaTextual
	}

	if id, ok := r.matchPorFingerprint(ctx, path, duracaoSegundos, langs); ok {
		return id, confFingerprint
	}

	return r.criarFaixaDraft(ctx, tags, rawNome), confDraft
}

// acharFaixaPorMbid é o lookup PURO (sem rede) por MBID de recording -- o
// caminho mais barato: o arquivo já foi catalogado antes, ou o mesmo arquivo
// (renomeado/movido) já resolveu esta faixa antes nesta mesma execução.
func (r *scanRun) acharFaixaPorMbid(ctx context.Context, trackMBID string) (string, bool) {
	e, err := sqlcgen.New(r.m.db.Read).ObterEntityPorMbid(ctx, nsOpt(trackMBID))
	if err != nil {
		return "", false
	}
	return e.ID, true
}

// importarEAcharPorMbid resolve o MBID do ARTISTA (embutido na tag, ou via
// resolução reversa faixa->artista quando só o MBID de faixa está presente) e
// dispara o import ESCOPADO (ADR-0029, enrich.ImportarFaixaPossuida): só o
// artista (identidade) + o ÁLBUM daquela faixa (resolvido pelo MBID de
// recording via MusicaAlbumProvider.BuscarAlbumDaFaixa) + a própria faixa --
// NUNCA a discografia inteira. ok=false em qualquer degradação (sem
// importador configurado, sem MBID de artista resolvível, ou o import
// escopado falhou -- sem match plausível na fonte, sem suporte à capacidade
// seletiva, ou erro de rede/banco). duracaoSegundos (0 = desconhecida) é só
// repassada no enrich.FaixaAlvo como sinal ADICIONAL de confirmação (briefing
// 0121 §8) -- nunca decide nada por si só neste método.
func (r *scanRun) importarEAcharPorMbid(ctx context.Context, tags mediainfo.AudioTags, duracaoSegundos float64, langs []string) (string, bool) {
	if r.m.musicaImportador == nil {
		return "", false
	}
	artistaMBID, ok := r.resolverArtistaMBID(ctx, tags)
	if !ok {
		return "", false
	}
	alvo := enrich.FaixaAlvo{RecordingMBID: tags.MusicBrainzTrackID, DuracaoSegundos: duracaoSegundos}
	faixaID, _, err := r.m.musicaImportador.ImportarFaixaPossuida(ctx, sourceMusicBrainz, artistaMBID, alvo, langs)
	if err != nil {
		log.Printf("scanner: import escopado da faixa %s (artista %s) falhou: %v", tags.MusicBrainzTrackID, artistaMBID, err)
		return "", false
	}
	return faixaID, faixaID != ""
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
// reais do dono, ver AGENTS.md/briefing 0107) -- repasse fino para
// Manager.matchTextualFaixa (briefing 0120), extraído para ser reutilizável
// pela reconciliação de faixas-rascunho fora de um scanRun (reconciliar.go).
func (r *scanRun) matchTextual(ctx context.Context, tags mediainfo.AudioTags, duracaoSegundos float64, langs []string) (string, bool) {
	return r.m.matchTextualFaixa(ctx, tags, duracaoSegundos, langs)
}

// matchTextualFaixa busca o artista por nome (album_artist, com fallback para
// artist) no provider e, existindo UM ÚNICO candidato cujo título bate com o
// limiar de matching.LimiarAutoMatch (92%, o MESMO limiar já usado pelo
// auto-match de filme -- reaproveitado de propósito para não inventar um
// segundo número mágico) contra o nome retornado pela fonte, dispara o import
// ESCOPADO (ADR-0029, briefing 0115: enrich.ImportarFaixaPossuida) pelo
// caminho TEXTUAL -- álbum localizado por nome (tags.Album, obrigatório aqui)
// e a faixa por título/track_number dentro dele, NUNCA a árvore inteira.
// Ambíguo (>1 candidato empatado no topo), sem álbum na tag, ou nenhum
// candidato forte o bastante: ok=false (cai para o draft, passo 5) -- nunca
// aplica um match "parecido". Método de Manager (não scanRun, briefing 0120)
// para ser chamável tanto do scan (matchTextual acima) quanto da
// reconciliação de faixa-rascunho órfã (reconciliar.go). duracaoSegundos (0 =
// desconhecida) é a duração do arquivo local, repassada no enrich.FaixaAlvo
// como sinal ADICIONAL de confirmação (briefing 0121 §8, matching.
// ToleranciaDuracaoSegundos) -- nunca decide nada por si só neste método.
func (m *Manager) matchTextualFaixa(ctx context.Context, tags mediainfo.AudioTags, duracaoSegundos float64, langs []string) (string, bool) {
	if m.musicaProvider == nil || m.musicaImportador == nil {
		return "", false
	}
	nomeArtista := tags.AlbumArtist
	if nomeArtista == "" {
		nomeArtista = tags.Artist
	}
	if nomeArtista == "" || tags.Album == "" {
		return "", false
	}

	resultados, err := m.musicaProvider.Buscar(ctx, nomeArtista, "artista", "")
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

	alvo := enrich.FaixaAlvo{AlbumNome: tags.Album, FaixaTitulo: tags.Title, FaixaNumero: tags.TrackNumber, DuracaoSegundos: duracaoSegundos}
	faixaID, _, err := m.musicaImportador.ImportarFaixaPossuida(ctx, sourceMusicBrainz, artistaMBID, alvo, langs)
	if err != nil {
		log.Printf("scanner: import escopado (busca textual artista %q album %q) falhou: %v", nomeArtista, tags.Album, err)
		return "", false
	}
	return faixaID, faixaID != ""
}

// candidatoTexto é um par (id,título) candidato a match textual do ARTISTA
// (resultado de Provider.Buscar) -- o match textual de álbum/faixa dentro do
// release já resolvido é papel do provider (musicbrainz.melhorReleaseGroupPorTitulo)
// e do enrich (melhorFaixaPorTitulo), não deste pacote (briefing 0115).
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

// matchPorFingerprint é o passo 4.5 (briefing 0126): ÚLTIMO fallback antes
// do draft, disparado SÓ quando o passo 4 (textual) já falhou -- nunca
// antes, nunca em paralelo (o chamador, resolverFaixa, garante a ordem).
// Calcula o fingerprint acústico do ARQUIVO (fpcalc, subprocesso --
// internal/fingerprint) e consulta a capacidade opcional
// provider.FingerprintProvider (hoje só internal/provider/acoustid) contra
// ele. Qualquer degradação -- calculador/provider não configurados, fpcalc
// indisponível, erro de cálculo, provider sem a capacidade, sem candidato,
// ou confiança abaixo de matching.LimiarConfiancaFingerprint -- devolve
// ok=false SEM erro fatal: o chamador cai para o passo 5 (draft), mesmo
// contrato de degradação silenciosa de qualquer capacidade opcional do
// projeto. Achando um candidato plausível, reaproveita o MESMO caminho de
// import ESCOPADO dos passos 2/3 (importarEAcharPorMbid, ADR-0029): monta
// uma AudioTags SINTÉTICA só com o MusicBrainzTrackID do candidato -- ZERO
// lógica de import nova, o fingerprint só produz um RecordingMBID que entra
// no mesmo cano que o passo 2/3 já usa (resolução reversa artista<-faixa via
// provider.FaixaResolverProvider, quando necessário, + enrich.
// ImportarFaixaPossuida).
func (r *scanRun) matchPorFingerprint(ctx context.Context, path string, duracaoSegundos float64, langs []string) (string, bool) {
	if r.m.fingerprintCalculador == nil || r.m.fingerprintProvider == nil {
		return "", false
	}
	fpProv, ok := r.m.fingerprintProvider.(provider.FingerprintProvider)
	if !ok {
		return "", false
	}
	if !r.m.fingerprintCalculador.Disponivel() {
		return "", false
	}

	fp, _, err := r.m.fingerprintCalculador.CalcularFingerprint(ctx, path)
	if err != nil {
		log.Printf("scanner: calculo de fingerprint acustico de %q falhou: %v", path, err)
		return "", false
	}

	cand, ok, err := fpProv.IdentificarPorFingerprint(ctx, fp, duracaoSegundos)
	if err != nil {
		log.Printf("scanner: lookup de fingerprint acustico de %q falhou: %v", path, err)
		return "", false
	}
	if !ok || cand.Confianca < matching.LimiarConfiancaFingerprint {
		return "", false
	}

	log.Printf("scanner: fingerprint acustico identificou %q como recording %s (confianca=%.2f)", path, cand.RecordingMBID, cand.Confianca)
	tagsSinteticas := mediainfo.AudioTags{MusicBrainzTrackID: cand.RecordingMBID}
	return r.importarEAcharPorMbid(ctx, tagsSinteticas, duracaoSegundos, langs)
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
