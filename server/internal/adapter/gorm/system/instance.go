package system

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/system/record"
	"github.com/QuantumNous/new-api/internal/system/domain"

	"gorm.io/gorm/clause"
)

func (s Store) UpsertSystemInstance(nodeName string, info any, startedAt int64, lastSeenAt int64) error {
	infoText, err := marshalSystemInstanceInfo(info)
	if err != nil {
		return err
	}
	if lastSeenAt == 0 {
		lastSeenAt = common.GetTimestamp()
	}
	instance := &record.SystemInstance{
		NodeName:   nodeName,
		Info:       infoText,
		StartedAt:  startedAt,
		LastSeenAt: lastSeenAt,
		UpdatedAt:  lastSeenAt,
	}
	return s.DB().Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "node_name"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"info",
			"started_at",
			"last_seen_at",
			"updated_at",
		}),
	}).Create(instance).Error
}

func (s Store) listSystemInstances() ([]*record.SystemInstance, error) {
	var instances []*record.SystemInstance
	err := s.DB().Order("last_seen_at desc").Find(&instances).Error
	return instances, err
}

func (s Store) DeleteStaleSystemInstances(now int64) (int64, error) {
	result := s.DB().Where("last_seen_at < ?", now-domain.SystemInstanceStaleAfterSeconds).Delete(&record.SystemInstance{})
	return result.RowsAffected, result.Error
}

func (s Store) DeleteStaleSystemInstance(nodeName string, now int64) (bool, error) {
	result := s.DB().Where("node_name = ? AND last_seen_at < ?", nodeName, now-domain.SystemInstanceStaleAfterSeconds).Delete(&record.SystemInstance{})
	return result.RowsAffected > 0, result.Error
}

func marshalSystemInstanceInfo(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	data, err := common.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s Store) ListSystemInstances() ([]domain.Instance, error) {
	rows, err := s.listSystemInstances()
	if err != nil {
		return nil, err
	}
	out := make([]domain.Instance, 0, len(rows))
	for _, v := range rows {
		out = append(out, domain.Instance{NodeName: v.NodeName, StartedAt: v.StartedAt, LastSeenAt: v.LastSeenAt, Info: v.Info})
	}
	return out, nil
}
