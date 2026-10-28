package common

import (
	"bytes"
	"errors"
	"math"

	"github.com/shopspring/decimal"
)

// OpenRouterCostBasis freezes the only multipliers applied to the amount
// charged by OpenRouter. It is also persisted in existing durable snapshots.
type OpenRouterCostBasis struct {
	Version             string  `json:"version"`
	EffectiveGroupRatio float64 `json:"effective_group_ratio"`
	QuotaPerUnit        float64 `json:"quota_per_unit"`
	EstimatedUSD        float64 `json:"estimated_usd"`
}

func (b OpenRouterCostBasis) Quota(cost float64) (int, *QuotaClamp, error) {
	for _, value := range []float64{cost, b.EffectiveGroupRatio, b.QuotaPerUnit, b.EstimatedUSD} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
			return 0, nil, errors.New("invalid OpenRouter monetary basis")
		}
	}
	if b.QuotaPerUnit <= 0 {
		return 0, nil, errors.New("invalid OpenRouter quota unit")
	}
	value := decimal.NewFromFloat(cost).Mul(decimal.NewFromFloat(b.EffectiveGroupRatio)).Mul(decimal.NewFromFloat(b.QuotaPerUnit))
	quota, clamp := QuotaFromDecimalChecked(value.Round(0))
	return quota, clamp, nil
}

// ValidateOpenRouterCost accepts a JSON number, including explicit zero. Null,
// strings, booleans and overflow are never evidence of an account charge.
func ValidateOpenRouterCost(raw []byte) (float64, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || !(raw[0] == '-' || raw[0] >= '0' && raw[0] <= '9') {
		return 0, errors.New("missing or nonnumeric OpenRouter usage.cost")
	}
	var value float64
	if err := Unmarshal(raw, &value); err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, errors.New("invalid OpenRouter usage.cost")
	}
	return value, nil
}
