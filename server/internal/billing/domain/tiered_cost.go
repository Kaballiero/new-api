package domain

import (
	"fmt"
	"math"
)

func BuildTieredTokenParams(usage *Usage, isClaudeUsageSemantic bool, usedVars map[string]bool) TokenParams {
	p := float64(usage.PromptTokens)
	c := float64(usage.CompletionTokens)
	cr := float64(usage.PromptTokensDetails.CachedTokens)
	cc5m := float64(usage.PromptTokensDetails.CacheCreationTokensTotal())
	cc1h := float64(0)

	if usage.UsageSemantic == "anthropic" {
		cc1h = float64(usage.ClaudeCacheCreation1hTokens)
		cc5m = float64(usage.ClaudeCacheCreation5mTokens)
	}

	img := float64(usage.PromptTokensDetails.ImageTokens)
	ai := float64(usage.PromptTokensDetails.AudioTokens)
	imgO := float64(usage.CompletionTokenDetails.ImageTokens)
	ao := float64(usage.CompletionTokenDetails.AudioTokens)

	// len = total input context length for tier condition evaluation.
	// Non-Claude: prompt_tokens already includes everything.
	// Claude: input_tokens is text-only, so add cache read + cache creation.
	inputLen := p
	if isClaudeUsageSemantic {
		inputLen = p + cr + cc5m + cc1h
	}

	if !isClaudeUsageSemantic {
		if usedVars["cr"] {
			p -= cr
		}
		if usedVars["cc"] {
			p -= cc5m
		}
		if usedVars["cc1h"] {
			p -= cc1h
		}
		if usedVars["img"] {
			p -= img
		}
		if usedVars["ai"] {
			p -= ai
		}
		if usedVars["img_o"] {
			c -= imgO
		}
		if usedVars["ao"] {
			c -= ao
		}
	}

	// OpenAI cache-write usage reports unadjusted prefix counts, so cr + cc can
	// exceed the prompt and drive the remainder negative. Clamp at zero.
	if p < 0 {
		p = 0
	}
	if c < 0 {
		c = 0
	}

	providerCost, _ := usage.Cost.(float64)
	return TokenParams{
		P:    p,
		C:    c,
		Len:  inputLen,
		CR:   cr,
		CC:   cc5m,
		CC1h: cc1h,
		Img:  img,
		ImgO: imgO,
		AI:   ai,
		AO:   ao,

		ProviderCost: providerCost,
	}
}

func BuildRealtimeTieredTokenParams(usage *Usage, usedVars map[string]bool) TokenParams {
	p := float64(usage.InputTokens)
	c := float64(usage.OutputTokens)
	cr := float64(usage.InputTokenDetails.CachedTokens)
	cc := float64(usage.InputTokenDetails.CacheCreationTokensTotal())
	ai := float64(usage.InputTokenDetails.AudioTokens)
	img := float64(usage.InputTokenDetails.ImageTokens)
	imgO := float64(usage.OutputTokenDetails.ImageTokens)
	ao := float64(usage.OutputTokenDetails.AudioTokens)

	if usage.CacheDiscountUnavailable || cr <= 0 {
		cr = 0
	} else if usedVars["cr"] {
		if details := usage.InputTokenDetails.CachedTokensDetails; details != nil {
			if usedVars["ai"] && details.AudioTokens != nil {
				ai -= float64(*details.AudioTokens)
			}
			if usedVars["img"] && details.ImageTokens != nil {
				img -= float64(*details.ImageTokens)
			}
		}
	}

	if usedVars["cr"] {
		p -= cr
	}
	if usedVars["cc"] {
		p -= cc
	}
	if usedVars["ai"] {
		p -= ai
	}
	if usedVars["img"] {
		p -= img
	}
	if usedVars["img_o"] {
		c -= imgO
	}
	if usedVars["ao"] {
		c -= ao
	}

	return TokenParams{
		P:    realtimeRemainder(usage.InputTokens, p),
		C:    realtimeRemainder(usage.OutputTokens, c),
		Len:  float64(usage.InputTokens),
		CR:   cr,
		CC:   cc,
		Img:  img,
		ImgO: imgO,
		AI:   ai,
		AO:   ao,
	}
}

func RealtimeCacheDiscountIssue(usage *Usage, usedVars map[string]bool) string {
	if usage == nil || !usedVars["cr"] {
		return ""
	}
	details := usage.InputTokenDetails.CachedTokensDetails
	if usage.InputTokenDetails.CachedTokens < 0 || usage.InputTokenDetails.CachedTokens > usage.InputTokens {
		return "inconsistent_overlap_details"
	}
	audioRequired := usedVars["ai"] && usage.InputTokenDetails.AudioTokens > 0
	imageRequired := usedVars["img"] && usage.InputTokenDetails.ImageTokens > 0
	if usage.InputTokenDetails.CachedTokens == 0 {
		if details != nil && ((details.TextTokens != nil && *details.TextTokens != 0) || (details.AudioTokens != nil && *details.AudioTokens != 0) || (details.ImageTokens != nil && *details.ImageTokens != 0)) && (audioRequired || imageRequired) {
			return "inconsistent_overlap_details"
		}
		return ""
	}
	if !usedVars["ai"] && !usedVars["img"] {
		return ""
	}
	if details != nil && ((details.TextTokens != nil && *details.TextTokens < 0) || (details.AudioTokens != nil && *details.AudioTokens < 0) || (details.ImageTokens != nil && *details.ImageTokens < 0)) {
		return "inconsistent_overlap_details"
	}
	if details == nil {
		if audioRequired || imageRequired {
			return "missing_overlap_details"
		}
		return ""
	}
	if (audioRequired && details.AudioTokens == nil) || (imageRequired && details.ImageTokens == nil) {
		return "missing_overlap_details"
	}

	textTokens, ok := realtimeRemaining(usage.InputTokens, usage.InputTokenDetails.AudioTokens, usage.InputTokenDetails.ImageTokens)
	if !ok || invalidCachedTokens(details.TextTokens, textTokens, usage.InputTokenDetails.CachedTokens) ||
		invalidCachedTokens(details.AudioTokens, usage.InputTokenDetails.AudioTokens, usage.InputTokenDetails.CachedTokens) ||
		invalidCachedTokens(details.ImageTokens, usage.InputTokenDetails.ImageTokens, usage.InputTokenDetails.CachedTokens) {
		return "inconsistent_overlap_details"
	}

	knownCached := 0
	unknownCapacity := 0
	for _, part := range []struct {
		cached   *int
		capacity int
	}{{details.TextTokens, textTokens}, {details.AudioTokens, usage.InputTokenDetails.AudioTokens}, {details.ImageTokens, usage.InputTokenDetails.ImageTokens}} {
		if part.cached != nil {
			if !realtimeAdd(&knownCached, *part.cached) {
				return "inconsistent_overlap_details"
			}
		} else if !realtimeAdd(&unknownCapacity, part.capacity) {
			return "inconsistent_overlap_details"
		}
	}
	remainingCached, ok := realtimeRemaining(usage.InputTokenDetails.CachedTokens, knownCached)
	if !ok || remainingCached > unknownCapacity {
		return "inconsistent_overlap_details"
	}
	requiredCached := 0
	requiredCapacity := 0
	if audioRequired {
		if !realtimeAdd(&requiredCached, *details.AudioTokens) || !realtimeAdd(&requiredCapacity, usage.InputTokenDetails.AudioTokens) {
			return "inconsistent_overlap_details"
		}
	}
	if imageRequired {
		if !realtimeAdd(&requiredCached, *details.ImageTokens) || !realtimeAdd(&requiredCapacity, usage.InputTokenDetails.ImageTokens) {
			return "inconsistent_overlap_details"
		}
	}
	remainingCached, ok = realtimeRemaining(usage.InputTokenDetails.CachedTokens, requiredCached)
	remainingInput, inputOK := realtimeRemaining(usage.InputTokens, requiredCapacity)
	if !ok || !inputOK || remainingCached > remainingInput {
		return "inconsistent_overlap_details"
	}
	if details.TextTokens != nil && details.AudioTokens != nil && details.ImageTokens != nil && knownCached != usage.InputTokenDetails.CachedTokens {
		return "inconsistent_overlap_details"
	}
	return ""
}

func ValidateRealtimeUsage(usage *Usage) error {
	if usage == nil {
		return fmt.Errorf("invalid realtime usage")
	}
	if usage.TotalTokens < 0 || usage.InputTokens < 0 || usage.OutputTokens < 0 {
		return fmt.Errorf("negative realtime usage total")
	}
	if usage.TotalTokens == 0 && (usage.InputTokens != 0 || usage.OutputTokens != 0) {
		return fmt.Errorf("inconsistent zero realtime usage total")
	}
	if !realtimeSubset(usage.InputTokens, usage.InputTokenDetails.TextTokens, usage.InputTokenDetails.AudioTokens, usage.InputTokenDetails.ImageTokens) ||
		!realtimeSubset(usage.OutputTokens, usage.OutputTokenDetails.TextTokens, usage.OutputTokenDetails.AudioTokens, usage.OutputTokenDetails.ImageTokens) {
		return fmt.Errorf("invalid realtime usage modalities")
	}
	return nil
}

func realtimeSubset(total int, parts ...int) bool {
	remaining := total
	for _, part := range parts {
		if part < 0 || part > remaining {
			return false
		}
		remaining -= part
	}
	return true
}

func realtimeRemaining(total int, parts ...int) (int, bool) {
	if !realtimeSubset(total, parts...) {
		return 0, false
	}
	for _, part := range parts {
		total -= part
	}
	return total, true
}

func realtimeAdd(total *int, value int) bool {
	if (value > 0 && *total > math.MaxInt-value) || (value < 0 && *total < math.MinInt-value) {
		return false
	}
	*total += value
	return true
}

func realtimeRemainder(raw int, value float64) float64 {
	if raw < 0 {
		return value
	}
	return max(value, 0)
}

func invalidCachedTokens(cached *int, total, cachedTotal int) bool {
	return cached != nil && (*cached < 0 || *cached > total || *cached > cachedTotal)
}

// SettleTiered preserves HTTP estimate fallback and WSS rejection as distinct policies.
func SettleTiered(engine ExpressionEvaluator, snap *TieredSnapshot, request RequestInput, reserved int, params TokenParams, reject bool) (bool, int, *TieredResult, error) {
	if snap == nil || snap.BillingMode != "tiered_expr" {
		return false, 0, nil, nil
	}
	result, err := engine.Compute(snap, params, request)
	if err != nil {
		if reject {
			return true, 0, nil, fmt.Errorf("invalid final tiered quota: %w", err)
		}
		quota := reserved
		if quota <= 0 {
			quota = snap.EstimatedQuotaAfterGroup
		}
		return true, quota, nil, nil
	}
	if reject && result.Clamp != nil {
		return true, 0, &result, result.Clamp
	}
	if reject && (result.ActualQuotaBeforeGroup < 0 || result.ActualQuotaAfterGroup < 0) {
		return true, 0, &result, fmt.Errorf("negative final tiered quota")
	}
	return true, result.ActualQuotaAfterGroup, &result, nil
}
