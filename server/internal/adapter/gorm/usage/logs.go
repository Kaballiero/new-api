package usage

import (
	"errors"
	"strings"

	"github.com/QuantumNous/new-api/common"
	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	keydomain "github.com/QuantumNous/new-api/internal/apikeys/domain"
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/types"

	"gorm.io/gorm"
)

func applyExplicitLogTextFilter(tx *gorm.DB, column string, value string) (*gorm.DB, error) {
	if value == "" {
		return tx, nil
	}
	if strings.Contains(value, "%") {
		condition, pattern, err := buildLogLikeCondition(column, value)
		if err != nil {
			return nil, err
		}
		return tx.Where(condition, pattern), nil
	}
	return tx.Where(column+" = ?", value), nil
}

func (Store) ValidateModelFilter(value string) error {
	if !strings.Contains(value, "%") {
		return nil
	}
	_, _, err := buildLogLikeCondition("logs.model_name", value)
	return err
}

func buildLogLikeCondition(column string, value string) (string, string, error) {
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		pattern, err := sanitizeClickHouseLikePattern(value)
		if err != nil {
			return "", "", err
		}
		return column + " LIKE ?", pattern, nil
	}

	pattern, err := keydomain.SanitizeSearchPattern(value)
	if err != nil {
		return "", "", err
	}
	return column + " LIKE ? ESCAPE '!'", pattern, nil
}

func sanitizeClickHouseLikePattern(input string) (string, error) {
	input = strings.ReplaceAll(input, `\`, `\\`)
	input = strings.ReplaceAll(input, `_`, `\_`)

	if err := keydomain.ValidateSearchPattern(input); err != nil {
		return "", err
	}
	return input, nil
}

func queryKeyLogs(tokenId, limit int) (logs []*record.Log, err error) {
	order := "id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("")
	}
	err = model.LOG_DB.Model(&record.Log{}).Where("token_id = ?", tokenId).Order(order).Limit(limit).Find(&logs).Error
	return logs, err
}

func queryAllLogs(
	logType int,
	startTimestamp int64,
	endTimestamp int64,
	modelName string,
	username string,
	tokenName string,
	startIdx int,
	num int,
	channel int,
	group string,
	requestId string,
	upstreamRequestId string,
) (logs []*record.Log, total int64, err error) {
	var tx *gorm.DB
	if logType == domain.LogTypeUnknown {
		tx = model.LOG_DB
	} else {
		tx = model.LOG_DB.Where("logs.type = ?", logType)
	}

	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
		return nil, 0, err
	}
	if tx, err = applyExplicitLogTextFilter(tx, "logs.username", username); err != nil {
		return nil, 0, err
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if channel != 0 {
		tx = tx.Where("logs.channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupColumn()+" = ?", group)
	}
	return readAdminLogs(tx, startIdx, num)
}

func queryUserIDsLogs(
	userIDs []int,
	logType int,
	startTimestamp,
	endTimestamp int64,
	modelName string,
	startIdx,
	num int,
) ([]*record.Log, int64, error) {
	tx := model.LOG_DB.Where("logs.user_id IN ?", userIDs)
	if logType != domain.LogTypeUnknown {
		tx = tx.Where("logs.type = ?", logType)
	}
	var err error
	if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
		return nil, 0, err
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	return readAdminLogs(tx, startIdx, num)
}

func readAdminLogs(tx *gorm.DB, startIdx, num int) (logs []*record.Log, total int64, err error) {
	err = tx.Model(&record.Log{}).Count(&total).Error
	if err != nil {
		return nil, 0, err
	}
	order := "logs.created_at desc, logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	err = tx.Order(order).Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		return nil, 0, err
	}
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		assignDisplayLogIds(logs, startIdx)
	}

	channelIds := types.NewSet[int]()
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds.Add(log.ChannelId)
		}
	}

	if channelIds.Len() > 0 {
		var channels []struct {
			Id   int    `gorm:"column:id"`
			Name string `gorm:"column:name"`
		}
		if common.MemoryCacheEnabled {
			// Cache get channel
			for _, channelId := range channelIds.Items() {
				if cacheChannel, err := channelstore.CacheGetChannel(channelId); err == nil {
					channels = append(channels, struct {
						Id   int    `gorm:"column:id"`
						Name string `gorm:"column:name"`
					}{
						Id:   channelId,
						Name: cacheChannel.Name,
					})
				}
			}
		} else {
			// Bulk query channels from DB
			if err = model.DB.Table("channels").Select("id, name").Where("id IN ?", channelIds.Items()).Find(&channels).Error; err != nil {
				return logs, total, err
			}
		}
		channelMap := make(map[int]string, len(channels))
		for _, channel := range channels {
			channelMap[channel.Id] = channel.Name
		}
		for i := range logs {
			logs[i].ChannelName = channelMap[logs[i].ChannelId]
		}
	}

	return logs, total, err
}

func queryOwnLogs(
	userId int,
	tokenId int,
	logType int,
	startTimestamp int64,
	endTimestamp int64,
	modelName string,
	tokenName string,
	startIdx int,
	num int,
	group string,
	requestId string,
	upstreamRequestId string,
	literalModel bool,
	countLimit int,
) (logs []*record.Log, total int64, err error) {
	var tx *gorm.DB
	if logType == domain.LogTypeUnknown {
		tx = model.LOG_DB.Where("logs.user_id = ?", userId)
	} else {
		tx = model.LOG_DB.Where("logs.user_id = ? and logs.type = ?", userId, logType)
	}

	if literalModel {
		condition := "LOWER(logs.model_name) LIKE ? ESCAPE '!'"
		escape := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_")
		if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
			condition = "LOWER(logs.model_name) LIKE ?"
			escape = strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`)
		}
		pattern := "%" + escape.Replace(strings.ToLower(modelName)) + "%"
		tx = tx.Where(condition, pattern)
	} else if tx, err = applyExplicitLogTextFilter(tx, "logs.model_name", modelName); err != nil {
		return nil, 0, err
	}
	if tokenName != "" {
		tx = tx.Where("logs.token_name = ?", tokenName)
	}
	if tokenId > 0 {
		tx = tx.Where("logs.token_id = ?", tokenId)
	}
	if requestId != "" {
		tx = tx.Where("logs.request_id = ?", requestId)
	}
	if upstreamRequestId != "" {
		tx = tx.Where("logs.upstream_request_id = ?", upstreamRequestId)
	}
	if startTimestamp != 0 {
		tx = tx.Where("logs.created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("logs.created_at <= ?", endTimestamp)
	}
	if group != "" {
		tx = tx.Where("logs."+logGroupColumn()+" = ?", group)
	}
	err = tx.Model(&record.Log{}).Limit(countLimit).Count(&total).Error
	if err != nil {
		common.SysError("failed to count user logs: " + err.Error())
		return nil, 0, domain.ErrLogQuery
	}
	order := "logs.id desc"
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		order = clickHouseLogOrder("logs.")
	}
	err = tx.Order(order).Limit(num).Offset(startIdx).Find(&logs).Error
	if err != nil {
		common.SysError("failed to search user logs: " + err.Error())
		return nil, 0, domain.ErrLogQuery
	}

	return logs, total, err
}

func queryLogStats(
	logType int,
	startTimestamp int64,
	endTimestamp int64,
	modelName string,
	username string,
	tokenName string,
	channel int,
	group string,
	recentSince func() int64,
) (stat domain.Stat, err error) {
	tx := model.LOG_DB.Table("logs").Select("COALESCE(sum(quota), 0) quota")

	// Build a separate query for RPM and TPM.
	rpmTpmQuery := model.LOG_DB.Table("logs").Select("count(*) rpm, COALESCE(sum(prompt_tokens), 0) + COALESCE(sum(completion_tokens), 0) tpm")

	if tx, err = applyExplicitLogTextFilter(tx, "username", username); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "username", username); err != nil {
		return stat, err
	}
	if tokenName != "" {
		tx = tx.Where("token_name = ?", tokenName)
		rpmTpmQuery = rpmTpmQuery.Where("token_name = ?", tokenName)
	}
	if startTimestamp != 0 {
		tx = tx.Where("created_at >= ?", startTimestamp)
	}
	if endTimestamp != 0 {
		tx = tx.Where("created_at <= ?", endTimestamp)
	}
	if tx, err = applyExplicitLogTextFilter(tx, "model_name", modelName); err != nil {
		return stat, err
	}
	if rpmTpmQuery, err = applyExplicitLogTextFilter(rpmTpmQuery, "model_name", modelName); err != nil {
		return stat, err
	}
	if channel != 0 {
		tx = tx.Where("channel_id = ?", channel)
		rpmTpmQuery = rpmTpmQuery.Where("channel_id = ?", channel)
	}
	if group != "" {
		tx = tx.Where(logGroupColumn()+" = ?", group)
		rpmTpmQuery = rpmTpmQuery.Where(logGroupColumn()+" = ?", group)
	}

	tx = tx.Where("type = ?", logType)
	rpmTpmQuery = rpmTpmQuery.Where("type = ?", logType)

	// Count RPM and TPM over the last 60 seconds.
	rpmTpmQuery = rpmTpmQuery.Where("created_at >= ?", recentSince())

	// Execute the aggregate queries.
	if err := tx.Scan(&stat).Error; err != nil {
		common.SysError("failed to query log stat: " + err.Error())
		return stat, domain.ErrLogStatsQuery
	}
	var rateStat struct {
		Rpm int
		Tpm int
	}
	if err := rpmTpmQuery.Scan(&rateStat).Error; err != nil {
		common.SysError("failed to query rpm/tpm stat: " + err.Error())
		return stat, domain.ErrLogStatsQuery
	}
	stat.Rpm = rateStat.Rpm
	stat.Tpm = rateStat.Tpm

	return stat, nil
}

func logGroupColumn() string {
	if common.UsingLogDatabase(common.DatabaseTypePostgreSQL) {
		return `"group"`
	}
	return "`group`"
}

func clickHouseLogOrder(prefix string) string {
	return prefix + "created_at desc, " + prefix + "request_id desc"
}

func assignDisplayLogIds(logs []*record.Log, start int) {
	for i, item := range logs {
		item.Id = start + i + 1
	}
}

func (Store) ListAdminLogs(q domain.LogQuery, policy domain.VisibilityPolicy) ([]*dto.Log, int64, error) {
	rows, total, err := queryAllLogs(q.Kind, q.StartTimestamp, q.EndTimestamp, q.ModelName,
		q.Username, q.TokenName, q.Start, q.Count, q.Channel, q.Group, q.RequestID, q.UpstreamRequestID)
	return logDTOs(rows, policy, q.Start), total, err
}

func (Store) QueryUserLogs(q domain.LogQuery, policy domain.VisibilityPolicy) ([]*dto.Log, int64, error) {
	rows, total, err := queryUserIDsLogs(q.UserIDs, q.Kind, q.StartTimestamp, q.EndTimestamp, q.ModelName, q.Start, q.Count)
	return logDTOs(rows, policy, q.Start), total, err
}

func (Store) ListOwnLogs(q domain.LogQuery, policy domain.VisibilityPolicy) ([]*dto.Log, int64, error) {
	rows, total, err := queryOwnLogs(q.UserID, q.TokenID, q.Kind, q.StartTimestamp, q.EndTimestamp,
		q.ModelName, q.TokenName, q.Start, q.Count, q.Group, q.RequestID, q.UpstreamRequestID, q.LiteralModel, q.CountLimit)
	return logDTOs(rows, policy, q.Start), total, err
}

func (Store) ListKeyLogs(tokenID, limit int, policy domain.VisibilityPolicy) ([]*dto.Log, error) {
	rows, err := queryKeyLogs(tokenID, limit)
	return logDTOs(rows, policy, 0), err
}

func (Store) LogStats(q domain.LogQuery, recentSince func() int64) (domain.Stat, error) {
	return queryLogStats(q.Kind, q.StartTimestamp, q.EndTimestamp, q.ModelName, q.Username, q.TokenName, q.Channel, q.Group, recentSince)
}

func (s Store) TokenOwner(tokenID, userID int) error {
	_, err := s.ReadToken(tokenID, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.ErrTokenNotFound
	}
	return err
}
