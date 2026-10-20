package get_pricing

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
	"maps"
)

type Reader interface {
	SnapshotWithEndpoints() ([]domain.Pricing, []domain.PricingVendor, map[string]common.EndpointInfo, string, error)
	GroupRatios() map[string]float64
	UserGroup(int) (string, error)
	GroupGroupRatio(string, string) (float64, bool)
	UsableGroups(string) map[string]string
	AutoGroups(string) []string
}
type Usecase struct{ reader Reader }

func NewUsecase(reader Reader) Usecase { return Usecase{reader: reader} }
func (u Usecase) Execute(input Input) (Output, error) {
	pricing, vendors, endpoints, _, err := u.reader.SnapshotWithEndpoints()
	if err != nil {
		return Output{}, err
	}
	userId, exists := input.UserID, input.HasUser
	usableGroup := map[string]string{}
	groupRatio := map[string]float64{}
	maps.Copy(groupRatio, u.reader.GroupRatios())
	var group string
	if exists {
		userGroup, err := u.reader.UserGroup(userId.(int))
		if err == nil {
			group = userGroup
			for g := range groupRatio {
				ratio, ok := u.reader.GroupGroupRatio(group, g)
				if ok {
					groupRatio[g] = ratio
				}
			}
		}
	}

	usableGroup = u.reader.UsableGroups(group)
	pricing = domain.FilterPricingByUsableGroups(pricing, usableGroup)
	for i := range pricing {
		sourcesByGroup := make(map[string]map[constant.EndpointType][]string)
		for _, group := range domain.EffectivePricingGroups(pricing[i].EnableGroup, usableGroup) {
			if sources := pricing[i].BillingSourcesForGroup(group); len(sources) > 0 {
				sourcesByGroup[group] = sources
			}
		}
		pricing[i].BillingSourcesByGroup = sourcesByGroup
	}
	// check groupRatio contains usableGroup
	for group := range u.reader.GroupRatios() {
		if _, ok := usableGroup[group]; !ok {
			delete(groupRatio, group)
		}
	}

	return Output{Data: pricing, Vendors: vendors, GroupRatio: groupRatio, UsableGroup: usableGroup, SupportedEndpoint: endpoints, AutoGroups: u.reader.AutoGroups(group), PricingVersion: "a42d372ccf0b5dd13ecf71203521f9d2"}, nil
}
