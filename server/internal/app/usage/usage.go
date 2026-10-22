// Package usage composes recording dependencies without gateway imports.
package usage

import (
	"context"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/usage/count_consumption"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/record_consumption"
	"github.com/QuantumNous/new-api/internal/usage/record_failure"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

// BRIDGE: service/{task_billing,batch_billing}.go and relay/mjproxy_handler.go via the single BillingGateway until wave 5.
type TaskMetadata interface {
	BillingFX(*model.LogOther, domain.RequestFacts, domain.PriceFacts, string)
	Clamp(*model.LogOther, *billingdomain.Clamp)
	Tiered(*model.LogOther, *domain.Tiered)
	Plugin(*model.LogOther, *domain.Plugin)
	MidjourneyOther(domain.RequestFacts, domain.Prices) *model.LogOther
}
type Usecases struct {
	Count    count_consumption.Usecase
	Record   record_consumption.Usecase
	Failure  record_failure.Usecase
	Metadata TaskMetadata
}

func New() Usecases {
	store := storage.Store{
		ConsumeEnabled: func() bool { return common.LogConsumeEnabled },
		ExportEnabled:  func() bool { return common.DataExportEnabled },
	}
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

// WithLogMetadata keeps raw storage data out of business facts and passes the
// original Gin context through to model writers without introducing fake Gin.
func WithLogMetadata(ctx context.Context, details []byte, affinity map[string]any) context.Context {
	return storage.WithLogMetadata(ctx, details, affinity)
}
