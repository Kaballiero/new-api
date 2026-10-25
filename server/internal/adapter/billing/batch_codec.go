package billing

import (
	"encoding/json"
	"io"
	"time"

	"github.com/QuantumNous/new-api/common"
	taskrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
)

type StoredFX struct {
	USDRate            float64 `json:"usd_rate"`
	Factor             float64 `json:"factor"`
	Source             string  `json:"source"`
	PublicationVersion int64   `json:"publication_version"`
	EffectiveAt        int64   `json:"effective_at"`
	FetchedAt          int64   `json:"fetched_at"`
}

type BatchBillingSnapshot struct {
	// Provider freezes envelope routing alongside the admission contract so a
	// worker restart never reconstructs it from mutable channel settings.
	Provider       json.RawMessage             `json:"provider,omitempty"`
	SettlementMode string                      `json:"settlement_mode,omitempty"`
	OpenRouterCost *common.OpenRouterCostBasis `json:"openrouter_cost,omitempty"`
	EvaluatedAt    time.Time                   `json:"evaluated_at"`
	Client         billingexpr.BillingSnapshot `json:"client"`
	// Preserve the procurement basis of existing batches. New batches freeze
	// the same expression used for client billing, before group/FX multipliers.
	PurchaseExpression string            `json:"purchase_expression"`
	FX                 StoredFX          `json:"fx"`
	PureGroupRatio     float64           `json:"pure_group_ratio"`
	Headers            map[string]string `json:"headers"`
}

type BatchCodec struct{}

func (BatchCodec) Marshal(value any) ([]byte, error)      { return common.Marshal(value) }
func (BatchCodec) Unmarshal(data []byte, value any) error { return common.Unmarshal(data, value) }
func (BatchCodec) UnmarshalJsonStr(data string, value any) error {
	return common.UnmarshalJsonStr(data, value)
}
func (BatchCodec) NewDecoder(r io.Reader) domain.Decoder { return common.NewJSONStreamDecoder(r) }
func (BatchCodec) Snapshot(raw string) (domain.BillingSnapshot, error) {
	var s BatchBillingSnapshot
	err := common.UnmarshalJsonStr(raw, &s)
	out := domain.BillingSnapshot{Provider: s.Provider, SettlementMode: s.SettlementMode, EvaluatedAt: s.EvaluatedAt,
		Client: *taskrecord.ToDomainTiered(&s.Client), PurchaseExpression: s.PurchaseExpression,
		FX: billingdomain.FXBasis(s.FX), PureGroupRatio: s.PureGroupRatio, Headers: s.Headers}
	if s.OpenRouterCost != nil {
		out.OpenRouterCost = &billingdomain.ProviderBasis{Version: s.OpenRouterCost.Version,
			EffectiveGroupRatio: s.OpenRouterCost.EffectiveGroupRatio, QuotaPerUnit: s.OpenRouterCost.QuotaPerUnit,
			EstimatedUSD: s.OpenRouterCost.EstimatedUSD}
	}
	return out, err
}
func (BatchCodec) EncodeSnapshot(s domain.BillingSnapshot) ([]byte, error) {
	out := BatchBillingSnapshot{Provider: s.Provider, SettlementMode: s.SettlementMode, EvaluatedAt: s.EvaluatedAt,
		Client: *taskrecord.FromDomainTiered(&s.Client), PurchaseExpression: s.PurchaseExpression,
		FX: StoredFX(s.FX), PureGroupRatio: s.PureGroupRatio, Headers: s.Headers}
	if s.OpenRouterCost != nil {
		out.OpenRouterCost = &common.OpenRouterCostBasis{Version: s.OpenRouterCost.Version,
			EffectiveGroupRatio: s.OpenRouterCost.EffectiveGroupRatio, QuotaPerUnit: s.OpenRouterCost.QuotaPerUnit,
			EstimatedUSD: s.OpenRouterCost.EstimatedUSD}
	}
	return common.Marshal(out)
}
