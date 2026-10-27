package settings

import (
	publication "github.com/QuantumNous/new-api/internal/adapter/settings/publication"

	pricingadapter "github.com/QuantumNous/new-api/internal/adapter/pricing"

	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

type Validation struct{}

func (Validation) StoredValue(key string, value string) error {
	if key == pricingdomain.PricingCatalogOptionKey {
		_, err := pricingdomain.DecodePricingCatalog(pricingadapter.Capabilities(), value)
		return err
	}
	switch publication.CanonicalGroupRatioOption(key) {
	case "GroupRatio":
		return ratio_setting.CheckGroupRatio(value)
	case "GroupGroupRatio":
		return ratio_setting.CheckGroupGroupRatio(value)
	}
	if key == operation_setting.ToolPriceOptionKey {
		return operation_setting.ValidateToolPricesJSON(value)
	}
	if key == operation_setting.ChannelTestConcurrencyOptionKey {
		return operation_setting.ValidateChannelTestConcurrency(value)
	}
	if key == "MaxTokenAutoGroups" {
		return setting.ValidateMaxTokenAutoGroups(value)
	}
	return nil
}
