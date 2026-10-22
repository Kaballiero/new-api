package helper

import (
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	billinggateway "github.com/QuantumNous/new-api/relay/billing"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/relayconvert/reasoning"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	hostreasoning "github.com/QuantumNous/new-api/setting/reasoning"
	hosttypes "github.com/QuantumNous/new-api/types"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
	"github.com/gin-gonic/gin"
)

func modelPriceNotConfiguredError(modelName string, userId int) error {
	if model.IsAdmin(userId) {
		return fmt.Errorf(
			"Model %s price not configured. Go to System Settings → Operation Settings to enable self-use mode, or configure the model price in System Settings → Group & Model Pricing.",
			modelName,
		)
	}
	return fmt.Errorf(
		"Model %s has not been priced by the administrator yet. Please contact the site administrator to enable this model.",
		modelName,
	)
}

// https://docs.claude.com/en/docs/build-with-claude/prompt-caching#1-hour-cache-duration
const claudeCacheCreation1hMultiplier = 6 / 3.75

// defaultTieredPreConsumeMaxTokens is the fallback completion-token estimate
// used for tiered expression pre-consume when the client omits max_tokens, so
// the pre-consumed quota still reflects a plausible output cost in paid groups.
const defaultTieredPreConsumeMaxTokens = 8192

// HandleGroupRatio checks for "auto_group" in the context and updates the group ratio and relayInfo.UsingGroup if present
func HandleGroupRatio(ctx *gin.Context, relayInfo *relaycommon.RelayInfo) hosttypes.GroupRatioInfo {
	groupRatioInfo := hosttypes.GroupRatioInfo{
		GroupRatio:        1.0, // default ratio
		GroupSpecialRatio: -1,
	}

	// check auto group
	autoGroup, exists := ctx.Get("auto_group")
	if exists {
		logger.LogDebug(ctx, "final group: %s", autoGroup)
		relayInfo.UsingGroup = autoGroup.(string)
	}

	// check user group special ratio
	userGroupRatio, ok := ratio_setting.GetGroupGroupRatio(relayInfo.UserGroup, relayInfo.UsingGroup)
	if ok {
		// user group special ratio
		groupRatioInfo.GroupSpecialRatio = userGroupRatio
		groupRatioInfo.GroupRatio = userGroupRatio
		groupRatioInfo.HasSpecialRatio = true
	} else {
		// normal group ratio
		groupRatioInfo.GroupRatio = ratio_setting.GetGroupRatio(relayInfo.UsingGroup)
	}

	return groupRatioInfo
}

func ModelPriceHelper(c *gin.Context, info *relaycommon.RelayInfo, promptTokens int, meta *types.TokenCountMeta) (hosttypes.PriceData, error) {
	if info != nil {
		if matched := resolveBillingModelName(info.GetOriginModelName()); matched != "" && matched != info.OriginModelName {
			info.BillingModelName = matched
		}
	}
	billingFXFactor, err := billinggateway.CaptureBillingFX(info)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	billingModelName := info.GetBillingModelName()
	modelPrice, usePrice := ratio_setting.GetModelPrice(billingModelName, false)

	groupRatioInfo := HandleGroupRatio(c, info)
	billingGroupRatio, err := billinggateway.ApplyBillingFX(groupRatioInfo.GroupRatio, billingFXFactor)
	if err != nil {
		return hosttypes.PriceData{}, err
	}

	if c.GetInt("channel_type") == constant.ChannelTypeOpenRouter {
		info.TieredBillingSnapshot = nil
		info.OpenRouterPricingDeferred = true
		info.PriceData = hosttypes.PriceData{GroupRatioInfo: groupRatioInfo, BillingGroupRatio: billingGroupRatio, FreeModel: true}
		return info.PriceData, nil
	}
	// Check if this model uses tiered_expr billing
	if billing_setting.GetBillingMode(billingModelName) == billing_setting.BillingModeTieredExpr {
		return modelPriceHelperTiered(c, info, billingModelName, promptTokens, meta, groupRatioInfo, billingGroupRatio)
	}

	var preConsumedQuota int
	var modelRatio float64
	var completionRatio float64
	var cacheRatio float64
	var imageRatio float64
	var cacheCreationRatio float64
	var cacheCreationRatio5m float64
	var cacheCreationRatio1h float64
	var audioRatio float64
	var audioCompletionRatio float64
	var freeModel bool
	if !usePrice {
		preConsumedTokens := common.Max(promptTokens, common.PreConsumedQuota)
		if meta.MaxTokens != 0 {
			preConsumedTokens += meta.MaxTokens
		}
		var success bool
		var matchName string
		modelRatio, success, matchName = ratio_setting.GetModelRatio(billingModelName)
		if !success {
			acceptUnsetRatio := false
			if info.UserSetting.AcceptUnsetRatioModel {
				acceptUnsetRatio = true
			}
			if !acceptUnsetRatio {
				return hosttypes.PriceData{}, modelPriceNotConfiguredError(matchName, info.UserId)
			}
		}
		completionRatio = ratio_setting.GetCompletionRatio(billingModelName)
		cacheRatio, _ = ratio_setting.GetCacheRatio(billingModelName)
		cacheCreationRatio, _ = ratio_setting.GetCreateCacheRatio(billingModelName)
		cacheCreationRatio5m = cacheCreationRatio
		// 固定1h和5min缓存写入价格的比例
		cacheCreationRatio1h = cacheCreationRatio * claudeCacheCreation1hMultiplier
		imageRatio, _ = ratio_setting.GetImageRatio(billingModelName)
		audioRatio = ratio_setting.GetAudioRatio(billingModelName)
		audioCompletionRatio = ratio_setting.GetAudioCompletionRatio(billingModelName)
		ratio := modelRatio * billingGroupRatio
		quota, err := common.QuotaFromFloatStrict(float64(preConsumedTokens) * ratio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		preConsumedQuota = quota
	} else {
		if meta.ImagePriceRatio != 0 {
			modelPrice = modelPrice * meta.ImagePriceRatio
		}
	}

	// check if free model pre-consume is disabled
	if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
		// if model price or ratio is 0, do not pre-consume quota
		if groupRatioInfo.GroupRatio == 0 {
			preConsumedQuota = 0
			freeModel = true
		} else if usePrice {
			if modelPrice == 0 {
				preConsumedQuota = 0
				freeModel = true
			}
		} else {
			if modelRatio == 0 {
				preConsumedQuota = 0
				freeModel = true
			}
		}
	}

	priceData := hosttypes.PriceData{
		FreeModel:            freeModel,
		ModelPrice:           modelPrice,
		ModelRatio:           modelRatio,
		CompletionRatio:      completionRatio,
		GroupRatioInfo:       groupRatioInfo,
		BillingGroupRatio:    billingGroupRatio,
		UsePrice:             usePrice,
		CacheRatio:           cacheRatio,
		ImageRatio:           imageRatio,
		AudioRatio:           audioRatio,
		AudioCompletionRatio: audioCompletionRatio,
		CacheCreationRatio:   cacheCreationRatio,
		CacheCreation5mRatio: cacheCreationRatio5m,
		CacheCreation1hRatio: cacheCreationRatio1h,
		QuotaToPreConsume:    preConsumedQuota,
	}
	if usePrice {
		for name, ratio := range meta.BillingRatios {
			priceData.AddOtherRatio(name, ratio)
		}
		quotaToPreConsume := priceData.ApplyOtherRatiosToFloat(modelPrice * common.QuotaPerUnit * billingGroupRatio)
		quota, err := common.QuotaFromFloatStrict(quotaToPreConsume)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		priceData.QuotaToPreConsume = quota
	}

	if common.DebugEnabled {
		logger.LogDebug(c, "model_price_helper result: %s", priceData.ToSetting())
	}
	info.PriceData = priceData
	return priceData, nil
}

// ModelPriceHelperPerCall 按次/按量计费的 PriceHelper (MJ、Task)
func ModelPriceHelperPerCall(c *gin.Context, info *relaycommon.RelayInfo) (hosttypes.PriceData, error) {
	billingFXFactor, err := billinggateway.CaptureBillingFX(info)
	if err != nil {
		return hosttypes.PriceData{}, err
	}
	groupRatioInfo := HandleGroupRatio(c, info)
	billingGroupRatio, err := billinggateway.ApplyBillingFX(groupRatioInfo.GroupRatio, billingFXFactor)
	if err != nil {
		return hosttypes.PriceData{}, err
	}

	modelPrice, success := ratio_setting.GetModelPrice(info.OriginModelName, true)
	usePrice := success
	var modelRatio float64

	if !success {
		defaultPrice, ok := ratio_setting.GetDefaultModelPriceMap()[info.OriginModelName]
		if ok {
			modelPrice = defaultPrice
			usePrice = true
		} else {
			var ratioSuccess bool
			var matchName string
			modelRatio, ratioSuccess, matchName = ratio_setting.GetModelRatio(info.OriginModelName)
			acceptUnsetRatio := false
			if info.UserSetting.AcceptUnsetRatioModel {
				acceptUnsetRatio = true
			}
			if !ratioSuccess && !acceptUnsetRatio {
				return hosttypes.PriceData{}, modelPriceNotConfiguredError(matchName, info.UserId)
			}
		}
	}

	var quota int
	freeModel := false

	if usePrice {
		quota, err = common.QuotaFromFloatStrict(modelPrice * common.QuotaPerUnit * billingGroupRatio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
			if groupRatioInfo.GroupRatio == 0 || modelPrice == 0 {
				quota = 0
				freeModel = true
			}
		}
	} else {
		// 按量计费：以模型倍率的一半作为预扣额度
		quota, err = common.QuotaFromFloatStrict(modelRatio / 2 * common.QuotaPerUnit * billingGroupRatio)
		if err != nil {
			return hosttypes.PriceData{}, err
		}
		modelPrice = -1
		if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
			if groupRatioInfo.GroupRatio == 0 || modelRatio == 0 {
				quota = 0
				freeModel = true
			}
		}
	}

	priceData := hosttypes.PriceData{
		FreeModel:         freeModel,
		ModelPrice:        modelPrice,
		ModelRatio:        modelRatio,
		UsePrice:          usePrice,
		Quota:             quota,
		GroupRatioInfo:    groupRatioInfo,
		BillingGroupRatio: billingGroupRatio,
	}
	return priceData, nil
}

func HasModelBillingConfig(modelName string) bool {
	if _, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		return true
	}
	if _, ok, _ := ratio_setting.GetModelRatio(modelName); ok {
		return true
	}
	if billing_setting.GetBillingMode(modelName) != billing_setting.BillingModeTieredExpr {
		return false
	}
	expr, ok := billing_setting.GetBillingExpr(modelName)
	return ok && strings.TrimSpace(expr) != ""
}

// HasPriceOrRatioEntry reports whether name has a configured price, ratio, or
// tiered billing-mode entry after a single wildcard normalization. Self-use
// fallback does not count as a configured ratio.
func HasPriceOrRatioEntry(name string) bool {
	formatted := ratio_setting.FormatMatchingModelName(name)
	if _, ok := ratio_setting.GetModelPrice(formatted, false); ok {
		return true
	}
	if ratio_setting.HasConfiguredModelRatio(formatted) {
		return true
	}
	return billing_setting.GetBillingMode(formatted) == billing_setting.BillingModeTieredExpr
}

func resolveBillingModelName(origin string) string {
	var candidates []string
	if !reasoning.ParseModelModifiers(origin).HasModifiers() {
		candidates = append(candidates, origin)
	}
	candidates = append(candidates, hostreasoning.CanonicalBillingModelNames(origin)...)
	base := hostreasoning.BaseModelName(origin)
	candidates = append(candidates, base)

	seen := make(map[string]struct{}, len(candidates))
	matched := ""
	for _, name := range candidates {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		if HasPriceOrRatioEntry(name) {
			matched = name
			break
		}
	}
	if matched == "" {
		matched = base
	}
	return matched
}

// ModelReservationEstimate is a base USD estimate, before group/FX or quota
// conversion. It does not install a final-billing snapshot on a request.
type ModelReservationEstimate struct {
	USD      float64
	Model    string
	ExprHash string
}

// EstimateConfiguredTaskReservation evaluates already validated task facts in
// USD/request. It never applies text-token scaling or installs final billing.
func EstimateConfiguredTaskReservation(modelName string, input billingexpr.RequestInput) (ModelReservationEstimate, error) {
	modelName = resolveBillingModelName(modelName)
	result := ModelReservationEstimate{Model: modelName}
	if billing_setting.GetBillingMode(modelName) == billing_setting.BillingModeTieredExpr {
		expression, ok := billing_setting.GetConfiguredBillingExpr(modelName)
		if !ok || strings.TrimSpace(expression) == "" {
			return result, fmt.Errorf("model %s has no configured reservation expression", modelName)
		}
		_, body := billingexpr.ParseExprVersion(expression)
		tree, err := parser.Parse(body)
		if err != nil {
			return result, err
		}
		var invalid error
		ast.Find(tree.Node, func(node ast.Node) bool {
			if identifier, ok := node.(*ast.IdentifierNode); ok {
				switch identifier.Value {
				case "p", "c", "len", "cr", "cc", "cc1h", "img", "img_o", "ai", "ao", "provider_cost", "fixed":
					invalid = fmt.Errorf("task reservation expression requires unavailable %s quantity", identifier.Value)
					return true
				}
			}
			call, ok := node.(*ast.CallNode)
			if !ok {
				return false
			}
			callee, ok := call.Callee.(*ast.IdentifierNode)
			if !ok || callee.Value != "u" {
				return false
			}
			if len(call.Arguments) != 1 {
				invalid = fmt.Errorf("task reservation requires literal usage keys")
				return true
			}
			key, literal := call.Arguments[0].(*ast.StringNode)
			if !literal {
				invalid = fmt.Errorf("task reservation requires literal usage keys")
				return true
			}
			if input.Usage[strings.TrimSpace(key.Value)] == nil {
				invalid = fmt.Errorf("task reservation quantity %q is unavailable", key.Value)
				return true
			}
			return false
		})
		if invalid != nil {
			return result, invalid
		}
		result.USD, _, err = billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{}, input)
		if err != nil {
			return result, err
		}
		result.ExprHash = billingexpr.ExprHashString(expression)
	} else if price, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		result.USD = price
	} else {
		return result, fmt.Errorf("model %s has no configured task reservation price", modelName)
	}
	if math.IsNaN(result.USD) || math.IsInf(result.USD, 0) || result.USD < 0 {
		return result, fmt.Errorf("invalid configured task reservation price for %s", modelName)
	}
	return result, nil
}

func EstimateConfiguredModelReservation(modelName string, promptTokens, completionTokens int, input billingexpr.RequestInput, unavailableVars ...string) (ModelReservationEstimate, error) {
	modelName = resolveBillingModelName(modelName)
	result := ModelReservationEstimate{Model: modelName}
	if promptTokens < 0 || completionTokens < 0 || completionTokens > MaxTokensLimit {
		return result, fmt.Errorf("invalid local reservation token estimate")
	}
	if billing_setting.GetBillingMode(modelName) == billing_setting.BillingModeTieredExpr {
		if completionTokens == 0 {
			completionTokens = defaultTieredPreConsumeMaxTokens
		}
		expression, ok := billing_setting.GetConfiguredBillingExpr(modelName)
		if !ok || strings.TrimSpace(expression) == "" {
			return result, fmt.Errorf("model %s has no configured reservation expression", modelName)
		}
		// Reservation has no measured cache or media subcategories.
		// Expressions depending on those facts must not receive invented zeroes.
		for variable := range billingexpr.UsedVars(expression) {
			if slices.Contains(unavailableVars, variable) {
				return result, fmt.Errorf("local reservation expression requires unavailable %s quantity", variable)
			}
			switch variable {
			case "cr", "cc", "cc1h", "img", "img_o", "ai", "ao", "u":
				return result, fmt.Errorf("local reservation expression requires unavailable %s quantity", variable)
			}
		}
		usd, _, err := estimateModelExpressionUSD(modelName, expression, promptTokens, completionTokens, input)
		if err != nil {
			return result, err
		}
		result.USD, result.ExprHash = usd, billingexpr.ExprHashString(expression)
	} else if price, ok := ratio_setting.GetModelPrice(modelName, false); ok {
		result.USD = price
	} else if ratio_setting.HasConfiguredModelRatio(modelName) {
		ratio, _, _ := ratio_setting.GetModelRatio(modelName)
		// Preserve the legacy reservation heuristic, not a claim about actual
		// completion/cache rates. Convert the unrounded quota convention to USD.
		result.USD = (float64(max(promptTokens, common.PreConsumedQuota)) + float64(completionTokens)) * ratio / common.QuotaPerUnit
	} else {
		return result, fmt.Errorf("model %s has no configured reservation price", modelName)
	}
	if math.IsNaN(result.USD) || math.IsInf(result.USD, 0) || result.USD < 0 {
		return result, fmt.Errorf("invalid configured reservation price for %s", modelName)
	}
	return result, nil
}

// estimateModelExpressionUSD is shared with ordinary tiered pre-consumption;
// callers decide whether to install a customer-billing snapshot.
func estimateModelExpressionUSD(modelName, expression string, promptTokens, completionTokens int, input billingexpr.RequestInput) (float64, billingexpr.TraceResult, error) {
	providerCost := float64(0)
	if billingexpr.UsedVars(expression)["provider_cost"] {
		var valid bool
		providerCost, valid = input.Usage["provider_cost"].(float64)
		if !valid || providerCost < 0 || math.IsNaN(providerCost) || math.IsInf(providerCost, 0) {
			return 0, billingexpr.TraceResult{}, fmt.Errorf("model %s requires a validated provider cost estimate", modelName)
		}
	}
	raw, trace, err := billingexpr.RunExprWithRequest(expression, billingexpr.TokenParams{
		P: float64(promptTokens), C: float64(completionTokens), Len: float64(promptTokens), ProviderCost: providerCost,
	}, input)
	if err != nil {
		return 0, trace, fmt.Errorf("model %s tiered expr run failed: %w", modelName, err)
	}
	return raw / 1_000_000, trace, nil
}

func modelPriceHelperTiered(c *gin.Context, info *relaycommon.RelayInfo, billingModelName string, promptTokens int, meta *types.TokenCountMeta, groupRatioInfo hosttypes.GroupRatioInfo, billingGroupRatio float64) (hosttypes.PriceData, error) {
	exprStr, ok := billing_setting.GetBillingExpr(billingModelName)
	if !ok {
		return hosttypes.PriceData{}, fmt.Errorf("model %s is configured as tiered_expr but has no billing expression", billingModelName)
	}
	if info.RelayFormat == types.RelayFormatOpenAIRealtime && billingexpr.UsesFixedPricing(exprStr) {
		return hosttypes.PriceData{}, fmt.Errorf("fixed pricing is not supported for Realtime requests")
	}

	estimatedCompletionTokens := meta.MaxTokens
	if estimatedCompletionTokens == 0 && groupRatioInfo.GroupRatio != 0 {
		estimatedCompletionTokens = defaultTieredPreConsumeMaxTokens
	}

	requestInput, err := ResolveIncomingBillingExprRequestInput(c, info)
	if err != nil {
		return hosttypes.PriceData{}, err
	}

	usd, trace, err := estimateModelExpressionUSD(billingModelName, exprStr, promptTokens, estimatedCompletionTokens, requestInput)
	if err != nil {
		return hosttypes.PriceData{}, err
	}

	// Expression coefficients are $/1M tokens prices; convert to quota the same way per-call billing does.
	quotaBeforeGroup := usd * common.QuotaPerUnit
	preConsumedQuota, err := billingexpr.QuotaRoundStrict(quotaBeforeGroup * billingGroupRatio)
	if err != nil {
		return hosttypes.PriceData{}, err
	}

	freeModel := false
	if !operation_setting.GetQuotaSetting().EnableFreeModelPreConsume {
		if groupRatioInfo.GroupRatio == 0 {
			preConsumedQuota = 0
			freeModel = true
		}
	}

	exprHash := billingexpr.ExprHashString(exprStr)
	snapshot := &billingexpr.BillingSnapshot{
		BillingMode:               billing_setting.BillingModeTieredExpr,
		ModelName:                 billingModelName,
		ExprString:                exprStr,
		ExprHash:                  exprHash,
		GroupRatio:                billingGroupRatio,
		EstimatedPromptTokens:     promptTokens,
		EstimatedCompletionTokens: estimatedCompletionTokens,
		EstimatedQuotaBeforeGroup: quotaBeforeGroup,
		EstimatedQuotaAfterGroup:  preConsumedQuota,
		EstimatedTier:             trace.MatchedTier,
		EstimatedBillingUnit:      trace.BillingUnit,
		EstimatedFixedPrice:       trace.FixedPrice,
		QuotaPerUnit:              common.QuotaPerUnit,
		ExprVersion:               billingexpr.ExprVersion(exprStr),
	}
	info.TieredBillingSnapshot = snapshot
	info.BillingRequestInput = &requestInput

	priceData := hosttypes.PriceData{
		FreeModel:         freeModel,
		GroupRatioInfo:    groupRatioInfo,
		BillingGroupRatio: billingGroupRatio,
		QuotaToPreConsume: preConsumedQuota,
	}

	logger.LogDebug(c, "model_price_helper_tiered result: model=%s preConsume=%d quotaBeforeGroup=%.2f groupRatio=%.2f tier=%s", billingModelName, preConsumedQuota, quotaBeforeGroup, groupRatioInfo.GroupRatio, trace.MatchedTier)

	info.PriceData = priceData
	return priceData, nil
}
