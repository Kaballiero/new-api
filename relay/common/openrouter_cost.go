package common

import (
	"encoding/json"
	"github.com/QuantumNous/new-api/common"
	"strings"
)

type OpenRouterCostBasis = common.OpenRouterCostBasis

// OpenRouterCostState is independent of token projections and estimates.
type OpenRouterCostState struct {
	Basis            common.OpenRouterCostBasis
	CostUSD          float64
	Valid            bool
	Final            bool
	Sent             bool
	CostDetails      json.RawMessage
	IsBYOK           *bool
	UnknownReason    string
	BufferedAudio    bool
	ChatImage        bool
	RoutingHash      string
	IdentityConflict bool
	// CaptureGeneration persists correlation before usage validation can fail.
	CaptureGeneration func(string) error `json:"-"`
	GenerationID      string
	CostSource        string
	// TaskID transfers financial ownership to the durable Task lifecycle.
	TaskID string
	// ChatText marks a plain chat completion whose missing cost may be
	// deferred to the durable Task through its generation identity.
	ChatText bool
	// ContentDelivered records that a forwarded stream chunk carried content,
	// reasoning or a tool call. Read-ahead and role-only chunks never count.
	ContentDelivered bool
	// UpstreamError keeps the error object of a successful-status envelope.
	UpstreamError json.RawMessage
	// ApproximateResearchReserveUSD records the accepted hold policy, not a
	// guaranteed maximum charge. Actual cost always uses the frozen Basis.
	ApproximateResearchReserveUSD *float64
	ApproximateReserveUSD         *float64
	ApproximateReserveModel       string
	AddOnReserveUSD               *float64
	AddOnReserveModel             string
	ConfiguredReserveUSD          *float64
	ConfiguredReserveModel        string
	ConfiguredReserveExprHash     string
}

func NewOpenRouterCostBasis(info *RelayInfo, estimatedUSD float64) common.OpenRouterCostBasis {
	return common.OpenRouterCostBasis{Version: "openrouter_cost_v1", EffectiveGroupRatio: info.PriceData.EffectiveGroupRatio(), QuotaPerUnit: common.QuotaPerUnit, EstimatedUSD: estimatedUSD}
}

// Observe updates every present cost, including invalid/null values. A final
// malformed snapshot must never retain an earlier provisional price.
func (s *OpenRouterCostState) Observe(body []byte, terminal bool) {
	var envelope struct {
		ID       string                     `json:"id"`
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
	if (s.ChatImage || s.ChatText) && envelope.ID != "" {
		s.ObserveGenerationID(envelope.ID)
	}
	// A text identity conflict only disables deferral; a valid cost still settles.
	if s.ChatImage && s.IdentityConflict {
		s.Valid, s.Final, s.UnknownReason = false, false, "generation_identity_unverified"
		return
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" || (envelope.Type == "response.failed" || envelope.Type == "response.error" || envelope.Type == "response.cancelled" || envelope.Type == "response.canceled") || envelope.Status == "failed" || envelope.Status == "cancelled" || envelope.Status == "canceled" {
		s.Valid = false
		s.Final = false
		s.UnknownReason = "upstream_error"
		if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
			s.UpstreamError = append(json.RawMessage(nil), envelope.Error...)
		}
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

// ObserveGenerationID accepts only OpenRouter generation identities, never a
// provider request ID. A disagreement or failed durable write stays fail closed.
func (s *OpenRouterCostState) ObserveGenerationID(id string) {
	if id == "" || id == s.GenerationID {
		return
	}
	valid := strings.HasPrefix(id, "gen-") && len(id) > 4 && len(id) <= 128 && strings.IndexFunc(id[4:], func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-')
	}) == -1
	if s.CaptureGeneration != nil {
		if err := s.CaptureGeneration(id); err != nil {
			s.IdentityConflict = true
			return
		}
	}
	if !valid {
		s.IdentityConflict = true
		return
	}
	if s.GenerationID != "" && s.GenerationID != id {
		s.IdentityConflict = true
		return
	}
	s.GenerationID = id
}
