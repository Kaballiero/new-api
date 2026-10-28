package subscriptions

import (
	shared "github.com/QuantumNous/new-api/internal/adapter/settings/payments"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

type Settings struct{ shared.Settings }

func (Settings) CreemTestMode() bool { return setting.CreemTestMode }
func (Settings) Currency() string {
	if operation_setting.GetGeneralSetting().QuotaDisplayType == operation_setting.QuotaDisplayTypeCNY {
		return "CNY"
	}
	return "USD"
}

func (Settings) PancakeStore() string  { return setting.WaffoPancakeStoreID }
func (Settings) PancakeReturn() string { return setting.WaffoPancakeReturnURL }
