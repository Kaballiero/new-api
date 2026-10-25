package batch

import (
	"errors"

	"github.com/QuantumNous/new-api/internal/adapter/gorm/batch/record"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	billing "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type LogBuilder interface {
	Item(domain.Job, domain.LogAccount, string, int, string) (string, error)
	Aggregate(domain.Job, domain.LogAccount, int, float64, billing.ProviderBasis) (string, error)
}
type QuotaCache interface {
	ApplyEvent(id int64, key, quotaField string, ownerID int, version, debit, held int64) error
	ConfirmToken(model.Token) error
}

type Store struct {
	cache     QuotaCache
	db, logDB func() *gorm.DB
	logs      LogBuilder
	math      domain.AggregateMath
}

func NewStore(db, logDB func() *gorm.DB, logs LogBuilder, math domain.AggregateMath, cache QuotaCache) Store {
	return Store{db: db, logDB: logDB, logs: logs, math: math, cache: cache}
}
func notFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrNotFound
	}
	return err
}

type BatchJob = record.BatchJob
type BatchItem = record.BatchItem
type BatchQuotaEvent = record.BatchQuotaEvent
type BatchLogEvent = record.BatchLogEvent
type BatchLogReceipt = record.BatchLogReceipt
type BatchJobCostReceipt = record.BatchJobCostReceipt
