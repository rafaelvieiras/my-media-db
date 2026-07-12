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

// Este arquivo implementa GET /api/v1/person/{id} (briefing 0029 Fase D): a página da
// PESSOA (ator/membro de equipe) deduplicada no catálogo mais DUAS seções de
// filmografia. `filmography` são as obras JÁ IMPORTADAS que a creditam (só o banco,
// funciona sem provider) — é o destino interno-first do card de elenco/equipe (quando o
// crédito traz `person_id`, ver creditos_interno.go). `filmography_external` (segunda
// seção, "ainda não importados") são os trabalhos da pessoa NO PROVIDER da sua fonte
// que ainda NÃO estão no catálogo: pega o provider de `pessoa.Source`, faz type-assert
// para provider.PessoaProvider (capacidade OPCIONAL) e DEGRADA para `[]` sem provider,
// sem a capacidade, ou se a fonte falhar (loga, a página não cai) — mesmo padrão do
// detalhe externo (external_detail.go). Read-only nesta fase — a pessoa não é
// catalogada como entity própria (sem edição, sem enrich dedicado). 404 quando o id não
// existe no catálogo.

// personDTO é a resposta de GET /person/{id}.
type personDTO struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	ProfileURL  *string           `json:"profile_url"`
	Source      string            `json:"source"`
	SourceID    string            `json:"source_id"`
	Filmography []personCreditDTO `json:"filmography"`
	// FilmographyExternal é a segunda seção ("ainda não importados"), ver o cabeçalho
	// do arquivo. Nunca nil (sempre `[]` quando vazia/indisponível).
	FilmographyExternal []personExternalCreditDTO `json:"filmography_external"`
}

// personExternalCreditDTO é uma obra da filmografia EXTERNA (ainda não importada):
// espelha personCreditDTO, mas com a identidade EXTERNA (source/source_id/type) em vez
// de entity_id — a UI navega ao detalhe externo para ver/importar. Sem Department (o
// contrato do provider já junta os cargos distintos em Job).
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
		ID:                  pessoa.ID,
		Name:                pessoa.Name,
		ProfileURL:          s.urlPerfilPessoa(pessoa.ProfilePath),
		Source:              pessoa.Source,
		SourceID:            pessoa.SourceID,
		Filmography:         filmografia,
		FilmographyExternal: s.montarFilmografiaExternaDaPessoa(ctx, pessoa),
	})
}

// montarFilmografiaExternaDaPessoa monta a segunda seção da página (0029 Fase D): a
// filmografia da pessoa NO PROVIDER da sua fonte que AINDA NÃO está no catálogo. Pega o
// provider de `pessoa.Source` (mesmo mecanismo do detalhe externo, ver
// provedorDetalhe) e faz type-assert para a capacidade OPCIONAL provider.PessoaProvider
// — sem provider, sem a capacidade, ou se a busca falhar (erro real ou
// ErrNaoEncontrado), DEGRADA para `[]` (loga a falha; a filmografia importada, que já
// rodou, segue de pé). O idioma é o PRINCIPAL das settings (a rota não tem `?lang=`).
// Cada crédito cuja obra já está no catálogo é EXCLUÍDO (já aparece em `filmography`).
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
			continue // ja importado: aparece na secao 1 (filmography), nao repete aqui
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
