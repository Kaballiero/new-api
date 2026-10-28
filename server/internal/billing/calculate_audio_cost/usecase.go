package calculate_audio_cost

import "github.com/QuantumNous/new-api/internal/billing/domain"

type Usecase struct {
	math   domain.QuotaMath
	engine domain.ExpressionEvaluator
	prices domain.AudioPrices
}

func NewUsecase(math domain.QuotaMath, engine domain.ExpressionEvaluator, prices domain.AudioPrices) Usecase {
	return Usecase{math: math, engine: engine, prices: prices}
}

type Input struct {
	Usage      domain.Usage
	Price      domain.AudioPrice
	Snapshot   *domain.TieredSnapshot
	Request    domain.RequestInput
	Reserved   int
	PriorClamp *domain.Clamp
}

func (u Usecase) Execute(in Input) domain.AudioCost {
	result := domain.AudioCost{Clamp: in.PriorClamp}
	var used map[string]bool
	if in.Snapshot != nil {
		used = u.engine.UsedVars(in.Snapshot.ExprString)
	}
	applied, quota, tiered, _ := domain.SettleTiered(u.engine, in.Snapshot, in.Request, in.Reserved, domain.BuildTieredTokenParams(&in.Usage, false, used), false)
	result.TieredApplied = applied
	result.TieredResult = tiered
	result.FixedPriceBilling = applied && domain.FixedSettlement(in.Snapshot, tiered)
	if tiered != nil {
		domain.NoteClamp(&result.Clamp, tiered.Clamp)
	}
	price := in.Price
	price.CompletionRatio, price.AudioRatio, price.AudioCompletionRatio = u.prices.Read(price.ModelName)
	price.QuotaPerUnit = u.prices.QuotaUnit()
	base, clamp := domain.AudioQuota(price, u.math)
	domain.NoteClamp(&result.Clamp, clamp)
	result.Quota = base
	if applied {
		result.Quota = quota
	}
	result.Billable = in.Usage.TotalTokens != 0 || result.FixedPriceBilling
	if !result.Billable {
		result.Quota = 0
	}
	return result
}
