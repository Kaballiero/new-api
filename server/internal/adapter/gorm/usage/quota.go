package usage

import (
	"github.com/QuantumNous/new-api/internal/usage/domain"
	"github.com/QuantumNous/new-api/internal/usage/dto"
	"github.com/QuantumNous/new-api/model"
)

func getQuotaDataByUsername(username string, startTime int64, endTime int64) (quotaData []*dto.QuotaData, err error) {
	var quotaDatas []*dto.QuotaData
	err = model.DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("username = ? and created_at >= ? and created_at <= ?", username, startTime, endTime).
		Group("user_id, username, model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func getQuotaDataByUserId(userId int, startTime int64, endTime int64, tokenId int) (quotaData []*dto.QuotaData, err error) {
	var quotaDatas []*dto.QuotaData
	query := model.DB.Table("quota_data").
		Select("user_id, username, model_name, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("user_id = ? and created_at >= ? and created_at <= ?", userId, startTime, endTime)
	if tokenId > 0 {
		query = query.Where("token_id = ?", tokenId)
	}
	err = query.Group("user_id, username, model_name, created_at").Order("created_at, model_name").Find(&quotaDatas).Error
	return quotaDatas, err
}

func getQuotaDataGroupByUser(startTime int64, endTime int64) (quotaData []*dto.QuotaData, err error) {
	var quotaDatas []*dto.QuotaData
	err = model.DB.Table("quota_data").
		Select("username, created_at, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used").
		Where("created_at >= ? and created_at <= ?", startTime, endTime).
		Group("username, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func getAllQuotaDates(startTime int64, endTime int64) (quotaData []*dto.QuotaData, err error) {
	var quotaDatas []*dto.QuotaData
	err = model.DB.Table("quota_data").
		Select("model_name, sum(count) as count, sum(quota) as quota, sum(token_used) as token_used, created_at").
		Where("created_at >= ? and created_at <= ?", startTime, endTime).
		Group("model_name, created_at").
		Find(&quotaDatas).Error
	return quotaDatas, err
}

func (Store) LegacyQuota(q domain.QuotaQuery) ([]*dto.QuotaData, error) {
	switch q.Mode {
	case domain.QuotaUsername:
		return getQuotaDataByUsername(q.Username, q.StartTime, q.EndTime)
	case domain.QuotaOwner:
		return getQuotaDataByUserId(q.UserID, q.StartTime, q.EndTime, q.TokenID)
	case domain.QuotaUsers:
		return getQuotaDataGroupByUser(q.StartTime, q.EndTime)
	default:
		return getAllQuotaDates(q.StartTime, q.EndTime)
	}
}
