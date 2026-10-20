package controller

import (
	repository "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/internal/channels/update_channel"
	"github.com/QuantumNous/new-api/model"
)

var channelSensitiveFields = update_channel.SensitiveFields
var channelOperationalFields = update_channel.OperationalFields
var channelReadOnlyFields = update_channel.ReadOnlyFields
var channelNonSensitiveFields = update_channel.NonSensitiveFields

func channelHasSensitiveChanges(channel *PatchChannel, origin *model.Channel, fields map[string]any) bool {
	patch := update_channel.PatchChannel{Channel: *repository.ToDomain(&channel.Channel), MultiKeyMode: channel.MultiKeyMode, KeyMode: channel.KeyMode}
	return update_channel.HasSensitiveChanges(&patch, repository.ToDomain(origin), fields)
}
func clearChannelReadOnlyFields(channel *PatchChannel, fields map[string]any) {
	patch := update_channel.PatchChannel{Channel: *repository.ToDomain(&channel.Channel), MultiKeyMode: channel.MultiKeyMode, KeyMode: channel.KeyMode}
	update_channel.ClearReadOnlyFields(&patch, fields)
	channel.Channel = *repository.FromDomain(&patch.Channel)
}
