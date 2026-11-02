package pricing

import (
	"strconv"
	"sync"

	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"golang.org/x/sync/singleflight"
)

// ProjectionCache keeps the latest projected pricing list of every tariff, one
// list per user group and using group, and per user when the projection
// depends on who reads it. When the inputs change, the previous list is served
// while one background rebuild per list replaces it; only a list that does not
// exist yet makes its readers wait, and they share one build.
type ProjectionCache[List any] struct {
	// Rebuild runs a rebuild outside the request that noticed the change.
	Rebuild func(rebuild func())
	// UserListLimit caps the lists kept for single users; past it the least
	// recently read one is dropped.
	UserListLimit  int
	mu             sync.Mutex
	lists          map[projectionTariff]projectedList[List]
	rebuilding     map[projectionTariff]bool
	userDependence map[projectionTariff]projectionUserDependence
	reads          uint64
	firstBuilds    singleflight.Group
}

type projectionTariff struct {
	userGroup, usingGroup string
	// user is set only for a list that differs between users of the tariff.
	user int
}

type projectedList[List any] struct {
	inputs domain.PricingProjectionInputs
	list   List
	read   uint64
}

type projectionUserDependence struct {
	inputs    domain.PricingProjectionInputs
	dependent bool
}

// LoadUserDependence tells whether the tariff's projection from inputs depends
// on the reading user. readsUserID runs once per inputs of the tariff.
func (c *ProjectionCache[List]) LoadUserDependence(userGroup, usingGroup string, inputs domain.PricingProjectionInputs, readsUserID func() bool) bool {
	tariff := projectionTariff{userGroup: userGroup, usingGroup: usingGroup}
	c.mu.Lock()
	known, ok := c.userDependence[tariff]
	c.mu.Unlock()
	if ok && known.inputs.Equal(inputs) {
		return known.dependent
	}
	dependent := readsUserID()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.userDependence == nil {
		c.userDependence = make(map[projectionTariff]projectionUserDependence)
	}
	c.userDependence[tariff] = projectionUserDependence{inputs: inputs, dependent: dependent}
	return dependent
}

func (c *ProjectionCache[List]) Load(userGroup, usingGroup string, inputs domain.PricingProjectionInputs, build func() List) List {
	return c.LoadForUser(userGroup, usingGroup, 0, inputs, build)
}

// LoadForUser returns the list of the tariff and user (0 for the list the
// tariff's users share), building it when there is none yet.
func (c *ProjectionCache[List]) LoadForUser(userGroup, usingGroup string, user int, inputs domain.PricingProjectionInputs, build func() List) List {
	if list, known := c.Cached(userGroup, usingGroup, user, inputs, build); known {
		return list
	}
	tariff := projectionTariff{userGroup: userGroup, usingGroup: usingGroup, user: user}
	first, _, _ := c.firstBuilds.Do(strconv.Quote(userGroup)+strconv.Quote(usingGroup)+strconv.Itoa(user), func() (any, error) {
		return c.buildFirst(tariff, inputs, build), nil
	})
	return first.(List)
}

// Cached returns the existing list of the tariff and user without waiting for
// a build; a list from other inputs is returned while build replaces it.
func (c *ProjectionCache[List]) Cached(userGroup, usingGroup string, user int, inputs domain.PricingProjectionInputs, build func() List) (List, bool) {
	tariff := projectionTariff{userGroup: userGroup, usingGroup: usingGroup, user: user}
	c.mu.Lock()
	cached, known := c.lists[tariff]
	if known {
		c.reads++
		cached.read = c.reads
		c.lists[tariff] = cached
	}
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
	return cached.list, known
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
	c.reads++
	c.lists[tariff] = projectedList[List]{inputs: inputs, list: list, read: c.reads}
	if tariff.user == 0 {
		return
	}
	userLists := 0
	var leastRecent projectionTariff
	for key, cached := range c.lists {
		if key.user == 0 {
			continue
		}
		userLists++
		if leastRecent.user == 0 || cached.read < c.lists[leastRecent].read {
			leastRecent = key
		}
	}
	if userLists > c.UserListLimit {
		delete(c.lists, leastRecent)
	}
}
