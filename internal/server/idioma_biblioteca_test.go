package server

import (
	"context"
	"database/sql"
	"testing"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// Tests of the per-item SEARCH LANGUAGE resolver.
// defaultLanguageSettings() (without PUT /settings/languages) gives
// Primary="pt-BR" -- it is the expected fallback in the cases without
// preference/ambiguous.

// semearLibrarySourceComIdioma inserts a library_source with preferred_language
// (nil = no preference), bypassing library.Service.Criar (same spirit as
// semearLibrarySource in health_system_test.go).
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

// TestIdiomaDeBusca_SemArquivoCaiPraGlobal covers an entity without any media_file:
// it falls straight to the global primary, without an error.
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

// TestIdiomaDeBusca_UnicaFontePreferidaVence covers the central case: a single
// present media_file, whose library_source has a defined preferred_language ->
// it uses that language, not the global one.
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

// TestIdiomaDeBusca_SemPreferenciaCaiPraGlobal covers a source WITHOUT
// preferred_language: behavior identical to before.
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

// TestIdiomaDeBusca_AmbiguidadeCaiPraGlobal covers an entity with files from
// TWO libraries with DIFFERENT preferred_language values: it resolves to the
// global one, without an error nor a decision screen.
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

// TestIdiomaDeBusca_ArquivoMissingIgnorado covers that only state=present files
// count: a 'missing' file from a source with a preference must not
// contaminate the resolution.
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

// TestIdiomaDeBusca_MesmaPreferenciaEmDuasFontesNaoEAmbigua covers that TWO
// sources with the SAME preferred_language are not treated as ambiguous (only
// a distinct value between them).
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
