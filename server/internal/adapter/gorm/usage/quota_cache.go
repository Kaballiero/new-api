package usage

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/usage/record"
	"github.com/QuantumNous/new-api/model"

	"gorm.io/gorm"
)

func (Store) SaveQuotaDataCache() {
	FlushQuotaDataCache(func(row *record.QuotaData) {
		existing := &record.QuotaData{}
		model.DB.Table("quota_data").Where(
			"user_id = ? and username = ? and model_name = ? and created_at = ? and use_group = ? and token_id = ? and channel_id = ? and node_name = ?",
			row.UserID, row.Username, row.ModelName, row.CreatedAt, row.UseGroup, row.TokenID, row.ChannelID, row.NodeName).First(existing)
		if existing.Id > 0 {
			increaseQuotaData(row)
		} else {
			model.DB.Table("quota_data").Create(row)
		}
	}, func(size int) { common.SysLog(fmt.Sprintf("dashboard usage data saved: %d rows", size)) })
}

func increaseQuotaData(quotaData *record.QuotaData) {
	err := model.DB.Table("quota_data").
		Where("user_id = ? and username = ? and model_name = ? and created_at = ? and use_group = ? and token_id = ? and channel_id = ? and node_name = ?",
			quotaData.UserID, quotaData.Username, quotaData.ModelName, quotaData.CreatedAt,
			quotaData.UseGroup, quotaData.TokenID, quotaData.ChannelID, quotaData.NodeName).
		Updates(map[string]any{
			"count":      gorm.Expr("count + ?", quotaData.Count),
			"quota":      gorm.Expr("quota + ?", quotaData.Quota),
			"token_used": gorm.Expr("token_used + ?", quotaData.TokenUsed),
		}).Error
	if err != nil {
		common.SysLog(fmt.Sprintf("increaseQuotaData error: %s", err))
	}
}
