package systemmetrics

import (
	systemmetrics "github.com/QuantumNous/new-api/internal/adapter/runtime/system"
	"net/http"

	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
)

func NewChannelStateMetricsHandler() http.Handler {
	return systemmetrics.NewChannelStateMetricsHandler(func() ([]systemmetrics.ChannelStateSnapshot, error) {
		rows, err := channelstore.ListChannelStateSnapshots()
		var snapshots []systemmetrics.ChannelStateSnapshot
		if rows != nil {
			snapshots = make([]systemmetrics.ChannelStateSnapshot, len(rows))
			for i, row := range rows {
				snapshots[i] = systemmetrics.ChannelStateSnapshot{ID: row.ID, Type: row.Type, Status: row.Status}
			}
		}
		return snapshots, err
	})
}
