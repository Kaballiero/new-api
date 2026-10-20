package model

import (
	catalogrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
)

func initDefaultVendorMapping(metadata map[string]*Model, vendors map[int]*Vendor, abilities []AbilityWithChannel) {
	m := make(map[string]*pricingdomain.BuildModel, len(metadata))
	v := make(map[int]*pricingdomain.BuildVendor, len(vendors))
	for name, row := range metadata {
		m[name] = catalogrecord.ToModel((*catalogrecord.Model)(row))
	}
	for id, row := range vendors {
		v[id] = catalogrecord.ToVendor((*catalogrecord.Vendor)(row))
	}
	pricingdomain.InitDefaultVendorMapping(m, v, pricingBuildAbilities(abilities))
	for name, row := range m {
		if metadata[name] == nil {
			metadata[name] = (*Model)(catalogrecord.FromModel(row))
		}
	}
	for id, row := range v {
		if vendors[id] == nil {
			vendors[id] = (*Vendor)(catalogrecord.FromVendor(row))
		}
	}
}
