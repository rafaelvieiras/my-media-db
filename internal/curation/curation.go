// Package curation expoe, como servico reusavel, as CONTAGENS das filas de
// curadoria (briefing 0018: revisao e orfas) para consumidores que nao sao
// HTTP -- hoje so o job de saude da biblioteca (briefing 0053). As queries em
// si (ListarRevisao/ListarOrfas, internal/db/queries/review.sql) ja existem e
// continuam servindo o GET /review e GET /orphans de internal/server; este
// pacote NAO duplica a logica SQL do predicado -- so empacota a MESMA
// consulta sqlc para devolver a CONTAGEM (len(rows)) em vez do shape de card
// completo, que o job nao precisa.
package curation

import (
	"context"
	"database/sql"

	"github.com/rafaelvieiras/my-media-db/internal/db"
	"github.com/rafaelvieiras/my-media-db/internal/db/sqlcgen"
)

// LimiarBaixaConfianca e o corte de confianca que separa "palpite alto" de
// "baixa confianca" (escala 0-100 do scanner) -- MESMO valor do
// limiarBaixaConfianca privado de internal/server/review.go (que alimenta o
// GET /review) e do LIMIAR_BAIXA_CONFIANCA do front
// (web/src/lib/entity-badges.ts). Os tres DEVEM casar; e um acoplamento ja
// documentado no briefing 0018 (candidato a virar config/endpoint no
// futuro), nao uma duplicacao de LOGICA -- e so um numero espelhado, a
// query em si (ListarRevisao) e reusada por inteiro.
const LimiarBaixaConfianca = 70

// Servico concentra as leituras das filas de curadoria por CONTAGEM (sem o
// shape de card completo) -- consumido pelo job de saude da biblioteca
// (internal/job/libraryhealth, briefing 0053).
type Servico struct {
	database *db.DB
}

// New cria o Servico a partir do pool de banco.
func New(database *db.DB) *Servico {
	return &Servico{database: database}
}

func (s *Servico) leitura() *sqlcgen.Queries { return sqlcgen.New(s.database.Read) }

// ContarRevisao devolve quantos itens estao na fila de revisao (draft OU
// baixa confianca) -- a MESMA query sqlc do GET /review (ListarRevisao), so
// contando as linhas em vez de devolver o shape de card completo.
func (s *Servico) ContarRevisao(ctx context.Context) (int, error) {
	rows, err := s.leitura().ListarRevisao(ctx, sql.NullFloat64{Float64: LimiarBaixaConfianca, Valid: true})
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

// ContarOrfas devolve quantas entities estao na fila de orfas -- a MESMA
// query sqlc do GET /orphans (ListarOrfas), so contando as linhas.
func (s *Servico) ContarOrfas(ctx context.Context) (int, error) {
	rows, err := s.leitura().ListarOrfas(ctx)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}
