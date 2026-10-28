package list_channels

import "github.com/QuantumNous/new-api/internal/channels/domain"

type Store interface {
	ListTags(domain.Filter, int, int) ([]*string, error)
	CountTags(domain.Filter) (int64, error)
	ListTag(domain.Filter, domain.Sort, string) ([]*domain.Channel, error)
	Count(domain.Filter) (int64, error)
	List(domain.Filter, domain.Sort, int, int) ([]*domain.Channel, error)
	TypeCounts(domain.Filter) (map[int64]int64, error)
}
type Usecase struct {
	store Store
	log   func(string)
}

func NewUsecase(store Store, log func(string)) Usecase { return Usecase{store: store, log: log} }
func (usecase Usecase) Execute(input Input) (Output, error) {
	channelData := make([]*domain.Channel, 0)

	var total int64

	if input.TagMode {
		tags, err := usecase.store.ListTags(input.Filter, input.Start, input.PageSize)
		if err != nil {
			usecase.log("failed to get paginated tags: " + err.Error())
			return Output{}, domain.ErrListTagsFailed
		}
		total, err = usecase.store.CountTags(input.Filter)
		if err != nil {
			usecase.log("failed to count tags: " + err.Error())
			return Output{}, domain.ErrCountTagsFailed
		}
		for _, tag := range tags {
			if tag == nil || *tag == "" {
				continue
			}
			tagChannels, err := usecase.store.ListTag(input.Filter, input.Sort, *tag)
			if err != nil {
				usecase.log("failed to get channels by tag: " + err.Error())
				return Output{}, domain.ErrListTagChannelsFailed
			}
			channelData = append(channelData, tagChannels...)
		}
	} else {
		var err error
		total, err = usecase.store.Count(input.Filter)
		if err != nil {
			usecase.log("failed to count channels: " + err.Error())
			return Output{}, domain.ErrCountChannelsFailed
		}

		channelData, err = usecase.store.List(input.Filter, input.Sort, input.Start, input.PageSize)
		if err != nil {
			usecase.log("failed to get channels: " + err.Error())
			return Output{}, domain.ErrListChannelsFailed
		}
	}

	for _, datum := range channelData {
		datum.HideMultiKeyFailures()
	}

	counts, err := usecase.store.TypeCounts(domain.Filter{Group: input.Filter.Group, Status: input.Filter.Status, Type: -1})
	if err != nil {
		usecase.log("failed to count channel types: " + err.Error())
		return Output{}, domain.ErrCountTypesFailed
	}
	return Output{Items: channelData, Total: total, Page: input.Page, PageSize: input.PageSize, TypeCounts: counts}, nil
}
