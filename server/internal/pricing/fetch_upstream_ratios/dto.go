package fetch_upstream_ratios

import (
	"github.com/QuantumNous/new-api/relaykit/dto"
)

type Request = dto.UpstreamRequest

// Input is the upstream ratio request together with the rendering of upstream
// errors in the language of the caller.
type Input struct {
	Request   Request
	ErrorText func(error) string
}

type ModelSyncPrices struct {
	Current   map[string]any            `json:"current"`
	Upstreams map[string]map[string]any `json:"upstreams"`
}
type Output struct {
	Differences map[string]map[string]dto.DifferenceItem `json:"differences"`
	Prices      map[string]ModelSyncPrices               `json:"prices"`
	TestResults []dto.TestResult                         `json:"test_results"`
}
