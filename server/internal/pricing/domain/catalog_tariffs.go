package domain

import (
	"encoding/json"
	"slices"
	"time"
)

// UpstreamCatalogTariffs are the endpoint tariffs of a published upstream
// catalog revision by model id (CatalogEndpointTariffs) with the time its
// catalog data last changed: the manifest fetchedAt the head stores.
type UpstreamCatalogTariffs struct {
	Revision  string
	FetchedAt time.Time
	Models    map[string]NativeEndpointResponse
}

// CatalogEndpointTariffs are the endpoint tariffs of upstream catalog
// documents by model id, in the shape of the endpoints OpenRouter publishes at
// request time. An endpoint of a model is an offer of its document with scope
// "endpoint" and eligibility.eligible true; its provider tag, facts.pricing
// object (the live pricing of the endpoint with that tag), variant
// quantization and capabilities limits and parameters keep their published
// values, and a null limit is no limit. An endpoint the document does not
// describe whole (no tag, no pricing object, undecodable capabilities or
// variant) leaves the whole model to live pricing, as does a document without
// endpoints, an alias among them.
func CatalogEndpointTariffs(deps Capabilities, documents map[string]*SourceDocument) map[string]NativeEndpointResponse {
	tariffs := make(map[string]NativeEndpointResponse, len(documents))
documents:
	for id, document := range documents {
		var endpoints []NativePricedEndpoint
		for _, offer := range document.Offers {
			eligible := offer.Eligibility != nil && offer.Eligibility.Eligible
			if offer.Scope != "endpoint" || !eligible {
				continue
			}
			if offer.Provider == nil || offer.Provider.Tag == nil || *offer.Provider.Tag == "" {
				continue documents
			}
			var facts struct {
				Pricing map[string]json.RawMessage `json:"pricing"`
			}
			if deps.Codec.Unmarshal(offer.Facts, &facts) != nil || facts.Pricing == nil {
				continue documents
			}
			var capabilities struct {
				ContextLength       int      `json:"context_length"`
				MaxPromptTokens     int      `json:"max_prompt_tokens"`
				MaxCompletionTokens int      `json:"max_completion_tokens"`
				SupportedParameters []string `json:"supported_parameters"`
			}
			if len(offer.Capabilities) > 0 && deps.Codec.Unmarshal(offer.Capabilities, &capabilities) != nil {
				continue documents
			}
			var variant struct {
				Quantization string `json:"quantization"`
			}
			if len(offer.Variant) > 0 && deps.Codec.Unmarshal(offer.Variant, &variant) != nil {
				continue documents
			}
			// Live lists reasoning_effort for every endpoint that supports
			// reasoning; the catalog lists it for few of them. Checked on
			// 2026-11-17 against live /endpoints of 7 models, 65 endpoints:
			// live lists reasoning_effort exactly where the catalog lists
			// reasoning or reasoning_effort.
			parameters := capabilities.SupportedParameters
			if slices.Contains(parameters, "reasoning") && !slices.Contains(parameters, "reasoning_effort") {
				parameters = append(parameters, "reasoning_effort")
			}
			endpoints = append(endpoints, NativePricedEndpoint{
				ModelID: id, Tag: *offer.Provider.Tag, Quantization: variant.Quantization,
				ContextLength: capabilities.ContextLength, MaxPromptTokens: capabilities.MaxPromptTokens, MaxCompletionTokens: capabilities.MaxCompletionTokens,
				SupportedParameters: parameters, Pricing: facts.Pricing,
			})
		}
		if len(endpoints) == 0 {
			continue
		}
		tariffs[id] = NativeEndpointResponse{ID: id, Endpoints: endpoints, OutputModalities: document.Modalities.Output}
	}
	return tariffs
}
