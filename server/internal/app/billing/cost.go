package billing

import (
	"github.com/QuantumNous/new-api/common"
	adapter "github.com/QuantumNous/new-api/internal/adapter/billing"
	settings "github.com/QuantumNous/new-api/internal/adapter/settings/billing"
	"github.com/QuantumNous/new-api/internal/billing/calculate_audio_cost"
	"github.com/QuantumNous/new-api/internal/billing/calculate_realtime_cost"
	"github.com/QuantumNous/new-api/internal/billing/calculate_text_cost"
	"github.com/QuantumNous/new-api/internal/billing/capture_fx_basis"
	"github.com/QuantumNous/new-api/internal/billing/domain"
	"github.com/QuantumNous/new-api/internal/billing/prepare_tiered_cost"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type Costs struct {
	AudioPrices domain.AudioPrices
	Text        calculate_text_cost.Usecase
	Audio       calculate_audio_cost.Usecase
	Realtime    calculate_realtime_cost.Usecase
	FX          capture_fx_basis.Usecase
	Prepare     prepare_tiered_cost.Usecase
}

func CostFXSettings() domain.FXSettings {
	return domain.FXSettings{QuotaUnit: func() float64 { return common.QuotaPerUnit }, AccountingRate: func() float64 { return operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate }, Read: settings.ReadFX}
}
func NewCosts() Costs {
	math, engine := adapter.CostMath{}, adapter.Expression{}
	audio := domain.AudioPrices{QuotaUnit: func() float64 { return common.QuotaPerUnit }, Read: func(model string) (float64, float64, float64) {
		return ratio_setting.GetCompletionRatio(model), ratio_setting.GetAudioRatio(model), ratio_setting.GetAudioCompletionRatio(model)
	}}
	text := calculate_text_cost.Prices{Tool: operation_setting.GetToolPriceForModel, InputAudio: operation_setting.GetGeminiInputAudioPricePerMillionTokens, QuotaUnit: func() float64 { return common.QuotaPerUnit }, DefaultRatio: func(model string) (float64, bool) {
		r, ok := ratio_setting.GetDefaultModelRatioMap()[model]
		return r, ok
	}}
	return Costs{AudioPrices: audio, Text: calculate_text_cost.NewUsecase(math, engine, text, common.SysError), Audio: calculate_audio_cost.NewUsecase(math, engine, audio), Realtime: calculate_realtime_cost.NewUsecase(math, engine, audio), FX: capture_fx_basis.NewUsecase(CostFXSettings()), Prepare: prepare_tiered_cost.NewUsecase(math)}
}
