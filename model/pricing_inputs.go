package model

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"sync/atomic"

	"github.com/QuantumNous/new-api/common"
)

// Cross-node metadata/channel writes have no local invalidation event. Poll
// only persisted projection inputs; timestamps, quota counters and priorities
// do not trigger the expensive pricing projection.
var pricingObservedSource atomic.Pointer[string]
var pricingObservedChannels atomic.Pointer[string]

func pricingPersistedInputFingerprint(metadata []Model, vendors []Vendor, channels []*Channel, abilities []AbilityWithChannel) (string, error) {
	byID := make(map[int]*Channel, len(channels))
	for _, channel := range channels {
		byID[channel.Id] = channel
	}
	rows := []string{}
	names := []string{}
	used := make(map[int]bool)
	for _, ability := range abilities {
		channel := byID[ability.ChannelId]
		value, err := common.Marshal(struct {
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
			identity, err := pricingChannelInputIdentity(channel)
			if err != nil {
				return "", err
			}
			rows = append(rows, "channel:"+identity)
			used[channel.Id] = true
		}
	}
	for name, record := range resolveModelMetadata(metadata, names) {
		value, err := common.Marshal(struct {
			Name   string
			Record Model
		}{name, Model{ModelName: record.ModelName, Description: record.Description, Icon: record.Icon, Tags: record.Tags, VendorID: record.VendorID, Endpoints: record.Endpoints, Status: record.Status, NameRule: record.NameRule}})
		if err != nil {
			return "", err
		}
		rows = append(rows, "model:"+string(value))
	}
	for _, record := range vendors {
		value, err := common.Marshal(PricingVendor{ID: record.Id, Name: record.Name, Description: record.Description, Icon: record.Icon})
		if err != nil {
			return "", err
		}
		rows = append(rows, "vendor:"+string(value))
	}
	slices.Sort(rows)
	value, err := common.Marshal(rows)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(value)), nil
}

func syncPricingCatalogueInputs() {
	generation, observed := pricingInputsGeneration.Load(), pricingObservedSource.Load()
	var metadata []Model
	if err := DB.Select("id", "model_name", "description", "icon", "tags", "vendor_id", "endpoints", "status", "name_rule").Find(&metadata).Error; err != nil {
		return
	}
	var vendors []Vendor
	if err := DB.Select("id", "name", "description", "icon").Find(&vendors).Error; err != nil {
		return
	}
	var channels []*Channel
	if err := DB.Select("id", "type", "status", "models", "model_mapping", "group", "base_url", "key", "setting", "settings", "param_override", "header_override").Find(&channels).Error; err != nil {
		return
	}
	var abilities []AbilityWithChannel
	if err := DB.Model(&Ability{}).Select([]string{"channel_id", "model", "group"}).Where("enabled = ?", true).Find(&abilities).Error; err != nil {
		return
	}
	fingerprint, err := pricingPersistedInputFingerprint(metadata, vendors, channels, abilities)
	if err != nil {
		return
	}
	pricingPublicationLock.Lock()
	defer pricingPublicationLock.Unlock()
	// A newer notification/publication supersedes this poll's captured inputs.
	if generation != pricingInputsGeneration.Load() || observed != pricingObservedSource.Load() {
		return
	}
	if observed != nil && *observed == fingerprint {
		return
	}
	pricingObservedSource.Store(&fingerprint)
	pricingInputsGeneration.Add(1)
}

func pricingChannelInputIdentity(channel *Channel) (string, error) {
	if channel.Status == common.ChannelStatusEnabled {
		// Credentials/overrides affect catalog source identity; expose only a digest.
		return pricingCatalogFingerprint(channel, "", "", []any{channel.Models, channel.GetModelMapping()}), nil
	}
	value, err := common.Marshal(struct {
		ID, Type, Status int
		Mapping          string
	}{channel.Id, channel.Type, channel.Status, channel.GetModelMapping()})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(value)), nil
}
