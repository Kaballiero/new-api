package controller

import (
	"io"

	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/QuantumNous/new-api/internal/app"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/gin-gonic/gin"
)

const (
	defaultTimeoutSeconds       = 10
	defaultEndpoint             = "/api/pricing"
	maxConcurrentFetches        = 8
	maxRatioConfigBytes         = 10 << 20 // 10MB
	floatEpsilon                = 1e-9
	officialRatioPresetID       = -100
	officialRatioPresetName     = "官方倍率预设"
	officialRatioPresetBaseURL  = "https://basellm.github.io"
	modelsDevPresetID           = -101
	modelsDevPresetName         = "models.dev 价格预设"
	modelsDevPresetBaseURL      = "https://models.dev"
	modelsDevHost               = "models.dev"
	modelsDevPath               = "/api.json"
	modelsDevInputCostRatioBase = 1000.0
)

func nearlyEqual(a, b float64) bool { return pricingdomain.NearlyEqual(a, b) }
func valuesEqual(a, b any) bool     { return pricingdomain.ValuesEqual(a, b) }

var pricingSyncFields = []string{
	"model_ratio",
	"completion_ratio",
	"cache_ratio",
	"create_cache_ratio",
	"image_ratio",
	"audio_ratio",
	"audio_completion_ratio",
	"model_price",
	billing_setting.BillingModeField,
	billing_setting.BillingExprField,
}
var numericPricingSyncFields = map[string]bool{
	"model_ratio":            true,
	"completion_ratio":       true,
	"cache_ratio":            true,
	"create_cache_ratio":     true,
	"image_ratio":            true,
	"audio_ratio":            true,
	"audio_completion_ratio": true,
	"model_price":            true,
}

type upstreamResult = pricingdomain.UpstreamResult

func valueMap(value any) map[string]any   { return pricingdomain.ValueMap(value) }
func asFloat64(value any) (float64, bool) { return pricingdomain.AsFloat64(value) }
func normalizeSyncValue(field string, value any) any {
	return pricingdomain.NormalizeSyncValue(field, value)
}
func getLocalPricingSyncData() map[string]any {
	data := billing_setting.GetPricingSyncData(map[string]any(ratio_setting.GetExposedData()))
	data["image_ratio"] = ratio_setting.GetImageRatioCopy()
	data["audio_ratio"] = ratio_setting.GetAudioRatioCopy()
	data["audio_completion_ratio"] = ratio_setting.GetAudioCompletionRatioCopy()
	return data
}
func effectivePricingSyncData(data map[string]any) map[string]any {
	return pricingdomain.EffectivePricingSyncData(data)
}
func modelPricingSyncValues(data map[string]any, name string) map[string]any {
	return pricingdomain.ModelPricingSyncValues(data, name)
}
func FetchUpstreamRatios(c *gin.Context) { app.NewFetchUpstreamRatiosHTTP().Handle(c) }
func buildDifferences(localData map[string]any, channels []struct {
	name string
	data map[string]any
}) map[string]map[string]dto.DifferenceItem {
	var values []pricingdomain.SyncChannel
	for _, ch := range channels {
		values = append(values, pricingdomain.SyncChannel{Name: ch.name, Data: ch.data})
	}
	return pricingdomain.BuildDifferences(localData, values)
}
func roundRatioValue(value float64) float64     { return pricingdomain.RoundRatioValue(value) }
func isModelsDevAPIEndpoint(rawURL string) bool { return pricingdomain.IsModelsDevAPIEndpoint(rawURL) }
func convertOpenRouterToRatioData(reader io.Reader) (map[string]any, error) {
	return pricingdomain.ConvertOpenRouterToRatioData(reader)
}

type modelsDevProvider = pricingdomain.ModelsDevProvider
type modelsDevModel = pricingdomain.ModelsDevModel
type modelsDevCost = pricingdomain.ModelsDevCost
type modelsDevCandidate = pricingdomain.ModelsDevCandidate

func cloneFloatPtr(v *float64) *float64     { return pricingdomain.CloneFloatPtr(v) }
func isValidNonNegativeCost(v float64) bool { return pricingdomain.IsValidNonNegativeCost(v) }
func buildModelsDevCandidate(provider string, cost modelsDevCost) (modelsDevCandidate, bool) {
	return pricingdomain.BuildModelsDevCandidate(provider, cost)
}
func shouldReplaceModelsDevCandidate(current, next modelsDevCandidate) bool {
	return pricingdomain.ShouldReplaceModelsDevCandidate(current, next)
}
func convertModelsDevToRatioData(reader io.Reader) (map[string]any, error) {
	return pricingdomain.ConvertModelsDevToRatioData(reader)
}
func GetSyncableChannels(c *gin.Context) { app.NewGetSyncableChannelsHTTP().Handle(c) }
