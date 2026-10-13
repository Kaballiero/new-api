package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/relay/channel/openrouter"
	"github.com/QuantumNous/new-api/service"
)

const pricingCatalogWorkers = 5
const pricingCatalogBodyLimit = 2 << 20
const pricingCatalogTimeout = 20 * time.Second

func refreshChannelPricingCache() {
	model.InitChannelCache()
	enqueuePricingCatalogRefresh()
}

func enqueuePricingCatalogRefresh() {
	if !(pricingCatalogHandler{}).Enabled() {
		return
	}
	if _, _, err := service.EnqueueSystemTask(model.SystemTaskTypePricingCatalog, nil); err != nil {
		common.SysLog("pricing catalog refresh enqueue failed")
	}
}

type pricingCatalogHandler struct{}

func (pricingCatalogHandler) Type() string { return model.SystemTaskTypePricingCatalog }
func (pricingCatalogHandler) Enabled() bool {
	return common.GetEnvOrDefaultBool("PRICING_CATALOG_TASK_ENABLED", true)
}
func (pricingCatalogHandler) Interval() time.Duration {
	minutes := common.GetEnvOrDefault("PRICING_CATALOG_TASK_INTERVAL_MINUTES", 30)
	if minutes < 1 {
		minutes = 30
	}
	return time.Duration(minutes) * time.Minute
}
func (pricingCatalogHandler) NewPayload() any { return nil }
func (pricingCatalogHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	summary, err := refreshPricingCatalog(ctx)
	status := model.SystemTaskStatusSucceeded
	if err != nil {
		status = model.SystemTaskStatusFailed
	}
	finishSystemTaskHandler(task, runnerID, status, summary, err)
}

type pricingCatalogRefreshSummary struct {
	Sources   int `json:"sources"`
	Published int `json:"published"`
	Failed    int `json:"failed"`
}
type pricingCatalogSource struct {
	channel     *model.Channel
	upstream    string
	endpoint    constant.EndpointType
	fingerprint string
}

func pricingCatalogSources() ([]pricingCatalogSource, error) {
	abilities, err := model.GetAllEnableAbilityWithChannels()
	if err != nil {
		return nil, err
	}
	pricing := make(map[string]model.Pricing)
	for _, item := range model.GetPricing() {
		pricing[item.ModelName] = item
	}
	channels := make(map[int]*model.Channel)
	seen := make(map[string]bool)
	sources := []pricingCatalogSource{}
	for _, ability := range abilities {
		// Concrete native actual-cost adapter. Configured tariffs for every provider
		// are projected by the pricing snapshot and do not require metadata HTTP.
		if ability.ChannelType != constant.ChannelTypeOpenRouter {
			continue
		}
		channel, exists := channels[ability.ChannelId]
		if !exists {
			channel, err = model.GetChannelById(ability.ChannelId, true)
			if err != nil {
				return nil, err
			}
			channels[ability.ChannelId] = channel
		}
		if channel.Status != common.ChannelStatusEnabled {
			continue
		}
		baseURL := channel.GetBaseURL()
		if baseURL == "" {
			baseURL = constant.GetChannelBaseURL(channel.Type)
		}
		if strings.TrimRight(baseURL, "/") != "https://openrouter.ai/api" {
			continue
		}
		if channel.ParamOverride != nil && strings.TrimSpace(*channel.ParamOverride) != "" && strings.TrimSpace(*channel.ParamOverride) != "{}" {
			continue
		}
		upstream, resolved := model.ResolvePricingUpstreamModel(channel, ability.Model)
		if !resolved || strings.Contains(upstream, ":") {
			continue
		}
		for endpoint, billingSources := range pricing[ability.Model].BillingSourcesForGroup(ability.Group) {
			if !slices.Contains(billingSources, model.BillingSourceOpenRouterActualCost) {
				continue
			}
			// Video has a typed catalog owned and published by the existing task plugin.
			if endpoint == constant.EndpointTypeOpenAIVideo || endpoint == model.PricingEndpointBatch {
				continue
			}
			if !slices.Contains(pricing[ability.Model].SupportedEndpointTypes, endpoint) {
				continue
			}
			operation, _ := consumerOperation(endpoint)
			if operation == "" {
				continue
			}
			key := fmt.Sprintf("%d:%s:%s", channel.Id, upstream, endpoint)
			if seen[key] {
				continue
			}
			seen[key] = true
			sources = append(sources, pricingCatalogSource{channel, upstream, endpoint, model.PricingCatalogSourceFingerprint(channel, upstream, endpoint)})
		}
	}
	return sources, nil
}

func refreshPricingCatalog(ctx context.Context) (pricingCatalogRefreshSummary, error) {
	sources, err := pricingCatalogSources()
	if err != nil {
		return pricingCatalogRefreshSummary{}, err
	}
	return refreshPricingCatalogSources(ctx, sources, fetchPricingCatalogSource, func(entry model.PricingCatalogEntry) error {
		return model.PublishPricingCatalogEntryContext(ctx, entry)
	})
}

func refreshPricingCatalogSources(ctx context.Context, sources []pricingCatalogSource, fetch func(context.Context, pricingCatalogSource) (model.PricingCatalogEntry, error), publish func(model.PricingCatalogEntry) error) (pricingCatalogRefreshSummary, error) {
	summary := pricingCatalogRefreshSummary{Sources: len(sources)}
	jobs := make(chan pricingCatalogSource)
	var workers sync.WaitGroup
	var resultLock sync.Mutex
	for range pricingCatalogWorkers {
		workers.Go(func() {
			for source := range jobs {
				entry, err := fetch(ctx, source)
				if err == nil && ctx.Err() == nil {
					err = publish(entry)
				}
				resultLock.Lock()
				if err != nil || ctx.Err() != nil {
					summary.Failed++
				} else {
					summary.Published++
				}
				resultLock.Unlock()
			}
		})
	}
enqueue:
	for _, source := range sources {
		select {
		case jobs <- source:
		case <-ctx.Done():
			break enqueue
		}
	}
	close(jobs)
	workers.Wait()
	if err := ctx.Err(); err != nil {
		return summary, err
	}
	if summary.Failed > 0 {
		return summary, fmt.Errorf("pricing catalog refresh failed for %d of %d sources", summary.Failed, summary.Sources)
	}
	return summary, nil
}

func fetchPricingCatalogSource(ctx context.Context, source pricingCatalogSource) (model.PricingCatalogEntry, error) {
	entry := model.PricingCatalogEntry{}
	parts := strings.Split(source.upstream, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return entry, errors.New("invalid native pricing model identity")
	}
	path := "/v1/models/"
	if source.endpoint == constant.EndpointTypeImageGeneration {
		path = "/v1/images/models/"
	}
	endpointURL := "https://openrouter.ai/api" + path + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/endpoints"
	key, err := selectChannelUpstreamModelDiscoveryKey(source.channel)
	if err != nil {
		return entry, errors.New("pricing discovery authentication unavailable")
	}
	headers, err := buildFetchModelsHeaders(source.channel, key)
	if err != nil {
		return entry, errors.New("pricing discovery headers unavailable")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL, nil)
	if err != nil {
		return entry, err
	}
	request.Header = headers
	settings := source.channel.GetSetting()
	client, err := service.GetHttpClientWithProxySettings(settings.Proxy, settings)
	if err != nil {
		return entry, errors.New("pricing discovery transport unavailable")
	}
	metadataClient := *client
	metadataClient.Timeout = pricingCatalogTimeout
	metadataClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := metadataClient.Do(request)
	if err != nil {
		return entry, errors.New("pricing metadata request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return entry, fmt.Errorf("pricing metadata HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, pricingCatalogBodyLimit+1))
	if err != nil || len(body) > pricingCatalogBodyLimit {
		return entry, errors.New("pricing metadata exceeds body limit")
	}
	var offers []model.CatalogOffer
	if source.endpoint == constant.EndpointTypeImageGeneration {
		offers, err = normalizeImagePricing(source.upstream, body)
		var metadata struct {
			Endpoints []openrouter.ImageEndpoint `json:"endpoints"`
		}
		if common.Unmarshal(body, &metadata) == nil && (len(metadata.Endpoints) == 0 && source.upstream == "meta/muse-image" || slices.ContainsFunc(metadata.Endpoints, func(endpoint openrouter.ImageEndpoint) bool {
			return endpoint.ProviderTag == "krea" && len(endpoint.Pricing) == 0
		})) {
			endpointURL = "https://openrouter.ai/" + source.upstream
		}
	} else {
		operation, main := consumerOperation(source.endpoint)
		offers, err = normalizeEndpointPricing(source.upstream, operation, main, body)
	}
	if err != nil {
		return entry, err
	}
	return model.PricingCatalogEntry{ChannelID: source.channel.Id, UpstreamModel: source.upstream, Endpoint: source.endpoint, Source: endpointURL, FetchedAt: time.Now().UTC().Format(time.RFC3339Nano), Fingerprint: source.fingerprint, Offers: selectPricingCatalogOffers(offers)}, nil
}

func selectPricingCatalogOffers(offers []model.CatalogOffer) []model.CatalogOffer {
	groups := make(map[string][]model.CatalogOffer)
	for _, offer := range offers {
		unit := "unknown"
		for _, price := range offer.Prices {
			if price.Component == offer.MainComponent {
				unit = price.Unit
				break
			}
		}
		key := offer.Operation + ":" + offer.Mode + ":" + unit
		groups[key] = append(groups[key], offer)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	result := []model.CatalogOffer{}
	for _, key := range keys {
		selected := model.SelectCatalogOffer(groups[key])
		if selected == nil {
			unknown := groups[key][0]
			unknown.Prices = nil
			selected = &unknown
		}
		result = append(result, *selected)
	}
	return result
}

func normalizeEndpointPricing(upstream, operation, main string, body []byte) ([]model.CatalogOffer, error) {
	var response struct {
		Data struct {
			ID           string                      `json:"id"`
			Endpoints    []openrouter.PricedEndpoint `json:"endpoints"`
			Architecture struct {
				InputModalities  []string `json:"input_modalities"`
				OutputModalities []string `json:"output_modalities"`
			} `json:"architecture"`
		} `json:"data"`
	}
	if common.Unmarshal(body, &response) != nil || response.Data.ID != upstream || response.Data.Endpoints == nil {
		return nil, errors.New("invalid native endpoint pricing response")
	}
	if operation == "text" && (!slices.Contains(response.Data.Architecture.InputModalities, "text") || !slices.Contains(response.Data.Architecture.OutputModalities, "text")) {
		return nil, errors.New("native endpoint has no proved text-only operation")
	}
	offers := []model.CatalogOffer{}
	for index, endpoint := range response.Data.Endpoints {
		prices := maps.Clone(endpoint.Pricing)
		raw, hasOverrides := prices["overrides"]
		delete(prices, "overrides")
		vectors := []map[string]json.RawMessage{prices}
		if hasOverrides && string(raw) != "null" {
			var overrides []map[string]json.RawMessage
			if common.Unmarshal(raw, &overrides) != nil || len(overrides) > 128 {
				return nil, errors.New("invalid native pricing overrides")
			}
			thresholds := make([]int, len(overrides))
			for index, override := range overrides {
				var threshold int
				if common.Unmarshal(override["min_prompt_tokens"], &threshold) != nil || threshold < 0 {
					return nil, errors.New("invalid native pricing threshold")
				}
				if threshold == 0 {
					vectors = nil
				}
				thresholds[index] = threshold
				for key := range override {
					if !slices.Contains([]string{"min_prompt_tokens", "prompt", "completion", "input_cache_read", "input_cache_write", "input_cache_write_1h", "request", "image", "audio", "audio_output", "input_audio_cache", "image_output", "internal_reasoning", "web_search"}, key) {
						return nil, errors.New("unsupported native pricing override condition or dimension")
					}
				}
			}
			for _, threshold := range thresholds {
				if endpoint.ContextLength > 0 && threshold > endpoint.ContextLength || endpoint.MaxPromptTokens > 0 && threshold > endpoint.MaxPromptTokens {
					continue
				}
				vector := maps.Clone(prices)
				for overrideIndex, override := range overrides {
					if thresholds[overrideIndex] > threshold {
						continue
					}
					for key, value := range override {
						if key != "min_prompt_tokens" {
							vector[key] = value
						}
					}
				}
				vectors = append(vectors, vector)
			}
		}
		for variant, vector := range vectors {
			offer := model.CatalogOffer{ID: fmt.Sprintf("%s:%d:%d", endpoint.Tag, index, variant), Operation: operation, Mode: "sync", MainComponent: main, Prices: []model.CatalogPrice{}}
			keys := make([]string, 0, len(vector))
			for key := range vector {
				keys = append(keys, key)
			}
			slices.Sort(keys)
			unprojectable := false
			for _, key := range keys {
				component, unit, multiplier := "", "million_tokens", 1e6
				switch key {
				case "prompt":
					component = "input"
				case "completion":
					component = "output"
				case "input_cache_read":
					component = "cache_read"
				case "input_cache_write":
					component = "cache_write"
				case "input_cache_write_1h":
					component = "cache_write_1h"
				case "internal_reasoning":
					component = "reasoning_output"
				case "web_search":
					component, unit, multiplier = "web_search", "search", 1
					amount, err := openrouter.ReadEndpointPrice(vector[key])
					if err != nil {
						return nil, err
					}
					if amount == 0 && !slices.Contains(endpoint.SupportedParameters, "web_search_options") {
						continue
					}
				case "request":
					component = "base"
					unit = "call"
					multiplier = 1
				case "image":
					component = "image_input"
					unit = "image"
					multiplier = 1
				case "discount":
					continue
				case "audio", "audio_output", "input_audio_cache", "image_output":
					if _, err := openrouter.ReadEndpointPrice(vector[key]); err != nil {
						return nil, err
					}
					continue
				default:
					amount, err := openrouter.ReadEndpointPrice(vector[key])
					if err != nil {
						return nil, err
					}
					if amount != 0 {
						unprojectable = true
					}
					continue
				}
				amount, err := openrouter.ReadEndpointPrice(vector[key])
				if err != nil {
					return nil, err
				}
				amount *= multiplier
				offer.Prices = append(offer.Prices, model.CatalogPrice{Component: component, Unit: unit, AmountUSD: &amount})
			}
			if unprojectable {
				offer.Prices = nil
			}
			offers = append(offers, offer)
		}
	}
	return offers, nil
}

func normalizeImagePricing(upstream string, body []byte) ([]model.CatalogOffer, error) {
	var response struct {
		ID        string                     `json:"id"`
		Endpoints []openrouter.ImageEndpoint `json:"endpoints"`
	}
	if common.Unmarshal(body, &response) != nil || response.ID != upstream || response.Endpoints == nil {
		return nil, errors.New("invalid native image pricing response")
	}
	// The native float DTO alone cannot distinguish a missing cost from free.
	var rawResponse struct {
		Endpoints []struct {
			Pricing []map[string]json.RawMessage `json:"pricing"`
		} `json:"endpoints"`
	}
	if common.Unmarshal(body, &rawResponse) != nil {
		return nil, errors.New("invalid native image rates")
	}
	offers := []model.CatalogOffer{}
	if len(response.Endpoints) == 0 {
		if endpoint, known := openrouter.PublishedMuseImageEndpoint(upstream); known {
			response.Endpoints = []openrouter.ImageEndpoint{endpoint}
		}
	}
	for index, endpoint := range response.Endpoints {
		published := len(endpoint.Pricing) == 0
		endpoint = openrouter.PublishedImageEndpoint(upstream, endpoint)
		variants := map[string]bool{"": true}
		for priceIndex, price := range endpoint.Pricing {
			if index < len(rawResponse.Endpoints) && !published {
				if _, err := openrouter.ReadEndpointPrice(rawResponse.Endpoints[index].Pricing[priceIndex]["cost_usd"]); err != nil {
					return nil, err
				}
			}
			if price.Variant != "" && imagePricingVariantSupported(price.Variant, endpoint.SupportedParameters) {
				variants[price.Variant] = true
			}
		}
		for variant := range variants {
			offer := model.CatalogOffer{ID: fmt.Sprintf("%s:%d:%s", endpoint.ProviderTag, index, variant), Operation: "image_generation", Mode: "sync", MainComponent: "image_output", Prices: []model.CatalogPrice{}}
			components := make(map[string]string)
			for _, price := range endpoint.Pricing {
				if price.Variant != "" && price.Variant != variant {
					continue
				}
				component := ""
				switch price.Billable {
				case "output_image":
					component = "image_output"
				case "input_image":
					component = "image_input"
				case "input_reference":
					component = "reference_input"
				case "input_text":
					component = "input"
				case "input_font":
					component = "font_input"
				default:
					return nil, errors.New("unsupported native image billable")
				}
				unit, amount := price.Unit, price.CostUSD
				switch unit {
				case "token":
					unit = "million_tokens"
					amount *= 1e6
				case "request":
					unit = "call"
				case "image", "megapixel":
				default:
					return nil, errors.New("unsupported native image unit")
				}
				if existingVariant, exists := components[component]; exists {
					if existingVariant == price.Variant {
						return nil, errors.New("ambiguous native image price vector")
					}
					if price.Variant == "" {
						continue
					}
					for index := range offer.Prices {
						if offer.Prices[index].Component == component {
							offer.Prices[index] = model.CatalogPrice{Component: component, Unit: unit, AmountUSD: &amount}
						}
					}
					components[component] = price.Variant
					continue
				}
				components[component] = price.Variant
				offer.Prices = append(offer.Prices, model.CatalogPrice{Component: component, Unit: unit, AmountUSD: &amount})
			}
			offers = append(offers, offer)
		}
	}
	return offers, nil
}

func imagePricingVariantSupported(variant string, parameters map[string]openrouter.ImageCapability) bool {
	for _, part := range strings.Split(variant, "_") {
		found := false
		for _, capability := range parameters {
			if capability.Type != "enum" {
				continue
			}
			for _, value := range capability.Values {
				if strings.EqualFold(part, value) {
					found = true
				}
			}
		}
		if !found {
			return false
		}
	}
	return true
}
