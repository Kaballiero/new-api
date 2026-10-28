package pricing

import (
	"context"

	"github.com/QuantumNous/new-api/constant"
	catalogrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	catalogdomain "github.com/QuantumNous/new-api/internal/catalog/domain"
	channeldomain "github.com/QuantumNous/new-api/internal/channels/domain"
	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"gorm.io/gorm"
)

type CatalogueEnvironment struct {
	DB                func() *gorm.DB
	Abilities         func() ([]domain.BuildAbility, error)
	Fingerprint       func([]catalogdomain.Model, []catalogdomain.Vendor, []*domain.BuildChannel, []domain.BuildAbility) (string, error)
	ConfiguredCatalog func(context.Context) (map[string]*domain.SourceDocument, map[string]string, bool, error)
	CatalogEntry      func(*domain.BuildChannel, string, constant.EndpointType) (domain.PricingCatalogEntry, bool)
}
type Catalogue struct{ env CatalogueEnvironment }

func NewCatalogue(env CatalogueEnvironment) Catalogue         { return Catalogue{env: env} }
func (s Catalogue) Abilities() ([]domain.BuildAbility, error) { return s.env.Abilities() }
func (s Catalogue) Metadata() ([]catalogdomain.Model, error) {
	var rows []catalogrecord.Model
	if err := s.env.DB().Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]catalogdomain.Model, len(rows))
	for i := range rows {
		out[i] = *catalogrecord.ToModel(&rows[i])
	}
	return out, nil
}
func (s Catalogue) Vendors() ([]catalogdomain.Vendor, error) {
	var rows []catalogrecord.Vendor
	if err := s.env.DB().Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]catalogdomain.Vendor, len(rows))
	for i := range rows {
		out[i] = *catalogrecord.ToVendor(&rows[i])
	}
	return out, nil
}
func (s Catalogue) Channels() ([]*domain.BuildChannel, error) {
	var rows []*channelrecord.Channel
	if err := s.env.DB().Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]*domain.BuildChannel, len(rows))
	for i, v := range rows {
		out[i] = (*domain.BuildChannel)(channelrecord.ToDomain(v))
	}
	return out, nil
}
func (s Catalogue) InputFingerprint(m []catalogdomain.Model, v []catalogdomain.Vendor, c []*domain.BuildChannel, a []domain.BuildAbility) (string, error) {
	return s.env.Fingerprint(m, v, c, a)
}
func (s Catalogue) ConfiguredCatalog(ctx context.Context) (map[string]*domain.SourceDocument, map[string]string, bool, error) {
	return s.env.ConfiguredCatalog(ctx)
}
func (s Catalogue) CatalogEntry(ch *domain.BuildChannel, upstream string, endpoint constant.EndpointType) (domain.PricingCatalogEntry, bool) {
	return s.env.CatalogEntry(ch, upstream, endpoint)
}
func FromBuildChannel(c *domain.BuildChannel) *channelrecord.Channel {
	return channelrecord.FromDomain((*channeldomain.Channel)(c))
}
