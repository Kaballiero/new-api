package poll_batches

import (
	"encoding/json"
	"errors"
	"math"

	"github.com/QuantumNous/new-api/internal/batch/domain"
	billingdomain "github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/shopspring/decimal"
)

type Billing interface {
	billingdomain.ExpressionEvaluator
	Cost([]byte) (float64, error)
	Provider(billingdomain.ProviderBasis, float64) (int, *billingdomain.Clamp, error)
	Raw(string, billingdomain.TokenParams, billingdomain.RequestInput) (float64, error)
}
type Metadata interface {
	ItemCost(domain.BillingSnapshot, int64, int, float64, map[string]json.RawMessage, *billingdomain.Clamp) ([]byte, error)
	Tiered(domain.BillingSnapshot, billingdomain.TieredResult) ([]byte, error)
}

func (u Usecase) settleResults(job *domain.Job) error {
	snapshot, err := u.codec.Snapshot(job.BillingSnapshot)
	if err != nil {
		return err
	}
	if snapshot.SettlementMode == domain.BatchSettlementOpenRouterJobCostV2 {
		return u.store.SettleBatchJobCost(job.ID)
	}
	// Each row is bounded; do not collect an entire batch's output in memory.
	var cursor int64
	for {
		item, err := u.store.NextUnsettled(job.ID, cursor)
		if err != nil {
			if errors.Is(err, domain.ErrNotFound) {
				return nil
			}
			return err
		}
		cursor = item.ID
		var result domain.Result
		if err := u.codec.UnmarshalJsonStr(item.Result, &result); err != nil {
			return err
		}
		details := domain.FinancialDetails{PurchaseStatus: "reconciliation_required"}
		quota := 0
		if result.Unbilled {
			zero := 0.0
			details.PurchaseUSD = &zero
			details.PurchaseStatus = "unbilled"
		} else if snapshot.SettlementMode == domain.BatchSettlementOpenRouterCostV1 {
			var envelope struct {
				Usage map[string]json.RawMessage `json:"usage"`
			}
			err := u.codec.Unmarshal(result.Body, &envelope)
			cost, costErr := u.billing.Cost(envelope.Usage["cost"])
			if err != nil || costErr != nil || (snapshot.OpenRouterCost == nil || snapshot.OpenRouterCost.Version != domain.BatchSettlementOpenRouterCostV1) ||
				result.StatusCode < 200 || result.StatusCode >= 300 {
				if err := u.store.MarkItemUnknown(item.ID); err != nil {
					return err
				}
				continue
			}
			var clamp *billingdomain.Clamp
			quota, clamp, err = u.billing.Provider(*snapshot.OpenRouterCost, cost)
			if err != nil {
				return err
			}
			details.PurchaseUSD, details.PurchaseStatus = &cost, "calculated"
			// Tokens are optional accounting evidence; their absence or malformed
			// projection cannot invalidate independently authoritative item cost.
			usageFacts, _ := u.gateway.UsageFacts(job, result)
			details.Usage = usageFacts.Wire
			details.Other, err = u.metadata.ItemCost(snapshot, item.ReservedQuota, quota, cost, envelope.Usage, clamp)
			if err != nil {
				return err
			}
			if clamp != nil {
				u.diagnostics.Saturation(job, item.CustomID, clamp)
			}
		} else {
			if snapshot.SettlementMode != "" {
				return errors.New("unknown batch settlement mode")
			}
			usageFacts, err := u.gateway.UsageFacts(job, result)
			if err != nil {
				if err := u.store.MarkItemUnknown(item.ID); err != nil {
					return err
				}
				continue
			}
			details.Usage = usageFacts.Wire
			billingUsage := usageFacts.Usage
			params := billingdomain.BuildTieredTokenParams(billingUsage, billingUsage.UsageSemantic == "anthropic",
				u.billing.UsedVars(snapshot.Client.ExprString))
			computed, err := u.billing.Compute(&snapshot.Client, params, billingdomain.RequestInput{Body: []byte(item.UpstreamBody),
				Headers: snapshot.Headers, EvaluatedAt: snapshot.EvaluatedAt})
			if err != nil {
				return err
			}
			quota = computed.ActualQuotaAfterGroup
			details.Other, err = u.metadata.Tiered(snapshot, computed)
			if err != nil {
				return err
			}
			if computed.Clamp != nil {
				u.diagnostics.Saturation(job, item.CustomID, computed.Clamp)
			}
			details.PurchaseUSD = u.purchaseUSD(job, result, billingUsage, snapshot, []byte(item.UpstreamBody))
			if details.PurchaseUSD != nil {
				details.PurchaseStatus = "calculated"
			}
		}
		details.ClientQuota = quota
		data, err := u.codec.Marshal(details)
		if err != nil {
			return err
		}
		if err := u.store.SettleBatchItem(job.ID, item.CustomID, quota, string(data)); err != nil {
			return err
		}
	}
}

func (u Usecase) purchaseUSD(job *domain.Job, result domain.Result, usage *billingdomain.Usage,
	snapshot domain.BillingSnapshot, body []byte) *float64 {
	if job.ChannelType == u.kinds.OpenRouter {
		var envelope struct {
			Usage struct {
				Cost *float64 `json:"cost"`
			} `json:"usage"`
		}
		if u.codec.Unmarshal(result.Body, &envelope) == nil && envelope.Usage.Cost != nil &&
			*envelope.Usage.Cost >= 0 && !math.IsNaN(*envelope.Usage.Cost) && !math.IsInf(*envelope.Usage.Cost,
			0) {
			return envelope.Usage.Cost
		}
		return nil
	}
	if snapshot.PurchaseExpression == "" {
		return nil
	}
	// Token expressions alone do not prove ancillary tool/grounding charges.
	var request map[string]json.RawMessage
	var response map[string]json.RawMessage
	if u.codec.Unmarshal(body, &request) != nil || u.codec.Unmarshal(result.Body, &response) != nil {
		return nil
	}
	if len(request["tools"]) > 0 || len(response["groundingMetadata"]) > 0 {
		return nil
	}

	billingUsage := usage
	params := billingdomain.BuildTieredTokenParams(billingUsage, billingUsage.UsageSemantic == "anthropic",
		u.billing.UsedVars(snapshot.PurchaseExpression))
	value, err := u.billing.Raw(snapshot.PurchaseExpression, params, billingdomain.RequestInput{Body: body,
		Headers: snapshot.Headers, EvaluatedAt: snapshot.EvaluatedAt})
	if err != nil || value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return nil
	}
	usd := value / 1_000_000
	return &usd
}

// ReconcileBatchPurchase runs independently of client settlement. Provider
// aggregate facts can arrive after client receipts; this never debits a wallet.
func (u Usecase) reconcilePurchase(job *domain.Job) error {
	current, err := u.store.ByID(job.ID)
	if err != nil {
		return err
	}
	if current.PurchaseStatus == "settled" {
		return nil
	}
	if job.ChannelType == u.kinds.OpenRouter && current.UpstreamUsage != "" {
		var facts struct {
			Cost *float64 `json:"cost"`
		}
		if u.codec.UnmarshalJsonStr(current.UpstreamUsage, &facts) == nil && facts.Cost != nil &&
			*facts.Cost >= 0 && !math.IsNaN(*facts.Cost) && !math.IsInf(*facts.Cost, 0) && domain.State(current.Status).Terminal() {
			return u.store.ReconcilePurchase(current, decimal.NewFromFloat(*facts.Cost).String(), "provider_aggregate")
		}
	}
	var cursor int64
	total := decimal.Zero
	count := 0
	for {
		item, err := u.store.NextPurchase(job.ID, cursor)
		if errors.Is(err, domain.ErrNotFound) {
			break
		}
		if err != nil {
			return err
		}
		cursor = item.ID
		if item.PurchaseStatus != "calculated" && item.PurchaseStatus != "unbilled" {
			return nil
		}
		var facts domain.FinancialDetails
		if err := u.codec.UnmarshalJsonStr(item.FinancialDetails, &facts); err != nil {
			return err
		}
		if facts.PurchaseUSD == nil || *facts.PurchaseUSD < 0 || math.IsNaN(*facts.PurchaseUSD) || math.IsInf(*facts.PurchaseUSD, 0) {
			return nil
		}
		total = total.Add(decimal.NewFromFloat(*facts.PurchaseUSD))
		count++
	}
	if count != current.RequestCount {
		return nil
	}
	return u.store.ReconcilePurchase(current, total.String(), "item_receipts")
}
