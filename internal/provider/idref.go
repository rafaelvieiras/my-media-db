package provider

// This file implements the ID DOOR of the external search: a search term that NAMES the
// item directly (a source id or the item URL) instead of describing it by title.
//
// WHY: the sources are LITERAL. A typo in the folder name ("2 Broken Girls" for "2 Broke
// Girls") makes /search/multi return ZERO results, and no amount of normalization saves
// it -- TMDB does not spell-correct, and neither do its consumers. What the mature tools
// do is give the user a deterministic door:
//
//   - tinyMediaManager (v4.1.4+): the search box accepts the id PREFIXED by the provider
//     -- `TMDB:39340`, `TVDB:248741`, `IMDB:tt1845307` (a bare `tt...` also works).
//   - Jellyfin: the "Identify" dialog accepts the provider id, and the folder name may
//     carry it (`Series Name (year) [tvdbid-12345]`).
//
// Both converge on the same design: never guess the title, let the human point at the
// id. Parsing lives here (provider-agnostic) so a new source only has to implement
// ResolvedorDeID; the search handler stays one code path.

import (
	"context"
	"net/url"
	"regexp"
	"strings"
)

// ReferenciaDeID is an item named DIRECTLY by its id in a source, extracted from what the
// user typed (or pasted) in the search box.
type ReferenciaDeID struct {
	// Fonte is the source that owns the id: "tmdb", "tvdb" or "imdb".
	Fonte string
	// ID is the id IN THAT SOURCE (numeric for tmdb/tvdb, "tt..." for imdb).
	ID string
	// Tipo is "tv" or "movie" when the input already says it (a themoviedb.org URL path,
	// or `TMDB:123` combined with ?type=); empty means "let the resolver decide", which
	// costs at most one extra request.
	Tipo string
}

// ResolvedorDeID is the OPTIONAL provider capability that turns a ReferenciaDeID into a
// search result. A provider without it simply does not benefit from the id door: the
// handler falls back to the literal search.
type ResolvedorDeID interface {
	ResolverID(ctx context.Context, ref ReferenciaDeID, lang string) ([]ResultadoExterno, error)
}

var (
	reNumeroID = regexp.MustCompile(`^\d+$`)
	reImdbID   = regexp.MustCompile(`^tt\d{5,}$`)
)

// InterpretarReferencia extracts a ReferenciaDeID from the search term. ok=false when the
// term is not an id/URL (the normal case: a title).
//
// Accepted forms (case-insensitive on the prefix/host, the id itself is normalized):
//
//	tmdb:39340   themoviedb:39340
//	tvdb:248741  thetvdb:248741
//	imdb:tt1845307   tt1845307
//	https://www.themoviedb.org/tv/39340-2-broke-girls
//	https://www.themoviedb.org/movie/603
//	https://www.thetvdb.com/dereferrer/series/248741
//	https://www.thetvdb.com/series/248741-2-broke-girls
//	https://www.imdb.com/title/tt1845307/
func InterpretarReferencia(termo string) (ReferenciaDeID, bool) {
	t := strings.TrimSpace(termo)
	if t == "" {
		return ReferenciaDeID{}, false
	}
	if ref, ok := referenciaPorURL(t); ok {
		return ref, true
	}
	if ref, ok := referenciaPorPrefixo(t); ok {
		return ref, true
	}
	// A bare IMDb id ("tt1845307") is unambiguous enough to accept on its own.
	if reImdbID.MatchString(strings.ToLower(t)) {
		return ReferenciaDeID{Fonte: "imdb", ID: strings.ToLower(t)}, true
	}
	return ReferenciaDeID{}, false
}

// referenciaPorPrefixo reads the `fonte:id` form (tinyMediaManager style).
func referenciaPorPrefixo(t string) (ReferenciaDeID, bool) {
	i := strings.Index(t, ":")
	if i <= 0 {
		return ReferenciaDeID{}, false
	}
	fonte := normalizarFonte(t[:i])
	if fonte == "" {
		return ReferenciaDeID{}, false
	}
	return comIDDeFonte(fonte, strings.TrimSpace(t[i+1:]))
}

// referenciaPorURL reads a source URL: the path carries both the id and (for TMDb) the
// type, so pasting the address bar is enough.
func referenciaPorURL(t string) (ReferenciaDeID, bool) {
	if !strings.Contains(t, "://") && !strings.HasPrefix(strings.ToLower(t), "www.") {
		return ReferenciaDeID{}, false
	}
	alvo := t
	if !strings.Contains(alvo, "://") {
		alvo = "https://" + alvo
	}
	u, err := url.Parse(alvo)
	if err != nil {
		return ReferenciaDeID{}, false
	}
	host := strings.ToLower(strings.TrimPrefix(u.Host, "www."))
	seg := segmentos(u.Path)
	switch {
	case strings.HasSuffix(host, "themoviedb.org"):
		return referenciaDePathTMDB(seg)
	case strings.HasSuffix(host, "thetvdb.com"):
		return referenciaDePathTVDB(seg)
	case strings.HasSuffix(host, "imdb.com"):
		// /title/tt1845307/ (any suffix after the id is ignored)
		for _, s := range seg {
			if reImdbID.MatchString(strings.ToLower(s)) {
				return ReferenciaDeID{Fonte: "imdb", ID: strings.ToLower(s)}, true
			}
		}
	}
	return ReferenciaDeID{}, false
}

// referenciaDePathTMDB reads /tv/<id>[-slug] and /movie/<id>[-slug]: the type comes from
// the path, which saves the resolver the "try tv, then movie" round trip.
func referenciaDePathTMDB(seg []string) (ReferenciaDeID, bool) {
	for i := 0; i+1 < len(seg); i++ {
		tipo := ""
		switch seg[i] {
		case "tv":
			tipo = "tv"
		case "movie":
			tipo = "movie"
		default:
			continue
		}
		if id, ok := idInicial(seg[i+1]); ok {
			return ReferenciaDeID{Fonte: "tmdb", ID: id, Tipo: tipo}, true
		}
	}
	return ReferenciaDeID{}, false
}

// referenciaDePathTVDB reads /series/<id>[-slug] and /dereferrer/series/<id>.
func referenciaDePathTVDB(seg []string) (ReferenciaDeID, bool) {
	for i := 0; i+1 < len(seg); i++ {
		if seg[i] != "series" {
			continue
		}
		if id, ok := idInicial(seg[i+1]); ok {
			return ReferenciaDeID{Fonte: "tvdb", ID: id, Tipo: "tv"}, true
		}
	}
	return ReferenciaDeID{}, false
}

// comIDDeFonte validates the id of a source and, for IMDb, normalizes the prefix (the
// site uses lowercase "tt").
func comIDDeFonte(fonte, id string) (ReferenciaDeID, bool) {
	switch fonte {
	case "imdb":
		id = strings.ToLower(id)
		if !reImdbID.MatchString(id) {
			return ReferenciaDeID{}, false
		}
		return ReferenciaDeID{Fonte: fonte, ID: id}, true
	default:
		if !reNumeroID.MatchString(id) {
			return ReferenciaDeID{}, false
		}
		return ReferenciaDeID{Fonte: fonte, ID: id}, true
	}
}

// normalizarFonte accepts the names the user and the sites actually use.
func normalizarFonte(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "tmdb", "themoviedb":
		return "tmdb"
	case "tvdb", "thetvdb":
		return "tvdb"
	case "imdb":
		return "imdb"
	}
	return ""
}

// segmentos splits a URL path into non-empty segments.
func segmentos(path string) []string {
	partes := strings.Split(path, "/")
	out := make([]string, 0, len(partes))
	for _, p := range partes {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// idInicial extracts the numeric id from a segment that may carry a slug
// ("39340-2-broke-girls" -> "39340").
//
// At least three digits are required when there IS a slug: TMDb always generates the URL
// as `<id>-<slug>`, so a real pasted URL carries the id. Without this floor, a hand-typed
// "/tv/2-broke-girls" would silently resolve item 2 -- the wrong series is worse than
// "found nothing", which is what a term that is not an id degrades to (a title search).
// A bare numeric segment ("/tv/2") has no such ambiguity and keeps being accepted.
func idInicial(seg string) (string, bool) {
	base := seg
	comSlug := false
	if i := strings.IndexByte(base, '-'); i > 0 {
		base = base[:i]
		comSlug = true
	}
	if !reNumeroID.MatchString(base) {
		return "", false
	}
	if comSlug && len(base) < 3 {
		return "", false
	}
	return base, true
}
