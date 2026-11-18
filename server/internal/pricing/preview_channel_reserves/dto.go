package preview_channel_reserves

import (
	"time"

	"github.com/QuantumNous/new-api/internal/pricing/domain"
)

// Output is the reserve preview of an OpenRouter channel. FetchedAt is the
// fetchedAt of the published upstream catalog, nil before any.
type Output struct {
	FetchedAt    *time.Time `json:"fetched_at"`
	CatalogStale bool       `json:"catalog_stale"`
	Reference    Reference  `json:"reference"`
	Items        []Item     `json:"items"`
}

// Reference is the request the Chat Completions and Embeddings examples
// price: a nonstream request without an output limit, so the gateway reserves
// OutputTokens capped by the endpoint. Speech and rerank examples price their
// own reference request (Item.ReferenceOperation).
type Reference struct {
	Operation    string `json:"operation"`
	PromptTokens int    `json:"prompt_tokens"`
	OutputTokens int    `json:"output_tokens"`
}

// Item is a channel model or a reserve setting key of a model outside the
// channel (InChannel false).
type Item struct {
	Model            string                  `json:"model"`
	UpstreamModel    *string                 `json:"upstream_model"`
	InChannel        bool                    `json:"in_channel"`
	ConfiguredSource *string                 `json:"configured_source"`
	ManualUSD        *float64                `json:"manual_usd"`
	ConfiguredUSD    *float64                `json:"configured_usd"`
	EffectiveSource  *domain.ReserveSource   `json:"effective_source"`
	AdmissionPolicy  *domain.AdmissionPolicy `json:"admission_policy"`
	// ReferenceOperation is the operation of the reference request the
	// example prices: chat/completions, embeddings, audio/speech or rerank;
	// nil without an example.
	ReferenceOperation *string       `json:"reference_operation"`
	ExampleUSD         *float64      `json:"example_usd"`
	ExampleStatus      ExampleStatus `json:"example_status"`
	ExampleError       *string       `json:"example_error"`
	Providers          []Provider    `json:"providers"`
}

// Provider is the example of an item restricted to one endpoint tag of the
// catalog model; USD is nil when the gateway refuses it.
type Provider struct {
	Tag  string             `json:"tag"`
	Tier domain.ServiceTier `json:"tier"`
	USD  *float64           `json:"usd"`
}

// ExampleStatus tells whether the example of an item is priced.
type ExampleStatus string

const (
	ExampleStatusOK ExampleStatus = "ok"
	// ExampleStatusLiveOnly is a model the upstream catalog does not price
	// for the channel: no published or a stale catalog, a channel off the
	// official OpenRouter API, a model outside the catalog or without text,
	// embeddings, speech or rerank output. The gateway prices it from live
	// endpoints.
	ExampleStatusLiveOnly ExampleStatus = "live_only"
	// ExampleStatusNeedsLiveCheck is a catalog model the gateway checks
	// against live endpoints, for example for a missing price.
	ExampleStatusNeedsLiveCheck ExampleStatus = "needs_live_check"
	// ExampleStatusRefused is a reference request the gateway refuses, or a
	// channel model whose model mapping does not resolve.
	ExampleStatusRefused ExampleStatus = "refused"
)
