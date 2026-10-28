package domain

type MetadataValues struct {
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Tags        string `json:"tags"`
	Vendor      string `json:"vendor"`
	Endpoints   string `json:"endpoints"`
	NameRule    int    `json:"name_rule"`
	Status      int    `json:"status"`
}

type MetadataSyncSelection struct {
	ModelName     string   `json:"model_name"`
	RecordVersion string   `json:"record_version"`
	Create        bool     `json:"create"`
	Fields        []string `json:"fields"`
}

type MetadataSyncUpdate struct {
	MetadataSyncSelection
	Values MetadataValues
}

type MetadataSyncResult struct {
	CreatedModels  []string                `json:"created_models"`
	UpdatedModels  []MetadataSyncSelection `json:"updated_models"`
	CreatedVendors []string                `json:"created_vendors"`
}

type VendorOperation struct {
	Action          string `json:"action"`
	VendorIDs       []int  `json:"vendor_ids"`
	ModelIDs        []int  `json:"model_ids"`
	TargetVendorID  int    `json:"target_vendor_id"`
	ExpectedVersion string `json:"expected_version,omitempty"`
}

type VendorAssignmentModel struct {
	ID          int    `json:"id"`
	ModelName   string `json:"model_name"`
	NameRule    int    `json:"name_rule"`
	VendorID    int    `json:"vendor_id"`
	VendorName  string `json:"vendor_name"`
	UpdatedTime int64  `json:"updated_time"`
}

type VendorOperationPreview struct {
	Action  string                  `json:"action"`
	Sources []Vendor                `json:"sources"`
	Target  *Vendor                 `json:"target"`
	Models  []VendorAssignmentModel `json:"models"`
	Version string                  `json:"version"`
}

type VendorOperationResult struct {
	UpdatedModels  []int `json:"updated_models"`
	DeletedVendors []int `json:"deleted_vendors"`
}

type VendorReferenceError struct {
	Counts map[int]int64 `json:"reference_counts"`
}

func (e *VendorReferenceError) Error() string {
	return "vendors are still referenced by models; transfer or clear their assignments first"
}

type ModelDeleteResult struct {
	DeletedCount    int `json:"deleted_count"`
	UpdatedChannels int `json:"updated_channels"`
}
