package channels

import legacy "github.com/QuantumNous/new-api/model"

type ChannelStateSnapshot struct {
	ID     int
	Type   int
	Status int
}

func ListChannelStateSnapshots() ([]ChannelStateSnapshot, error) {
	var snapshots []ChannelStateSnapshot
	err := legacy.DB.Model(&legacy.Channel{}).
		Select("id", "type", "status").
		Order("id ASC").
		Find(&snapshots).Error
	return snapshots, err
}
