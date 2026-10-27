package domain

import (
	"crypto/sha256"
	"fmt"
	"github.com/QuantumNous/new-api/constant"
	catalogdomain "github.com/QuantumNous/new-api/internal/catalog/domain"
	"slices"
)

func PersistedInputFingerprint(deps Capabilities, metadata []BuildModel, vendors []BuildVendor, channels []*BuildChannel, abilities []BuildAbility, catalogRevision string) (string, error) {
	byID := make(map[int]*BuildChannel, len(channels))
	for _, channel := range channels {
		byID[channel.Id] = channel
	}

	rows := []string{"upstream_catalog:" + catalogRevision}
	names := []string{}
	used := make(map[int]bool)
	for _, ability := range abilities {
		channel := byID[ability.ChannelId]
		value, err := deps.Codec.Marshal(struct {
			Group, Model string
			ChannelID    int
		}{ability.Group, ability.Model, ability.ChannelId})
		if err != nil {
			return "", err
		}
		rows = append(rows, "ability:"+string(value))
		names = append(names, ability.Model)
		if !used[ability.ChannelId] {
			if channel == nil {
				rows = append(rows, fmt.Sprintf("channel:missing:%d", ability.ChannelId))
				used[ability.ChannelId] = true
				continue
			}
			identity, err := ChannelInputIdentity(deps, channel)
			if err != nil {
				return "", err
			}
			rows = append(rows, "channel:"+identity)
			used[channel.Id] = true
		}
	}
	for name, record := range catalogdomain.ResolveModelMetadata(metadata, names) {
		value, err := deps.Codec.Marshal(struct {
			Name   string
			Record BuildModel
		}{name, BuildModel{ModelName: record.ModelName, Description: record.Description, Icon: record.Icon, Tags: record.Tags, VendorID: record.VendorID, Endpoints: record.Endpoints, Status: record.Status, NameRule: record.NameRule}})
		if err != nil {
			return "", err
		}
		rows = append(rows, "model:"+string(value))
	}
	for _, record := range vendors {
		value, err := deps.Codec.Marshal(PricingVendor{ID: record.Id, Name: record.Name, Description: record.Description, Icon: record.Icon})
		if err != nil {
			return "", err
		}
		rows = append(rows, "vendor:"+string(value))
	}
	slices.Sort(rows)
	value, err := deps.Codec.Marshal(rows)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(value)), nil
}
func ChannelInputIdentity(deps Capabilities, channel *BuildChannel) (string, error) {
	if channel.Status == constant.ChannelStatusEnabled {

		return PricingCatalogFingerprint(deps, channel, "", "", []any{channel.Models, channel.GetModelMapping()}), nil
	}
	value, err := deps.Codec.Marshal(struct {
		ID, Type, Status int
		Mapping          string
	}{channel.Id, channel.Type, channel.Status, channel.GetModelMapping()})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(value)), nil
}
