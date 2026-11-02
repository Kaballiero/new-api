package pricing

import (
	"strconv"
	"sync"

	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"golang.org/x/sync/singleflight"
)

// ProjectionCache keeps the latest projected pricing list of every tariff, one
// list per user group and using group. When the inputs change, the previous
// list is served while one background rebuild per tariff replaces it; only a
// tariff without any list makes its readers wait, and they share one build.
type ProjectionCache[List any] struct {
	// Rebuild runs a rebuild outside the request that noticed the change.
	Rebuild     func(rebuild func())
	mu          sync.Mutex
	lists       map[projectionTariff]projectedList[List]
	rebuilding  map[projectionTariff]bool
	firstBuilds singleflight.Group
}

type projectionTariff struct{ userGroup, usingGroup string }

type projectedList[List any] struct {
	inputs domain.PricingProjectionInputs
	list   List
}

func (c *ProjectionCache[List]) Load(userGroup, usingGroup string, inputs domain.PricingProjectionInputs, build func() List) List {
	tariff := projectionTariff{userGroup: userGroup, usingGroup: usingGroup}
	c.mu.Lock()
	cached, known := c.lists[tariff]
	stale := known && !c.rebuilding[tariff] && !cached.inputs.Equal(inputs)
	if stale {
		if c.rebuilding == nil {
			c.rebuilding = make(map[projectionTariff]bool)
		}
		c.rebuilding[tariff] = true
	}
	c.mu.Unlock()
	if stale {
		c.Rebuild(func() { c.rebuild(tariff, inputs, build) })
	}
	if known {
		return cached.list
	}
	first, _, _ := c.firstBuilds.Do(strconv.Quote(userGroup)+strconv.Quote(usingGroup), func() (any, error) {
		return c.buildFirst(tariff, inputs, build), nil
	})
	return first.(List)
}

// buildFirst builds the first list of a tariff unless a build that finished
// after the caller's lookup has already stored one.
func (c *ProjectionCache[List]) buildFirst(tariff projectionTariff, inputs domain.PricingProjectionInputs, build func() List) List {
	c.mu.Lock()
	cached, known := c.lists[tariff]
	c.mu.Unlock()
	if known {
		return cached.list
	}
	list := build()
	c.store(tariff, inputs, list)
	return list
}

func (c *ProjectionCache[List]) rebuild(tariff projectionTariff, inputs domain.PricingProjectionInputs, build func() List) {
	// A failed build releases the tariff, so a later request retries it.
	defer func() {
		c.mu.Lock()
		delete(c.rebuilding, tariff)
		c.mu.Unlock()
	}()
	c.store(tariff, inputs, build())
}

func (c *ProjectionCache[List]) store(tariff projectionTariff, inputs domain.PricingProjectionInputs, list List) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lists == nil {
		c.lists = make(map[projectionTariff]projectedList[List])
	}
	c.lists[tariff] = projectedList[List]{inputs: inputs, list: list}
}
