package model

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

var pricingCatalogueRevision string
var pricingCatalogueRevisionError error

func GetPricingCatalogueSnapshot() ([]Pricing, []PricingVendor, string, error) {
	updatePricingLock.Lock()
	defer updatePricingLock.Unlock()
	refreshPricingCatalogueLocked()
	if pricingCatalogueRevision == "" || lastGetPricingTime.IsZero() {
		return nil, nil, "", errors.New("pricing catalogue unavailable")
	}
	return pricingMap, vendorsList, pricingCatalogueRevision, pricingCatalogueRevisionError
}

func pricingCatalogueFingerprint(pricing []Pricing, vendors []PricingVendor) (string, error) {
	rows := make([]string, 0, len(pricing))
	for _, item := range pricing {
		groups := append([]string{}, item.EnableGroup...)
		slices.Sort(groups)
		endpoints := append([]constant.EndpointType{}, item.SupportedEndpointTypes...)
		slices.Sort(endpoints)
		offers := make(map[string][]string, len(item.CatalogOffersByGroup))
		for group, candidates := range item.CatalogOffersByGroup {
			offers[group] = make([]string, 0, len(candidates))
			for _, offer := range candidates {
				offer.Prices = append([]CatalogPrice{}, offer.Prices...)
				slices.SortFunc(offer.Prices, func(left, right CatalogPrice) int { return comparePricingFingerprintPrices(left, right) })
				value, err := common.Marshal(offer)
				if err != nil {
					return "", err
				}
				offers[group] = append(offers[group], string(value))
			}
			slices.Sort(offers[group])
		}
		canonical := make(map[string]string, len(item.ConsumerCanonicalModelsByGroup))
		for group, base := range item.ConsumerCanonicalModelsByGroup {
			canonical[group] = base
		}
		row := struct {
			ModelName, Description, Icon, Tags, OwnerBy string
			VendorID                                    int
			Groups                                      []string
			Endpoints                                   []constant.EndpointType
			Offers                                      map[string][]string
			CanonicalGroups                             map[string]string
		}{item.ModelName, item.Description, item.Icon, item.Tags, item.OwnerBy, item.VendorID, groups, endpoints, offers, canonical}
		value, err := common.Marshal(row)
		if err != nil {
			return "", err
		}
		rows = append(rows, string(value))
	}
	slices.Sort(rows)
	creators := append([]PricingVendor{}, vendors...)
	slices.SortFunc(creators, func(left, right PricingVendor) int { return left.ID - right.ID })
	value, err := common.Marshal(struct {
		Rows    []string
		Vendors []PricingVendor
	}{rows, creators})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:]), nil
}

func comparePricingFingerprintPrices(left, right CatalogPrice) int {
	if left.Component < right.Component {
		return -1
	}
	if left.Component > right.Component {
		return 1
	}
	if left.Unit < right.Unit {
		return -1
	}
	if left.Unit > right.Unit {
		return 1
	}
	return 0
}
