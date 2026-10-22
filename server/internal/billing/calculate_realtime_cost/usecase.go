package calculate_realtime_cost

import (
	"fmt"

	"github.com/QuantumNous/new-api/internal/billing/domain"
)

type Usecase struct {
	math   domain.QuotaMath
	engine domain.ExpressionEvaluator
	prices domain.AudioPrices
}

func NewUsecase(math domain.QuotaMath, engine domain.ExpressionEvaluator, prices domain.AudioPrices) Usecase {
	return Usecase{math: math, engine: engine, prices: prices}
}

type Input struct {
	Usage      *domain.Usage
	Price      domain.AudioPrice
	Snapshot   *domain.TieredSnapshot
	Request    domain.RequestInput
	Reserved   int
	PriorClamp *domain.Clamp
}

func (u Usecase) Execute(in Input) (domain.AudioCost, error) {
	result := domain.AudioCost{Clamp: in.PriorClamp}
	if err := domain.ValidateRealtimeUsage(in.Usage); err != nil {
		return result, fmt.Errorf("invalid final WSS usage: %w", err)
	}
	var used map[string]bool
	if in.Snapshot != nil {
		used = u.engine.UsedVars(in.Snapshot.ExprString)
	}
	applied, quota, tiered, err := domain.SettleTiered(u.engine, in.Snapshot, in.Request, in.Reserved, domain.BuildRealtimeTieredTokenParams(in.Usage, used), true)
	if tiered != nil {
		domain.NoteClamp(&result.Clamp, tiered.Clamp)
	}
	if err != nil {
		return result, fmt.Errorf("invalid final WSS tiered quota: %w", err)
	}
	result.TieredApplied = applied
	if applied {
		result.TieredResult = tiered
	}
	price := in.Price
	price.CompletionRatio, price.AudioRatio, price.AudioCompletionRatio = u.prices.Read(price.ModelName)
	price.QuotaPerUnit = u.prices.QuotaUnit()
	base, clamp := domain.AudioQuota(price, u.math)
	domain.NoteClamp(&result.Clamp, clamp)
	if clamp != nil {
		return result, clamp
	}
	result.Quota = base
	if applied {
		result.Quota = quota
	}
	result.Billable = in.Usage.TotalTokens != 0
	if !result.Billable {
		result.Quota = 0
	}
	return result, nil
}
