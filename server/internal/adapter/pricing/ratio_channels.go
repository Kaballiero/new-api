package pricing

import (
	"github.com/QuantumNous/new-api/internal/pricing/fetch_upstream_ratios"
	"github.com/QuantumNous/new-api/internal/pricing/list_syncable_channels"
	"github.com/QuantumNous/new-api/relaykit/dto"
)

type RatioChannels struct {
	ReadIDs func([]int) ([]fetch_upstream_ratios.Channel, error)
	ReadAll func() ([]list_syncable_channels.SyncableChannel, error)
}

func (c RatioChannels) ByIDs(ids []int) ([]fetch_upstream_ratios.Channel, error) {
	return c.ReadIDs(ids)
}
func (c RatioChannels) All() ([]list_syncable_channels.SyncableChannel, error) { return c.ReadAll() }

// DecodeUpstreamRequest retains the original Gin wire binding and validation
// types, including their diagnostic names, then exposes pricing-owned facts.
func DecodeUpstreamRequest(bind func(any) error) (fetch_upstream_ratios.Request, error) {
	var native dto.UpstreamRequest
	err := bind(&native)
	result := fetch_upstream_ratios.Request{ChannelIDs: native.ChannelIDs, Timeout: native.Timeout}
	if native.Upstreams != nil {
		result.Upstreams = make([]fetch_upstream_ratios.RatioUpstream, len(native.Upstreams))
		for i, value := range native.Upstreams {
			result.Upstreams[i] = fetch_upstream_ratios.RatioUpstream{ID: value.ID, Name: value.Name, BaseURL: value.BaseURL, Endpoint: value.Endpoint}
		}
	}
	return result, err
}
