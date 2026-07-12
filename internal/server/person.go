package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/richfields"
)

// Este arquivo implementa GET /api/v1/person/{id} (briefing 0029 Fase D): a página da
// PESSOA (ator/membro de equipe) deduplicada no catálogo mais a sua FILMOGRAFIA — as
// obras JÁ IMPORTADAS que a creditam. É o destino interno-first do card de elenco/
// equipe (quando o crédito traz `person_id`, ver creditos_interno.go). Read-only nesta
// fase — a pessoa não é catalogada como entity própria (sem edição, sem enrich
// dedicado). 404 quando o id não existe no catálogo.

// personDTO é a resposta de GET /person/{id}.
type personDTO struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	ProfileURL  *string           `json:"profile_url"`
	Source      string            `json:"source"`
	SourceID    string            `json:"source_id"`
	Filmography []personCreditDTO `json:"filmography"`
}

// personCreditDTO é uma obra da filmografia: a entity do catálogo (card mínimo) mais o
// papel/cargo da pessoa naquela obra. `character` só faz sentido em kind=cast;
// `job`/`department` em kind=crew — o outro par fica nil (sem inventar vazio).
type personCreditDTO struct {
	EntityID   string  `json:"entity_id"`
	Type       string  `json:"type"`
	Title      string  `json:"title"`
	Year       *string `json:"year"`
	PosterURL  *string `json:"poster_url"`
	Kind       string  `json:"kind"`
	Character  *string `json:"character"`
	Job        *string `json:"job"`
	Department *string `json:"department"`
}

// personImagemBaseURL é a base da foto de perfil da pessoa do TMDB — mesma constante
// (não-exportada) de richfields.creditoImagemBaseURL; replicada aqui porque só o
// server tem acesso ao proxy (urlImagemServida) para decidir a URL final servida.
const personImagemBaseURL = "https://image.tmdb.org/t/p/w185"

// getPerson trata GET /api/v1/person/{id}: 404 quando a pessoa não existe no
// catálogo; 200 com a pessoa e a filmografia (`[]` quando nenhuma obra a credita).
func (s *Server) getPerson(w http.ResponseWriter, r *http.Request) {
	if s.entitiesUnavailable(w) {
		return
	}
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	pessoa, err := s.leitura().ObterPersonPorID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "not_found", "pessoa não encontrada")
			return
		}
		respondErroInterno(w)
		return
	}

	filmografia, err := s.montarFilmografia(ctx, id)
	if err != nil {
		respondErroInterno(w)
		return
	}

	writeJSON(w, http.StatusOK, personDTO{
		ID:          pessoa.ID,
		Name:        pessoa.Name,
		ProfileURL:  s.urlPerfilPessoa(pessoa.ProfilePath),
		Source:      pessoa.Source,
		SourceID:    pessoa.SourceID,
		Filmography: filmografia,
	})
}

// urlPerfilPessoa monta a URL da foto de perfil (proxyada, quando aplicável) a partir
// do fragmento cru gravado em person.profile_path, ou nil quando ausente.
func (s *Server) urlPerfilPessoa(profilePath sql.NullString) *string {
	if !profilePath.Valid || profilePath.String == "" {
		return nil
	}
	direta := personImagemBaseURL + profilePath.String
	return s.urlImagemServida(&direta)
}

// montarFilmografia lê os créditos da pessoa (ListarFilmografiaDaPessoa) e monta um
// card por OBRA, reusando ObterEntityCard/cardDTODeMembro — a MESMA resolução de
// title/poster_url da lista e dos membros (sem reinventar). AGRUPA por entity: uma
// pessoa com vários créditos na mesma obra (ex.: um diretor que também roteiriza e
// produz) vira UM card com os papéis reunidos (mesclarCreditoFilmografia), em vez de a
// obra se repetir. Uma linha cuja entity foi purgada no meio é ignorada (degrada, não
// derruba a filmografia). Preserva a ordem da query (type/ordering). Nunca nil.
func (s *Server) montarFilmografia(ctx context.Context, personID string) ([]personCreditDTO, error) {
	linhas, err := s.leitura().ListarFilmografiaDaPessoa(ctx, sql.NullString{String: personID, Valid: true})
	if err != nil {
		return nil, err
	}
	out := make([]personCreditDTO, 0, len(linhas))
	indice := make(map[string]int, len(linhas)) // entity_id -> posicao em out (agrupa a obra)
	for _, linha := range linhas {
		if pos, ok := indice[linha.EntityID]; ok {
			mesclarCreditoFilmografia(&out[pos], linha)
			continue
		}
		row, err := s.leitura().ObterEntityCard(ctx, linha.EntityID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue // entity purgada no meio: ignora (o credit some junto por CASCADE)
			}
			return nil, err
		}
		card := cardDTODeMembro(row)
		indice[linha.EntityID] = len(out)
		out = append(out, personCreditDTOde(card, row, linha))
	}
	return out, nil
}

// mesclarCreditoFilmografia funde um crédito ADICIONAL da mesma obra no card já criado:
// junta cargos de equipe distintos (" · "), preenche o personagem se ainda vazio e
// promove o kind para "cast" quando a pessoa também atuou na obra (ator+equipe, ex.: um
// diretor que atua) — assim uma obra aparece UMA vez, com todos os papéis reunidos. Um
// crédito pode então trazer character E job juntos (a UI mostra o que houver).
func mesclarCreditoFilmografia(dto *personCreditDTO, linha sqlcgen.ListarFilmografiaDaPessoaRow) {
	if linha.Kind == kindCastCredito {
		dto.Kind = kindCastCredito
		if dto.Character == nil {
			dto.Character = nsPtrOuNil(linha.Character)
		}
	}
	if linha.Kind == kindCrewCredito {
		if dto.Department == nil {
			dto.Department = nsPtrOuNil(linha.Department)
		}
		if j := nsPtrOuNil(linha.Job); j != nil {
			dto.Job = juntarCargo(dto.Job, *j)
		}
	}
}

// juntarCargo acrescenta um cargo à lista " · "-separada de dto.Job, sem duplicar (a
// mesma pessoa pode ter o mesmo job em créditos distintos da obra) e preservando a ordem
// de chegada (bilhagem/ordering da query).
func juntarCargo(atual *string, novo string) *string {
	if atual == nil || *atual == "" {
		return &novo
	}
	for _, c := range strings.Split(*atual, " · ") {
		if c == novo {
			return atual
		}
	}
	junto := *atual + " · " + novo
	return &junto
}

// personCreditDTOde monta o item da filmografia a partir do card (title/poster_url),
// do release_date bruto (para o ano) e da linha de crédito (kind/character/job/
// department). character só sai em kind=cast; job/department só em kind=crew — evita
// exibir um par que não faz sentido para aquele kind.
func personCreditDTOde(card entityCardDTO, row sqlcgen.ObterEntityCardRow, linha sqlcgen.ListarFilmografiaDaPessoaRow) personCreditDTO {
	dto := personCreditDTO{
		EntityID:  card.ID,
		Type:      card.Type,
		Title:     card.Title,
		PosterURL: card.PosterURL,
		Kind:      linha.Kind,
	}
	if row.ReleaseDate.Valid {
		dto.Year = richfields.AnoDePtr(&row.ReleaseDate.String)
	}
	if linha.Kind == kindCastCredito {
		dto.Character = nsPtrOuNil(linha.Character)
	}
	if linha.Kind == kindCrewCredito {
		dto.Job = nsPtrOuNil(linha.Job)
		dto.Department = nsPtrOuNil(linha.Department)
	}
	return dto
}

// kindCastCredito/kindCrewCredito espelham as constantes kindCast/kindCrew de
// internal/enrich/creditos.go (pacote diferente, mesmo valor gravado em credit.kind) —
// evita importar o pacote enrich só por duas strings.
const (
	kindCastCredito = "cast"
	kindCrewCredito = "crew"
)

// nsPtrOuNil converte um sql.NullString em *string (nil quando NULL/inválido).
func nsPtrOuNil(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}
