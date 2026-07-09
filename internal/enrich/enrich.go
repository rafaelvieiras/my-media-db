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
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

const (
	// timeoutImagem limita o download de UM pôster; estouro degrada (sem pôster).
	timeoutImagem = 15 * time.Second
	// tamanhoMaxImagem é o teto de bytes de um pôster baixado (guarda contra uma
	// fonte hostil/gigante). 20 MiB cobre com folga um w342.
	tamanhoMaxImagem = 20 << 20
	// kindPoster/originExternal são as etiquetas do asset cacheado.
	kindPoster     = "poster"
	originExternal = "external"
)

// Servico executa o enriquecimento. Recebe os pools de banco, o provider (fonte) e
// o diretório confiável de assets (onde o binário do pôster é gravado).
type Servico struct {
	database  *db.DB
	provider  provider.Provider
	assetsDir string
	// httpImagem baixa o binário do pôster (timeout curto). Injetável em teste.
	httpImagem *http.Client
	// novoID gera ids de linhas novas (media_external/asset). Injetável em teste.
	novoID func() string
}

// New cria o serviço. provider e assetsDir são obrigatórios para operar; o main só
// constrói o serviço quando ambos existem (sem credencial, enrich fica desligado).
func New(database *db.DB, prov provider.Provider, assetsDir string) *Servico {
	return &Servico{
		database:   database,
		provider:   prov,
		assetsDir:  assetsDir,
		httpImagem: &http.Client{Timeout: timeoutImagem},
		novoID:     func() string { return uuid.NewString() },
	}
}

// Enriquecer enriquece uma entity JÁ EXISTENTE: puxa o detalhe, mescla a
// identidade externa e persiste a base externa + pôster. A falha da fonte é
// propagada; a da imagem degrada. Fluxo do POST /entities/{id}/enrich.
func (s *Servico) Enriquecer(ctx context.Context, entityID, source, sourceID, tipo string) error {
	raw, err := s.provider.BuscarDetalhe(ctx, sourceID, tipo)
	if err != nil {
		return fmt.Errorf("enrich: buscar detalhe: %w", err)
	}
	if err := s.mesclarExternalIds(ctx, entityID, source, sourceID); err != nil {
		return err
	}
	return s.persistir(ctx, entityID, source, sourceID, raw)
}

// Importar é o find-or-create por id externo (MD4) do POST /import: se já existe
// uma entity com aquele id na fonte, reusa e re-enriquece (criada=false); senão
// cria (status DEFAULT 'cataloged', título inicial do detalhe) e enriquece
// (criada=true). Devolve o id da entity resolvível pelo chamador.
func (s *Servico) Importar(ctx context.Context, source, sourceID, tipo string) (id string, criada bool, err error) {
	if existente, ok, err := s.acharPorIDExterno(ctx, source, sourceID); err != nil {
		return "", false, err
	} else if ok {
		return existente, false, s.Enriquecer(ctx, existente, source, sourceID, tipo)
	}

	// Não existe: puxa o detalhe uma vez (para o título inicial) e cria a entity
	// já com a identidade externa; em seguida persiste a base externa e o pôster.
	raw, err := s.provider.BuscarDetalhe(ctx, sourceID, tipo)
	if err != nil {
		return "", false, fmt.Errorf("enrich: buscar detalhe: %w", err)
	}
	titulo := s.tituloInicial(raw, sourceID)

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
	if u, ok := campos["poster_url"].(string); ok && u != "" {
		if err := s.cachearPoster(ctx, entityID, u); err != nil {
			log.Printf("enrich: cache do poster de %s falhou (segue sem poster): %v", entityID, err)
		}
	}
	return nil
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

// cachearPoster baixa o binário do pôster, grava em {assetsDir}/{entityID}/poster.<ext>
// e cria (ou reusa, dedup) o asset origin='external'. Nunca casa/apaga arte
// origin='user': o lookup filtra por origin. Erro é devolvido para o chamador
// apenas LOGAR (degrada), nunca abortar o enrich.
func (s *Servico) cachearPoster(ctx context.Context, entityID, posterURL string) error {
	dados, contentType, err := s.baixar(ctx, posterURL)
	if err != nil {
		return err
	}

	dir := filepath.Join(s.assetsDir, entityID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	destino := filepath.Join(dir, "poster"+extensaoImagem(contentType, posterURL))
	if err := os.WriteFile(destino, dados, 0o644); err != nil {
		return err
	}
	return s.registrarAsset(ctx, entityID, destino)
}

// registrarAsset faz o DEDUP do asset de pôster externo: se já existe um (mesma
// entity, kind='poster', origin='external'), atualiza seu caminho (removendo o
// binário antigo se a extensão mudou) em vez de duplicar; senão cria um novo.
func (s *Servico) registrarAsset(ctx context.Context, entityID, destino string) error {
	existente, err := s.leitura().ObterAssetPorEntityKindOrigin(ctx, sqlcgen.ObterAssetPorEntityKindOriginParams{
		EntityID: entityID,
		Kind:     kindPoster,
		Origin:   originExternal,
	})
	if err != nil {
		if isNoRows(err) {
			_, cerr := s.escrita().CriarAsset(ctx, sqlcgen.CriarAssetParams{
				ID:        s.novoID(),
				EntityID:  entityID,
				Kind:      kindPoster,
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
func (s *Servico) tituloInicial(raw json.RawMessage, sourceID string) string {
	if campos, err := s.provider.MapearDetalhe(raw); err == nil {
		if t, ok := campos["title"].(string); ok && t != "" {
			return t
		}
	}
	return sourceID
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

// isNoRows diz se o erro é o sql.ErrNoRows (linha ausente — não é falha real).
func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }
