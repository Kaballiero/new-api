package channels

import (
	routing "github.com/QuantumNous/new-api/gateway/routing"
)

type AffinityCache struct{}

func NewAffinityCache() AffinityCache { return AffinityCache{} }
func (AffinityCache) ClearAll() int   { return routing.ClearChannelAffinityCacheAll() }
func (AffinityCache) ClearRule(rule string) (int, error) {
	return routing.ClearChannelAffinityCacheByRuleName(rule)
}
