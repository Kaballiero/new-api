package pricing

import (
	catalogrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	catalogdomain "github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

type Ability struct {
	ChannelId    int
	Model, Group string
	Enabled      bool
}
type Observations struct{ store Catalogue }

func NewObservations(store Catalogue) Observations { return Observations{store: store} }
func (s Observations) Fingerprint() (string, error) {
	db := s.store.env.DB()
	var metadata []catalogrecord.Model
	if err := db.Select("id", "model_name", "description", "icon", "tags", "vendor_id", "endpoints", "status", "name_rule").Find(&metadata).Error; err != nil {
		return "", err
	}
	var vendors []catalogrecord.Vendor
	if err := db.Select("id", "name", "description", "icon").Find(&vendors).Error; err != nil {
		return "", err
	}
	var channels []*channelrecord.Channel
	if err := db.Select("id", "type", "status", "models", "model_mapping", "group", "base_url", "key", "setting", "settings", "param_override", "header_override").Find(&channels).Error; err != nil {
		return "", err
	}
	var abilities []domain.BuildAbility
	if err := db.Model(&Ability{}).Select([]string{"channel_id", "model", "group"}).Where("enabled = ?", true).Find(&abilities).Error; err != nil {
		return "", err
	}
	m := make([]catalogdomain.Model, len(metadata))
	for i := range metadata {
		m[i] = *catalogrecord.ToModel(&metadata[i])
	}
	v := make([]catalogdomain.Vendor, len(vendors))
	for i := range vendors {
		v[i] = *catalogrecord.ToVendor(&vendors[i])
	}
	c := make([]*domain.BuildChannel, len(channels))
	for i, row := range channels {
		c[i] = (*domain.BuildChannel)(channelrecord.ToDomain(row))
	}
	return s.store.InputFingerprint(m, v, c, abilities)
}
