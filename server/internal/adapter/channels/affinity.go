package channels

import "github.com/QuantumNous/new-api/service"

type AffinityCache struct{}

func NewAffinityCache() AffinityCache { return AffinityCache{} }
func (AffinityCache) ClearAll() int   { return service.ClearChannelAffinityCacheAll() }
func (AffinityCache) ClearRule(rule string) (int, error) {
	return service.ClearChannelAffinityCacheByRuleName(rule)
}
