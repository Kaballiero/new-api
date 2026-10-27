package model_test

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	channelrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/channels/record"
	pricingruntime "github.com/QuantumNous/new-api/internal/app/pricingruntime"

	catalogstore "github.com/QuantumNous/new-api/internal/adapter/gorm/catalog"

	legacy "github.com/QuantumNous/new-api/model"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func clearPreferredOwnerTables(t *testing.T) {
	t.Helper()
	require.NoError(t, legacy.DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, legacy.DB.Exec("DELETE FROM channels").Error)
}

func insertPreferredOwnerCandidate(
	t *testing.T,
	channelID int,
	modelName string,
	group string,
	channelType int,
	priority int64,
	weight uint,
	channelStatus int,
	abilityEnabled bool,
) {
	t.Helper()
	require.NoError(t, legacy.DB.Create(&legacy.Channel{
		Id:     channelID,
		Type:   channelType,
		Key:    fmt.Sprintf("key-%d", channelID),
		Status: channelStatus,
		Name:   fmt.Sprintf("channel-%d", channelID),
	}).Error)
	require.NoError(t, legacy.DB.Create(&channelrecord.Ability{
		Group:     group,
		Model:     modelName,
		ChannelId: channelID,
		Enabled:   abilityEnabled,
		Priority:  &priority,
		Weight:    weight,
	}).Error)
}

func TestGetPreferredModelOwnerChannelTypes(t *testing.T) {
	const modelName = "gpt-5.4"

	tests := []struct {
		name     string
		setup    func(t *testing.T)
		groups   []string
		expected int
		found    bool
	}{
		{
			name: "openai only",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 1, modelName, "default", constant.ChannelTypeOpenAI, 0, 0, common.ChannelStatusEnabled, true)
			},
			groups:   []string{"default"},
			expected: constant.ChannelTypeOpenAI,
			found:    true,
		},
		{
			name: "codex only",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 1, modelName, "default", constant.ChannelTypeCodex, 0, 0, common.ChannelStatusEnabled, true)
			},
			groups:   []string{"default"},
			expected: constant.ChannelTypeCodex,
			found:    true,
		},
		{
			name: "priority wins",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 1, modelName, "default", constant.ChannelTypeOpenAI, 1, 100, common.ChannelStatusEnabled, true)
				insertPreferredOwnerCandidate(t, 2, modelName, "default", constant.ChannelTypeCodex, 2, 0, common.ChannelStatusEnabled, true)
			},
			groups:   []string{"default"},
			expected: constant.ChannelTypeCodex,
			found:    true,
		},
		{
			name: "weight wins when priority is equal",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 1, modelName, "default", constant.ChannelTypeOpenAI, 1, 10, common.ChannelStatusEnabled, true)
				insertPreferredOwnerCandidate(t, 2, modelName, "default", constant.ChannelTypeCodex, 1, 20, common.ChannelStatusEnabled, true)
			},
			groups:   []string{"default"},
			expected: constant.ChannelTypeCodex,
			found:    true,
		},
		{
			name: "channel id stabilizes exact ties",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 2, modelName, "default", constant.ChannelTypeCodex, 1, 10, common.ChannelStatusEnabled, true)
				insertPreferredOwnerCandidate(t, 1, modelName, "default", constant.ChannelTypeOpenAI, 1, 10, common.ChannelStatusEnabled, true)
			},
			groups:   []string{"default"},
			expected: constant.ChannelTypeOpenAI,
			found:    true,
		},
		{
			name: "group filter excludes other groups",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 1, modelName, "vip", constant.ChannelTypeCodex, 10, 100, common.ChannelStatusEnabled, true)
				insertPreferredOwnerCandidate(t, 2, modelName, "default", constant.ChannelTypeOpenAI, 1, 0, common.ChannelStatusEnabled, true)
			},
			groups:   []string{"default"},
			expected: constant.ChannelTypeOpenAI,
			found:    true,
		},
		{
			name: "disabled candidates are ignored",
			setup: func(t *testing.T) {
				insertPreferredOwnerCandidate(t, 1, modelName, "default", constant.ChannelTypeCodex, 10, 100, common.ChannelStatusEnabled, false)
				insertPreferredOwnerCandidate(t, 2, modelName, "default", constant.ChannelTypeOpenAI, 1, 0, common.ChannelStatusManuallyDisabled, true)
			},
			groups: []string{"default"},
			found:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearPreferredOwnerTables(t)
			tt.setup(t)

			owners, err := preferredModelOwnerChannelTypes([]string{modelName}, tt.groups)
			require.NoError(t, err)

			got, ok := owners[modelName]
			require.Equal(t, tt.found, ok)
			if tt.found {
				require.Equal(t, tt.expected, got)
			}
		})
	}
}

func preferredModelOwnerChannelTypes(names, groups []string) (map[string]int, error) {
	return catalogstore.NewAvailabilityStore(func() *gorm.DB { return legacy.DB }, pricingruntime.NewCatalogPersistence().GroupColumn).PreferredOwners(names, groups)
}

func TestCatalogAvailabilityUsesLateGroupColumn(t *testing.T) {
	clearPreferredOwnerTables(t)
	insertPreferredOwnerCandidate(t, 1, "column-fixture", "default", constant.ChannelTypeOpenRouter, 1, 1, common.ChannelStatusEnabled, true)
	store := catalogstore.NewAvailabilityStore(func() *gorm.DB { return legacy.DB }, pricingruntime.NewCatalogPersistence().GroupColumn)
	before := *legacy.CatalogGroupColumn
	t.Cleanup(func() { *legacy.CatalogGroupColumn = before })
	owners, err := store.PreferredOwners([]string{"column-fixture"}, []string{"default"})
	require.NoError(t, err)
	require.Equal(t, constant.ChannelTypeOpenRouter, owners["column-fixture"])
	*legacy.CatalogGroupColumn = `"missing_fixture_group"`
	_, err = store.PreferredOwners([]string{"column-fixture"}, []string{"default"})
	require.Error(t, err)
	_, err = store.ReserveBilled([]string{"column-fixture"}, []string{"default"})
	require.Error(t, err)
	*legacy.CatalogGroupColumn = before
	_, err = store.ReserveBilled([]string{"column-fixture"}, []string{"default"})
	require.NoError(t, err)
}
