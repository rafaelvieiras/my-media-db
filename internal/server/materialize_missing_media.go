package server

// materialize_missing_media.go implementa POST
// /api/v1/entities/{id}/materialize-missing-media (briefing 0088, menu
// Gerenciar > "Baixar Artes Faltantes"): ação SEMPRE visível (não depende de
// catalog_completeness) que primeiro força uma NOVA consulta ao(s)
// ArteProvider(s) registrado(s) (hoje: fanart.tv) para atualizar o
// inventário de artes da entity (media_external(source='fanarttv')) e, em
// seguida, reaproveita SEM ALTERAR a mesma materialização do job periódico
// midias_faltantes (MaterializarMidiasFaltantes, midias_faltantes.go,
// briefing 0064) para gravar na pasta real da biblioteca a arte hoje
// VENCEDORA de cada kind ainda sem local_path, e o .nfo quando ausente.
//
// A atualização do inventário é BEST-EFFORT (degradação silenciosa,
// AGENTS.md §6): sem s.enrich configurado (nenhuma fonte externa com
// credencial) ou sem match no(s) provider(s) de arte, o passo é pulado/loga
// e a materialização roda do mesmo jeito sobre o inventário que já existir
// — nunca torna o request inteiro um erro.

import (
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// materializeMissingMediaEntity trata POST
// /api/v1/entities/{id}/materialize-missing-media. 404 se a entity não
// existe; 200 sempre depois disso (MaterializeMissingMediaResult, mesmo
// shape que MaterializarMidiasFaltantes já devolve) — cada subetapa é
// best-effort, nunca 5xx só por "o provider de arte não achou nada novo".
func (s *Server) materializeMissingMediaEntity(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()
	ent, ok := s.exigirEntity(w, ctx, id)
	if !ok {
		return
	}

	// (1) Nova consulta ao(s) ArteProvider(s) registrado(s) para atualizar o
	// inventário de artes da entity — best-effort: sem s.enrich (sem fonte
	// externa configurada) ou sem capacidade/match no provider, apenas segue
	// para materializar sobre o inventário que já existir (degradação
	// silenciosa, AGENTS.md §6).
	if s.enrich != nil {
		langs, err := s.idiomasPermitidos(ctx, w)
		if err != nil {
			return
		}
		if err := s.enrich.AtualizarInventarioArtes(ctx, id, ent.Type, langs); err != nil {
			log.Printf("materialize-missing-media: atualizar inventario de artes da entity %q falhou (segue com o inventario atual): %v", id, err)
		}
	}

	// (2) MESMA materialização do job midias_faltantes, sem alterar sua
	// lógica: grava na pasta real a arte hoje vencedora de cada kind ainda
	// sem local_path, e o .nfo quando ausente.
	artesBaixadas, nfoGravado, err := s.MaterializarMidiasFaltantes(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}

	writeJSON(w, http.StatusOK, materializeMissingMediaResultDTO{
		ArtesBaixadas: artesBaixadasOuVazio(artesBaixadas),
		NfoGravado:    nfoGravado,
	})
}

// materializeMissingMediaResultDTO é a resposta de POST
// /entities/{id}/materialize-missing-media (MaterializeMissingMediaResult no
// contrato, openapi.yaml) — espelha o retorno de MaterializarMidiasFaltantes.
type materializeMissingMediaResultDTO struct {
	ArtesBaixadas []string `json:"artes_baixadas"`
	NfoGravado    bool     `json:"nfo_gravado"`
}

// artesBaixadasOuVazio devolve um slice NUNCA nil (serializa `[]`, não
// `null`), mesmo padrão de downloadMissingArtworkResultDTO.
func artesBaixadasOuVazio(artes []string) []string {
	if artes == nil {
		return []string{}
	}
	return artes
}
