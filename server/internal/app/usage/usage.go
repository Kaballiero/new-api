// Package usage composes recording dependencies without gateway imports.
package usage

import (
	"context"

	"github.com/QuantumNous/new-api/constant"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"
	usagerecord "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	"github.com/QuantumNous/new-api/internal/app/usagestorage"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/usage/count_consumption"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/record_consumption"
	"github.com/QuantumNous/new-api/internal/usage/record_failure"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// TaskMetadata exposes the single usage serializer to task gateway mapping.
type TaskMetadata interface {
	TaskIdentity(*usagerecord.LogOther, domain.TaskIdentity)
	BillingFX(*usagerecord.LogOther, domain.RequestFacts, domain.PriceFacts, string)
	Clamp(*usagerecord.LogOther, *billingdomain.Clamp)
	Tiered(*usagerecord.LogOther, *domain.Tiered)
	Plugin(*usagerecord.LogOther, *domain.Plugin)
	MidjourneyOther(domain.RequestFacts, domain.Prices) *usagerecord.LogOther
}

type Usecases struct {
	Count    count_consumption.Usecase
	Record   record_consumption.Usecase
	Failure  record_failure.Usecase
	Metadata TaskMetadata
}

func New() Usecases {
	store := usagestorage.Store()
	renderer := storage.Content{Quota: logger.LogQuotaEnglish}
	errorsEnabled := func() bool { return constant.ErrorLogEnabled }
	return Usecases{
		Count: count_consumption.NewUsecase(store),
		Record: record_consumption.NewUsecase(store, renderer, record_consumption.AudioPrices{
			Completion: ratio_setting.GetCompletionRatio,
			Input:      ratio_setting.GetAudioRatio,
			Output:     ratio_setting.GetAudioCompletionRatio,
		}),
		Failure:  record_failure.NewUsecase(store, renderer, errorsEnabled),
		Metadata: storage.Metadata{},
	}
}

// WithLogMetadata keeps raw storage data out of business facts.
func WithLogMetadata(ctx context.Context, details []byte, affinity map[string]any) context.Context {
	return storage.WithLogMetadata(ctx, details, affinity)
}

// RequestObservation is plain technical data captured at the gateway boundary.
type RequestObservation = storage.LogObservation

func WithRequestObservation(ctx context.Context, observation RequestObservation) context.Context {
	return storage.WithRequestObservation(ctx, observation)
}
