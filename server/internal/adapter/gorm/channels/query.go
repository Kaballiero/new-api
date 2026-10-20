package channels

import (
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

func listQuery(filter domain.Filter) *gorm.DB {
	query := model.ApplyChannelGroupFilter(model.DB.Model(&model.Channel{}), filter.Group)
	if filter.Status == 1 {
		query = query.Where("status = ?", 1)
	} else if filter.Status == 0 {
		query = query.Where("status != ?", 1)
	}
	if filter.Type >= 0 {
		query = query.Where("type = ?", filter.Type)
	}
	return query
}
func convertList(channels []*model.Channel) []*domain.Channel {
	if channels == nil {
		return nil
	}
	result := make([]*domain.Channel, len(channels))
	for i, ch := range channels {
		result[i] = ToDomain(ch)
	}
	return result
}
func (Store) ListTags(filter domain.Filter, start, size int) ([]*string, error) {
	return model.GetPaginatedChannelTags(listQuery(filter), start, size)
}
func (Store) CountTags(filter domain.Filter) (int64, error) {
	return model.CountChannelTags(listQuery(filter))
}
func (Store) Count(filter domain.Filter) (int64, error) {
	var total int64
	err := listQuery(filter).Count(&total).Error
	return total, err
}
func (Store) List(filter domain.Filter, sort domain.Sort, start, size int) ([]*domain.Channel, error) {
	var channels []*model.Channel
	options := model.NewChannelSortOptions(sort.By, sort.Order, sort.ID)
	err := options.Apply(listQuery(filter)).Limit(size).Offset(start).Omit("key").Find(&channels).Error
	return convertList(channels), err
}
func (Store) ListTag(filter domain.Filter, sort domain.Sort, tag string) ([]*domain.Channel, error) {
	var channels []*model.Channel
	options := model.NewChannelSortOptions(sort.By, sort.Order, sort.ID)
	err := options.Apply(listQuery(filter).Where("tag = ?", tag)).Omit("key").Find(&channels).Error
	return convertList(channels), err
}
func (Store) TypeCounts(filter domain.Filter) (map[int64]int64, error) {
	var results []struct {
		Type  int64
		Count int64
	}
	err := listQuery(filter).Select("type, count(*) as count").Group("type").Find(&results).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int64)
	for _, r := range results {
		counts[r.Type] = r.Count
	}
	return counts, nil
}
func (Store) SearchTags(keyword, group, modelKeyword string, idSort bool) ([]*string, error) {
	return model.SearchTags(keyword, group, modelKeyword, idSort)
}
func (Store) Search(keyword, group, modelKeyword string, sort domain.Sort) ([]*domain.Channel, error) {
	options := model.NewChannelSortOptions(sort.By, sort.Order, sort.ID)
	channels, err := model.SearchChannels(keyword, group, modelKeyword, sort.ID, options)
	return convertList(channels), err
}
