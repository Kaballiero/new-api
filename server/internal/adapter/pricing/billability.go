package pricing

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

type billablePricingCacheEntry struct {
	revision string
	builtAt  time.Time
	billable map[string]bool
}

// BillableCache bounds public pricing staleness after reserve or billing changes.
type BillableCache struct {
	mu      sync.Mutex
	entries map[string]billablePricingCacheEntry
	SelfUse func() bool
	Filter  func([]string, []string) []string
}

func (c *BillableCache) Models(pricing []domain.Pricing, group, revision string) map[string]bool {
	if c.SelfUse() {
		return nil
	}
	c.mu.Lock()
	cached, ok := c.entries[group]
	c.mu.Unlock()
	if ok && revision != "" && cached.revision == revision && time.Since(cached.builtAt) < time.Minute {
		return cached.billable
	}
	names := make([]string, 0, len(pricing))
	for _, item := range pricing {
		names = append(names, item.ModelName)
	}
	billable := make(map[string]bool, len(names))
	for _, name := range c.Filter(names, []string{group}) {
		billable[name] = true
	}
	if revision != "" {
		c.mu.Lock()
		if c.entries == nil {
			c.entries = make(map[string]billablePricingCacheEntry)
		}
		c.entries[group] = billablePricingCacheEntry{revision: revision, builtAt: time.Now(), billable: billable}
		c.mu.Unlock()
	}
	return billable
}
