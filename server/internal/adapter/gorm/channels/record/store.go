package record

import (
	"errors"
	"fmt"
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/samber/lo"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
)

type Environment struct {
	CachedRead    func(int) (*Channel, error)
	CacheStatus   func(int, int)
	StatusLock    func() func()
	PollingLock   func(int) func()
	AbilityStatus func(int, bool) error

	DB                            func() *gorm.DB
	LockForUpdate                 func(*gorm.DB) *gorm.DB
	GroupColumn, KeyColumn        func() string
	AddAbilities, UpdateAbilities func(*Channel, *gorm.DB) error
	DeleteAbilities               func(*Channel) error
	DeleteAbilityIDs              func(*gorm.DB, []int) error
	UpdateAbilityStatusByTag      func(string, bool) error
	UpdateAbilityByTag            func(string, *string, *int64, *uint) error
}
type Store struct {
	Codec
	environment Environment
	states      map[*domain.Channel]*Channel
}

func NewStore(environment Environment) Store {
	return Store{Codec: Codec{Log: common.SysLog}, environment: environment, states: make(map[*domain.Channel]*Channel)}
}
func (store Store) GetAllChannels(startIdx int, num int, selectAll bool, idSort bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	var err error
	order := resolveChannelSortOptions(idSort, sortOptions)
	if selectAll {
		err = order.Apply(store.environment.DB()).Find(&channels).Error
	} else {
		err = order.Apply(store.environment.DB()).Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	}
	return channels, err
}
func (store Store) GetChannelsByTag(tag string, idSort bool, selectAll bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	order := resolveChannelSortOptions(idSort, sortOptions)
	query := order.Apply(store.environment.DB().Where("tag = ?", tag))
	if !selectAll {
		query = query.Omit("key")
	}
	err := query.Find(&channels).Error
	return channels, err
}
func (store Store) SearchChannels(keyword string, group string, model string, idSort bool, sortOptions ...ChannelSortOptions) ([]*Channel, error) {
	var channels []*Channel
	modelsCol := "`models`"

	// PostgreSQL quotes identifiers with double quotes
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		modelsCol = `"models"`
	}

	baseURLCol := "`base_url`"
	// PostgreSQL quotes identifiers with double quotes
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		baseURLCol = `"base_url"`
	}

	order := resolveChannelSortOptions(idSort, sortOptions)

	// base query
	baseQuery := store.environment.DB().Model(&Channel{}).Omit("key")

	// WHERE clause
	whereClause := "(id = ? OR name LIKE ? OR " + store.environment.KeyColumn() + " = ? OR " + baseURLCol + " LIKE ?) AND " + modelsCol + " LIKE ?"
	args := []any{common.String2Int(keyword), "%" + keyword + "%", keyword, "%" + keyword + "%", "%" + model + "%"}
	baseQuery = store.ApplyChannelGroupFilter(baseQuery.Where(whereClause, args...), group)

	// run the query
	err := order.Apply(baseQuery).Find(&channels).Error
	if err != nil {
		return nil, err
	}
	return channels, nil
}
func (store Store) GetChannelById(id int, selectAll bool) (*Channel, error) {
	channel := &Channel{Id: id}
	var err error = nil
	if selectAll {
		err = store.environment.DB().First(channel, "id = ?", id).Error
	} else {
		err = store.environment.DB().Omit("key").First(channel, "id = ?", id).Error
	}
	if err != nil {
		return nil, err
	}
	return channel, nil
}
func (store Store) GetChannelByIdForUpdate(tx *gorm.DB, id int) (*Channel, error) {
	channel := &Channel{Id: id}
	if err := store.environment.LockForUpdate(tx).First(channel, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return channel, nil
}
func (store Store) BatchInsertChannels(channels []Channel) error {
	if len(channels) == 0 {
		return nil
	}
	tx := store.environment.DB().Begin()
	if tx.Error != nil {
		return tx.Error
	}
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	for _, chunk := range lo.Chunk(channels, 50) {
		if err := tx.Create(&chunk).Error; err != nil {
			tx.Rollback()
			return err
		}
		for _, channel_ := range chunk {
			if err := store.environment.AddAbilities(&channel_, tx); err != nil {
				tx.Rollback()
				return err
			}
		}
	}
	return tx.Commit().Error
}
func (store Store) BatchDeleteChannels(ids []int) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	// delete channels and their abilities in batches within one transaction
	tx := store.environment.DB().Begin()
	if tx.Error != nil {
		return 0, tx.Error
	}
	var deletedCount int64
	for _, chunk := range lo.Chunk(ids, 200) {
		result := tx.Where("id in (?)", chunk).Delete(&Channel{})
		if result.Error != nil {
			tx.Rollback()
			return 0, result.Error
		}
		deletedCount += result.RowsAffected
		if err := store.environment.DeleteAbilityIDs(tx, chunk); err != nil {
			tx.Rollback()
			return 0, err
		}
	}
	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return deletedCount, nil
}
func (store Store) EnableChannelByTag(tag string) error {
	err := store.environment.DB().Model(&Channel{}).Where("tag = ?", tag).Update("status", common.ChannelStatusEnabled).Error
	if err != nil {
		return err
	}
	err = store.environment.UpdateAbilityStatusByTag(tag, true)
	return err
}
func (store Store) DisableChannelByTag(tag string) error {
	err := store.environment.DB().Model(&Channel{}).Where("tag = ?", tag).Update("status", common.ChannelStatusManuallyDisabled).Error
	if err != nil {
		return err
	}
	err = store.environment.UpdateAbilityStatusByTag(tag, false)
	return err
}
func (store Store) EditChannelByTag(tag string, newTag *string, modelMapping *string, models *string, group *string, priority *int64, weight *uint, paramOverride *string, headerOverride *string) error {
	updateData := Channel{}
	shouldReCreateAbilities := false
	updatedTag := tag
	// rename the tag when newTag is set and differs from tag
	if newTag != nil && *newTag != tag {
		updateData.Tag = newTag
		updatedTag = *newTag
	}
	if modelMapping != nil {
		updateData.ModelMapping = modelMapping
	}
	if models != nil && *models != "" {
		shouldReCreateAbilities = true
		updateData.Models = *models
	}
	if group != nil && *group != "" {
		shouldReCreateAbilities = true
		updateData.Group = *group
	}
	if priority != nil {
		updateData.Priority = priority
	}
	if weight != nil {
		updateData.Weight = weight
	}
	if paramOverride != nil {
		updateData.ParamOverride = paramOverride
	}
	if headerOverride != nil {
		updateData.HeaderOverride = headerOverride
	}

	err := store.environment.DB().Model(&Channel{}).Where("tag = ?", tag).Updates(updateData).Error
	if err != nil {
		return err
	}
	if shouldReCreateAbilities {
		channels, err := store.GetChannelsByTag(updatedTag, false, false)
		if err == nil {
			for _, channel := range channels {
				err = store.environment.UpdateAbilities(channel, nil)
				if err != nil {
					common.SysLog(fmt.Sprintf("failed to update abilities: channel_id=%d, tag=%s, error=%v", channel.Id, channel.GetTag(), err))
				}
			}
		}
	} else {
		err := store.environment.UpdateAbilityByTag(tag, newTag, priority, weight)
		if err != nil {
			return err
		}
	}
	return nil
}
func (store Store) DeleteChannelByStatus(status int64) (int64, error) {
	result := store.environment.DB().Where("status = ?", status).Delete(&Channel{})
	return result.RowsAffected, result.Error
}
func (store Store) DeleteDisabledChannel() (int64, error) {
	result := store.environment.DB().Where("status = ? or status = ?", common.ChannelStatusAutoDisabled, common.ChannelStatusManuallyDisabled).Delete(&Channel{})
	return result.RowsAffected, result.Error
}
func (store Store) GetPaginatedTags(offset int, limit int) ([]*string, error) {
	return store.GetPaginatedChannelTags(store.environment.DB().Model(&Channel{}), offset, limit)
}
func (store Store) GetPaginatedChannelTags(query *gorm.DB, offset int, limit int) ([]*string, error) {
	var tags []*string
	err := query.
		Select("DISTINCT tag").
		Where("tag is not null AND tag != ''").
		Order(clause.OrderByColumn{Column: clause.Column{Name: "tag"}}).
		Offset(offset).
		Limit(limit).
		Find(&tags).Error
	return tags, err
}
func (store Store) SearchTags(keyword string, group string, model string, idSort bool) ([]*string, error) {
	var tags []*string
	modelsCol := "`models`"

	// PostgreSQL quotes identifiers with double quotes
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		modelsCol = `"models"`
	}

	baseURLCol := "`base_url`"
	// PostgreSQL quotes identifiers with double quotes
	if common.UsingMainDatabase(common.DatabaseTypePostgreSQL) {
		baseURLCol = `"base_url"`
	}

	order := "priority desc"
	if idSort {
		order = "id desc"
	}

	// base query
	baseQuery := store.environment.DB().Model(&Channel{}).Omit("key")

	// WHERE clause
	whereClause := "(id = ? OR name LIKE ? OR " + store.environment.KeyColumn() + " = ? OR " + baseURLCol + " LIKE ?) AND " + modelsCol + " LIKE ?"
	args := []any{common.String2Int(keyword), "%" + keyword + "%", keyword, "%" + keyword + "%", "%" + model + "%"}
	baseQuery = store.ApplyChannelGroupFilter(baseQuery.Where(whereClause, args...), group)

	subQuery := baseQuery.
		Select("tag").
		Where("tag != ''").
		Order(order)

	err := store.environment.DB().Table("(?) as sub", subQuery).
		Select("DISTINCT tag").
		Find(&tags).Error

	if err != nil {
		return nil, err
	}

	return tags, nil
}
func (store Store) GetChannelsByIds(ids []int) ([]*Channel, error) {
	var channels []*Channel
	err := store.environment.DB().Where("id in (?)", ids).Find(&channels).Error
	return channels, err
}
func (store Store) BatchSetChannelTag(ids []int, tag *string) error {
	// begin the transaction
	tx := store.environment.DB().Begin()
	if tx.Error != nil {
		return tx.Error
	}

	// update the tag
	err := tx.Model(&Channel{}).Where("id in (?)", ids).Update("tag", tag).Error
	if err != nil {
		tx.Rollback()
		return err
	}

	// update ability status
	channels, err := store.GetChannelsByIds(ids)
	if err != nil {
		tx.Rollback()
		return err
	}

	for _, channel := range channels {
		err = store.environment.UpdateAbilities(channel, tx)
		if err != nil {
			tx.Rollback()
			return err
		}
	}

	// commit the transaction
	return tx.Commit().Error
}
func (store Store) CountAllChannels() (int64, error) {
	var total int64
	err := store.environment.DB().Model(&Channel{}).Count(&total).Error
	return total, err
}
func (store Store) CountAllTags() (int64, error) {
	return store.CountChannelTags(store.environment.DB().Model(&Channel{}))
}
func (store Store) CountChannelTags(query *gorm.DB) (int64, error) {
	var total int64
	err := query.Where("tag is not null AND tag != ''").Distinct("tag").Count(&total).Error
	return total, err
}
func (store Store) GetChannelsByType(startIdx int, num int, idSort bool, channelType int) ([]*Channel, error) {
	var channels []*Channel
	order := "priority desc"
	if idSort {
		order = "id desc"
	}
	err := store.environment.DB().Where("type = ?", channelType).Order(order).Limit(num).Offset(startIdx).Omit("key").Find(&channels).Error
	return channels, err
}
func (store Store) CountChannelsByType(channelType int) (int64, error) {
	var count int64
	err := store.environment.DB().Model(&Channel{}).Where("type = ?", channelType).Count(&count).Error
	return count, err
}
func (store Store) CountChannelsGroupByType() (map[int64]int64, error) {
	type result struct {
		Type  int64 `gorm:"column:type"`
		Count int64 `gorm:"column:count"`
	}
	var results []result
	err := store.environment.DB().Model(&Channel{}).Select("type, count(*) as count").Group("type").Find(&results).Error
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int64)
	for _, r := range results {
		counts[r.Type] = r.Count
	}
	return counts, nil
}
func (store Store) Save(channel *Channel) error {
	return store.environment.DB().Save(channel).Error
}
func (store Store) SaveStatusState(channel *Channel) error {
	if channel.Id == 0 {
		return errors.New("channel ID is 0")
	}
	updates := map[string]any{
		"status":     channel.Status,
		"other_info": channel.OtherInfo,
	}
	if channel.ChannelInfo.IsMultiKey {
		updates["channel_info"] = channel.ChannelInfo
	}
	return store.environment.DB().Model(&Channel{}).Where("id = ?", channel.Id).Updates(updates).Error
}
func (store Store) Insert(channel *Channel) error {
	var err error
	err = store.environment.DB().Create(channel).Error
	if err != nil {
		return err
	}
	err = store.environment.AddAbilities(channel, nil)
	return err
}
func (store Store) Update(channel *Channel) error {
	return store.environment.DB().Transaction(func(tx *gorm.DB) error {
		existing, err := store.GetChannelByIdForUpdate(tx, channel.Id)
		if err != nil {
			return err
		}
		if channel.ChannelInfo.IsMultiKey && channel.Key == "" {
			channel.Key = existing.Key
		}
		return store.UpdateWithTx(channel, tx)
	})
}
func (store Store) UpdateWithTx(channel *Channel, tx *gorm.DB) error {
	value := ToDomain(channel)
	if value.ChannelInfo.IsMultiKey {
		domain.ReconcileMultiKeyInfo(value, store.ReconciledKeys(value.Key))
	}
	channel.ChannelInfo = FromDomain(value).ChannelInfo
	if err := tx.Model(channel).Updates(channel).Error; err != nil {
		return err
	}
	if err := tx.Model(channel).First(channel, "id = ?", channel.Id).Error; err != nil {
		return err
	}
	return store.environment.UpdateAbilities(channel, tx)
}
func (store Store) Delete(channel *Channel) error {
	var err error
	err = store.environment.DB().Delete(channel).Error
	if err != nil {
		return err
	}
	err = store.environment.DeleteAbilities(channel)
	return err
}
func (store Store) UpdateResponseTime(channel *Channel, responseTime int64) {
	err := store.environment.DB().Model(channel).Select("response_time", "test_time").Updates(Channel{
		TestTime:     common.GetTimestamp(),
		ResponseTime: int(responseTime),
	}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update response time: channel_id=%d, error=%v", channel.Id, err))
	}
}
func (store Store) UpdateBalance(channel *Channel, balance float64) {
	err := store.environment.DB().Model(channel).Select("balance_updated_time", "balance").Updates(Channel{
		BalanceUpdatedTime: common.GetTimestamp(),
		Balance:            balance,
	}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to update balance: channel_id=%d, error=%v", channel.Id, err))
	}
}
func NormalizeChannelGroupFilter(group string) string {
	group = strings.TrimSpace(group)
	if group == "" || strings.EqualFold(group, "all") || strings.EqualFold(group, "null") {
		return ""
	}
	return group
}
func (store Store) GroupFilterCondition() string {
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		return `CONCAT(',', ` + store.environment.GroupColumn() + `, ',') LIKE ? ESCAPE '!'`
	}
	return `(',' || ` + store.environment.GroupColumn() + ` || ',') LIKE ? ESCAPE '!'`
}
func GroupFilterPattern(group string) string {
	group = strings.NewReplacer(
		"!", "!!",
		"%", "!%",
		"_", "!_",
	).Replace(group)
	return "%," + group + ",%"
}
func (store Store) ApplyChannelGroupFilter(query *gorm.DB, group string) *gorm.DB {
	group = NormalizeChannelGroupFilter(group)
	if group == "" {
		return query
	}
	return query.Where(store.GroupFilterCondition(), GroupFilterPattern(group))
}

func (store Store) SaveChannelInfo(channel *Channel) error {
	return store.environment.DB().Model(channel).Update("channel_info", channel.ChannelInfo).Error
}
