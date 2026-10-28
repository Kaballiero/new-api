package catalog

import (
	"github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	"github.com/QuantumNous/new-api/internal/catalog/domain"

	"gorm.io/gorm"
)

type PrefillStore struct{ db func() *gorm.DB }

func NewPrefillStore(db func() *gorm.DB) PrefillStore { return PrefillStore{db: db} }
func (s PrefillStore) Duplicate(id int, name string) (bool, error) {
	if name == "" {
		return false, nil
	}
	var count int64
	err := s.db().Model(&record.PrefillGroup{}).Where("name = ? AND id <> ?", name, id).Count(&count).Error
	return count > 0, err
}
func (s PrefillStore) Insert(value *domain.PrefillGroup) error {
	r := prefillRecord(value)
	err := s.db().Create(&r).Error
	*value = prefillValue(&r)
	return err
}
func (s PrefillStore) Save(value *domain.PrefillGroup) error {
	r := prefillRecord(value)
	err := s.db().Save(&r).Error
	*value = prefillValue(&r)
	return err
}
func (s PrefillStore) Delete(id int) error { return s.db().Delete(&record.PrefillGroup{}, id).Error }
func (s PrefillStore) List(kind string) ([]domain.PrefillGroup, error) {
	var rows []*record.PrefillGroup
	q := s.db().Model(&record.PrefillGroup{})
	if kind != "" {
		q = q.Where("type = ?", kind)
	}
	if err := q.Order("updated_time DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	if rows == nil {
		return nil, nil
	}
	out := make([]domain.PrefillGroup, len(rows))
	for i, v := range rows {
		out[i] = prefillValue(v)
	}
	return out, nil
}
func prefillRecord(v *domain.PrefillGroup) record.PrefillGroup {
	return record.PrefillGroup{Id: v.Id, Name: v.Name, Type: v.Type, Items: record.JSONValue(v.Items), Description: v.Description, CreatedTime: v.CreatedTime, UpdatedTime: v.UpdatedTime}
}
func prefillValue(v *record.PrefillGroup) domain.PrefillGroup {
	return domain.PrefillGroup{Id: v.Id, Name: v.Name, Type: v.Type, Items: []byte(v.Items), Description: v.Description, CreatedTime: v.CreatedTime, UpdatedTime: v.UpdatedTime}
}
