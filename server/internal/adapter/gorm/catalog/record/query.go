package record

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"gorm.io/gorm"
	"strings"
)

func (store Store) AllModelValues() ([]domain.Model, error) {
	var rows []Model
	err := store.env.DB().Find(&rows).Error
	var out []domain.Model
	for i := range rows {
		out = append(out, *ToModel(&rows[i]))
	}
	return out, err
}
func (store Store) ConfiguredChannels() (map[string][]int, error) {
	var rows []struct {
		ID     int
		Models string
	}
	if err := store.env.DB().Table("channels").Select("id", "models").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string][]int)
	for _, v := range rows {
		var names []string
		if v.Models != "" {
			names = strings.Split(strings.Trim(v.Models, ","), ",")
		}
		for _, name := range domain.NormalizeLookupValues(names) {
			out[name] = append(out[name], v.ID)
		}
	}
	return out, nil
}
func (store Store) Connections() ([]domain.ModelConnection, error) {
	var values []domain.ModelConnection
	err := store.env.DB().Table("abilities").Select("abilities.*, channels.type as channel_type, channels.name as channel_name").Joins("JOIN channels ON abilities.channel_id = channels.id").Where("abilities.enabled = ? AND channels.status = ?", true, common.ChannelStatusEnabled).Order("abilities.model, abilities.channel_id").Scan(&values).Error
	return values, err
}
func (store Store) ExactNames() ([]string, error) {
	var values []string
	err := store.env.DB().Model(&Model{}).Where("name_rule = ?", domain.NameRuleExact).Pluck("model_name", &values).Error
	return values, err
}
func (store Store) ExistingNames(names []string) ([]string, error) {
	var values []string
	err := store.env.DB().Model(&Model{}).Where("model_name IN ?", names).Pluck("model_name", &values).Error
	return values, err
}
func (store Store) SyncStateOn(db *gorm.DB) (map[string]*domain.Model, map[string]*domain.Vendor, error) {
	return (syncMutation{store: store, db: db}).State()
}

func (store Store) Model(id int) (*Model, error) {
	var v Model
	err := store.env.DB().First(&v, id).Error
	return &v, err
}
func (store Store) ModelExists(id int) error {
	var v Model
	return store.env.DB().Select("id").First(&v, id).Error
}
func (store Store) SetModelStatus(id, status int) error {
	return store.env.DB().Model(&Model{}).Where("id = ?", id).Update("status", status).Error
}
