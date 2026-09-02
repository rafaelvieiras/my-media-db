// Acoustid é o fake de provider.FingerprintProvider (briefing 0126),
// companheiro de `Musicbrainz` (que serve a fixture da árvore/álbum de
// música): permite ao scanner (e aos testes de internal/scanner) exercitar
// o passo 4.5 de fingerprint acústico SEM subir fpcalc/rede real. Registrado
// sob Nome()="acoustid" -- mesma convenção dos demais fakes deste pacote
// (nome real da fonte).
package fake

import (
	"context"
	"encoding/json"

	"github.com/rafaelvieiras/my-media-db/internal/provider"
)

// AcoustidRecordingMBID é o MBID de recording que Acoustid devolve para o
// fingerprint fixture AcoustidFingerprintConhecido -- os testes de scanner
// usam este valor para montar o candidato esperado (ex.: casando com uma
// faixa já presente no fake Musicbrainz).
const (
	// AcoustidFingerprintConhecido é o fingerprint fixture que Acoustid
	// RECONHECE (IdentificarPorFingerprint devolve um candidato com
	// confiança acima de matching.LimiarConfiancaFingerprint).
	AcoustidFingerprintConhecido = "fp-conhecido-fake"
	// AcoustidRecordingMBID é o MBID devolvido para AcoustidFingerprintConhecido.
	AcoustidRecordingMBID = MusicbrainzFaixa1ID
)

// Asserção de interface: Acoustid é um Provider core (stubs) + a capacidade
// de fingerprint.
var (
	_ provider.Provider            = (*Acoustid)(nil)
	_ provider.FingerprintProvider = (*Acoustid)(nil)
)

// Acoustid é o fake -- imutável, sem estado mutável, seguro para
// concorrência (mesmo espírito de Musicbrainz).
type Acoustid struct{}

// NewAcoustid constrói o fake de fingerprint acústico.
func NewAcoustid() *Acoustid { return &Acoustid{} }

func (a *Acoustid) Nome() string { return "acoustid" }

// Buscar/BuscarDetalhe/MapearDetalhe são NO-OP: o AcoustID real também só
// serve a capacidade FingerprintProvider (ver internal/provider/acoustid) --
// stubs inertes, só para satisfazer o core provider.Provider.
func (a *Acoustid) Buscar(context.Context, string, string, string) ([]provider.ResultadoExterno, error) {
	return nil, nil
}

func (a *Acoustid) BuscarDetalhe(context.Context, string, string, []string) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (a *Acoustid) MapearDetalhe(json.RawMessage) (map[string]any, error) {
	return map[string]any{}, nil
}

// IdentificarPorFingerprint (provider.FingerprintProvider): reconhece só o
// fingerprint fixture AcoustidFingerprintConhecido, devolvendo-o com
// confiança 0.9 (acima de matching.LimiarConfiancaFingerprint) apontando
// para MusicbrainzFaixa1ID -- assim um teste de scanner pode encadear o
// passo 4.5 até o MESMO caminho de import escopado que
// Musicbrainz.BuscarAlbumDaFaixa(MusicbrainzFaixa1ID) já serve. Qualquer
// outro fingerprint (incluindo "") devolve (zero,false,nil) -- sem match,
// graça, mesmo contrato do AcoustID real sem resultado algum.
func (a *Acoustid) IdentificarPorFingerprint(_ context.Context, fingerprint string, _ float64) (provider.RecordingCandidato, bool, error) {
	if fingerprint != AcoustidFingerprintConhecido {
		return provider.RecordingCandidato{}, false, nil
	}
	return provider.RecordingCandidato{
		RecordingMBID: AcoustidRecordingMBID,
		Confianca:     0.9,
	}, true, nil
}
