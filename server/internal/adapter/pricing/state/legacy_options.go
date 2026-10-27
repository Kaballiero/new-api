package pricingstate

import "github.com/QuantumNous/new-api/setting/ratio_setting"

func LegacyPricingOptionValue(key string) string {
	switch key {
	case "ModelRatio":
		return ratio_setting.ModelRatio2JSONString()
	case "ModelPrice":
		return ratio_setting.ModelPrice2JSONString()
	case "CompletionRatio":
		return ratio_setting.CompletionRatio2JSONString()
	case "CacheRatio":
		return ratio_setting.CacheRatio2JSONString()
	case "CreateCacheRatio":
		return ratio_setting.CreateCacheRatio2JSONString()
	case "ImageRatio":
		return ratio_setting.ImageRatio2JSONString()
	case "AudioRatio":
		return ratio_setting.AudioRatio2JSONString()
	case "AudioCompletionRatio":
		return ratio_setting.AudioCompletionRatio2JSONString()
	}
	return ""
}
