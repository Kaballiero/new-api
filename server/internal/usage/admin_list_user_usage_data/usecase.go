package admin_list_user_usage_data

import (
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/dto"
)

type Reader interface {
	LegacyQuota(domain.QuotaQuery) ([]*dto.QuotaData, error)
	BucketUsers(domain.QuotaQuery) ([]*dto.BucketedUserQuotaData, error)
}

type Usecase struct {
	reader Reader
}

func NewUsecase(reader Reader) Usecase {
	return Usecase{reader: reader}
}

type Result struct {
	Legacy   []*dto.QuotaData
	Bucketed []*dto.BucketedUserQuotaData
}

func (u Usecase) Execute(q domain.QuotaQuery) (Result, error) {
	if err := domain.ValidateQuotaGranularity(q.Granularity); err != nil {
		return Result{}, err
	}
	// These endpoints never accepted a username filter, including bucketed reads.
	q.Username = ""
	q.Mode = domain.QuotaUsers
	q.Grouping = domain.QuotaBucketByUser
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
	rows, err := u.reader.BucketUsers(q)
	return Result{Bucketed: rows}, err
}
