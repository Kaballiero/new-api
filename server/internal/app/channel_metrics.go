package app

import (
	"net/http"

	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	"github.com/QuantumNous/new-api/service"
)

func NewChannelStateMetricsHandler() http.Handler {
	return service.NewChannelStateMetricsHandler(func() ([]service.ChannelStateSnapshot, error) {
		rows, err := channelstore.ListChannelStateSnapshots()
		var snapshots []service.ChannelStateSnapshot
		if rows != nil {
			snapshots = make([]service.ChannelStateSnapshot, len(rows))
			for i, row := range rows {
				snapshots[i] = service.ChannelStateSnapshot{ID: row.ID, Type: row.Type, Status: row.Status}
			}
		}
		return snapshots, err
	})
}
