package domain

import "github.com/shopspring/decimal"

type AudioPrice struct {
	InputDetails, OutputDetails                                     TokenDetails
	ModelName                                                       string
	UsePrice                                                        bool
	ModelPrice, ModelRatio, GroupRatio, FXFactor                    float64
	CompletionRatio, AudioRatio, AudioCompletionRatio, QuotaPerUnit float64
}

func AudioQuota(info AudioPrice, math QuotaMath) (int, *Clamp) {
	if info.UsePrice {
		modelPrice := decimal.NewFromFloat(info.ModelPrice)
		quotaPerUnit := decimal.NewFromFloat(info.QuotaPerUnit)
		groupRatio := decimal.NewFromFloat(info.GroupRatio)
		if info.FXFactor != 0 && info.FXFactor != 1 {
			groupRatio = groupRatio.Mul(decimal.NewFromFloat(info.FXFactor))
		}

		quota := modelPrice.Mul(quotaPerUnit).Mul(groupRatio)
		return math.Decimal(quota)
	}

	completionRatio := decimal.NewFromFloat(info.CompletionRatio)
	audioRatio := decimal.NewFromFloat(info.AudioRatio)
	audioCompletionRatio := decimal.NewFromFloat(info.AudioCompletionRatio)

	groupRatio := decimal.NewFromFloat(info.GroupRatio)
	if info.FXFactor != 0 && info.FXFactor != 1 {
		groupRatio = groupRatio.Mul(decimal.NewFromFloat(info.FXFactor))
	}
	modelRatio := decimal.NewFromFloat(info.ModelRatio)
	ratio := groupRatio.Mul(modelRatio)

	inputTextTokens := decimal.NewFromInt(int64(info.InputDetails.TextTokens))
	outputTextTokens := decimal.NewFromInt(int64(info.OutputDetails.TextTokens))
	inputAudioTokens := decimal.NewFromInt(int64(info.InputDetails.AudioTokens))
	outputAudioTokens := decimal.NewFromInt(int64(info.OutputDetails.AudioTokens))

	quota := decimal.Zero
	quota = quota.Add(inputTextTokens)
	quota = quota.Add(outputTextTokens.Mul(completionRatio))
	quota = quota.Add(inputAudioTokens.Mul(audioRatio))
	quota = quota.Add(outputAudioTokens.Mul(audioRatio).Mul(audioCompletionRatio))

	quota = quota.Mul(ratio)

	// If ratio is not zero and quota is less than or equal to zero, set quota to 1
	if !ratio.IsZero() && quota.LessThanOrEqual(decimal.Zero) {
		quota = decimal.NewFromInt(1)
	}

	return math.Decimal(quota)
}
