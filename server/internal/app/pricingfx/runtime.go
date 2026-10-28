package pricingfx

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	storage "github.com/QuantumNous/new-api/internal/adapter/gorm/pricing/fx"
	httpfx "github.com/QuantumNous/new-api/internal/adapter/http/pricing/fx"
	state "github.com/QuantumNous/new-api/internal/adapter/pricing/fx"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	refresh "github.com/QuantumNous/new-api/internal/pricing/refresh_cost_fx"
	"github.com/QuantumNous/new-api/logger"
)

const (
	modelCostFXSource       = domain.ModelCostFXSource
	modelCostFXReloadPeriod = time.Minute
	modelCostFXHTTPTimeout  = httpfx.Timeout
)

var modelCostFXEndpoint = "https://www.cbr-xml-daily.ru/daily_json.js"

type store struct{}

func (store) Load(ctx context.Context, source string) error {
	return storage.LoadModelCostFX(ctx, source)
}
func (store) Current(source string) (domain.ModelCostFXSnapshot, error) {
	return state.CurrentModelCostFX(source)
}
func (store) Save(ctx context.Context, s domain.ModelCostFXSnapshot, version int64) error {
	return storage.SaveModelCostFX(ctx, s, version)
}

var Refresh = sync.OnceValue(func() refresh.Usecase {
	return refresh.NewUsecase(store{}, httpfx.Client{Endpoint: func() string { return modelCostFXEndpoint }}.Fetch)
})
var modelCostFXStartupOnce sync.Once

// StartModelCostFX loads the durable snapshot on every node and keeps replicas
// current without putting database or network work on request paths.
func StartModelCostFX() {
	modelCostFXStartupOnce.Do(func() {
		go func() {
			loadModelCostFXFromDatabase()
			ticker := time.NewTicker(modelCostFXReloadPeriod)
			defer ticker.Stop()
			for range ticker.C {
				loadModelCostFXFromDatabase()
			}
		}()
	})
}

func loadModelCostFXFromDatabase() {
	ctx, cancel := context.WithTimeout(context.Background(), modelCostFXHTTPTimeout)
	defer cancel()
	if err := storage.LoadModelCostFX(ctx, modelCostFXSource); err != nil && !errors.Is(err, domain.ErrModelCostFXUnavailable) {
		logger.LogWarn(ctx, fmt.Sprintf("model cost FX startup/reload failed: %v", err))
	}
}
