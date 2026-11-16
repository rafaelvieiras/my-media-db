// Acoustid is the fake of provider.FingerprintProvider, companion of
// `Musicbrainz` (which serves the music tree/album fixture): it allows the
// scanner (and the tests of internal/scanner) to exercise step 4.5 of acoustic
// fingerprint WITHOUT bringing up fpcalc/real network. Registered under
// Nome()="acoustid" -- the same convention as the other fakes of this package
// (the real name of the source).
package fake

import (
	"context"
	"encoding/json"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// AcoustidRecordingMBID is the recording MBID that Acoustid returns for the
// AcoustidFingerprintConhecido fingerprint fixture -- the scanner tests use this
// value to build the expected candidate (e.g. matching a track already present
// in the Musicbrainz fake).
const (
	// AcoustidFingerprintConhecido is the fingerprint fixture that Acoustid
	// RECOGNIZES (IdentificarPorFingerprint returns a candidate with a confidence
	// above matching.LimiarConfiancaFingerprint).
	AcoustidFingerprintConhecido = "fp-conhecido-fake"
	// AcoustidRecordingMBID is the MBID returned for AcoustidFingerprintConhecido.
	AcoustidRecordingMBID = MusicbrainzFaixa1ID
)

// Interface assertion: Acoustid is a core Provider (stubs) + the fingerprint
// capability.
var (
	_ provider.Provider            = (*Acoustid)(nil)
	_ provider.FingerprintProvider = (*Acoustid)(nil)
)

// Acoustid is the fake -- immutable, with no mutable state, safe for
// concurrency (same spirit as Musicbrainz).
type Acoustid struct{}

// NewAcoustid builds the acoustic fingerprint fake.
func NewAcoustid() *Acoustid { return &Acoustid{} }

func (a *Acoustid) Nome() string { return "acoustid" }

// Buscar/BuscarDetalhe/MapearDetalhe are NO-OP: the real AcoustID also only
// serves the FingerprintProvider capability (see internal/provider/acoustid) --
// inert stubs, only to satisfy the core provider.Provider.
func (a *Acoustid) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (a *Acoustid) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (a *Acoustid) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

// IdentificarPorFingerprint (provider.FingerprintProvider): recognizes only the
// AcoustidFingerprintConhecido fingerprint fixture, returning it with confidence
// 0.9 (above matching.LimiarConfiancaFingerprint) pointing at
// MusicbrainzFaixa1ID -- so a scanner test can chain step 4.5 up to the SAME
// scoped import path that Musicbrainz.BuscarAlbumDaFaixa(MusicbrainzFaixa1ID)
// already serves. Any other fingerprint (including "") returns
// (zero,false,nil) -- no match, gracefully, the same contract of the real
// AcoustID with no result at all.
func (a *Acoustid) IdentificarPorFingerprint(_ context.Context, fingerprint string, _ float64) (provider.RecordingCandidato, bool, error) {
	if fingerprint != AcoustidFingerprintConhecido {
		return provider.RecordingCandidato{}, false, nil
	}
	return provider.RecordingCandidato{
		RecordingMBID: AcoustidRecordingMBID,
		Confianca:     0.9,
	}, true, nil
}
