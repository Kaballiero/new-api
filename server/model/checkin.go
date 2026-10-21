package model

import (
	checkincommand "github.com/QuantumNous/new-api/internal/accounts/check_in"
	"github.com/QuantumNous/new-api/internal/accounts/domain"
	checkinquery "github.com/QuantumNous/new-api/internal/accounts/get_checkin_status"
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/accounts/record"
	checkinsettings "github.com/QuantumNous/new-api/internal/adapter/settings/checkin"
)

// Checkin 签到记录
type Checkin record.Checkin

// CheckinRecord 用于API返回的签到记录（不包含敏感字段）
type CheckinRecord struct {
	CheckinDate  string `json:"checkin_date"`
	QuotaAwarded int    `json:"quota_awarded"`
}

func (Checkin) TableName() string {
	return "checkins"
}

// GetUserCheckinRecords 获取用户在指定日期范围内的签到记录
func GetUserCheckinRecords(userId int, startDate, endDate string) ([]Checkin, error) {
	var records []Checkin
	err := DB.Where("user_id = ? AND checkin_date >= ? AND checkin_date <= ?",
		userId, startDate, endDate).
		Order("checkin_date DESC").
		Find(&records).Error
	return records, err
}

// HasCheckedInToday 检查用户今天是否已签到
func HasCheckedInToday(userId int) (bool, error) {
	return checkinquery.NewUsecase(NewCheckinStore()).HasCheckedInToday(userId)
}

// UserCheckin 执行用户签到
// MySQL 和 PostgreSQL 使用事务保证原子性
// SQLite 不支持嵌套事务，使用顺序操作 + 手动回滚
func UserCheckin(userId int) (*Checkin, error) {
	record, err := checkincommand.NewUsecase(NewCheckinStore(), checkinsettings.Current).Execute(userId)
	return (*Checkin)(record), err
}

// GetUserCheckinStats 获取用户签到统计信息
func GetUserCheckinStats(userId int, month string) (map[string]any, error) {
	stats, err := checkinquery.NewUsecase(NewCheckinStore()).Execute(userId, month)
	if err != nil {
		return nil, err
	}
	values := stats["records"].([]domain.CheckinRecord)
	records := make([]CheckinRecord, len(values))
	for i, value := range values {
		records[i] = CheckinRecord(value)
	}
	stats["records"] = records
	return stats, nil
}
