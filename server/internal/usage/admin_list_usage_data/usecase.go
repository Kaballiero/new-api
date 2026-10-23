package admin_list_usage_data

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
	if err := domain.ValidateQuotaGranularity(q.Granularity); err != nil {
		return Result{}, err
	}
	q.Mode = domain.QuotaModels
	q.Grouping = domain.QuotaBucketByModel
	if q.Username != "" {
		q.Mode = domain.QuotaUsername
		q.Grouping = domain.QuotaBucketByUserAndModel
	}
	if q.Granularity == "" {
		rows, err := u.reader.LegacyQuota(q)
		return Result{Legacy: rows}, err
	}
	if q.StartTime <= 0 || q.EndTime <= 0 {
		return Result{}, domain.ErrPositiveQuotaTimes
	}
	if err := domain.ValidateQuotaBucket(q); err != nil {
		return Result{}, err
	}
	rows, err := u.reader.BucketQuota(q)
	return Result{Bucketed: rows}, err
}
