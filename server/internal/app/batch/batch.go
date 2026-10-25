package batch

import (
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	diagnostics "github.com/QuantumNous/new-api/internal/adapter/batchruntime"
	math "github.com/QuantumNous/new-api/internal/adapter/billing"
	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/batch"
	usageadapter "github.com/QuantumNous/new-api/internal/adapter/gorm/usage"
	rediscache "github.com/QuantumNous/new-api/internal/adapter/redis/batch"
	settings "github.com/QuantumNous/new-api/internal/adapter/settings/batch"
	billingapp "github.com/QuantumNous/new-api/internal/app/billing"
	"github.com/QuantumNous/new-api/internal/batch/create_batch"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/internal/batch/poll_batches"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func NewStore() storage.Store {
	return storage.NewStore(func() *gorm.DB { return model.DB }, func() *gorm.DB { return model.LOG_DB }, usageadapter.BatchLogs{}, math.BatchMath{}, rediscache.Cache{})
}
func NewPolling(g poll_batches.Gateway, kinds domain.ChannelKinds) poll_batches.Usecase {
	return poll_batches.NewUsecase(NewStore(), g, math.BatchCodec{}, math.BatchMath{}, usageadapter.BatchMetadata{}, diagnostics.Diagnostics{}, time.Now, kinds)
}
func NewCreation(g domain.AdmissionGateway, v create_batch.Validation) create_batch.Usecase {
	fx := billingapp.NewCosts().FX
	return create_batch.NewUsecase(NewStore(), g, math.BatchCodec{}, v, math.BatchMath{},
		settings.Prices{}, func() (billingdomain.FXBasis, error) { return fx.Execute(billingdomain.FXBasis{}) },
		func() bool { return constant.UpdateTask }, time.Now, constant.ChannelTypeOpenRouter,
		func() float64 { return common.QuotaPerUnit }, common.MaxWalletQuota)
}
