package usage

import (
	"sync"
	"time"

	"github.com/QuantumNous/new-api/internal/usage/get_rankings"
)

type snapshot struct {
	data    *get_rankings.RankingsResponse
	expires time.Time
}

type RankingsCache struct {
	mu      sync.Mutex
	entries map[string]snapshot
}

func NewRankingsCache() *RankingsCache {
	return &RankingsCache{entries: make(map[string]snapshot)}
}

func (c *RankingsCache) Get(key string) (*get_rankings.RankingsResponse, time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	item, ok := c.entries[key]
	return item.data, item.expires, ok
}

func (c *RankingsCache) Put(key string, data *get_rankings.RankingsResponse, expires time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = snapshot{data: data, expires: expires}
}
