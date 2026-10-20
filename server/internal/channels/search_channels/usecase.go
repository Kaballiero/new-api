package search_channels

import "github.com/QuantumNous/new-api/internal/channels/domain"

type Store interface {
	SearchTags(string, string, string, bool) ([]*string, error)
	ListTag(domain.Filter, domain.Sort, string) ([]*domain.Channel, error)
	Search(string, string, string, domain.Sort) ([]*domain.Channel, error)
}
type Usecase struct{ store Store }

func NewUsecase(store Store) Usecase { return Usecase{store: store} }
func (usecase Usecase) Execute(input Input) (Output, error) {

	channelData := make([]*domain.Channel, 0)
	if input.TagMode {
		tags, err := usecase.store.SearchTags(input.Keyword, input.Group, input.Model, input.Sort.ID)
		if err != nil {
			return Output{}, err
		}
		for _, tag := range tags {
			if tag != nil && *tag != "" {
				tagChannels, err := usecase.store.ListTag(domain.Filter{Group: input.Group, Status: -1, Type: -1}, input.Sort, *tag)
				if err != nil {
					return Output{}, err
				}
				channelData = append(channelData, tagChannels...)
			}
		}
	} else {
		channels, err := usecase.store.Search(input.Keyword, input.Group, input.Model, input.Sort)
		if err != nil {
			return Output{}, err
		}
		channelData = channels
	}

	if input.Status == 1 || input.Status == 0 {
		filtered := make([]*domain.Channel, 0, len(channelData))
		for _, ch := range channelData {
			if input.Status == 1 && ch.Status != 1 {
				continue
			}
			if input.Status == 0 && ch.Status == 1 {
				continue
			}
			filtered = append(filtered, ch)
		}
		channelData = filtered
	}

	// calculate type counts for search results
	typeCounts := make(map[int64]int64)
	for _, channel := range channelData {
		typeCounts[int64(channel.Type)]++
	}

	if input.Type >= 0 {
		filtered := make([]*domain.Channel, 0, len(channelData))
		for _, ch := range channelData {
			if ch.Type == input.Type {
				filtered = append(filtered, ch)
			}
		}
		channelData = filtered
	}

	page, pageSize := input.Page, input.PageSize

	total := len(channelData)
	startIdx := (page - 1) * pageSize
	if startIdx > total {
		startIdx = total
	}
	endIdx := startIdx + pageSize
	if endIdx > total {
		endIdx = total
	}

	pagedData := channelData[startIdx:endIdx]

	for _, datum := range pagedData {
		datum.HideMultiKeyFailures()
	}

	return Output{Items: pagedData, Total: total, TypeCounts: typeCounts}, nil
}
