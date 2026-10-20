package pricing

import (
	"github.com/QuantumNous/new-api/internal/pricing/fetch_upstream_ratios"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

type RatioChannels struct {
	ReadIDs func([]int) ([]fetch_upstream_ratios.Channel, error)
	ReadAll func() ([]dto.SyncableChannel, error)
}

func (c RatioChannels) ByIDs(ids []int) ([]fetch_upstream_ratios.Channel, error) {
	return c.ReadIDs(ids)
}
func (c RatioChannels) All() ([]dto.SyncableChannel, error) { return c.ReadAll() }
