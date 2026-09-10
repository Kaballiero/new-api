package controller

import (
	"maps"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/gin-gonic/gin"
)

func filterPricingByUsableGroups(pricing []model.Pricing, usableGroup map[string]string) []model.Pricing {
	if len(pricing) == 0 {
		return pricing
	}
	if len(usableGroup) == 0 {
		return []model.Pricing{}
	}

	filtered := make([]model.Pricing, 0, len(pricing))
	for _, item := range pricing {
		if common.StringsContains(item.EnableGroup, "all") {
			filtered = append(filtered, item)
			continue
		}
		for _, group := range item.EnableGroup {
			if _, ok := usableGroup[group]; ok {
				filtered = append(filtered, item)
				break
			}
		}
	}
	return filtered
}

func GetPricing(c *gin.Context) {
	pricing := model.GetPricing()
	userId, exists := c.Get("id")
	usableGroup := map[string]string{}
	groupRatio := map[string]float64{}
	maps.Copy(groupRatio, ratio_setting.GetGroupRatioCopy())
	var group string
	if exists {
		user, err := model.GetUserCache(userId.(int))
		if err == nil {
			group = user.Group
			for g := range groupRatio {
				ratio, ok := ratio_setting.GetGroupGroupRatio(group, g)
				if ok {
					groupRatio[g] = ratio
				}
			}
		}
	}

	usableGroup = service.GetUserUsableGroups(group)
	pricing = filterPricingByUsableGroups(pricing, usableGroup)
	// check groupRatio contains usableGroup
	for group := range ratio_setting.GetGroupRatioCopy() {
		if _, ok := usableGroup[group]; !ok {
			delete(groupRatio, group)
		}
	}

	c.JSON(200, gin.H{
		"success":            true,
		"data":               pricing,
		"vendors":            model.GetVendors(),
		"group_ratio":        groupRatio,
		"usable_group":       usableGroup,
		"supported_endpoint": model.GetSupportedEndpointMap(),
		"auto_groups":        service.GetUserAutoGroup(group),
		"pricing_version":    "a42d372ccf0b5dd13ecf71203521f9d2",
	})
}

type effectivePricingResponse struct {
	SchemaVersion string                  `json:"schema_version"`
	UserGroup     string                  `json:"user_group"`
	Currency      string                  `json:"currency"`
	PriceKind     string                  `json:"price_kind"`
	PriceScope    string                  `json:"price_scope"`
	FX            service.BillingFXBasis  `json:"fx"`
	Accounting    effectiveAccounting     `json:"accounting"`
	AutoGroups    []string                `json:"auto_groups"`
	Data          []effectivePricingModel `json:"data"`
}

type effectiveAccounting struct {
	QuotaPerUnit float64 `json:"quota_per_unit"`
	RubPerUnit   float64 `json:"rub_per_unit"`
	QuotaPerRub  float64 `json:"quota_per_rub"`
}

type effectivePricingModel struct {
	ModelName              string                  `json:"model_name"`
	Description            string                  `json:"description,omitempty"`
	Icon                   string                  `json:"icon,omitempty"`
	Tags                   string                  `json:"tags,omitempty"`
	VendorID               int                     `json:"vendor_id,omitempty"`
	OwnerBy                string                  `json:"owner_by"`
	SupportedEndpointTypes []constant.EndpointType `json:"supported_endpoint_types"`
	GroupPrices            []effectiveGroupPricing `json:"group_prices"`
}

type effectiveGroupPricing struct {
	UsingGroup            string                               `json:"using_group"`
	PureGroupRatio        float64                              `json:"pure_group_ratio"`
	EffectiveBillingRatio float64                              `json:"effective_billing_ratio"`
	BillingMode           string                               `json:"billing_mode"`
	BillingSurface        string                               `json:"billing_surface"`
	Status                string                               `json:"status"`
	UnitPrices            []effectiveUnitPrice                 `json:"unit_prices,omitempty"`
	Tiers                 []effectivePricingTier               `json:"tiers"`
	IsFree                bool                                 `json:"is_free"`
	Formula               *effectivePricingFormula             `json:"formula,omitempty"`
	Limitations           []string                             `json:"limitations,omitempty"`
	UsageSchema           map[string]jsplugin.UsageFieldSchema `json:"usage_schema,omitempty"`
}

type effectiveUnitPrice struct {
	Component string  `json:"component"`
	Unit      string  `json:"unit"`
	AmountRub float64 `json:"amount_rub"`
}

type effectivePricingFormula struct {
	Expression          string  `json:"expression"`
	Kind                string  `json:"kind"`
	OutputToQuotaFactor float64 `json:"output_to_quota_factor"`
	OutputToRubFactor   float64 `json:"output_to_rub_factor"`
}

// GetEffectivePricing returns a read-only per-user tariff projection. It does
// not reuse /api/pricing's response contract and does not call debit paths.
func GetEffectivePricing(c *gin.Context) {
	user, err := model.GetUserCache(c.GetInt("id"))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "failed to resolve user pricing group"})
		return
	}
	basis, err := service.CurrentBillingFXBasis()
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": err.Error()})
		return
	}

	usableGroups := service.GetUserUsableGroups(user.Group)
	pricing := filterPricingByUsableGroups(model.GetPricing(), usableGroups)
	response := effectivePricingResponse{
		SchemaVersion: "v1",
		UserGroup:     user.Group,
		Currency:      "RUB",
		PriceKind:     "current_tariff",
		PriceScope:    "user_group",
		FX:            basis,
		Accounting: effectiveAccounting{
			QuotaPerUnit: common.QuotaPerUnit,
			RubPerUnit:   100,
			QuotaPerRub:  float64(common.QuotaPerUnit) / 100,
		},
		AutoGroups: service.GetUserAutoGroup(user.Group),
		Data:       make([]effectivePricingModel, 0, len(pricing)),
	}
	for _, item := range pricing {
		response.Data = append(response.Data, buildEffectivePricingModel(user.Group, usableGroups, basis.Factor, item))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": response})
}

func buildEffectivePricingModel(userGroup string, usableGroups map[string]string, fxFactor float64, item model.Pricing) effectivePricingModel {
	result := effectivePricingModel{
		ModelName:              item.ModelName,
		Description:            item.Description,
		Icon:                   item.Icon,
		Tags:                   item.Tags,
		VendorID:               item.VendorID,
		OwnerBy:                item.OwnerBy,
		SupportedEndpointTypes: item.SupportedEndpointTypes,
	}
	groups := effectivePricingGroups(item.EnableGroup, usableGroups)
	result.GroupPrices = make([]effectiveGroupPricing, 0, len(groups))
	for _, group := range groups {
		pure := service.GetUserGroupRatio(userGroup, group)
		effective, err := service.ApplyBillingFX(pure, fxFactor)
		groupPrice := buildEffectiveGroupPricing(item, group, pure, effective)
		if err != nil {
			groupPrice.Status = "unavailable"
			groupPrice.Limitations = append(groupPrice.Limitations, "invalid_group_ratio")
		}
		result.GroupPrices = append(result.GroupPrices, groupPrice)
	}
	return result
}

func effectivePricingGroups(enableGroups []string, usableGroups map[string]string) []string {
	if common.StringsContains(enableGroups, "all") {
		groups := make([]string, 0, len(usableGroups))
		for group := range usableGroups {
			groups = append(groups, group)
		}
		return groups
	}
	groups := make([]string, 0, len(enableGroups))
	for _, group := range enableGroups {
		if _, ok := usableGroups[group]; ok {
			groups = append(groups, group)
		}
	}
	return groups
}

func buildEffectiveGroupPricing(item model.Pricing, group string, pureRatio, effectiveRatio float64) effectiveGroupPricing {
	groupPrice := effectiveGroupPricing{
		UsingGroup:            group,
		PureGroupRatio:        pureRatio,
		EffectiveBillingRatio: effectiveRatio,
		BillingMode:           billing_setting.BillingModeRatio,
		BillingSurface:        "token",
	}
	if item.BillingMode == billing_setting.BillingModeTieredExpr && strings.TrimSpace(item.BillingExpr) != "" {
		groupPrice.BillingMode = item.BillingMode
		kind := "token_expression"
		outputToQuota := float64(common.QuotaPerUnit) * effectiveRatio / 1_000_000
		outputToRub := outputToQuota / (float64(common.QuotaPerUnit) / 100)
		if len(item.BillingUsageSchema) > 0 {
			kind = "task_usage_expression"
			groupPrice.BillingSurface = "task"
			outputToQuota = float64(common.QuotaPerUnit) * effectiveRatio
			outputToRub = outputToQuota / (float64(common.QuotaPerUnit) / 100)
			groupPrice.UsageSchema = make(map[string]jsplugin.UsageFieldSchema, len(item.BillingUsageSchema))
			for key, schema := range item.BillingUsageSchema {
				groupPrice.UsageSchema[key] = schema
			}
		}
		groupPrice.Status = "formula"
		groupPrice.Formula = &effectivePricingFormula{Expression: item.BillingExpr, Kind: kind, OutputToQuotaFactor: outputToQuota, OutputToRubFactor: outputToRub}
		groupPrice.setTiers([]effectivePricingTier{})
		if kind == "token_expression" {
			groupPrice.setTiers(tokenExpressionTiers(item.BillingExpr, outputToRub))
		}
		return groupPrice
	}
	if item.QuotaType == 1 {
		groupPrice.BillingSurface = "per_call"
		groupPrice.Status = "unit_prices"
		groupPrice.UnitPrices = []effectiveUnitPrice{{Component: "base", Unit: "call", AmountRub: item.ModelPrice * 100 * effectiveRatio}}
		groupPrice.Limitations = []string{"provider-specific multipliers may apply for some request parameters"}
		groupPrice.setTiers([]effectivePricingTier{{UnitPrices: groupPrice.UnitPrices}})
		return groupPrice
	}
	groupPrice.Status = "unit_prices"
	baseInput := item.ModelRatio * effectiveRatio * 200
	groupPrice.UnitPrices = []effectiveUnitPrice{
		{Component: "input", Unit: "million_tokens", AmountRub: baseInput},
		{Component: "output", Unit: "million_tokens", AmountRub: baseInput * item.CompletionRatio},
	}
	if item.CacheRatio != nil {
		groupPrice.UnitPrices = append(groupPrice.UnitPrices, effectiveUnitPrice{Component: "cache_read", Unit: "million_tokens", AmountRub: baseInput * *item.CacheRatio})
	}
	if item.CreateCacheRatio != nil {
		groupPrice.UnitPrices = append(groupPrice.UnitPrices, effectiveUnitPrice{Component: "cache_write", Unit: "million_tokens", AmountRub: baseInput * *item.CreateCacheRatio})
	}
	if item.ImageRatio != nil {
		groupPrice.UnitPrices = append(groupPrice.UnitPrices, effectiveUnitPrice{Component: "image_input", Unit: "million_tokens", AmountRub: baseInput * *item.ImageRatio})
	}
	if item.AudioRatio != nil {
		groupPrice.UnitPrices = append(groupPrice.UnitPrices, effectiveUnitPrice{Component: "audio_input", Unit: "million_tokens", AmountRub: baseInput * *item.AudioRatio})
	}
	if item.AudioCompletionRatio != nil {
		groupPrice.UnitPrices = append(groupPrice.UnitPrices, effectiveUnitPrice{Component: "audio_output", Unit: "million_tokens", AmountRub: baseInput * *item.AudioCompletionRatio})
	}
	groupPrice.setTiers([]effectivePricingTier{{UnitPrices: groupPrice.UnitPrices}})
	return groupPrice
}

func ResetModelRatio(c *gin.Context) {
	defaultStr := ratio_setting.DefaultModelRatio2JSONString()
	err := model.UpdateOption("ModelRatio", defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	err = ratio_setting.UpdateModelRatioByJSONString(defaultStr)
	if err != nil {
		c.JSON(200, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	c.JSON(200, gin.H{
		"success": true,
		"message": "重置模型倍率成功",
	})
}
