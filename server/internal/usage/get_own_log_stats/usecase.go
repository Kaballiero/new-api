package get_own_log_stats

import (
	"time"

	"github.com/QuantumNous/new-api/internal/usage/domain"
)

type Reader interface {
	LogStats(domain.LogQuery, func() int64) (domain.Stat, error)
}

type Input struct {
	Query    domain.LogQuery
	Username string
}

type Usecase struct {
	reader Reader
	now    func() time.Time
}

func NewUsecase(reader Reader, now func() time.Time) Usecase {
	return Usecase{reader: reader, now: now}
}

func (u Usecase) Execute(in Input) (domain.Stat, error) {
	query := in.Query
	query.Username = in.Username
	query.Kind = domain.LogTypeConsume
	return u.reader.LogStats(query, func() int64 { return u.now().Add(-60 * time.Second).Unix() })
}
