package fetch_upstream_ratios

import "github.com/QuantumNous/new-api/internal/pricing/domain"

type RatioUpstream struct {
	ID       int    `json:"id,omitempty"`
	Name     string `json:"name" binding:"required"`
	BaseURL  string `json:"base_url" binding:"required"`
	Endpoint string `json:"endpoint"`
}
type Request struct {
	ChannelIDs []int64         `json:"channel_ids"`
	Upstreams  []RatioUpstream `json:"upstreams"`
	Timeout    int             `json:"timeout"`
}
type RatioTestResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

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
	Differences map[string]map[string]domain.RatioDifference `json:"differences"`
	Prices      map[string]ModelSyncPrices                   `json:"prices"`
	TestResults []RatioTestResult                            `json:"test_results"`
}
