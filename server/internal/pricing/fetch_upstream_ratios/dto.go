package fetch_upstream_ratios

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
)

type Request = dto.UpstreamRequest
type ModelSyncPrices struct {
	Current   map[string]any            `json:"current"`
	Upstreams map[string]map[string]any `json:"upstreams"`
}
type Output struct {
	Differences map[string]map[string]dto.DifferenceItem `json:"differences"`
	Prices      map[string]ModelSyncPrices               `json:"prices"`
	TestResults []dto.TestResult                         `json:"test_results"`
}
