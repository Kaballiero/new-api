package record

import (
	"database/sql/driver"
	"encoding/json"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// JSONValue preserves JSON bytes returned as either bytes or text by database drivers.
type JSONValue json.RawMessage

// Value returns text because PostgreSQL simple protocol encodes []byte as bytea,
// which fails for a JSON column with SQLSTATE 22P02.
func (j JSONValue) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	return string(j), nil
}

func (j *JSONValue) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*j = nil
		return nil
	case []byte:
		// Copy driver bytes instead of retaining its reusable buffer.
		b := make([]byte, len(v))
		copy(b, v)
		*j = JSONValue(b)
		return nil
	case string:
		*j = JSONValue([]byte(v))
		return nil
	default:
		// Serialize other driver values as JSON.
		b, err := common.Marshal(v)
		if err != nil {
			return err
		}
		*j = JSONValue(b)
		return nil
	}
}

func (j JSONValue) MarshalJSON() ([]byte, error) {
	if j == nil {
		return []byte("null"), nil
	}
	return j, nil
}

func (j *JSONValue) UnmarshalJSON(data []byte) error {
	if data == nil {
		*j = nil
		return nil
	}
	b := make([]byte, len(data))
	copy(b, data)
	*j = JSONValue(b)
	return nil
}

type PrefillGroup struct {
	Id          int            `json:"id"`
	Name        string         `json:"name" gorm:"size:64;not null;uniqueIndex:uk_prefill_name,where:deleted_at IS NULL"`
	Type        string         `json:"type" gorm:"size:32;index;not null"`
	Items       JSONValue      `json:"items" gorm:"type:json"`
	Description string         `json:"description,omitempty" gorm:"type:varchar(255)"`
	CreatedTime int64          `json:"created_time" gorm:"bigint"`
	UpdatedTime int64          `json:"updated_time" gorm:"bigint"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}
