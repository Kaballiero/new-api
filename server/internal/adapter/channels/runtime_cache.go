package channels

import "fmt"

type RuntimeCache struct {
	enabled             func() bool
	initialize, pricing func()
	log                 func(string)
}

func NewRuntimeCache(enabled func() bool, initialize, pricing func(), log func(string)) RuntimeCache {
	return RuntimeCache{enabled: enabled, initialize: initialize, pricing: pricing, log: log}
}
func (cache RuntimeCache) Refresh() {
	if cache.enabled() {
		func() {
			defer func() {
				if r := recover(); r != nil {
					cache.log(fmt.Sprintf("InitChannelCache panic: %v", r))
				}
			}()
			cache.initialize()
		}()
	}
	cache.pricing()
}
