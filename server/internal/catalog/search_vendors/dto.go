package search_vendors

import (
	"github.com/QuantumNous/new-api/internal/catalog/domain"
)

type Input struct {
	Keyword, Association string
	Page, PageSize       int
}
type Output struct {
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
	Total    int              `json:"total"`
	Items    []*domain.Vendor `json:"items"`
}
