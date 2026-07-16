// Package midiasfaltantes implementa o Handler do job `midias_faltantes`
// (briefing 0064, fecha o ciclo do 0062 em escala): periódico, só LÊ o banco
// (nunca lista diretório -- essa é a fronteira com o scan/0009), materializa
// na pasta REAL da biblioteca a arte "hoje vencedora" (mesmo agregador da
// galeria, montarGruposArtwork/0048) e o .nfo (0062) de toda entity que já
// tem arquivo presente mas ainda não tem essa mídia no disco. Mesmo padrão de
// interfaces pequenas injetadas do internal/job/libraryhealth (briefing 0053)
// -- evita ciclo de import (o Handler é construído DE DENTRO do server,
// server.WithMidiasFaltantesJob, com o próprio *server.Server satisfazendo as
// duas interfaces abaixo estruturalmente).
package midiasfaltantes

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
)

// nomeJob é o Handler.Nome() -- casa com o slug seedado em job_definition
// pela migration 0016.
const nomeJob = "midias_faltantes"

// EntityLister é o subconjunto de *server.Server que o Handler consome: o
// universo elegível (toda entity com pelo menos um media_file 'present',
// briefing 0064 §4). Interface pequena para permitir um fake em teste sem
// banco real.
type EntityLister interface {
	ListarEntitiesComMediaFilePresente(ctx context.Context) ([]string, error)
}

// Materializador é o subconjunto de *server.Server que o Handler consome:
// materializar, para UMA entity, a arte vencedora (por kind) que ainda
// faltava no disco + o NFO quando ausente -- reusa os MESMOS mecanismos do
// briefing 0062 (materializarAsset/gravarNFO) e do agregador da galeria
// (montarGruposArtwork, 0048), nunca escolhe um candidato novo. artesBaixadas
// lista os kinds efetivamente materializados nesta chamada (vazio quando
// nada faltava ou não havia candidato); nfoGravado indica se o .nfo foi
// (re)gravado (só quando estava ausente).
type Materializador interface {
	MaterializarMidiasFaltantes(ctx context.Context, entityID string) (artesBaixadas []string, nfoGravado bool, err error)
}

// Handler é o Handler do job midias_faltantes.
type Handler struct {
	entities       EntityLister
	materializador Materializador
}

// New cria o Handler a partir do listador de entities elegíveis e do
// materializador -- injetados pelo chamador (server.WithMidiasFaltantesJob),
// mesmo padrão de libraryhealth.New.
func New(entities EntityLister, materializador Materializador) *Handler {
	return &Handler{entities: entities, materializador: materializador}
}

// Nome devolve o slug estável do job.
func (h *Handler) Nome() string { return nomeJob }

// Executar processa TODA entity elegível numa única execução (sem paginação
// dedicada -- o custo real é rede/download, não a iteração do banco, mesmo
// espírito do briefing 0064 §4) e resume quantas foram processadas, quantas
// artes baixadas no total e quantos NFOs gravados. Uma falha EM UMA entity
// (rede, disco) não aborta as demais -- best-effort, só loga e segue (mesmo
// espírito do restante do sistema de jobs, 0053/0054/0056); erro só é
// devolvido se a PRÓPRIA listagem de entities elegíveis falhar (sem
// universo, não há o que processar).
func (h *Handler) Executar(ctx context.Context, _ json.RawMessage) (string, error) {
	ids, err := h.entities.ListarEntitiesComMediaFilePresente(ctx)
	if err != nil {
		return "", fmt.Errorf("listar entities elegiveis: %w", err)
	}

	var processadas, artesTotal, nfosGravados, falhas int
	for _, id := range ids {
		artes, nfo, err := h.materializador.MaterializarMidiasFaltantes(ctx, id)
		if err != nil {
			falhas++
			log.Printf("midias_faltantes: falha ao materializar a entity %q (best-effort, ignorado): %v", id, err)
			continue
		}
		processadas++
		artesTotal += len(artes)
		if nfo {
			nfosGravados++
		}
	}

	return montarMensagem(len(ids), processadas, artesTotal, nfosGravados, falhas), nil
}

// montarMensagem compõe o resumo final: quantas entities elegíveis, quantas
// processadas com sucesso (com o total de falhas, se houver), quantas artes
// materializadas no total e quantos NFOs gravados.
func montarMensagem(total, processadas, artesTotal, nfosGravados, falhas int) string {
	base := fmt.Sprintf("%d entity(ies) elegível(is), %d processada(s), %d arte(s) materializada(s), %d NFO(s) gravado(s)",
		total, processadas, artesTotal, nfosGravados)
	if falhas > 0 {
		base = fmt.Sprintf("%s, %d falha(s)", base, falhas)
	}
	return base
}
