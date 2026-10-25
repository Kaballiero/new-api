package domain

import (
	"encoding/json"
	"time"

	billing "github.com/QuantumNous/new-api/internal/billing/domain"
)

type BillingSnapshot struct {
	Provider           json.RawMessage
	SettlementMode     string
	OpenRouterCost     *billing.ProviderBasis
	EvaluatedAt        time.Time
	Client             billing.TieredSnapshot
	PurchaseExpression string
	FX                 billing.FXBasis
	PureGroupRatio     float64
	Headers            map[string]string
}

// FinancialDetails retains native usage and metadata JSON without reprojecting it.
type FinancialDetails struct {
	ClientQuota    int             `json:"client_quota"`
	Usage          json.RawMessage `json:"usage,omitempty"`
	PurchaseUSD    *float64        `json:"purchase_usd,omitempty"`
	PurchaseStatus string          `json:"purchase_status"`
	Other          json.RawMessage `json:"other,omitempty"`
}
type UsageFacts struct {
	Usage *billing.Usage
	Wire  json.RawMessage
}
