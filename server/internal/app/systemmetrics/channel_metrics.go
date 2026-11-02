package systemmetrics

import (
	"net/http"

	channelstore "github.com/QuantumNous/new-api/internal/adapter/gorm/channels"
	systemmetrics "github.com/QuantumNous/new-api/internal/adapter/runtime/system"
)

func NewChannelStateMetricsHandler() http.Handler {
	return systemmetrics.NewChannelStateMetricsHandler(func() ([]systemmetrics.ChannelStateSnapshot, error) {
		rows, err := channelstore.ListChannelStateSnapshots()
		var snapshots []systemmetrics.ChannelStateSnapshot
		if rows != nil {
			snapshots = make([]systemmetrics.ChannelStateSnapshot, len(rows))
			for i, row := range rows {
				snapshots[i] = systemmetrics.ChannelStateSnapshot{ID: row.ID, Type: row.Type, Status: row.Status, Name: row.Name, OtherInfo: row.OtherInfo, Balance: row.Balance}
			}
		}
		return snapshots, err
	})
}
