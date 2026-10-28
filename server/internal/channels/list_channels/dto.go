package list_channels

import "github.com/QuantumNous/new-api/internal/channels/domain"

type Input struct {
	Filter                domain.Filter
	Sort                  domain.Sort
	TagMode               bool
	Start, Page, PageSize int
}
type Output struct {
	Items      []*domain.Channel `json:"items"`
	Total      int64             `json:"total"`
	Page       int               `json:"page"`
	PageSize   int               `json:"page_size"`
	TypeCounts map[int64]int64   `json:"type_counts"`
}
