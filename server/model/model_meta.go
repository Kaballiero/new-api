package model

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/internal/adapter/gorm/catalog/record"
	"github.com/QuantumNous/new-api/internal/catalog/delete_models_batch"
	"github.com/QuantumNous/new-api/internal/catalog/domain"
	"github.com/QuantumNous/new-api/internal/catalog/insert_model_record"
	"github.com/QuantumNous/new-api/internal/catalog/list_configured_models"
	"github.com/QuantumNous/new-api/internal/catalog/update_model_record"
	kitdto "github.com/QuantumNous/new-api/relaykit/dto"
)

const (
	NameRuleExact = iota
	NameRulePrefix
	NameRuleContains
	NameRuleSuffix
)

type ModelSquareState = record.ModelSquareState

const (
	ModelSquareVisible     ModelSquareState = "visible"
	ModelSquareUnavailable ModelSquareState = "unavailable"
	ModelSquareHidden      ModelSquareState = "hidden"
	ModelSquarePartial     ModelSquareState = "partial"
)

type BoundChannel = record.BoundChannel

type Model record.Model

// MatchesName applies a metadata rule to a concrete channel model name.
func (mi *Model) MatchesName(name string) bool { return ((*record.Model)(mi)).MatchesName(name) }

// resolveModelMetadata preserves catalog precedence: exact, prefix, suffix,
// then contains. The first matching record within a rule type wins.
func resolveModelMetadata(records []Model, names []string) map[string]*Model {
	values := make([]domain.Model, len(records))
	reverse := make(map[*domain.Model]*Model, len(records))
	for i := range records {
		values[i] = *record.ToModel((*record.Model)(&records[i]))
		reverse[&values[i]] = &records[i]
	}
	out := make(map[string]*Model)
	for name, v := range domain.ResolveModelMetadata(values, names) {
		out[name] = reverse[v]
	}
	return out
}

// FillModelSquareStates applies the same metadata policy as the public catalog
// to live routes, then aggregates concrete models for metadata rule rows.
func FillModelSquareStates(rows []*Model, configured map[string][]int, connections []ModelConnection) error {
	metadata, err := NewCatalogPersistence().AllModelValues()
	if err != nil {
		return err
	}
	values := make([]*domain.Model, len(rows))
	for i, v := range rows {
		values[i] = record.ToModel((*record.Model)(v))
	}
	domainConnections := make([]domain.ModelConnection, len(connections))
	for i, v := range connections {
		domainConnections[i] = catalogConnectionValue(v)
	}
	if err := domain.FillModelSquareStates(values, configured, domainConnections, metadata); err != nil {
		return err
	}
	for i, v := range values {
		if v != nil {
			rows[i].SquareState = v.SquareState
		}
	}
	return nil
}

// GetConfiguredModelChannels includes disabled channels and reads no credentials.
func GetConfiguredModelChannels() (map[string][]int, error) {
	return NewCatalogPersistence().ConfiguredChannels()
}

// SearchModelsWithChannels augments metadata with concrete configured names.
// Synthetic rows never persist and never affect the public pricing catalog.
func SearchModelsWithChannels(keyword, vendor, status, syncOfficial string, offset, limit int) ([]*Model, int64, error) {
	values, n, err := list_configured_models.NewUsecase(catalogReadBridge{}).Execute(keyword, vendor, status, syncOfficial, offset, limit)
	if values == nil {
		return nil, n, err
	}
	out := make([]*Model, len(values))
	for i, v := range values {
		out[i] = (*Model)(record.FromModel(v))
	}
	return out, n, err
}

func (value *Model) Insert() error {
	native := (*record.Model)(value)
	input := record.ToModel(native)
	defer func() { saved := native.DeletedAt; *native = *record.FromModel(input); native.DeletedAt = saved }()
	return insert_model_record.NewUsecase(catalogMetadataWriteBridge{}, common.GetTimestamp).Execute(input)
}

func IsModelNameDuplicated(id int, name string) (bool, error) {
	return NewCatalogPersistence().IsModelNameDuplicated(id, name)
}

func (value *Model) Update() error {
	native := (*record.Model)(value)
	input := record.ToModel(native)
	defer func() { saved := native.DeletedAt; *native = *record.FromModel(input); native.DeletedAt = saved }()
	return update_model_record.NewUsecase(catalogMetadataWriteBridge{}, common.GetTimestamp).Execute(input)
}

func (mi *Model) Delete() error {
	_, err := DeleteModelMetadata([]int{mi.Id}, false, false)
	return err
}

type ModelDeleteResult = domain.ModelDeleteResult

// DeleteModelMetadata optionally removes exact model names from every channel.
// Channel removal requires exact-match metadata records. Pricing removal
// clears the selected names without expanding metadata matching rules.
func DeleteModelMetadata(ids []int, removeFromChannels, removePricing bool) (ModelDeleteResult, error) {
	return delete_models_batch.NewUsecase(NewCatalogPersistence(), InitChannelCache, RefreshPricing, func(delete_models_batch.Input, delete_models_batch.Output) {}).Execute(delete_models_batch.Input{ModelIDs: ids, RemoveFromChannels: removeFromChannels, RemovePricing: removePricing, AllowRemovePricing: true})
}

func GetVendorModelCounts() (map[int64]int64, error) {
	return NewCatalogPersistence().GetVendorModelCounts()
}

func GetAllModels(offset int, limit int) ([]*Model, error) {
	models, _, err := SearchModels("", "", "", "", offset, limit)
	return models, err
}

// ModelConnection describes an enabled route independently of catalog visibility or price.
type ModelConnection struct {
	AbilityWithChannel
	ChannelName string `json:"channel_name"`
}

func GetModelConnections() ([]ModelConnection, error) {
	values, err := NewCatalogPersistence().Connections()
	if values == nil {
		return nil, err
	}
	out := make([]ModelConnection, len(values))
	for i, v := range values {
		out[i] = ModelConnection{AbilityWithChannel: AbilityWithChannel{Ability: Ability{Group: v.Group, Model: v.Model, ChannelId: v.ChannelId, Enabled: v.Enabled, Priority: v.Priority, Weight: v.Weight, Tag: v.Tag}, ChannelType: v.ChannelType}, ChannelName: v.ChannelName}
	}
	return out, err
}

func normalizeLookupValues(values []string) []string { return domain.NormalizeLookupValues(values) }

func GetPreferredModelOwnerChannelTypes(modelNames []string, groups []string) (map[string]int, error) {
	result := make(map[string]int)
	modelNames = normalizeLookupValues(modelNames)
	if len(modelNames) == 0 {
		return result, nil
	}

	type row struct {
		Model       string
		ChannelType int
	}
	var rows []row

	query := DB.Table("abilities").
		Select("abilities.model as model, channels.type as channel_type").
		Joins("JOIN channels ON abilities.channel_id = channels.id").
		Where("abilities.model IN ? AND abilities.enabled = ? AND channels.status = ?", modelNames, true, common.ChannelStatusEnabled).
		Order("COALESCE(abilities.priority, 0) DESC").
		Order("abilities.weight DESC").
		Order("abilities.channel_id ASC")

	groups = normalizeLookupValues(groups)
	if len(groups) > 0 {
		query = query.Where("abilities."+commonGroupCol+" IN ?", groups)
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

func GetOpenRouterReserveBilledModels(modelNames []string, groups []string) (map[string]bool, error) {
	result := make(map[string]bool)
	modelNames = normalizeLookupValues(modelNames)
	groups = normalizeLookupValues(groups)
	if len(modelNames) == 0 || len(groups) == 0 {
		return result, nil
	}

	var rows []struct {
		Model        string
		Setting      *string
		ModelMapping *string
	}
	err := DB.Table("abilities").
		Select("abilities.model as model, channels.setting as setting, channels.model_mapping as model_mapping").
		Joins("JOIN channels ON abilities.channel_id = channels.id").
		Where("abilities.model IN ? AND abilities.enabled = ? AND channels.status = ? AND channels.type = ?", modelNames, true, common.ChannelStatusEnabled, constant.ChannelTypeOpenRouter).
		Where("abilities."+commonGroupCol+" IN ?", groups).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		if result[row.Model] || row.Setting == nil || strings.TrimSpace(*row.Setting) == "" {
			continue
		}
		var settings kitdto.ChannelSettings
		if common.UnmarshalJsonStr(*row.Setting, &settings) != nil {
			continue
		}
		upstream, ok := ResolvePricingUpstreamModel(&Channel{ModelMapping: row.ModelMapping}, row.Model)
		if !ok {
			continue
		}
		if reserve := settings.OpenRouterReserveUSD(upstream); reserve != nil && *reserve > 0 {
			result[row.Model] = true
		}
	}
	return result, nil
}

func SearchModels(keyword string, vendor string, status string, syncOfficial string, offset int, limit int) ([]*Model, int64, error) {
	values, n, e := NewCatalogPersistence().SearchModels(keyword, vendor, status, syncOfficial, offset, limit)
	if values == nil {
		return nil, n, e
	}
	out := make([]*Model, len(values))
	for i, v := range values {
		out[i] = (*Model)(v)
	}
	return out, n, e
}

// parseModelStatusFilter maps UI/API status values to the models.status column.
// Returns ok=false when no status filter should be applied.
func parseModelStatusFilter(value string) (int, bool) { return domain.ParseModelStatusFilter(value) }

// parseModelSyncFilter maps UI/API sync values to the models.sync_official column.
// Returns ok=false when no sync filter should be applied.
func parseModelSyncFilter(value string) (int, bool) { return domain.ParseModelSyncFilter(value) }
