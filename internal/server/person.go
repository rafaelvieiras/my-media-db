package server

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
	"github.com/rafaelvieiras/my-media-db/internal/richfields"
)

// This file implements GET /api/v1/person/{id}: the page of the PERSON
// (actor/crew member) deduplicated in the catalog plus TWO filmography
// sections. `filmography` are the works ALREADY IMPORTED that credit them (the database
// only, it works without a provider) — it is the interno-first destination of the cast/crew
// card (when the credit carries `person_id`, see creditos_interno.go).
// `filmography_external` (second section, "not imported yet") are the person's works
// IN THE PROVIDER of their source that are NOT in the catalog yet: it takes the provider of
// `pessoa.Source`, type-asserts to provider.PessoaProvider (an OPTIONAL capability) and
// DEGRADES to `[]` without a provider, without the capability, or if the source fails
// (it logs, the page does not fall over) — same pattern as the external detail
// (external_detail.go). Read-only at this stage — the person is not cataloged as its own
// entity (no editing, no dedicated enrich). 404 when the id does not exist in the
// catalog.

// personDTO is the response of GET /person/{id}.
type personDTO struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	ProfileURL  *string           `json:"profile_url"`
	Source      string            `json:"source"`
	SourceID    string            `json:"source_id"`
	Filmography []personCreditDTO `json:"filmography"`
	// FilmographyExternal is the second section ("not imported yet"), see the file
	// header. Never nil (always `[]` when empty/unavailable).
	FilmographyExternal []personExternalCreditDTO `json:"filmography_external"`
}

// personExternalCreditDTO is a work of the EXTERNAL filmography (not imported yet):
// it mirrors personCreditDTO, but with the EXTERNAL identity (source/source_id/type) instead
// of entity_id — the UI navigates to the external detail to view/import it. No Department (the
// provider contract already joins the distinct roles into Job).
type personExternalCreditDTO struct {
	Source    string  `json:"source"`
	SourceID  string  `json:"source_id"`
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Year      *string `json:"year"`
	PosterURL *string `json:"poster_url"`
	Kind      string  `json:"kind"`
	Character *string `json:"character"`
	Job       *string `json:"job"`
}

// personCreditDTO is a work of the filmography: the catalog entity (minimal card) plus
// the person's role/job in that work. `character` only makes sense in kind=cast;
// `job`/`department` in kind=crew — the other pair stays nil (no inventing empty).
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

// personImagemBaseURL is the base of the person profile picture from TMDB — the same
// (unexported) constant of richfields.creditoImagemBaseURL; replicated here because only
// the server has access to the proxy (urlImagemServida) to decide the final served URL.
const personImagemBaseURL = "https://image.tmdb.org/t/p/w185"

// getPerson handles GET /api/v1/person/{id}: 404 when the person does not exist in the
// catalog; 200 with the person and the filmography (`[]` when no work credits them).
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
		ID:                  pessoa.ID,
		Name:                pessoa.Name,
		ProfileURL:          s.urlPerfilPessoa(pessoa.ProfilePath),
		Source:              pessoa.Source,
		SourceID:            pessoa.SourceID,
		Filmography:         filmografia,
		FilmographyExternal: s.montarFilmografiaExternaDaPessoa(ctx, pessoa),
	})
}

// montarFilmografiaExternaDaPessoa assembles the second section of the page: the
// person's filmography IN THE PROVIDER of their source that is NOT in the catalog YET. It takes
// the provider of `pessoa.Source` (same mechanism as the external detail, see
// provedorDetalhe) and type-asserts to the OPTIONAL provider.PessoaProvider capability
// — without a provider, without the capability, or if the search fails (real error or
// ErrNaoEncontrado), it DEGRADES to `[]` (it logs the failure; the imported filmography, which
// already ran, stays up). The language is the PRIMARY one from the settings (the route has no `?lang=`).
// Every credit whose work is already in the catalog is EXCLUDED (it already appears in `filmography`).
func (s *Server) montarFilmografiaExternaDaPessoa(ctx context.Context, pessoa sqlcgen.Person) []personExternalCreditDTO {
	prov, ok := s.providers.Get(pessoa.Source)
	if !ok {
		return []personExternalCreditDTO{}
	}
	pp, ok := prov.(provider.PessoaProvider)
	if !ok {
		return []personExternalCreditDTO{}
	}

	lang := defaultLanguageSettings().Primary
	if cfg, err := s.obterLanguageSettings(ctx); err == nil {
		lang = cfg.Primary
	}

	creditos, err := pp.BuscarFilmografia(ctx, pessoa.SourceID, lang)
	if err != nil {
		log.Printf("server: filmografia externa da pessoa (source=%s source_id=%s) falhou (degrada para []): %v",
			pessoa.Source, pessoa.SourceID, err)
		return []personExternalCreditDTO{}
	}

	out := make([]personExternalCreditDTO, 0, len(creditos))
	for _, c := range creditos {
		if s.idInternoItem(ctx, c.Source, c.SourceID) != nil {
			continue // already imported: it appears in section 1 (filmography), not repeated here
		}
		out = append(out, personExternalCreditDTO{
			Source:    c.Source,
			SourceID:  c.SourceID,
			Type:      c.Type,
			Title:     c.Title,
			Year:      c.Year,
			PosterURL: s.urlImagemServida(c.PosterURL),
			Kind:      c.Kind,
			Character: c.Character,
			Job:       c.Job,
		})
	}
	return out
}

// urlPerfilPessoa builds the URL of the profile picture (proxied, when applicable) from
// the raw fragment stored in person.profile_path, or nil when absent.
func (s *Server) urlPerfilPessoa(profilePath sql.NullString) *string {
	if !profilePath.Valid || profilePath.String == "" {
		return nil
	}
	direta := personImagemBaseURL + profilePath.String
	return s.urlImagemServida(&direta)
}

// montarFilmografia reads the person's credits (ListarFilmografiaDaPessoa) and builds one
// card per WORK, reusing ObterEntityCard/cardDTODeMembro — the SAME title/poster_url
// resolution of the list and of the members (no reinventing). It GROUPS by entity: a
// person with several credits in the same work (e.g.: a director who also writes and
// produces) becomes ONE card with the roles brought together (mesclarCreditoFilmografia),
// instead of the work repeating itself. A row whose entity was purged in the middle is ignored
// (it degrades, it does not take the filmography down). It preserves the query order
// (type/ordering). Never nil.
func (s *Server) montarFilmografia(ctx context.Context, personID string) ([]personCreditDTO, error) {
	linhas, err := s.leitura().ListarFilmografiaDaPessoa(ctx, sql.NullString{String: personID, Valid: true})
	if err != nil {
		return nil, err
	}
	out := make([]personCreditDTO, 0, len(linhas))
	indice := make(map[string]int, len(linhas)) // entity_id -> position in out (groups the work)
	for _, linha := range linhas {
		if pos, ok := indice[linha.EntityID]; ok {
			mesclarCreditoFilmografia(&out[pos], linha)
			continue
		}
		row, err := s.leitura().ObterEntityCard(ctx, linha.EntityID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue // entity purged in the middle: ignore it (the credit goes away with the CASCADE)
			}
			return nil, err
		}
		card := cardDTODeMembro(row)
		indice[linha.EntityID] = len(out)
		out = append(out, personCreditDTOde(card, row, linha))
	}
	return out, nil
}

// mesclarCreditoFilmografia merges an ADDITIONAL credit of the same work into the card already
// created: it joins distinct crew jobs (" · "), fills the character if still empty and
// promotes the kind to "cast" when the person also acted in the work (actor+crew, e.g.: a
// director who acts) — so a work appears ONCE, with all the roles brought together. A
// credit may then bring character AND job together (the UI shows whatever there is).
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

// juntarCargo appends a job to the " · "-separated list of dto.Job, without duplicating (the
// same person may have the same job in distinct credits of the work) and preserving the
// arrival order (billing/ordering of the query).
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

// personCreditDTOde builds the filmography item from the card (title/poster_url),
// from the raw release_date (for the year) and from the credit row (kind/character/job/
// department). character only comes out in kind=cast; job/department only in kind=crew — it avoids
// showing a pair that makes no sense for that kind.
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

// kindCastCredito/kindCrewCredito mirror the kindCast/kindCrew constants of
// internal/enrich/creditos.go (different package, same value written in credit.kind) —
// it avoids importing the enrich package just for two strings.
const (
	kindCastCredito = "cast"
	kindCrewCredito = "crew"
)

// nsPtrOuNil converts a sql.NullString into *string (nil when NULL/invalid).
func nsPtrOuNil(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}
	return &s.String
}
