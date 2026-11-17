package domain

// VideoModelPricing is a video model as OpenRouter publishes it at
// /videos/models: its pricing SKUs and the durations, sizes, resolutions and
// aspect ratios it supports.
type VideoModelPricing struct {
	ID                    string            `json:"id"`
	PricingSKUs           map[string]string `json:"pricing_skus"`
	SupportedDurations    []int             `json:"supported_durations"`
	SupportedSizes        []string          `json:"supported_sizes"`
	SupportedResolutions  []string          `json:"supported_resolutions"`
	SupportedAspectRatios []string          `json:"supported_aspect_ratios"`
}

// CatalogVideoTariffs are the video tariffs of upstream catalog documents by
// model id: the /videos/models entry (facts.raw.video_api) of the eligible
// endpoint offer of a document. A model whose eligible endpoint offers carry
// no such entry or more than one, whose offer facts do not decode, or whose
// entry names another model is left to live pricing.
func CatalogVideoTariffs(deps Capabilities, documents map[string]*SourceDocument) map[string]VideoModelPricing {
	tariffs := make(map[string]VideoModelPricing)
documents:
	for id, document := range documents {
		var entries []VideoModelPricing
		for _, offer := range document.Offers {
			eligible := offer.Eligibility != nil && offer.Eligibility.Eligible
			if offer.Scope != "endpoint" || !eligible {
				continue
			}
			var facts struct {
				Raw struct {
					VideoAPI *VideoModelPricing `json:"video_api"`
				} `json:"raw"`
			}
			if deps.Codec.Unmarshal(offer.Facts, &facts) != nil {
				continue documents
			}
			if facts.Raw.VideoAPI != nil {
				entries = append(entries, *facts.Raw.VideoAPI)
			}
		}
		if len(entries) != 1 || entries[0].ID != id {
			continue
		}
		tariffs[id] = entries[0]
	}
	return tariffs
}
