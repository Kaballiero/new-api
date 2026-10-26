package domain

import (
	"errors"
)

type VendorMetadataMutation interface {
	SavedVendor(int) (*Vendor, error)
	OtherVendors(int) ([]Vendor, error)
	InsertVendor(*Vendor) error
	UpdateVendor(*Vendor) error
}

func ValidateVendorName(v *Vendor, others []Vendor) error {
	for _, other := range others {
		if VendorNameEqual(other.Name, v.Name) {
			return errors.New("vendor name already exists")
		}
	}
	return nil
}
