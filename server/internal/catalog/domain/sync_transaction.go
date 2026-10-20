package domain

type MetadataMutation interface {
	OtherVendors(int) ([]Vendor, error)
	State() (map[string]*Model, map[string]*Vendor, error)
	InsertVendor(*Vendor) error
	InsertModel(map[string]any) error
	UpdateModel(int, map[string]any) error
}
