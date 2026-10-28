package catalog

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	pricingstate "github.com/QuantumNous/new-api/internal/adapter/pricing/state"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	legacy "github.com/QuantumNous/new-api/model"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"

	"gorm.io/gorm"
)

type AvailabilityStore struct {
	db          func() *gorm.DB
	groupColumn func() string
}

func NewAvailabilityStore(db func() *gorm.DB, groupColumn func() string) AvailabilityStore {
	return AvailabilityStore{db: db, groupColumn: groupColumn}
}
func (s AvailabilityStore) PreferredOwners(modelNames []string, groups []string) (map[string]int, error) {
	result := make(map[string]int)
	modelNames = domain.NormalizeLookupValues(modelNames)
	if len(modelNames) == 0 {
		return result, nil
	}

	type row struct {
		Model       string
		ChannelType int
	}
	var rows []row

	query := s.db().Table("abilities").
		Select("abilities.model as model, channels.type as channel_type").
		Joins("JOIN channels ON abilities.channel_id = channels.id").
		Where("abilities.model IN ? AND abilities.enabled = ? AND channels.status = ?", modelNames, true, common.ChannelStatusEnabled).
		Order("COALESCE(abilities.priority, 0) DESC").
		Order("abilities.weight DESC").
		Order("abilities.channel_id ASC")

	groups = domain.NormalizeLookupValues(groups)
	if len(groups) > 0 {
		query = query.Where("abilities."+s.groupColumn()+" IN ?", groups)
	}

	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}

	for _, r := range rows {
		if _, ok := result[r.Model]; ok {
			continue
		}
		result[r.Model] = r.ChannelType
	}
	return result, nil
}

func (s AvailabilityStore) ReserveBilled(modelNames []string, groups []string) (map[string]bool, error) {
	reserves, err := s.ModelReserves(modelNames, groups)
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(reserves))
	for name := range reserves {
		result[name] = true
	}
	return result, nil
}

func (s AvailabilityStore) ModelReserves(modelNames []string, groups []string) (map[string]float64, error) {
	result := make(map[string]float64)
	modelNames = domain.NormalizeLookupValues(modelNames)
	groups = domain.NormalizeLookupValues(groups)
	if len(modelNames) == 0 || len(groups) == 0 {
		return result, nil
	}

	var rows []struct {
		Model        string
		Setting      *string
		ModelMapping *string
	}
	err := s.db().Table("abilities").
		Select("abilities.model as model, channels.setting as setting, channels.model_mapping as model_mapping").
		Joins("JOIN channels ON abilities.channel_id = channels.id").
		Where("abilities.model IN ? AND abilities.enabled = ? AND channels.status = ? AND channels.type = ?", modelNames, true, common.ChannelStatusEnabled, constant.ChannelTypeOpenRouter).
		Where("abilities."+s.groupColumn()+" IN ?", groups).
		Order("channels.id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		if _, seen := result[row.Model]; seen || row.Setting == nil || strings.TrimSpace(*row.Setting) == "" {
			continue
		}
		var settings kitdto.ChannelSettings
		if common.UnmarshalJsonStr(*row.Setting, &settings) != nil {
			continue
		}
		upstream, ok := pricingstate.ResolvePricingUpstreamModel(&legacy.Channel{ModelMapping: row.ModelMapping}, row.Model)
		if !ok {
			continue
		}
		if reserve := settings.OpenRouterReserveUSD(upstream); reserve != nil && *reserve > 0 {
			result[row.Model] = *reserve
		}
	}
	return result, nil
}
