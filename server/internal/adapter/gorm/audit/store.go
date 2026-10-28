package audit

import (
	"context"
	"errors"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/audit/record"
	"github.com/QuantumNous/new-api/internal/audit/domain"
	"github.com/QuantumNous/new-api/internal/audit/dto"

	clickhouse "github.com/ClickHouse/clickhouse-go/v2"
	"gorm.io/gorm"
)

type Store struct {
	database func() *gorm.DB
	username func(int) string
}

func NewStore(database func() *gorm.DB, username func(int) string) Store {
	return Store{database: database, username: username}
}

func (s Store) Username(id int) string {
	return s.username(id)
}

func (s Store) Append(_ context.Context, event domain.Event) error {
	db := s.database()
	if db == nil {
		return errors.New("log database unavailable")
	}
	entry := eventRecord(event)
	row, err := record.InsertRow(&entry)
	if err != nil {
		return err
	}
	return db.Table("audit_logs").Create(row).Error
}

func (s Store) List(filter domain.Query, policy domain.VisibilityPolicy) ([]*dto.Event, int64, error) {
	query := s.database().Model(&record.AuditLog{})
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		// Decode native JSON through database/sql as text for AuditOther.Scan.
		// Preserve numeric metadata instead of returning quoted Int64 values.
		query = query.WithContext(clickhouse.Context(query.Statement.Context, clickhouse.WithSettings(clickhouse.Settings{
			"output_format_native_write_json_as_string": 1,
			"output_format_json_quote_64bit_integers":   0,
		})))
	}
	if policy.ActorRoles != nil {
		query = query.Where("actor_role IN ?", policy.ActorRoles)
	}
	if filter.UserId > 0 {
		query = query.Where("user_id = ?", filter.UserId)
	}
	if filter.Username != "" {
		query = query.Where("username = ?", filter.Username)
	}
	if filter.Category != "" {
		query = query.Where("category = ?", filter.Category)
	}
	if filter.TokenRef != "" {
		query = query.Where("token_ref = ?", filter.TokenRef)
	}
	if filter.ExcludeTokenRef != "" {
		query = query.Where("token_ref <> ?", filter.ExcludeTokenRef)
	}
	if filter.RequestId != "" {
		query = query.Where("request_id = ?", filter.RequestId)
	}
	if filter.StartTimestamp > 0 {
		query = query.Where("created_at >= ?", filter.StartTimestamp)
	}
	if filter.EndTimestamp > 0 {
		query = query.Where("created_at <= ?", filter.EndTimestamp)
	}
	if filter.Success != nil {
		query = query.Where("success = ?", *filter.Success)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	logs := make([]*record.AuditLog, 0)
	if err := query.Order("created_at DESC").Order("event_id DESC").Offset(filter.Offset).Limit(filter.Limit).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	return rowsDTO(logs, policy), total, nil
}
