package usage

import (
	"encoding/json"

	"github.com/QuantumNous/new-api/common"
	usagerecord "github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	batch "github.com/QuantumNous/new-api/internal/batch/domain"
	billing "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/usage/domain"
)

type BatchMetadata struct{}

func (BatchMetadata) ItemCost(s batch.BillingSnapshot, reserved int64, quota int, cost float64,
	raw map[string]json.RawMessage, clamp *billing.Clamp) ([]byte, error) {
	other := new(usagerecord.LogOther)
	other.SetPublic("settlement_mode", s.SettlementMode)
	other.SetPublic("reserved_quota", reserved)
	other.SetPublic("actual_quota", quota)
	other.SetAdmin("upstream_cost_usd", cost)
	if value, ok := raw["is_byok"]; ok {
		other.SetAdmin("upstream_is_byok", value)
	}
	if value, ok := raw["cost_details"]; ok {
		other.SetAdmin("upstream_cost_details", value)
	}
	other.SetAdmin("openrouter_cost_basis", storedCostBasis(*s.OpenRouterCost))
	(Metadata{}).Clamp(other, clamp)
	return common.Marshal(other)
}
func (BatchMetadata) Tiered(s batch.BillingSnapshot, result billing.TieredResult) ([]byte, error) {
	other := new(usagerecord.LogOther)
	actual := &domain.TieredResult{MatchedTier: result.MatchedTier, BillingUnit: result.BillingUnit,
		FixedPrice: result.FixedPrice, RequestRules: result.RequestRules}
	(Metadata{}).Tiered(other, &domain.Tiered{ExprString: s.Client.ExprString, EstimatedTier: s.Client.EstimatedTier,
		EstimatedBillingUnit: s.Client.EstimatedBillingUnit, EstimatedFixedPrice: s.Client.EstimatedFixedPrice,
		Actual: actual})
	(Metadata{}).Clamp(other, result.Clamp)
	return common.Marshal(other)
}
