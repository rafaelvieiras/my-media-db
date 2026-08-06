package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/matching"
	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// Testes de melhorCandidatoExterno com TÍTULOS ALTERNATIVOS (briefing 0084):
// repontuação dos top-3 candidatos por matching.PontuarComAlternativos,
// usando um provider fake que também implementa provider.TituloAlternativoProvider
// e conta as chamadas -- prova o teto de top-3 e o cache TTL sem bater em rede
// real.

// fakeAltProvider é um provider fake (Nome configurável) que implementa
// provider.TituloAlternativoProvider: devolve os alternativos configurados por
// sourceID e conta as chamadas por sourceID (para provar o teto do top-3 e o
// cache).
type fakeAltProvider struct {
	nome         string
	alternativos map[string][]string
	chamadas     map[string]int
}

func (p *fakeAltProvider) Nome() string { return p.nome }

func (p *fakeAltProvider) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (p *fakeAltProvider) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (p *fakeAltProvider) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

func (p *fakeAltProvider) BuscarTitulosAlternativos(_ context.Context, _, sourceID string) ([]string, error) {
	p.chamadas[sourceID]++
	return p.alternativos[sourceID], nil
}

// TestMelhorCandidatoExterno_TituloAlternativoDoTop3Vence cobre o critério
// central do briefing 0084: um candidato com título principal bem diferente
// da consulta vence quando um dos seus títulos ALTERNATIVOS bate exatamente
// -- e matched_title (aqui, o tituloVencedor devolvido) reflete a origem do
// score.
func TestMelhorCandidatoExterno_TituloAlternativoDoTop3Vence(t *testing.T) {
	titulo := "Blade Runner"
	candidatos := []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "1", Type: "movie", Title: "Blade Runner Extended Cut"},
		{Source: "tmdb", SourceID: "2", Type: "movie", Title: "A Completely Unrelated Title"},
		{Source: "tmdb", SourceID: "3", Type: "movie", Title: "Something Else Entirely"},
	}
	prov := &fakeAltProvider{
		nome:         "tmdb",
		alternativos: map[string][]string{"2": {titulo}}, // título exato, alternativo
		chamadas:     map[string]int{},
	}
	s := New("v-teste", nil, WithSearchProvider(prov))

	cand, score, vencedor, achou := s.melhorCandidatoExterno(context.Background(), candidatos, titulo, 0)
	if !achou {
		t.Fatal("deveria achar candidato")
	}
	if cand.SourceID != "2" {
		t.Fatalf("candidato vencedor = %s, quero 2 (título alternativo bate exato)", cand.SourceID)
	}
	if score != 100 {
		t.Errorf("score = %v, quero 100 (título alternativo idêntico à consulta)", score)
	}
	if vencedor != titulo {
		t.Errorf("tituloVencedor = %q, quero %q", vencedor, titulo)
	}
}

// TestMelhorCandidatoExterno_TituloPrincipalJaOtimoNaoExpoeVencedor cobre o
// caso "sem ruído": quando o TÍTULO PRINCIPAL do vencedor já é o melhor
// (nenhum alternativo bate melhor), tituloVencedor volta vazio.
func TestMelhorCandidatoExterno_TituloPrincipalJaOtimoNaoExpoeVencedor(t *testing.T) {
	titulo := "Blade Runner"
	candidatos := []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "1", Type: "movie", Title: titulo},
	}
	prov := &fakeAltProvider{
		nome:         "tmdb",
		alternativos: map[string][]string{"1": {"Alguma Coisa Bem Diferente"}},
		chamadas:     map[string]int{},
	}
	s := New("v-teste", nil, WithSearchProvider(prov))

	cand, score, vencedor, achou := s.melhorCandidatoExterno(context.Background(), candidatos, titulo, 0)
	if !achou || cand.SourceID != "1" {
		t.Fatalf("candidato = %+v achou=%v, quero id=1", cand, achou)
	}
	if score != 100 {
		t.Errorf("score = %v, quero 100", score)
	}
	if vencedor != "" {
		t.Errorf("tituloVencedor = %q, quero vazio (título principal já era o melhor)", vencedor)
	}
}

// TestMelhorCandidatoExterno_ForaDoTop3NuncaDisparaBusca cobre o critério de
// aceite "só os top-3 candidatos disparam busca de títulos alternativos":
// monta 5 candidatos com score de título único ESTRITAMENTE decrescente
// (prefixos cada vez mais curtos do título de consulta -- a fixture confirma
// a ordem via matching.Pontuar, sem supor o comportamento do textsim) e prova
// que só os 3 primeiros disparam BuscarTitulosAlternativos.
func TestMelhorCandidatoExterno_ForaDoTop3NuncaDisparaBusca(t *testing.T) {
	titulo := "Blade Runner"
	titulos := []string{"Blade Runner", "Blade Runne", "Blade Runn", "Blade Ru", "Bl"}

	candidatos := make([]provider.ResultadoExterno, 0, len(titulos))
	for i, tt := range titulos {
		candidatos = append(candidatos, provider.ResultadoExterno{
			Source: "tmdb", SourceID: strconv.Itoa(i + 1), Type: "movie", Title: tt,
		})
	}
	// Confirma que a fixture é de fato decrescente por score de título único
	// -- não assume o comportamento interno do textsim.
	for i := 0; i < len(candidatos)-1; i++ {
		s1, _ := matching.Pontuar(titulo, 0, candidatos[i].Title, 0)
		s2, _ := matching.Pontuar(titulo, 0, candidatos[i+1].Title, 0)
		if s1 < s2 {
			t.Fatalf("fixture não decrescente: score(%q)=%v < score(%q)=%v",
				candidatos[i].Title, s1, candidatos[i+1].Title, s2)
		}
	}

	prov := &fakeAltProvider{nome: "tmdb", chamadas: map[string]int{}}
	s := New("v-teste", nil, WithSearchProvider(prov))

	if _, _, _, achou := s.melhorCandidatoExterno(context.Background(), candidatos, titulo, 0); !achou {
		t.Fatal("deveria achar candidato")
	}

	for i := 0; i < candidatosAlternativosTopN; i++ {
		id := strconv.Itoa(i + 1)
		if prov.chamadas[id] == 0 {
			t.Errorf("candidato do top-%d (id=%s) deveria ter disparado busca de títulos alternativos", candidatosAlternativosTopN, id)
		}
	}
	for i := candidatosAlternativosTopN; i < len(candidatos); i++ {
		id := strconv.Itoa(i + 1)
		if prov.chamadas[id] != 0 {
			t.Errorf("candidato fora do top-%d (id=%s) NÃO deveria disparar busca, chamadas=%d",
				candidatosAlternativosTopN, id, prov.chamadas[id])
		}
	}
}

// TestMelhorCandidatoExterno_CacheEvitaSegundaChamadaDentroDoTTL cobre o
// critério de aceite: duas buscas pelo MESMO candidato dentro do TTL do
// cache não disparam nova chamada de títulos alternativos.
func TestMelhorCandidatoExterno_CacheEvitaSegundaChamadaDentroDoTTL(t *testing.T) {
	titulo := "Blade Runner"
	candidatos := []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "1", Type: "movie", Title: "Blade Runner Extended"},
	}
	prov := &fakeAltProvider{nome: "tmdb", chamadas: map[string]int{}}
	s := New("v-teste", nil, WithSearchProvider(prov))

	s.melhorCandidatoExterno(context.Background(), candidatos, titulo, 0)
	s.melhorCandidatoExterno(context.Background(), candidatos, titulo, 0)

	if prov.chamadas["1"] != 1 {
		t.Errorf("chamadas = %d, quero 1 (segunda busca deveria vir do cache)", prov.chamadas["1"])
	}
}

// TestMelhorCandidatoExterno_ProviderSemCapacidadeDegrada cobre o degrade
// gracioso quando a fonte NÃO implementa TituloAlternativoProvider
// (automatchProvider, o fake padrão dos outros testes de auto-match): o
// score final é o mesmo de matching.Pontuar puro, sem pânico.
func TestMelhorCandidatoExterno_ProviderSemCapacidadeDegrada(t *testing.T) {
	titulo := "Blade Runner"
	candidatos := []provider.ResultadoExterno{
		{Source: "tmdb", SourceID: "1", Type: "movie", Title: "Blade Runner Extended"},
	}
	prov := &automatchProvider{resultados: candidatos}
	s := New("v-teste", nil, WithSearchProvider(prov))

	_, score, vencedor, achou := s.melhorCandidatoExterno(context.Background(), candidatos, titulo, 0)
	if !achou {
		t.Fatal("deveria achar candidato")
	}
	scoreEsperado, _ := matching.Pontuar(titulo, 0, "Blade Runner Extended", 0)
	if score != scoreEsperado {
		t.Errorf("score = %v, quero %v (degrada para título único sem a capacidade)", score, scoreEsperado)
	}
	if vencedor != "" {
		t.Errorf("tituloVencedor = %q, quero vazio (sem capacidade de títulos alternativos)", vencedor)
	}
}

// TestAutoMatchLocalNuncaDisparaBuscaDeTitulosAlternativos cobre o critério
// de aceite "candidato LOCAL nunca dispara títulos alternativos": um item que
// resolve via o catálogo LOCAL (melhorCandidatoLocal) nunca deveria ter
// tocado o provider externo -- prova end-to-end via o endpoint de auto-match,
// com um fakeAltProvider que contaria qualquer chamada.
func TestAutoMatchLocalNuncaDisparaBuscaDeTitulosAlternativos(t *testing.T) {
	prov := &fakeAltProvider{nome: "tmdb", chamadas: map[string]int{}}
	h, d := setupSearchServer(t, WithSearchProvider(prov), WithAssetsDir(t.TempDir()))
	semearEntityComAno(t, d, "e-target", "movie", "The Matrix", "1999-03-31")
	semearEntity(t, d, "e-draft", "movie", "Draft Sem Nome")
	definirStatus(t, d, "e-draft", "draft")
	semearFile(t, d, "f-draft", "e-draft", "/lib/The Matrix (1999)/The Matrix (1999).mkv", "present", 0, "")

	rec := do(t, h, http.MethodPost, "/api/v1/entities/e-draft/auto-match", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, quero 200 (corpo=%s)", rec.Code, rec.Body.String())
	}
	got := decodeAutoMatch(t, rec)
	if !got.Resolved || got.Method != "local" {
		t.Fatalf("resultado = %+v, quero resolved:true method:local", got)
	}
	for id, n := range prov.chamadas {
		if n != 0 {
			t.Errorf("candidato local disparou busca de títulos alternativos para %s (chamadas=%d)", id, n)
		}
	}
}
