package list_own_usage_data

import (
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/dto"
)

type Reader interface {
	LegacyQuota(domain.QuotaQuery) ([]*dto.QuotaData, error)
	BucketQuota(domain.QuotaQuery) ([]*dto.BucketedQuotaData, error)
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}

type Result struct {
	Legacy   []*dto.QuotaData
	Bucketed []*dto.BucketedQuotaData
}

func (u Usecase) Execute(q domain.QuotaQuery) (Result, error) {
	if q.EndTime-q.StartTime > domain.MaxOwnQuotaSpanSeconds {
		return Result{}, domain.ErrQuotaSpan
	}
	if err := domain.ValidateQuotaGranularity(q.Granularity); err != nil {
		return Result{}, err
	}
	// These endpoints never accepted a username filter, including bucketed reads.
	q.Username = ""
	q.Mode = domain.QuotaOwner
	q.Grouping = domain.QuotaBucketByUserAndModel
	if q.Granularity == "" {
		rows, err := u.reader.LegacyQuota(q)
		return Result{Legacy: rows}, err
	}
	if err := domain.ValidateQuotaBucket(q); err != nil {
		return Result{}, err
	}
	rows, err := u.reader.BucketQuota(q)
	return Result{Bucketed: rows}, err
}
