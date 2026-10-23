package record

import "github.com/QuantumNous/new-api/common"

// InsertRow binds JSON explicitly because native ClickHouse inserts bypass Valuer.
func InsertRow(entry *AuditLog) (any, error) {
	if !common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return entry, nil
	}
	encoded, err := common.Marshal(entry.Other)
	if err != nil {
		return nil, err
	}
	return &struct {
		AuditLog     `gorm:"embedded"`
		EncodedOther string `gorm:"column:other;type:json"`
	}{AuditLog: *entry, EncodedOther: string(encoded)}, nil
}
