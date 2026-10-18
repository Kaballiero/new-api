package common

import (
	"encoding/json"
	"github.com/QuantumNous/new-api/common"
)

type OpenRouterCostBasis = common.OpenRouterCostBasis

// OpenRouterCostState is independent of token projections and estimates.
type OpenRouterCostState struct {
	Basis         common.OpenRouterCostBasis
	CostUSD       float64
	Valid         bool
	Final         bool
	Sent          bool
	CostDetails   json.RawMessage
	IsBYOK        *bool
	UnknownReason string
	// ApproximateResearchReserveUSD records the accepted hold policy, not a
	// guaranteed maximum charge. Actual cost always uses the frozen Basis.
	ApproximateResearchReserveUSD *float64
	ApproximateReserveUSD         *float64
	ApproximateReserveModel       string
}

func NewOpenRouterCostBasis(info *RelayInfo, estimatedUSD float64) common.OpenRouterCostBasis {
	return common.OpenRouterCostBasis{Version: "openrouter_cost_v1", EffectiveGroupRatio: info.PriceData.EffectiveGroupRatio(), QuotaPerUnit: common.QuotaPerUnit, EstimatedUSD: estimatedUSD}
}

// Observe updates every present cost, including invalid/null values. A final
// malformed snapshot must never retain an earlier provisional price.
func (s *OpenRouterCostState) Observe(body []byte, terminal bool) {
	var envelope struct {
		Usage    map[string]json.RawMessage `json:"usage"`
		Response json.RawMessage            `json:"response"`
		Type     string                     `json:"type"`
		Status   string                     `json:"status"`
		Error    json.RawMessage            `json:"error"`
	}
	if common.Unmarshal(body, &envelope) != nil {
		s.Valid = false
		s.Final = false
		s.UnknownReason = "malformed_usage"
		return
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" || (envelope.Type == "response.failed" || envelope.Type == "response.error" || envelope.Type == "response.cancelled" || envelope.Type == "response.canceled") || envelope.Status == "failed" || envelope.Status == "cancelled" || envelope.Status == "canceled" {
		s.Valid = false
		s.Final = false
		s.UnknownReason = "upstream_error"
		return
	}
	terminal = terminal || envelope.Type == "response.completed" || envelope.Type == "response.done" || envelope.Type == "response.incomplete"
	var fields map[string]json.RawMessage
	if common.Unmarshal(body, &fields) == nil {
		if data, ok := fields["data"]; ok && len(data) > 0 && len(envelope.Usage) == 0 {
			s.Observe(data, terminal)
			return
		}
		if _, present := fields["usage"]; present {
			if _, hasCost := envelope.Usage["cost"]; !hasCost {
				s.Valid = false
				s.Final = false
				s.UnknownReason = "missing_cost"
			}
		}
	}
	if len(envelope.Response) > 0 && string(envelope.Response) != "null" {
		s.Observe(envelope.Response, terminal)
		return
	}
	if terminal {
		if _, present := envelope.Usage["cost"]; !present {
			s.Valid = false
			s.Final = false
			s.UnknownReason = "missing_terminal_cost"
			return
		}
	}
	if raw, present := envelope.Usage["cost_details"]; present {
		s.CostDetails = append(json.RawMessage(nil), raw...)
	}
	if raw, present := envelope.Usage["is_byok"]; present {
		var byok bool
		if common.Unmarshal(raw, &byok) == nil {
			s.IsBYOK = &byok
		}
	}
	if raw, present := envelope.Usage["cost"]; present {
		cost, err := common.ValidateOpenRouterCost(raw)
		s.Valid = err == nil
		s.Final = false
		if err != nil {
			s.UnknownReason = "invalid_cost"
		} else {
			s.CostUSD = cost
			s.UnknownReason = ""
		}
	}

	if terminal {
		s.Final = s.Valid
		if !s.Valid && s.UnknownReason == "" {
			s.UnknownReason = "missing_terminal_cost"
		}
	}
}
