// Package trailertema implementa o Handler do job `trailer_tema_faltantes`
// (briefing 0104, ADR-0026): periódico, best-effort, varre toda a
// biblioteca (mesmo universo elegível de midias_faltantes,
// ListarEntitiesComMediaFilePresente) baixando trailer/tema musical faltante
// -- reaproveita 100% da lógica de download já existente
// (server.BaixarTrailerEntity/BaixarTemaEntity, extraídas dos botões manuais
// 0102/0103), nunca escolhe um candidato diferente do que já está cacheado
// (media_external, source=tmdb/tvdb/themerrdb). Mesmo padrão de interfaces
// pequenas injetadas do internal/job/midiasfaltantes -- evita ciclo de
// import (o Handler é construído DE DENTRO do server, via
// server.WithTrailerTemaJob, com o próprio *server.Server satisfazendo as
// duas interfaces abaixo estruturalmente).
package trailertema

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
)

// nomeJob é o Handler.Nome() -- casa com o slug seedado em job_definition
// pela migration deste briefing.
const nomeJob = "trailer_tema_faltantes"

// EntityLister é o subconjunto de *server.Server que o Handler consome: o
// universo elegível (toda entity com pelo menos um media_file 'present',
// mesmo critério de midiasfaltantes.EntityLister). Interface pequena para
// permitir um fake em teste sem banco real.
type EntityLister interface {
	ListarEntitiesComMediaFilePresente(ctx context.Context) ([]string, error)
}

// YtdlpDisponivel é checado UMA VEZ no início de Executar (não por entity,
// briefing 0104 §4) -- evita golpear o binário yt-dlp (--version) uma vez
// por entity da biblioteca quando ele está simplesmente indisponível.
// disponivel=false não é falha do job: Executar encerra cedo com mensagem
// informativa, sem tentar baixar nada.
type YtdlpDisponivel interface {
	YtdlpDisponivel(ctx context.Context) (disponivel bool, detalhe string)
}

// TrailerTemaBaixador é o subconjunto de *server.Server que o Handler
// consome: baixar (best-effort) trailer/tema para UMA entity -- reaproveita
// as MESMAS funções puras extraídas dos botões manuais (0102/0103) pelo
// briefing 0104, sem escolher um candidato diferente do que a UI/o botão
// manual já usaria. baixado=false, err=nil cobre TODOS os casos de "nada a
// fazer" (sem candidato, já existe local, sem destino, yt-dlp indisponível);
// err só carrega falha real (rede, banco).
type TrailerTemaBaixador interface {
	YtdlpDisponivel
	BaixarTrailerEntity(ctx context.Context, entityID string) (baixado bool, err error)
	BaixarTemaEntity(ctx context.Context, entityID string) (baixado bool, err error)
}

// Handler é o Handler do job trailer_tema_faltantes.
type Handler struct {
	entities EntityLister
	baixador TrailerTemaBaixador
}

// New cria o Handler a partir do listador de entities elegíveis e do
// baixador -- injetados pelo chamador (server.WithTrailerTemaJob), mesmo
// padrão de midiasfaltantes.New.
func New(entities EntityLister, baixador TrailerTemaBaixador) *Handler {
	return &Handler{entities: entities, baixador: baixador}
}

// Nome devolve o slug estável do job.
func (h *Handler) Nome() string { return nomeJob }

// Executar processa TODA entity elegível numa única execução, best-effort
// por entity (erro numa não aborta as outras -- mesmo espírito do
// midias_faltantes/0053/0054/0056), e resume quantos trailers/temas foram
// baixados. O status do yt-dlp é checado UMA VEZ no início (não por
// entity) -- se indisponível, a execução encerra cedo com mensagem
// informativa, sem tentar nada (nem contar como falha). Erro só é devolvido
// se a PRÓPRIA listagem de entities elegíveis falhar (sem universo, não há
// o que processar).
func (h *Handler) Executar(ctx context.Context, _ json.RawMessage) (string, error) {
	if disponivel, detalhe := h.baixador.YtdlpDisponivel(ctx); !disponivel {
		msg := "yt-dlp indisponivel"
		if detalhe != "" {
			msg += ": " + detalhe
		}
		return msg + " -- nenhuma tentativa realizada", nil
	}

	ids, err := h.entities.ListarEntitiesComMediaFilePresente(ctx)
	if err != nil {
		return "", fmt.Errorf("listar entities elegiveis: %w", err)
	}

	var trailersBaixados, temasBaixados, falhas int
	for _, id := range ids {
		if baixou, err := h.baixador.BaixarTrailerEntity(ctx, id); err != nil {
			falhas++
			log.Printf("trailer_tema_faltantes: falha ao baixar trailer da entity %q (best-effort, ignorado): %v", id, err)
		} else if baixou {
			trailersBaixados++
		}

		if baixou, err := h.baixador.BaixarTemaEntity(ctx, id); err != nil {
			falhas++
			log.Printf("trailer_tema_faltantes: falha ao baixar tema da entity %q (best-effort, ignorado): %v", id, err)
		} else if baixou {
			temasBaixados++
		}
	}

	return montarMensagem(len(ids), trailersBaixados, temasBaixados, falhas), nil
}

// montarMensagem compõe o resumo final: quantas entities elegíveis, quantos
// trailers e quantos temas baixados no total.
func montarMensagem(total, trailersBaixados, temasBaixados, falhas int) string {
	base := fmt.Sprintf("%d entity(ies) elegível(is), %d trailer(s) baixado(s), %d tema(s) musical(is) baixado(s)",
		total, trailersBaixados, temasBaixados)
	if falhas > 0 {
		base = fmt.Sprintf("%s, %d falha(s)", base, falhas)
	}
	return base
}
