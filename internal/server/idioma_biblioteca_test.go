package server

import (
	"context"
	"database/sql"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Testes do resolvedor de IDIOMA DE BUSCA por item (briefing 0084): §4/§5 do
// briefing. defaultLanguageSettings() (sem PUT /settings/languages) dá
// Primary="pt-BR" -- é o fallback esperado nos casos sem preferência/ambíguos.

// semearLibrarySourceComIdioma insere uma library_source com preferred_language
// (nil = sem preferência), bypassando library.Service.Criar (mesmo espírito de
// semearLibrarySource em health_system_test.go).
func semearLibrarySourceComIdioma(t *testing.T, d *db.DB, id, path string, preferredLanguage *string) {
	t.Helper()
	var ns sql.NullString
	if preferredLanguage != nil {
		ns = sql.NullString{String: *preferredLanguage, Valid: true}
	}
	if _, err := sqlcgen.New(d.Write).CriarLibrarySource(context.Background(), sqlcgen.CriarLibrarySourceParams{
		ID: id, Path: path, Type: "movie", ContentType: "movie", PreferredLanguage: ns,
	}); err != nil {
		t.Fatalf("CriarLibrarySource(%s): %v", id, err)
	}
}

// TestIdiomaDeBusca_SemArquivoCaiPraGlobal cobre entity sem media_file algum:
// cai direto no primary global, sem erro.
func TestIdiomaDeBusca_SemArquivoCaiPraGlobal(t *testing.T) {
	_, d := setupEntitiesServer(t)
	semearEntity(t, d, "e1", "movie", "Sem Arquivo")

	s := New("v-teste", d)
	lang, err := s.idiomaDeBusca(context.Background(), "e1")
	if err != nil {
		t.Fatalf("idiomaDeBusca: %v", err)
	}
	if lang != "pt-BR" {
		t.Errorf("lang = %q, quero %q (primary global default)", lang, "pt-BR")
	}
}

// TestIdiomaDeBusca_UnicaFontePreferidaVence cobre o caso central: um único
// media_file present, cuja library_source tem preferred_language definido ->
// usa esse idioma, não o global.
func TestIdiomaDeBusca_UnicaFontePreferidaVence(t *testing.T) {
	_, d := setupEntitiesServer(t)
	en := "en"
	semearLibrarySourceComIdioma(t, d, "src-en", "/lib/en", &en)
	semearEntity(t, d, "e1", "movie", "Filme")
	semearFileComFonte(t, d, "f1", "e1", "/lib/en/f1.mkv", "present", 100, "src-en")

	s := New("v-teste", d)
	lang, err := s.idiomaDeBusca(context.Background(), "e1")
	if err != nil {
		t.Fatalf("idiomaDeBusca: %v", err)
	}
	if lang != "en" {
		t.Errorf("lang = %q, quero %q (idioma preferido da biblioteca)", lang, "en")
	}
}

// TestIdiomaDeBusca_SemPreferenciaCaiPraGlobal cobre uma fonte SEM
// preferred_language: comportamento idêntico a antes do briefing 0084.
func TestIdiomaDeBusca_SemPreferenciaCaiPraGlobal(t *testing.T) {
	_, d := setupEntitiesServer(t)
	semearLibrarySourceComIdioma(t, d, "src-sem-pref", "/lib/sem-pref", nil)
	semearEntity(t, d, "e1", "movie", "Filme")
	semearFileComFonte(t, d, "f1", "e1", "/lib/sem-pref/f1.mkv", "present", 100, "src-sem-pref")

	s := New("v-teste", d)
	lang, err := s.idiomaDeBusca(context.Background(), "e1")
	if err != nil {
		t.Fatalf("idiomaDeBusca: %v", err)
	}
	if lang != "pt-BR" {
		t.Errorf("lang = %q, quero %q (sem preferência cai no global)", lang, "pt-BR")
	}
}

// TestIdiomaDeBusca_AmbiguidadeCaiPraGlobal cobre uma entity com arquivos de
// DUAS bibliotecas com preferred_language DIFERENTES entre si: resolve para o
// global, sem erro nem tela de decisão (briefing 0084 §6).
func TestIdiomaDeBusca_AmbiguidadeCaiPraGlobal(t *testing.T) {
	_, d := setupEntitiesServer(t)
	en, es := "en", "es"
	semearLibrarySourceComIdioma(t, d, "src-en", "/lib/en", &en)
	semearLibrarySourceComIdioma(t, d, "src-es", "/lib/es", &es)
	semearEntity(t, d, "e1", "movie", "Filme")
	semearFileComFonte(t, d, "f1", "e1", "/lib/en/f1.mkv", "present", 100, "src-en")
	semearFileComFonte(t, d, "f2", "e1", "/lib/es/f2.mkv", "present", 100, "src-es")

	s := New("v-teste", d)
	lang, err := s.idiomaDeBusca(context.Background(), "e1")
	if err != nil {
		t.Fatalf("idiomaDeBusca: %v", err)
	}
	if lang != "pt-BR" {
		t.Errorf("lang = %q, quero %q (ambíguo, cai no global)", lang, "pt-BR")
	}
}

// TestIdiomaDeBusca_ArquivoMissingIgnorado cobre que só arquivos state=present
// contam: um arquivo 'missing' de uma fonte com preferência não deve
// contaminar a resolução.
func TestIdiomaDeBusca_ArquivoMissingIgnorado(t *testing.T) {
	_, d := setupEntitiesServer(t)
	en := "en"
	semearLibrarySourceComIdioma(t, d, "src-en", "/lib/en", &en)
	semearEntity(t, d, "e1", "movie", "Filme")
	semearFileComFonte(t, d, "f1", "e1", "/lib/en/f1.mkv", "missing", 100, "src-en")

	s := New("v-teste", d)
	lang, err := s.idiomaDeBusca(context.Background(), "e1")
	if err != nil {
		t.Fatalf("idiomaDeBusca: %v", err)
	}
	if lang != "pt-BR" {
		t.Errorf("lang = %q, quero %q (arquivo missing não conta)", lang, "pt-BR")
	}
}

// TestIdiomaDeBusca_MesmaPreferenciaEmDuasFontesNaoEAmbigua cobre que DUAS
// fontes com o MESMO preferred_language não são tratadas como ambíguas (só
// um valor distinto entre elas).
func TestIdiomaDeBusca_MesmaPreferenciaEmDuasFontesNaoEAmbigua(t *testing.T) {
	_, d := setupEntitiesServer(t)
	en1, en2 := "en", "en"
	semearLibrarySourceComIdioma(t, d, "src-en-1", "/lib/en1", &en1)
	semearLibrarySourceComIdioma(t, d, "src-en-2", "/lib/en2", &en2)
	semearEntity(t, d, "e1", "movie", "Filme")
	semearFileComFonte(t, d, "f1", "e1", "/lib/en1/f1.mkv", "present", 100, "src-en-1")
	semearFileComFonte(t, d, "f2", "e1", "/lib/en2/f2.mkv", "present", 100, "src-en-2")

	s := New("v-teste", d)
	lang, err := s.idiomaDeBusca(context.Background(), "e1")
	if err != nil {
		t.Fatalf("idiomaDeBusca: %v", err)
	}
	if lang != "en" {
		t.Errorf("lang = %q, quero %q (mesmo valor em duas fontes não é ambíguo)", lang, "en")
	}
}
