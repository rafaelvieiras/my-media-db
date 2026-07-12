package tmdb

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Este arquivo implementa a CAPACIDADE de filmografia de PESSOA do provider
// (provider.PessoaProvider, briefing 0029 Fase D, segunda seção "ainda não
// importados"): GET {base}/person/{id}?append_to_response=combined_credits numa só
// ida (o TMDB já mescla filme+série em combined_credits) e o mapeamento dos créditos
// (cast+crew) para o canônico CreditoFilmografia. O handler cruza o resultado contra o
// catálogo (interno-first) e descarta o que já foi importado; cabe só a nós entregar um
// conjunto ENXUTO: dedup por obra (Type+SourceID, prefere cast), ordenado por ano
// decrescente (sem ano por último) e cortado num teto — filmografias reais de gente
// prolífica somam centenas de créditos, e esta é uma PRÉVIA, não um catálogo completo.

const (
	// posterFilmografiaBaseURL é a base do pôster nos itens de filmografia externa —
	// menor (w185) que o pôster de card (w342, imagemBaseURL) por ser potencialmente uma
	// lista grande; o server ainda proxya cada URL como as demais.
	posterFilmografiaBaseURL = "https://image.tmdb.org/t/p/w185"
	// tetoFilmografiaExterna é o teto de créditos devolvidos por BuscarFilmografia, já
	// após dedup e ordenação.
	tetoFilmografiaExterna = 40

	// kindCastExterno/kindCrewExterno são os valores do papel na filmografia externa
	// (o mesmo par kind "cast"/"crew" da filmografia interna, ver server/person.go).
	kindCastExterno = "cast"
	kindCrewExterno = "crew"
)

// pessoaDetalheTMDB espelha GET /person/{id}?append_to_response=combined_credits (só
// os campos usados). O `id` da própria pessoa não nos interessa aqui (já o temos via
// sourceID); só os créditos combinados.
type pessoaDetalheTMDB struct {
	CombinedCredits combinedCreditsTMDB `json:"combined_credits"`
}

// combinedCreditsTMDB é o bloco combined_credits: cast e crew já mesclando filme+série
// (media_type discrimina). A MESMA obra pode aparecer em ambas as listas quando a
// pessoa atuou E também fez parte da equipe.
type combinedCreditsTMDB struct {
	Cast []creditoCombinadoTMDB `json:"cast"`
	Crew []creditoCombinadoTMDB `json:"crew"`
}

// creditoCombinadoTMDB é um item cru de combined_credits.{cast,crew}. title/
// release_date são de filme; name/first_air_date de série (mesmo padrão de itemMulti);
// character só faz sentido em cast, job/department só em crew.
type creditoCombinadoTMDB struct {
	ID           int64  `json:"id"`
	MediaType    string `json:"media_type"`
	Title        string `json:"title"`
	Name         string `json:"name"`
	PosterPath   string `json:"poster_path"`
	ReleaseDate  string `json:"release_date"`
	FirstAirDate string `json:"first_air_date"`
	Character    string `json:"character"`
	Job          string `json:"job"`
}

// BuscarFilmografia puxa a pessoa `sourceID` com combined_credits em `lang` e mapeia
// para o canônico (dedup+ordenação+corte em montarFilmografiaExterna, PURO). 404 do
// TMDB vira provider.ErrNaoEncontrado (via getBytes); erro real propaga. Cumpre
// provider.PessoaProvider.
func (c *Client) BuscarFilmografia(ctx context.Context, sourceID, lang string) ([]provider.CreditoFilmografia, error) {
	if sourceID == "" {
		return nil, fmt.Errorf("tmdb: sourceID vazio")
	}
	params := url.Values{"append_to_response": {"combined_credits"}}
	if lang != "" {
		params.Set("language", lang)
	}
	body, err := c.getBytes(ctx, "/person/"+url.PathEscape(sourceID), params)
	if err != nil {
		return nil, err
	}
	var d pessoaDetalheTMDB
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("tmdb: filmografia invalida: %w", err)
	}
	return montarFilmografiaExterna(d.CombinedCredits), nil
}

// montarFilmografiaExterna deduplica os créditos combinados por obra (Type+SourceID) —
// cast SEMPRE vence sobre crew (papel mais proeminente); quando só há crédito(s) de
// crew para a obra, os cargos distintos são reunidos (" · ", sem duplicar). O resultado
// é ordenado por ano decrescente (sem ano por último) e cortado em
// tetoFilmografiaExterna. É PURO (sem rede) — separado de BuscarFilmografia para manter
// a função de I/O simples e este mapeamento testável sem httptest.
func montarFilmografiaExterna(cc combinedCreditsTMDB) []provider.CreditoFilmografia {
	porObra := make(map[string]*provider.CreditoFilmografia, len(cc.Cast)+len(cc.Crew))
	ordem := make([]string, 0, len(cc.Cast)+len(cc.Crew))

	for _, it := range cc.Cast {
		chave := chaveObraExterna(it)
		if chave == "" || porObra[chave] != nil {
			continue // sem tipo utilizavel, ou obra ja tem um credito de cast (1a ocorrencia vence)
		}
		credito := mapearCreditoCombinado(it, kindCastExterno)
		porObra[chave] = &credito
		ordem = append(ordem, chave)
	}
	for _, it := range cc.Crew {
		chave := chaveObraExterna(it)
		if chave == "" {
			continue
		}
		if existente, ok := porObra[chave]; ok {
			if existente.Kind == kindCastExterno {
				continue // cast ja venceu esta obra
			}
			existente.Job = juntarCargoExterno(existente.Job, it.Job)
			continue
		}
		credito := mapearCreditoCombinado(it, kindCrewExterno)
		porObra[chave] = &credito
		ordem = append(ordem, chave)
	}

	out := make([]provider.CreditoFilmografia, 0, len(ordem))
	for _, chave := range ordem {
		out = append(out, *porObra[chave])
	}
	ordenarFilmografiaExterna(out)
	if len(out) > tetoFilmografiaExterna {
		out = out[:tetoFilmografiaExterna]
	}
	return out
}

// chaveObraExterna monta a chave de dedup "type|source_id" de um crédito combinado, ou
// "" quando o media_type não é movie/tv (descarta o crédito).
func chaveObraExterna(it creditoCombinadoTMDB) string {
	if it.MediaType != "movie" && it.MediaType != "tv" {
		return ""
	}
	return it.MediaType + "|" + itoa(it.ID)
}

// mapearCreditoCombinado converte um item cru no canônico, com character preenchido só
// em cast e job só em crew (o par que não se aplica ao kind fica nil).
func mapearCreditoCombinado(it creditoCombinadoTMDB, kind string) provider.CreditoFilmografia {
	credito := provider.CreditoFilmografia{
		Source:    "tmdb",
		SourceID:  itoa(it.ID),
		Type:      it.MediaType,
		Title:     primeiroNaoVazio(it.Title, it.Name),
		Year:      anoDe(primeiroNaoVazio(it.ReleaseDate, it.FirstAirDate)),
		PosterURL: posterFilmografiaURL(it.PosterPath),
		Kind:      kind,
	}
	if kind == kindCastExterno {
		credito.Character = ptrNaoVazio(it.Character)
	} else {
		credito.Job = ptrNaoVazio(it.Job)
	}
	return credito
}

// juntarCargoExterno acrescenta `novo` à lista " · "-separada de `atual`, sem duplicar
// (mesma pessoa pode repetir o job em créditos crus distintos da obra) e sem alterar
// nada quando `novo` é vazio. Espelha juntarCargo de server/person.go (pacote
// diferente, mesma semântica — evita importar server aqui).
func juntarCargoExterno(atual *string, novo string) *string {
	if novo == "" {
		return atual
	}
	if atual == nil || *atual == "" {
		v := novo
		return &v
	}
	for _, cargo := range strings.Split(*atual, " · ") {
		if cargo == novo {
			return atual
		}
	}
	junto := *atual + " · " + novo
	return &junto
}

// ordenarFilmografiaExterna ordena por Year decrescente; créditos sem ano vão para o
// fim. Estável: obras de mesmo ano preservam a ordem de chegada (cast antes de crew,
// cada bloco na ordem da fonte) — determinístico para os testes.
func ordenarFilmografiaExterna(creditos []provider.CreditoFilmografia) {
	sort.SliceStable(creditos, func(i, j int) bool {
		yi, yj := creditos[i].Year, creditos[j].Year
		switch {
		case yi == nil && yj == nil:
			return false
		case yi == nil:
			return false // i sem ano: nao entra antes de j
		case yj == nil:
			return true // j sem ano: i (com ano) entra antes
		default:
			return *yi > *yj
		}
	})
}

// posterFilmografiaURL monta a URL direta do pôster (tamanho w185) de um item da
// filmografia externa, ou nil sem poster_path.
func posterFilmografiaURL(posterPath string) *string {
	if posterPath == "" {
		return nil
	}
	u := posterFilmografiaBaseURL + posterPath
	return &u
}
