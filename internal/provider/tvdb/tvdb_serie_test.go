package tvdb

import (
	"context"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// arvoreLooney fetches the tree of Looney Tunes (72514) in the pt-BR language (with fallback).
func arvoreLooney(t *testing.T) provider.SerieArvore {
	t.Helper()
	c := servidorTVDB(t)
	arv, err := c.BuscarSerieArvore(context.Background(), "72514", []string{"pt-BR"})
	if err != nil {
		t.Fatalf("BuscarSerieArvore: %v", err)
	}
	return arv
}

// acharEp finds an episode in the tree by SourceID (it scans the seasons).
func acharEp(arv provider.SerieArvore, sourceID string) *provider.EpisodioExterno {
	for i := range arv.Temporadas {
		for j := range arv.Temporadas[i].Episodios {
			if arv.Temporadas[i].Episodios[j].SourceID == sourceID {
				return &arv.Temporadas[i].Episodios[j]
			}
		}
	}
	return nil
}

// TestArvoreOrdenacoes validates the default ordering and the list of season types.
func TestArvoreOrdenacoes(t *testing.T) {
	arv := arvoreLooney(t)
	if arv.DefaultSeasonType != "official" {
		t.Errorf("default = %q, quero official", arv.DefaultSeasonType)
	}
	if len(arv.SeasonTypes) != 3 || arv.SeasonTypes[0] != "official" {
		t.Errorf("seasonTypes = %v, quero [official dvd absolute]", arv.SeasonTypes)
	}
}

// numsTemporadas collects the season numbers of the tree (num→present).
func numsTemporadas(arv provider.SerieArvore) map[int]bool {
	nums := map[int]bool{}
	for _, tp := range arv.Temporadas {
		nums[tp.SeasonNumber] = true
	}
	return nums
}

// TestArvoreTemporadasSoDefault guarantees that ONLY the seasons of the default order
// become TemporadaExterna (the DVD season in seasons[] must NOT appear) -- invariant 2.
func TestArvoreTemporadasSoDefault(t *testing.T) {
	arv := arvoreLooney(t)
	for _, tp := range arv.Temporadas {
		if tp.Ordering != "official" {
			t.Errorf("temporada %d com ordering %q, quero official", tp.SeasonNumber, tp.Ordering)
		}
	}
	// The default seasons of the fixture: 0, 1933, 1938 (the DVD 23 does not enter).
	nums := numsTemporadas(arv)
	for _, quero := range []int{0, 1933, 1938} {
		if !nums[quero] {
			t.Errorf("faltou temporada %d; temporadas=%v", quero, nums)
		}
	}
	if nums[23] {
		t.Error("temporada 23 (DVD) NAO deveria virar temporada (invariante 2)")
	}
}

// TestArvoreTemporadaPosterAno confirms that the default season 1933 brings a poster and a
// year (coming from extended.seasons[]).
func TestArvoreTemporadaPosterAno(t *testing.T) {
	arv := arvoreLooney(t)
	for _, tp := range arv.Temporadas {
		if tp.SeasonNumber != 1933 {
			continue
		}
		if tp.Year != "1933" || tp.PosterURL == "" {
			t.Errorf("temporada 1933: year=%q poster=%q", tp.Year, tp.PosterURL)
		}
	}
}

// TestArvoreEpisodioAncoradoNoID + multi-order Numbering: the SAME episode (id 4113194)
// carries official/dvd/absolute with different (season,number) and a stable absoluteNumber.
func TestArvoreNumberingMultiOrdem(t *testing.T) {
	arv := arvoreLooney(t)
	ep := acharEp(arv, "4113194") // Bosko's Picture Show
	if ep == nil {
		t.Fatal("episodio 4113194 nao encontrado")
	}
	casos := map[string]provider.Numero{
		"official": {Season: 1933, Number: 16},
		"dvd":      {Season: 23, Number: 5},
		"absolute": {Season: 1, Number: 373},
	}
	for ordem, quero := range casos {
		if got := ep.Numbering[ordem]; got != quero {
			t.Errorf("Numbering[%s] = %+v, quero %+v", ordem, got, quero)
		}
	}
	if ep.AbsoluteNumber == nil || *ep.AbsoluteNumber != 373 {
		t.Errorf("absoluteNumber = %v, quero 373", ep.AbsoluteNumber)
	}
	// The anchor is the id, not the (season,number) pair: the episode appears ONLY once, in
	// the season of the default order (1933), not in 23 nor in 1.
	if ep.Numbering["official"].Season != 1933 {
		t.Errorf("episodio deveria estar ancorado na temporada default 1933")
	}
}

// TestArvoreFallbackIdioma: the episode WITH a por translation has Title[pt-BR]; the one
// WITHOUT a translation has no pt-BR in the map (the resolver falls back to the original
// language en) -- invariant 3.
func TestArvoreFallbackIdioma(t *testing.T) {
	arv := arvoreLooney(t)
	comPor := acharEp(arv, "4113194") // has "por"
	semPor := acharEp(arv, "4113285") // no "por"
	if comPor == nil || semPor == nil {
		t.Fatal("episodios de fallback nao encontrados")
	}
	if comPor.Title["pt-BR"] != "O Show do Bosko" || comPor.Title["pt"] != "O Show do Bosko" {
		t.Errorf("com por: Title = %v, quero pt-BR/pt = O Show do Bosko", comPor.Title)
	}
	if comPor.Title["en"] != "Bosko's Picture Show" {
		t.Errorf("com por: Title[en] = %q, quero o original", comPor.Title["en"])
	}
	if _, tem := semPor.Title["pt-BR"]; tem {
		t.Errorf("sem por: NAO deveria ter pt-BR: %v", semPor.Title)
	}
	if semPor.Title["en"] != "Porky's Hare Hunt" {
		t.Errorf("sem por: Title[en] = %q, quero o original (fallback)", semPor.Title["en"])
	}
}

// TestArvoreExternalIDsEpisodio: from the tree, the episode brings {tvdb: id} (the
// tmdb/imdb crossing per episode requires the extended, deferred).
func TestArvoreExternalIDsEpisodio(t *testing.T) {
	arv := arvoreLooney(t)
	ep := acharEp(arv, "4113194")
	if ep == nil || ep.ExternalIDs["tvdb"] != "4113194" {
		t.Errorf("ExternalIDs = %v, quero tvdb=4113194", ep)
	}
}

// TestArvoreSerieInexistente propagates ErrNaoEncontrado.
func TestArvoreSerieInexistente(t *testing.T) {
	c := servidorTVDB(t)
	_, err := c.BuscarSerieArvore(context.Background(), "000", nil)
	if err == nil {
		t.Fatal("quero erro para serie inexistente")
	}
}
