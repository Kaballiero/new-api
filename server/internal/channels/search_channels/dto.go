package search_channels

import "github.com/QuantumNous/new-api/internal/channels/domain"

type Input struct {
	Keyword, Group, Model string
	Status, Type          int
	Sort                  domain.Sort
	TagMode               bool
	Page, PageSize        int
}
type SearchChannelsResponse struct {
	Items      []*domain.Channel `json:"items"`
	Total      int               `json:"total"`
	TypeCounts map[int64]int64   `json:"type_counts"`
}
type Output = SearchChannelsResponse
