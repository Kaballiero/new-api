package get_pricing

import (
	"github.com/QuantumNous/new-api/common"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

type Input struct {
	UserID  any
	HasUser bool
}
type Output struct {
	Data              []domain.Pricing               `json:"data"`
	Vendors           []domain.PricingVendor         `json:"vendors"`
	GroupRatio        map[string]float64             `json:"group_ratio"`
	UsableGroup       map[string]string              `json:"usable_group"`
	SupportedEndpoint map[string]common.EndpointInfo `json:"supported_endpoint"`
	AutoGroups        []string                       `json:"auto_groups"`
	PricingVersion    string                         `json:"pricing_version"`
}
