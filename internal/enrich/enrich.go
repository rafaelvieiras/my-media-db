// Package enrich é o serviço de ENRIQUECIMENTO (import explícito, briefing 0015):
// puxa o detalhe de um provider, persiste a base externa (media_external, payload
// cru — ADR-0011), grava a identidade autoritativa (external_ids, MD4) e cacheia o
// pôster em disco como asset origin='external'. É NÃO-DESTRUTIVO (ADR-0012): nunca
// toca media_override nem arte origin='user'. Todas as escritas passam pelo pool de
// escrita (escritor único, ADR-0010). A falha da IMAGEM degrada (loga, segue sem
// pôster); a falha da FONTE (BuscarDetalhe) é propagada ao chamador.
package enrich

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/externaldetail"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	// timeoutImagem limita o download de UM pôster; estouro degrada (sem pôster).
	timeoutImagem = 15 * time.Second
	// tamanhoMaxImagem é o teto de bytes de um pôster baixado (guarda contra uma
	// fonte hostil/gigante). 20 MiB cobre com folga um w342.
	tamanhoMaxImagem = 20 << 20
	// kindPoster/kindFanart/originExternal são as etiquetas dos assets cacheados.
	// fanart = imagem de fundo (backdrop); cacheada localmente como o pôster para
	// não depender da fonte externa em runtime (mesmo motivo do pôster, 0015).
	kindPoster     = "poster"
	kindFanart     = "fanart"
	originExternal = "external"
	// tipoColecao/relacaoColecao: a coleção é uma entity `type='collection'` (briefing
	// 0025) e seus filmes ligam-se por entity_relation com kind='collection'.
	tipoColecao    = "collection"
	relacaoColecao = "collection"
)

// Servico executa o enriquecimento. Recebe os pools de banco, o provider (fonte) e
// o diretório confiável de assets (onde o binário do pôster é gravado).
type Servico struct {
	database  *db.DB
	provider  provider.Provider
	assetsDir string
	// detalhe resolve o detalhe externo com CACHE-FIRST (external_detail, 0022): o
	// import/enrich passa a puxar por aqui, reusando o cache do detalhe rico quando
	// fresco (sem 2ª ida à fonte) e semeando media_external a partir dele.
	detalhe *externaldetail.Servico
	// httpImagem baixa o binário do pôster (timeout curto). Injetável em teste.
	httpImagem *http.Client
	// novoID gera ids de linhas novas (media_external/asset). Injetável em teste.
	novoID func() string
}

// New cria o serviço. provider e assetsDir são obrigatórios para operar; o main só
// constrói o serviço quando ambos existem (sem credencial, enrich fica desligado). O
// cache do detalhe externo (external_detail, 0022) é construído aqui e compartilha a
// tabela com o handler GET do detalhe externo (o estado é o banco, não a instância).
func New(database *db.DB, prov provider.Provider, assetsDir string) *Servico {
	return &Servico{
		database:   database,
		provider:   prov,
		assetsDir:  assetsDir,
		detalhe:    externaldetail.New(database, prov, 0),
		httpImagem: &http.Client{Timeout: timeoutImagem},
		novoID:     func() string { return uuid.NewString() },
	}
}

// Enriquecer enriquece uma entity JÁ EXISTENTE: puxa o detalhe (nos idiomas
// permitidos `langs`, briefing 0020), mescla a identidade externa e persiste a base
// externa + pôster + original_language. A falha da fonte é propagada; a da imagem
// degrada. Fluxo do POST /entities/{id}/enrich.
func (s *Servico) Enriquecer(ctx context.Context, entityID, source, sourceID, tipo string, langs []string) error {
	raw, err := s.detalhe.PayloadFresco(ctx, source, sourceID, tipo, langs)
	if err != nil {
		return fmt.Errorf("enrich: buscar detalhe: %w", err)
	}
	if err := s.mesclarExternalIds(ctx, entityID, source, sourceID); err != nil {
		return err
	}
	if err := s.persistir(ctx, entityID, source, sourceID, raw); err != nil {
		return err
	}
	return s.curarTitulo(ctx, entityID, sourceID, raw, langs)
}

// Importar é o find-or-create por id externo (MD4) do POST /import: se já existe
// uma entity com aquele id na fonte, reusa e re-enriquece (criada=false); senão
// cria (status DEFAULT 'cataloged', título inicial do detalhe) e enriquece
// (criada=true). Devolve o id da entity resolvível pelo chamador.
func (s *Servico) Importar(ctx context.Context, source, sourceID, tipo string, langs []string) (id string, criada bool, err error) {
	if tipo == "collection" {
		return s.importarColecao(ctx, source, sourceID, langs)
	}
	if existente, ok, err := s.acharPorIDExterno(ctx, source, sourceID); err != nil {
		return "", false, err
	} else if ok {
		return existente, false, s.Enriquecer(ctx, existente, source, sourceID, tipo, langs)
	}

	// Não existe: puxa o detalhe uma vez (para o título inicial) e cria a entity
	// já com a identidade externa; em seguida persiste a base externa e o pôster. O
	// detalhe vem do cache-first (external_detail): a 1ª ida grava, próximas reusam.
	raw, err := s.detalhe.PayloadFresco(ctx, source, sourceID, tipo, langs)
	if err != nil {
		return "", false, fmt.Errorf("enrich: buscar detalhe: %w", err)
	}
	titulo := s.tituloInicial(raw, sourceID, langs)

	extIDs, err := json.Marshal(map[string]string{source: sourceID})
	if err != nil {
		return "", false, err
	}
	novo := s.novoID()
	if _, err := s.escrita().CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:              novo,
		Type:            tipo,
		Title:           titulo,
		ExternalIdsJson: nsValid(string(extIDs)),
	}); err != nil {
		return "", false, err
	}
	if err := s.persistir(ctx, novo, source, sourceID, raw); err != nil {
		return novo, true, err
	}
	return novo, true, nil
}

// importarColecao importa uma COLEÇÃO (briefing 0025) como entity `type='collection'`:
// find-or-create da coleção (dedup por id tmdb, restrito a type='collection'), cacheia
// as artes do conjunto (pôster/fundo) e importa CADA filme membro (reusando o Importar
// de movie, que faz find-or-create + enrich), ligando-o via entity_relation
// (kind='collection'). É idempotente: re-importar reusa a coleção e só liga membros
// ainda não ligados. A falha de UMA parte degrada (loga e segue) — a coleção parcial é
// válida. Requer a capacidade opcional provider.ColecaoProvider (senão erro → 502).
func (s *Servico) importarColecao(ctx context.Context, source, sourceID string, langs []string) (string, bool, error) {
	cp, ok := s.provider.(provider.ColecaoProvider)
	if !ok {
		return "", false, fmt.Errorf("enrich: fonte %q nao suporta colecao", source)
	}
	col, err := cp.BuscarColecao(ctx, sourceID, langPrincipal(langs))
	if err != nil {
		return "", false, fmt.Errorf("enrich: buscar colecao: %w", err)
	}
	colID, criada, err := s.acharOuCriarColecao(ctx, source, sourceID, col.Name)
	if err != nil {
		return "", false, err
	}
	s.cachearArtes(ctx, colID, map[string]any{
		"poster_url":   derefStr(col.PosterURL),
		"backdrop_url": derefStr(col.BackdropURL),
	})
	s.ligarMembros(ctx, colID, source, col.Parts, langs)
	return colID, criada, nil
}

// ligarMembros importa cada filme membro (find-or-create + enrich) e o liga à coleção
// por entity_relation(kind='collection'). É idempotente: carrega os já-ligados e pula
// os que já existem (o UNIQUE ainda protege de corrida). Falha de UMA parte degrada
// (loga e segue) — a coleção parcial é válida.
func (s *Servico) ligarMembros(ctx context.Context, colID, source string, parts []provider.ResultadoExterno, langs []string) {
	ligados := map[string]bool{}
	if rels, err := s.leitura().ListarRelacoesFilhas(ctx, colID); err == nil {
		for _, r := range rels {
			if r.Kind == relacaoColecao {
				ligados[r.ChildID] = true
			}
		}
	}
	for _, p := range parts {
		filmeID, _, err := s.Importar(ctx, source, p.SourceID, p.Type, langs)
		if err != nil {
			log.Printf("enrich: importar parte %s da colecao falhou (segue): %v", p.SourceID, err)
			continue
		}
		if ligados[filmeID] {
			continue
		}
		if _, err := s.escrita().CriarEntityRelation(ctx, sqlcgen.CriarEntityRelationParams{
			ID: s.novoID(), ParentID: colID, ChildID: filmeID, Kind: relacaoColecao,
		}); err != nil {
			log.Printf("enrich: ligar parte %s a colecao %s falhou (segue): %v", filmeID, colID, err)
			continue
		}
		ligados[filmeID] = true
	}
}

// langPrincipal devolve o primeiro idioma (principal) da lista, ou "" quando vazia —
// a coleção é mono-idioma pragmático (0025), busca só no idioma principal.
func langPrincipal(langs []string) string {
	if len(langs) > 0 {
		return langs[0]
	}
	return ""
}

// acharOuCriarColecao devolve o id da entity coleção: reusa a existente (dedup por id
// tmdb + type='collection') ou cria uma nova `type='collection'` com o nome do conjunto
// e a identidade externa {source: sourceID}. `criada` diz se nasceu agora (para o 201).
func (s *Servico) acharOuCriarColecao(ctx context.Context, source, sourceID, nome string) (string, bool, error) {
	if source == "tmdb" {
		existente, err := s.leitura().ObterColecaoPorTmdb(ctx, nsValid(sourceID))
		if err == nil {
			return existente.ID, false, nil
		}
		if !isNoRows(err) {
			return "", false, err
		}
	}
	titulo := nome
	if titulo == "" {
		titulo = sourceID
	}
	extIDs, err := json.Marshal(map[string]string{source: sourceID})
	if err != nil {
		return "", false, err
	}
	novo := s.novoID()
	if _, err := s.escrita().CriarEntity(ctx, sqlcgen.CriarEntityParams{
		ID:              novo,
		Type:            tipoColecao,
		Title:           titulo,
		ExternalIdsJson: nsValid(string(extIDs)),
	}); err != nil {
		return "", false, err
	}
	return novo, true, nil
}

// acharPorIDExterno procura uma entity pelo id na fonte (dedup MD4). Hoje só a
// fonte "tmdb" tem coluna gerada indexada; outras fontes ainda não deduplicam
// (find-or-create sempre cria) — segue o escopo do MVP.
func (s *Servico) acharPorIDExterno(ctx context.Context, source, sourceID string) (string, bool, error) {
	if source != "tmdb" {
		return "", false, nil
	}
	ent, err := s.leitura().ObterEntityPorTmdb(ctx, nsValid(sourceID))
	if err != nil {
		if isNoRows(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return ent.ID, true, nil
}

// persistir grava a base externa (UPSERT do payload cru por entity_id+source) e
// dispara o cache do pôster. A imagem é best-effort: sua falha é logada e NÃO
// aborta o enrich (o item fica sem pôster, o import é parcial mas válido).
func (s *Servico) persistir(ctx context.Context, entityID, source, sourceID string, raw json.RawMessage) error {
	if _, err := s.escrita().UpsertMediaExternal(ctx, sqlcgen.UpsertMediaExternalParams{
		ID:          s.novoID(),
		EntityID:    entityID,
		Source:      source,
		SourceID:    nsValid(sourceID),
		PayloadJson: nsValid(string(raw)),
	}); err != nil {
		return err
	}

	campos, err := s.provider.MapearDetalhe(raw)
	if err != nil {
		log.Printf("enrich: mapear detalhe de %s falhou (segue sem poster): %v", entityID, err)
		return nil
	}
	// Grava a âncora do fallback (briefing 0020): original_language do detalhe. É um
	// fato da fonte (não dado do dono), então re-enriquecer apenas o reafirma.
	if ol, ok := campos["original_language"].(string); ok && ol != "" {
		if err := s.escrita().AtualizarOriginalLanguage(ctx, sqlcgen.AtualizarOriginalLanguageParams{
			OriginalLanguage: nsValid(ol),
			ID:               entityID,
		}); err != nil {
			return err
		}
	}
	// Denormaliza a release_date neutra na coluna base (mesmo tratamento de
	// original_language): fato da fonte, deixa a ordenacao/listagem por ano barata (ex.:
	// membros de colecao, 0025) sem resolver cada item. Nao muda a resolucao (base=externo).
	if rd, ok := campos["release_date"].(string); ok && rd != "" {
		if err := s.escrita().AtualizarReleaseDate(ctx, sqlcgen.AtualizarReleaseDateParams{
			ReleaseDate: nsValid(rd),
			ID:          entityID,
		}); err != nil {
			return err
		}
	}
	s.cachearArtes(ctx, entityID, campos)
	return nil
}

// cachearArtes cacheia localmente o pôster e o FUNDO (fanart/backdrop) do canônico,
// cada um best-effort (0015): a tela de detalhe rica serve as duas do disco, sem
// depender da fonte em runtime. Falha em uma só degrada (sem aquela arte), nunca
// aborta o enrich.
func (s *Servico) cachearArtes(ctx context.Context, entityID string, campos map[string]any) {
	if u, ok := campos["poster_url"].(string); ok && u != "" {
		if err := s.cachearArte(ctx, entityID, u, kindPoster, "poster"); err != nil {
			log.Printf("enrich: cache do poster de %s falhou (segue sem poster): %v", entityID, err)
		}
	}
	if u, ok := campos["backdrop_url"].(string); ok && u != "" {
		if err := s.cachearArte(ctx, entityID, u, kindFanart, "fanart"); err != nil {
			log.Printf("enrich: cache do backdrop de %s falhou (segue sem fundo): %v", entityID, err)
		}
	}
}

// mesclarExternalIds lê o external_ids atual, junta {source: sourceID} e grava — a
// identidade é autoritativa (MD4). Preserva ids de outras fontes; sobrescreve o da
// própria fonte (re-import sob id corrigido). Valores não-string do JSON legado são
// descartados (o modelo é string→string).
func (s *Servico) mesclarExternalIds(ctx context.Context, entityID, source, sourceID string) error {
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		return err
	}
	ids := map[string]string{}
	if ent.ExternalIdsJson.Valid && ent.ExternalIdsJson.String != "" {
		bruto := map[string]any{}
		if json.Unmarshal([]byte(ent.ExternalIdsJson.String), &bruto) == nil {
			for k, v := range bruto {
				if str, ok := v.(string); ok {
					ids[k] = str
				}
			}
		}
	}
	ids[source] = sourceID
	b, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return s.escrita().AtualizarExternalIds(ctx, sqlcgen.AtualizarExternalIdsParams{
		ExternalIdsJson: nsValid(string(b)),
		ID:              entityID,
	})
}

// cachearArte baixa o binário de uma arte (pôster ou fanart/backdrop), grava em
// {assetsDir}/{entityID}/{nomeBase}.<ext> e cria (ou reusa, dedup) o asset
// origin='external' do `kind`. Nunca casa/apaga arte origin='user' (o lookup
// filtra por origin). Erro é devolvido ao chamador apenas para LOGAR (degrada),
// nunca abortar o enrich.
func (s *Servico) cachearArte(ctx context.Context, entityID, url, kind, nomeBase string) error {
	dados, contentType, err := s.baixar(ctx, url)
	if err != nil {
		return err
	}

	dir := filepath.Join(s.assetsDir, entityID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	destino := filepath.Join(dir, nomeBase+extensaoImagem(contentType, url))
	if err := os.WriteFile(destino, dados, 0o644); err != nil {
		return err
	}
	return s.registrarAsset(ctx, entityID, destino, kind)
}

// registrarAsset faz o DEDUP do asset externo daquele `kind`: se já existe um
// (mesma entity, kind, origin='external'), atualiza seu caminho (removendo o
// binário antigo se a extensão mudou) em vez de duplicar; senão cria um novo.
func (s *Servico) registrarAsset(ctx context.Context, entityID, destino, kind string) error {
	existente, err := s.leitura().ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{
		EntityID: entityID,
		Kind:     kind,
		Origin:   originExternal,
	})
	if err != nil {
		if isNoRows(err) {
			_, cerr := s.escrita().CriarAsset(ctx, sqlcgen.CriarAssetParams{
				ID:        s.novoID(),
				EntityID:  entityID,
				Kind:      kind,
				Origin:    originExternal,
				PathOrUri: destino,
			})
			return cerr
		}
		return err
	}
	if existente.PathOrUri != destino {
		_ = os.Remove(existente.PathOrUri) // best-effort: some o binário antigo (ext diferente)
	}
	return s.escrita().AtualizarAssetPath(ctx, sqlcgen.AtualizarAssetPathParams{
		PathOrUri: destino,
		MetaJson:  existente.MetaJson,
		ID:        existente.ID,
	})
}

// baixar faz o GET do pôster com timeout e teto de tamanho. Devolve os bytes e o
// content-type. Status != 200 ou I/O é erro (o chamador degrada).
func (s *Servico) baixar(ctx context.Context, posterURL string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, posterURL, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := s.httpImagem.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("enrich: status %d ao baixar poster", resp.StatusCode)
	}
	dados, err := io.ReadAll(io.LimitReader(resp.Body, tamanhoMaxImagem))
	if err != nil {
		return nil, "", err
	}
	return dados, resp.Header.Get("Content-Type"), nil
}

// tituloInicial extrai o título do detalhe cru para o item recém-criado; cai para
// o sourceID quando o detalhe não traz título (item pobre) — nunca vazio (title é
// NOT NULL no schema).
func (s *Servico) tituloInicial(raw json.RawMessage, sourceID string, langs []string) string {
	if campos, err := s.provider.MapearDetalhe(raw); err == nil {
		if t := tituloLegivel(campos, langs); t != "" {
			return t
		}
	}
	return sourceID
}

// curarTitulo AUTO-CURA o "numero no lugar do nome" nos itens importados ANTES do
// conserto do tituloInicial: se o titulo base ainda e exatamente o fallback numerico
// (== sourceID gravado na criacao), recomputa do detalhe (idioma principal). A
// condicao e ESTRITA (== sourceID) de proposito -- um titulo legivel, mesmo que
// numerico de verdade ("1917"), nunca casa o id da fonte, entao jamais e sobrescrito.
// Nao toca override/externo (a resolucao segue dando autoridade ao dono). Degrada em
// silencio: qualquer tropeco aqui mantem o titulo atual (a cura e best-effort).
func (s *Servico) curarTitulo(ctx context.Context, entityID, sourceID string, raw json.RawMessage, langs []string) error {
	ent, err := s.leitura().ObterEntity(ctx, entityID)
	if err != nil {
		return err
	}
	if ent.Title != sourceID {
		return nil // titulo ja e legivel -- nada a curar
	}
	campos, err := s.provider.MapearDetalhe(raw)
	if err != nil {
		return nil
	}
	novo := tituloLegivel(campos, langs)
	if novo == "" || novo == ent.Title {
		return nil
	}
	return s.escrita().AtualizarTitulo(ctx, sqlcgen.AtualizarTituloParams{Title: novo, ID: entityID})
}

// tituloLegivel extrai um titulo exibivel do mapa canonico do provider, tolerando
// o campo "title" tanto como ESCALAR (payload legado, mono-idioma) quanto como
// map[idioma]valor (envelope multi-idioma do 0020). Prefere o titulo num dos
// idiomas permitidos (na ordem -- langs[0] e o principal), cai para o
// original_title e, por fim, para qualquer titulo presente. Devolve "" so quando
// nao ha nenhum titulo -- e o chamador entao usa o sourceID.
//
// Sem esta tolerancia, a assercao antiga `campos["title"].(string)` falhava para
// TODO item importado pos-0020 (o title virou map), gravando o id numerico da
// fonte como entity.title -- o "numero no lugar do nome" na lista.
func tituloLegivel(campos map[string]any, langs []string) string {
	switch t := campos["title"].(type) {
	case string:
		return t
	case map[string]any:
		return tituloDoMapa(t, comoString(campos["original_title"]), langs)
	}
	return ""
}

// tituloDoMapa escolhe um titulo do mapa idioma->valor: o primeiro idioma pedido
// (na ordem) com valor, senao o original_title, senao qualquer titulo presente. ""
// se o mapa nao tem nenhum.
func tituloDoMapa(porIdioma map[string]any, original string, langs []string) string {
	for _, l := range langs {
		if v := comoString(porIdioma[l]); v != "" {
			return v
		}
	}
	if original != "" {
		return original
	}
	for _, v := range porIdioma {
		if s := comoString(v); s != "" {
			return s
		}
	}
	return ""
}

// comoString devolve o valor como string quando ele e uma string; "" caso
// contrario (inclui nil). Enxuga as assercoes de tipo dos titulos canonicos.
func comoString(v any) string {
	s, _ := v.(string)
	return s
}

// escrita/leitura devolvem os Queries ligados aos pools de escrita/leitura (ADR-0010).
func (s *Servico) escrita() *sqlcgen.Queries { return sqlcgen.New(s.database.Write) }
func (s *Servico) leitura() *sqlcgen.Queries { return sqlcgen.New(s.database.Read) }

// extensaoImagem escolhe a extensão do arquivo do pôster: primeiro pelo
// content-type (o que a fonte declarou), depois pela extensão da URL; default .jpg.
func extensaoImagem(contentType, u string) string {
	switch {
	case strings.Contains(contentType, "png"):
		return ".png"
	case strings.Contains(contentType, "webp"):
		return ".webp"
	case strings.Contains(contentType, "jpeg"), strings.Contains(contentType, "jpg"):
		return ".jpg"
	}
	switch strings.ToLower(filepath.Ext(u)) {
	case ".png":
		return ".png"
	case ".webp":
		return ".webp"
	case ".jpg", ".jpeg":
		return ".jpg"
	}
	return ".jpg"
}

// nsValid embrulha uma string num sql.NullString válido (não-NULL).
func nsValid(s string) sql.NullString { return sql.NullString{String: s, Valid: true} }

// derefStr desreferencia um *string, devolvendo "" quando nil — para montar o mapa
// canônico que cachearArtes consome (ele ignora chave vazia).
func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// isNoRows diz se o erro é o sql.ErrNoRows (linha ausente — não é falha real).
func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
