package service

import (
	"fmt"
	"math"
	"net/http"

	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
)

// TieredResultWrapper wraps billingexpr.TieredResult for use at the service layer.
type TieredResultWrapper = billingexpr.TieredResult

// BuildTieredTokenParams constructs billingexpr.TokenParams from a dto.Usage,
// normalizing P and C so they mean "tokens not separately priced by the
// expression". Sub-categories (cache, image, audio) are only subtracted
// when the expression references them via their own variable.
//
// GPT-format APIs report prompt_tokens / completion_tokens as totals that
// include all sub-categories (cache, image, audio). Claude-format APIs
// report them as text-only. This function normalizes to text-only when
// sub-categories are separately priced.
func BuildTieredTokenParams(usage *dto.Usage, isClaudeUsageSemantic bool, usedVars map[string]bool) billingexpr.TokenParams {
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

	return billingexpr.TokenParams{
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
	}
}

func BuildRealtimeTieredTokenParams(usage *dto.RealtimeUsage, usedVars map[string]bool) billingexpr.TokenParams {
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

	return billingexpr.TokenParams{
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

func RealtimeCacheDiscountIssue(usage *dto.RealtimeUsage, usedVars map[string]bool) string {
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

func ValidateRealtimeUsage(usage *dto.RealtimeUsage) error {
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

func refreshTieredBillingGroup(relayInfo *relaycommon.RelayInfo) (*billingexpr.BillingSnapshot, error) {
	if relayInfo == nil {
		return nil, nil
	}
	snap := relayInfo.TieredBillingSnapshot
	if snap == nil || snap.BillingMode != "tiered_expr" {
		return nil, nil
	}

	groupRatio := relayInfo.PriceData.EffectiveGroupRatio()
	if relayInfo.BillingFXFactor != 0 {
		var err error
		groupRatio, err = ApplyBillingFX(relayInfo.PriceData.GroupRatioInfo.GroupRatio, relayInfo.BillingFXFactor)
		if err != nil {
			return nil, err
		}
	}
	if snap.GroupRatio == groupRatio {
		return snap, nil
	}

	estimatedQuotaAfterGroup := snap.EstimatedQuotaBeforeGroup * groupRatio
	estimatedQuota, err := billingexpr.QuotaRoundStrict(estimatedQuotaAfterGroup)
	if err != nil {
		return nil, err
	}
	snap.GroupRatio = groupRatio
	snap.EstimatedQuotaAfterGroup = estimatedQuota
	return snap, nil
}

// PrepareTieredBillingForSelectedGroup refreshes routing-dependent billing
// state before an upstream attempt. An existing session reserves any higher
// estimate before sending. If the initial group was free and skipped
// pre-consume, switching to a paid group creates the session at that point.
func PrepareTieredBillingForSelectedGroup(c *gin.Context, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	snap, err := refreshTieredBillingGroup(relayInfo)
	if err != nil {
		if admissionErr := BillingAdmissionError(c, relayInfo, err); admissionErr != nil {
			return admissionErr
		}
		return types.NewErrorWithStatusCode(
			err,
			types.ErrorCodeModelPriceError,
			http.StatusBadRequest,
			types.ErrOptionWithSkipRetry(),
		)
	}
	if snap == nil {
		return nil
	}
	if relayInfo.QuotaClamp != nil {
		return BillingAdmissionError(c, relayInfo, relayInfo.QuotaClamp)
	}
	if snap.GroupRatio == 0 {
		// Paid-to-free keeps FreeModel as-is: FreeModel means "pre-consume was
		// skipped", which is not true once a session exists, and settlement
		// already yields 0 for a zero group ratio.
		return nil
	}

	// The selected group is paid; clear a FreeModel flag frozen when the
	// initial group was free so downstream state stays consistent.
	relayInfo.PriceData.FreeModel = false

	if relayInfo.Billing == nil {
		return PreConsumeBilling(c, snap.EstimatedQuotaAfterGroup, relayInfo)
	}
	if err := relayInfo.Billing.Reserve(snap.EstimatedQuotaAfterGroup); err != nil {
		return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
	}
	relayInfo.FinalPreConsumedQuota = relayInfo.Billing.GetPreConsumedQuota()
	return nil
}

// TryTieredSettle checks if the request uses tiered_expr billing and, if so,
// computes the actual quota using the captured BillingSnapshot. Returns:
//   - ok=true, quota, result  when tiered billing applies
//   - ok=false, 0, nil        when it doesn't (caller should fall through to existing logic)
func TryTieredSettle(relayInfo *relaycommon.RelayInfo, params billingexpr.TokenParams) (ok bool, quota int, result *billingexpr.TieredResult) {
	ok, quota, result, _ = tryTieredSettle(relayInfo, params, false)
	return ok, quota, result
}

// TryTieredWssSettle keeps WSS from treating an invalid final tiered result as
// a valid replacement quota. Ordinary HTTP callers retain TryTieredSettle's
// historical estimate fallback on expression errors.
func TryTieredWssSettle(relayInfo *relaycommon.RelayInfo, params billingexpr.TokenParams) (ok bool, quota int, result *billingexpr.TieredResult, err error) {
	return tryTieredSettle(relayInfo, params, true)
}

func tryTieredSettle(relayInfo *relaycommon.RelayInfo, params billingexpr.TokenParams, rejectInvalidFinal bool) (ok bool, quota int, result *billingexpr.TieredResult, err error) {
	snap := relayInfo.TieredBillingSnapshot
	if snap == nil || snap.BillingMode != "tiered_expr" {
		return false, 0, nil, nil
	}

	requestInput := billingexpr.RequestInput{}
	if relayInfo.BillingRequestInput != nil {
		requestInput = *relayInfo.BillingRequestInput
	}

	tr, err := billingexpr.ComputeTieredQuotaWithRequest(snap, params, requestInput)
	if err != nil {
		if rejectInvalidFinal {
			return true, 0, nil, fmt.Errorf("invalid final tiered quota: %w", err)
		}
		quota = relayInfo.FinalPreConsumedQuota
		if quota <= 0 {
			quota = snap.EstimatedQuotaAfterGroup
		}
		return true, quota, nil, nil
	}

	// Surface any single-request saturation from settlement onto RelayInfo so the
	// consume log records it under admin_info, regardless of which caller
	// (text, audio, WSS) consumes the returned quota. First non-nil wins.
	noteQuotaClamp(relayInfo, tr.Clamp)
	if rejectInvalidFinal && tr.Clamp != nil {
		return true, 0, nil, tr.Clamp
	}
	if rejectInvalidFinal && (tr.ActualQuotaBeforeGroup < 0 || tr.ActualQuotaAfterGroup < 0) {
		return true, 0, nil, fmt.Errorf("negative final tiered quota")
	}

	return true, tr.ActualQuotaAfterGroup, &tr, nil
}

// A failed evaluation retains the reservation and its estimated billing unit.
// Successful evaluations always use the actual branch, including zero prices.
func isFixedPriceSettlement(info *relaycommon.RelayInfo, result *billingexpr.TieredResult) bool {
	if result != nil {
		return result.BillingUnit == billingexpr.BillingUnitRequest
	}
	snap := info.TieredBillingSnapshot
	return snap != nil && snap.BillingMode == "tiered_expr" && snap.EstimatedBillingUnit == billingexpr.BillingUnitRequest
}
