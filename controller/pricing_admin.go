package controller

import (
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
)

// ============================================================================
// GET /api/option/pricing/models/:channel_type — list known models per provider
// ============================================================================

// ListProviderModelsResponse is the response payload for the per-provider model
// list endpoint. Source: the channel adaptor's static GetModelList() — i.e. the
// upstream-aware list shipped with the relay adapter, not the runtime channel
// table.
type ListProviderModelsResponse struct {
	ChannelType int      `json:"channel_type"`
	ChannelName string   `json:"channel_name"`
	APIType     int      `json:"api_type"`
	Models      []string `json:"models"`
}

// ListProviderModels handles GET /api/option/pricing/models/:channel_type.
// Returns the static model list bundled with the channel adapter (used by the
// admin UI to populate the bulk-pricing selector).
//
// Errors:
//   - 400 channel_type missing or not an int
//   - 404 channel type has no registered adapter (no APIType mapping)
func ListProviderModels(c *gin.Context) {
	raw := c.Param("channel_type")
	channelType, err := strconv.Atoi(raw)
	if err != nil {
		common.ApiErrorI18nStatus(c, http.StatusBadRequest, i18n.MsgInvalidParams)
		return
	}
	apiType, ok := common.ChannelType2APIType(channelType)
	if !ok {
		common.ApiErrorI18nStatus(c, http.StatusNotFound, i18n.MsgChannelTypeInvalid, map[string]any{"Type": channelType})
		return
	}
	adaptor := relay.GetAdaptor(apiType)
	if adaptor == nil {
		common.ApiErrorI18nStatus(c, http.StatusNotFound, i18n.MsgChannelTypeInvalid, map[string]any{"Type": channelType})
		return
	}
	models := adaptor.GetModelList()
	if models == nil {
		models = []string{}
	}
	common.ApiSuccess(c, ListProviderModelsResponse{
		ChannelType: channelType,
		ChannelName: adaptor.GetChannelName(),
		APIType:     apiType,
		Models:      models,
	})
}

// ============================================================================
// POST /api/option/pricing/adjust — bulk multiplier across models + scopes
// ============================================================================

// pricingScope is the canonical name of a price/ratio dimension. Each scope
// corresponds to one in-memory map in `setting/ratio_setting` and one option
// row persisted via `model.UpdateOptionsBulk`.
type pricingScope string

const (
	scopeModelRatio           pricingScope = "model_ratio"
	scopeModelPrice           pricingScope = "model_price"
	scopeCompletionRatio      pricingScope = "completion_ratio"
	scopeCacheRatio           pricingScope = "cache_ratio"
	scopeCreateCacheRatio     pricingScope = "create_cache_ratio"
	scopeImageRatio           pricingScope = "image_ratio"
	scopeAudioRatio           pricingScope = "audio_ratio"
	scopeAudioCompletionRatio pricingScope = "audio_completion_ratio"
)

// scopeDef describes how to read/write a single pricing dimension.
type scopeDef struct {
	OptionKey string                       // option row key in DB / OptionMap
	GetCopy   func() map[string]float64    // snapshot of the in-memory map
}

var pricingScopes = map[pricingScope]scopeDef{
	scopeModelRatio:           {OptionKey: "ModelRatio", GetCopy: ratio_setting.GetModelRatioCopy},
	scopeModelPrice:           {OptionKey: "ModelPrice", GetCopy: ratio_setting.GetModelPriceCopy},
	scopeCompletionRatio:      {OptionKey: "CompletionRatio", GetCopy: ratio_setting.GetCompletionRatioCopy},
	scopeCacheRatio:           {OptionKey: "CacheRatio", GetCopy: ratio_setting.GetCacheRatioCopy},
	scopeCreateCacheRatio:     {OptionKey: "CreateCacheRatio", GetCopy: ratio_setting.GetCreateCacheRatioCopy},
	scopeImageRatio:           {OptionKey: "ImageRatio", GetCopy: ratio_setting.GetImageRatioCopy},
	scopeAudioRatio:           {OptionKey: "AudioRatio", GetCopy: ratio_setting.GetAudioRatioCopy},
	scopeAudioCompletionRatio: {OptionKey: "AudioCompletionRatio", GetCopy: ratio_setting.GetAudioCompletionRatioCopy},
}

// defaultAdjustScopes is the fallback scope set when the client omits `scopes`.
// Covers the two "real money" dimensions (input ratio + flat per-request price).
// completion_ratio is a multiplier, not a price — including it would compound
// the adjustment on output tokens, which is rarely what the operator wants.
var defaultAdjustScopes = []pricingScope{scopeModelRatio, scopeModelPrice}

// AdjustModelPricingRequest is the body for POST /api/option/pricing/adjust.
// Multiplier is applied as `new = old * multiplier` for every (model, scope)
// pair where an existing price entry is found. Missing entries are skipped and
// reported in the response — no new price is invented from the multiplier alone.
type AdjustModelPricingRequest struct {
	// Models to apply the adjustment to. Required, non-empty, max 500.
	Models []string `json:"models"`
	// Multiplier — any positive float. 1.10 → +10%, 0.95 → -5%, 2.0 → x2.
	Multiplier float64 `json:"multiplier"`
	// Scopes to touch. Optional — defaults to [model_ratio, model_price].
	// Values must be one of: model_ratio, model_price, completion_ratio,
	// cache_ratio, create_cache_ratio, image_ratio, audio_ratio,
	// audio_completion_ratio.
	Scopes []string `json:"scopes"`
}

// PricingChange captures the before/after value for a single (model, scope) edit.
type PricingChange struct {
	Model string  `json:"model"`
	Scope string  `json:"scope"`
	From  float64 `json:"from"`
	To    float64 `json:"to"`
}

// PricingSkip captures a (model, scope) pair that was requested but not applied.
type PricingSkip struct {
	Model  string `json:"model"`
	Scope  string `json:"scope"`
	Reason string `json:"reason"` // "no_entry" — model has no existing price in this scope
}

// AdjustModelPricingResponse summarizes the outcome of an adjust request.
type AdjustModelPricingResponse struct {
	Multiplier        float64         `json:"multiplier"`
	Applied           []PricingChange `json:"applied"`
	Skipped           []PricingSkip   `json:"skipped"`
	OptionKeysWritten []string        `json:"option_keys_written"`
}

const adjustPricingMaxModels = 500

// AdjustModelPricing handles POST /api/option/pricing/adjust.
//
// Atomicity: all touched option keys are written in a single DB transaction
// via model.UpdateOptionsBulk; in-memory maps are updated only after the tx
// commits. If the tx fails, in-memory state is untouched.
//
// REST codes:
//   - 400 empty models, models > 500, multiplier <= 0, unknown scope
//   - 200 success (skipped reported in the body; possibly empty applied list
//     when every requested (model,scope) pair was skipped)
//   - 500 DB transaction failure
func AdjustModelPricing(c *gin.Context) {
	var req AdjustModelPricingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorI18nStatus(c, http.StatusBadRequest, i18n.MsgInvalidParams)
		return
	}
	if len(req.Models) == 0 {
		common.ApiErrorI18nStatus(c, http.StatusBadRequest, i18n.MsgInvalidParams)
		return
	}
	if len(req.Models) > adjustPricingMaxModels {
		common.ApiErrorI18nStatus(c, http.StatusBadRequest, i18n.MsgBatchTooMany, map[string]any{"Max": adjustPricingMaxModels})
		return
	}
	if req.Multiplier <= 0 {
		common.ApiErrorI18nStatus(c, http.StatusBadRequest, i18n.MsgPricingMultiplierInvalid)
		return
	}

	scopes, err := resolveScopes(req.Scopes)
	if err != nil {
		common.ApiErrorI18nStatus(c, http.StatusBadRequest, i18n.MsgPricingScopeInvalid, map[string]any{"Scope": err.Error()})
		return
	}

	resp := AdjustModelPricingResponse{
		Multiplier: req.Multiplier,
		Applied:    []PricingChange{},
		Skipped:    []PricingSkip{},
	}
	pendingWrites := map[string]string{}

	for _, scope := range scopes {
		def := pricingScopes[scope]
		snapshot := def.GetCopy()
		touched := false
		for _, modelName := range req.Models {
			oldVal, ok := snapshot[modelName]
			if !ok {
				resp.Skipped = append(resp.Skipped, PricingSkip{Model: modelName, Scope: string(scope), Reason: "no_entry"})
				continue
			}
			newVal := oldVal * req.Multiplier
			snapshot[modelName] = newVal
			touched = true
			resp.Applied = append(resp.Applied, PricingChange{
				Model: modelName,
				Scope: string(scope),
				From:  oldVal,
				To:    newVal,
			})
		}
		if !touched {
			continue
		}
		jsonBytes, mErr := common.Marshal(snapshot)
		if mErr != nil {
			common.ApiErrorStatus(c, http.StatusInternalServerError, mErr)
			return
		}
		pendingWrites[def.OptionKey] = string(jsonBytes)
		resp.OptionKeysWritten = append(resp.OptionKeysWritten, def.OptionKey)
	}

	if len(pendingWrites) > 0 {
		if err := model.UpdateOptionsBulk(pendingWrites); err != nil {
			common.ApiErrorStatus(c, http.StatusInternalServerError, err)
			return
		}
	}

	common.ApiSuccess(c, resp)
}

// resolveScopes normalizes user-supplied scope names to canonical pricingScope
// values. Empty input → defaultAdjustScopes. Unknown scope → error with the
// offending name as the error string (used as the i18n template arg).
func resolveScopes(raw []string) ([]pricingScope, error) {
	if len(raw) == 0 {
		out := make([]pricingScope, len(defaultAdjustScopes))
		copy(out, defaultAdjustScopes)
		return out, nil
	}
	seen := make(map[pricingScope]struct{}, len(raw))
	out := make([]pricingScope, 0, len(raw))
	for _, name := range raw {
		scope := pricingScope(name)
		if _, ok := pricingScopes[scope]; !ok {
			return nil, &scopeNameError{name: name}
		}
		if _, dup := seen[scope]; dup {
			continue
		}
		seen[scope] = struct{}{}
		out = append(out, scope)
	}
	return out, nil
}

type scopeNameError struct{ name string }

func (e *scopeNameError) Error() string { return e.name }
