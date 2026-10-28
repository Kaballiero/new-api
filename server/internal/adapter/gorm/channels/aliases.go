package channels

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/adapter/taskplugins/aliases"
	legacy "github.com/QuantumNous/new-api/model"
)

var TaskAliases = aliases.NewView(aliasChannels)

func aliasChannels() ([]legacy.Channel, error) {
	if legacy.DB == nil {
		return nil, nil
	}
	var rows []legacy.Channel
	err := legacy.DB.Select("id", "type", "models", "model_mapping").Where("status = ?", common.ChannelStatusEnabled).Find(&rows).Error
	return rows, err
}
