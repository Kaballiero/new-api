package pricing

import (
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

type Reader struct {
	SnapshotEndpointsRead func() ([]domain.Pricing, []domain.PricingVendor, map[string]common.EndpointInfo, string, error)
	GroupRatiosRead       func() map[string]float64
	GroupGroupRatioRead   func(string, string) (float64, bool)

	SnapshotRead      func() ([]domain.Pricing, []domain.PricingVendor, string, error)
	FXRead            func() (domain.BillingFXBasis, error)
	AutoGroupsRead    func(string) []string
	UsableGroupsRead  func(string) map[string]string
	GroupRatioRead    func(string, string) float64
	UserGroupRead     func(int) (string, error)
	ContainsGroupRead func(string) bool
}

func (r Reader) Snapshot() ([]domain.Pricing, []domain.PricingVendor, string, error) {
	return r.SnapshotRead()
}
func (r Reader) FX() (domain.BillingFXBasis, error)          { return r.FXRead() }
func (r Reader) AutoGroups(group string) []string            { return r.AutoGroupsRead(group) }
func (r Reader) UsableGroups(group string) map[string]string { return r.UsableGroupsRead(group) }
func (r Reader) GroupRatio(group, target string) float64     { return r.GroupRatioRead(group, target) }
func (r Reader) UserGroup(id int) (string, error)            { return r.UserGroupRead(id) }
func (r Reader) ContainsGroup(group string) bool             { return r.ContainsGroupRead(group) }

type ConsumerCache struct {
	mu      sync.RWMutex
	entries map[string]domain.ConsumerPricingResponse
}

func (c *ConsumerCache) Get(group string) (domain.ConsumerPricingResponse, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	r, ok := c.entries[group]
	return r, ok
}
func (c *ConsumerCache) Put(group string, response domain.ConsumerPricingResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]domain.ConsumerPricingResponse)
	}
	c.entries[group] = response
}

func (r Reader) SnapshotWithEndpoints() ([]domain.Pricing, []domain.PricingVendor, map[string]domain.PricingEndpointInfo, string, error) {
	pricing, vendors, endpoints, revision, err := r.SnapshotEndpointsRead()
	var result map[string]domain.PricingEndpointInfo
	if endpoints != nil {
		result = make(map[string]domain.PricingEndpointInfo, len(endpoints))
		for key, value := range endpoints {
			result[key] = domain.PricingEndpointInfo(value)
		}
	}
	return pricing, vendors, result, revision, err
}
func (r Reader) GroupRatios() map[string]float64 { return r.GroupRatiosRead() }
func (r Reader) GroupGroupRatio(group, target string) (float64, bool) {
	return r.GroupGroupRatioRead(group, target)
}
