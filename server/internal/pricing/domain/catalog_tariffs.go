package domain

import (
	"encoding/json"
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
// "endpoint", eligibility.eligible true, a provider tag and a facts.pricing
// object; facts.pricing is the live pricing of the endpoint with that tag. The
// tag, the quantization of the variant and the limits and parameters of the
// capabilities keep their published values; a null limit is no limit. A
// document without such an offer, an alias among them, prices nothing.
func CatalogEndpointTariffs(deps Capabilities, documents map[string]*SourceDocument) map[string]NativeEndpointResponse {
	tariffs := make(map[string]NativeEndpointResponse, len(documents))
	for id, document := range documents {
		var endpoints []NativePricedEndpoint
		for _, offer := range document.Offers {
			eligible := offer.Eligibility != nil && offer.Eligibility.Eligible
			tagged := offer.Provider != nil && offer.Provider.Tag != nil && *offer.Provider.Tag != ""
			if offer.Scope != "endpoint" || !eligible || !tagged {
				continue
			}
			var facts struct {
				Pricing map[string]json.RawMessage `json:"pricing"`
			}
			if deps.Codec.Unmarshal(offer.Facts, &facts) != nil || facts.Pricing == nil {
				continue
			}
			var capabilities struct {
				ContextLength       int      `json:"context_length"`
				MaxPromptTokens     int      `json:"max_prompt_tokens"`
				MaxCompletionTokens int      `json:"max_completion_tokens"`
				SupportedParameters []string `json:"supported_parameters"`
			}
			if len(offer.Capabilities) > 0 && deps.Codec.Unmarshal(offer.Capabilities, &capabilities) != nil {
				continue
			}
			var variant struct {
				Quantization string `json:"quantization"`
			}
			if len(offer.Variant) > 0 && deps.Codec.Unmarshal(offer.Variant, &variant) != nil {
				continue
			}
			endpoints = append(endpoints, NativePricedEndpoint{
				ModelID: id, Tag: *offer.Provider.Tag, Quantization: variant.Quantization,
				ContextLength: capabilities.ContextLength, MaxPromptTokens: capabilities.MaxPromptTokens, MaxCompletionTokens: capabilities.MaxCompletionTokens,
				SupportedParameters: capabilities.SupportedParameters, Pricing: facts.Pricing,
			})
		}
		if len(endpoints) == 0 {
			continue
		}
		tariffs[id] = NativeEndpointResponse{ID: id, Endpoints: endpoints, OutputModalities: document.Modalities.Output}
	}
	return tariffs
}
