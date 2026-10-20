package list_models

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Input struct {
	Keyword, Vendor, Status, SyncOfficial, SquareState string
	Page, PageSize                                     int
	IncludeChannelModels                               bool
}
type ListModelsMetaResponse struct {
	Items        []*domain.Model `json:"items"`
	Total        int64           `json:"total"`
	Page         int             `json:"page"`
	PageSize     int             `json:"page_size"`
	VendorCounts map[int64]int64 `json:"vendor_counts"`
}
type Output = ListModelsMetaResponse
