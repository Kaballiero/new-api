package channels

import legacy "github.com/QuantumNous/new-api/model"

type ChannelStateSnapshot struct {
	ID        int
	Type      int
	Status    int
	Name      string
	OtherInfo string
	Balance   float64
}

func ListChannelStateSnapshots() ([]ChannelStateSnapshot, error) {
	var snapshots []ChannelStateSnapshot
	err := legacy.DB.Model(&legacy.Channel{}).
		Select("id", "type", "status", "name", "other_info", "balance").
		Order("id ASC").
		Find(&snapshots).Error
	return snapshots, err
}
