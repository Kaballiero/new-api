package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	"github.com/QuantumNous/new-api/setting/billing_setting"
	"golang.org/x/net/html"
)

const pricingCatalogWorkers = 5
const pricingCatalogBodyLimit = 2 << 20
const pricingCatalogTimeout = 20 * time.Second
const pricingCatalogFailureLimit = 20

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
	Sources           int                     `json:"sources"`
	Published         int                     `json:"published"`
	Failed            int                     `json:"failed"`
	FailedSources     []pricingCatalogFailure `json:"failed_sources,omitempty"`
	FailuresTruncated bool                    `json:"failures_truncated,omitempty"`
}
type pricingCatalogFailure struct {
	ChannelID     int                   `json:"channel_id"`
	UpstreamModel string                `json:"upstream_model"`
	Endpoint      constant.EndpointType `json:"endpoint"`
	Class         string                `json:"class"`
}
type pricingCatalogSource struct {
	channel     *model.Channel
	upstream    string
	endpoint    constant.EndpointType
	fingerprint string
}

func pricingCatalogSources(ctx context.Context) ([]pricingCatalogSource, error) {
	abilities, err := model.GetAllEnableAbilityWithChannels()
	if err != nil {
		return nil, err
	}
	pricing := make(map[string]model.Pricing)
	for _, item := range model.GetPricing() {
		pricing[item.ModelName] = item
	}
	channels := make(map[int]*model.Channel)
	imageModels := make(map[int]map[string]bool)
	var discoveryError error
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
		if !resolved || strings.Contains(upstream, ":") && !strings.HasSuffix(upstream, ":batch") && !strings.HasSuffix(upstream, ":free") {
			continue
		}
		if _, loaded := imageModels[channel.Id]; !loaded {
			imageModels[channel.Id] = make(map[string]bool)
			body, fetchErr := fetchPricingMetadata(ctx, channel, "https://openrouter.ai/api/v1/images/models")
			var response struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if fetchErr == nil {
				fetchErr = common.Unmarshal(body, &response)
			}
			if fetchErr != nil || response.Data == nil {
				discoveryError = errors.Join(discoveryError, errors.New("native image capability discovery failed"))
			} else {
				for _, item := range response.Data {
					imageModels[channel.Id][item.ID] = true
				}
			}
		}
		appendSource := func(endpoint constant.EndpointType) {
			key := fmt.Sprintf("%d:%s:%s", channel.Id, upstream, endpoint)
			if seen[key] {
				return
			}
			seen[key] = true
			sources = append(sources, pricingCatalogSource{channel, upstream, endpoint, model.PricingCatalogSourceFingerprint(channel, upstream, endpoint)})
		}
		for endpoint, billingSources := range pricing[ability.Model].BillingSourcesForGroup(ability.Group) {
			if !slices.Contains(billingSources, model.BillingSourceOpenRouterActualCost) || endpoint == constant.EndpointTypeOpenAIVideo {
				continue
			}
			if endpoint == model.PricingEndpointBatch {
				expression, configured := billing_setting.GetBatchBillingExpr(ability.Model)
				if configured && expression != "" {
					appendSource(endpoint)
				}
				continue
			}
			if strings.HasSuffix(upstream, ":batch") {
				continue
			}
			if !slices.Contains(pricing[ability.Model].SupportedEndpointTypes, endpoint) {
				continue
			}
			if endpoint == constant.EndpointTypeImageGeneration {
				if imageModels[channel.Id][upstream] {
					appendSource(endpoint)
				}
				continue
			}
			operation, _ := consumerOperation(endpoint)
			if operation == "" {
				continue
			}
			appendSource(endpoint)
		}
		if imageModels[channel.Id][upstream] {
			appendSource(constant.EndpointTypeImageGeneration)
		}

	}
	return sources, discoveryError
}

func refreshPricingCatalog(ctx context.Context) (pricingCatalogRefreshSummary, error) {
	sources, discoveryErr := pricingCatalogSources(ctx)
	summary, refreshErr := refreshPricingCatalogSources(ctx, sources, fetchPricingCatalogSource, func(entry model.PricingCatalogEntry) error {
		return model.PublishPricingCatalogEntryContext(ctx, entry)
	})
	if discoveryErr != nil {
		summary.Failed++
	}
	return summary, errors.Join(discoveryErr, refreshErr)
}

func refreshPricingCatalogSources(ctx context.Context, sources []pricingCatalogSource, fetch func(context.Context, pricingCatalogSource) (model.PricingCatalogEntry, error), publish func(model.PricingCatalogEntry) error) (pricingCatalogRefreshSummary, error) {
	summary := pricingCatalogRefreshSummary{Sources: len(sources)}
	jobs := make(chan pricingCatalogSource)
	var workers sync.WaitGroup
	var resultLock sync.Mutex
	for range pricingCatalogWorkers {
		workers.Go(func() {
			for source := range jobs {
				failureClass := "metadata_failed"
				entry, err := fetch(ctx, source)
				if err == nil && ctx.Err() == nil {
					failureClass = "publication_failed"
					err = publish(entry)
				}
				resultLock.Lock()
				if err != nil || ctx.Err() != nil {
					summary.Failed++
					if ctx.Err() != nil {
						failureClass = "cancelled"
					} else if failureClass == "metadata_failed" {
						if strings.Contains(err.Error(), "override") || strings.Contains(err.Error(), "threshold") {
							failureClass = "pricing_override_invalid"
						}
						if strings.Contains(err.Error(), "identity") || strings.Contains(err.Error(), "endpoint pricing response") {
							failureClass = "metadata_identity_or_schema_invalid"
						}
					}
					channelID := 0
					if source.channel != nil {
						channelID = source.channel.Id
					}
					upstream := source.upstream
					if len(upstream) > 256 {
						upstream = upstream[:256]
					}
					summary.FailedSources = append(summary.FailedSources, pricingCatalogFailure{channelID, upstream, source.endpoint, failureClass})
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
	slices.SortFunc(summary.FailedSources, func(left, right pricingCatalogFailure) int {
		if left.ChannelID != right.ChannelID {
			return left.ChannelID - right.ChannelID
		}
		if order := strings.Compare(left.UpstreamModel, right.UpstreamModel); order != 0 {
			return order
		}
		return strings.Compare(string(left.Endpoint), string(right.Endpoint))
	})
	if len(summary.FailedSources) > pricingCatalogFailureLimit {
		summary.FailuresTruncated = true
		summary.FailedSources = summary.FailedSources[:pricingCatalogFailureLimit]
	}
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
	discoveryModel := source.upstream
	if source.endpoint == model.PricingEndpointBatch {
		discoveryModel = strings.TrimSuffix(source.upstream, ":batch") + ":batch"
	}
	parts := strings.Split(discoveryModel, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return entry, errors.New("invalid native pricing model identity")
	}
	path := "/v1/models/"
	if source.endpoint == constant.EndpointTypeImageGeneration {
		path = "/v1/images/models/"
	}
	endpointURL := "https://openrouter.ai/api" + path + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/endpoints"
	body, err := fetchPricingMetadata(ctx, source.channel, endpointURL)
	if err != nil {
		return entry, err
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
		offers, err = normalizeEndpointPricing(discoveryModel, operation, main, body)
		if err == nil && operation == "text" && source.endpoint != model.PricingEndpointBatch && strings.HasSuffix(discoveryModel, ":free") && len(offers) == 1 && offers[0].ID == "native:unknown" {
			pageURL := "https://openrouter.ai/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1])
			if page, fetchErr := fetchPricingMetadata(ctx, source.channel, pageURL); fetchErr == nil {
				if published := publishedFreeTextPricing(discoveryModel, page); published != nil {
					offers = published
					endpointURL = pageURL
				}
			}
		}
		if source.endpoint == model.PricingEndpointBatch {
			for index := range offers {
				offers[index].Mode = "batch"
			}
		}
	}
	if err != nil {
		return entry, err
	}
	return model.PricingCatalogEntry{ChannelID: source.channel.Id, UpstreamModel: source.upstream, Endpoint: source.endpoint, Source: endpointURL, FetchedAt: time.Now().UTC().Format(time.RFC3339Nano), Fingerprint: source.fingerprint, Offers: selectPricingCatalogOffers(offers)}, nil
}

func publishedFreeTextPricing(upstream string, body []byte) []model.CatalogOffer {
	if !strings.HasSuffix(upstream, ":free") {
		return nil
	}
	tokenizer := html.NewTokenizer(strings.NewReader(string(body)))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return nil
		case html.StartTagToken:
			token := tokenizer.Token()
			if token.Data != "script" || !slices.ContainsFunc(token.Attr, func(attribute html.Attribute) bool {
				return attribute.Key == "type" && attribute.Val == "application/ld+json"
			}) || tokenizer.Next() != html.TextToken {
				continue
			}
			var application struct {
				Type        string   `json:"@type"`
				URL         string   `json:"url"`
				FeatureList []string `json:"featureList"`
				Offers      []struct {
					Type          string          `json:"@type"`
					Price         json.RawMessage `json:"price"`
					PriceCurrency string          `json:"priceCurrency"`
				} `json:"offers"`
			}
			if common.Unmarshal(tokenizer.Text(), &application) != nil || application.Type != "SoftwareApplication" || len(application.Offers) != 1 {
				continue
			}
			if application.URL != "https://openrouter.ai/"+upstream && application.URL != "https://openrouter.ai/"+strings.TrimSuffix(upstream, ":free") {
				continue
			}
			if !slices.Contains(application.FeatureList, "Input modalities: text") || !slices.Contains(application.FeatureList, "Output modalities: text") {
				continue
			}
			offer := application.Offers[0]
			amount, err := openrouter.ReadEndpointPrice(offer.Price)
			if err != nil || amount != 0 || offer.Type != "Offer" || offer.PriceCurrency != "USD" {
				return nil
			}
			return []model.CatalogOffer{{ID: "published:free", Operation: "text", Mode: "sync", MainComponent: "output", Prices: []model.CatalogPrice{
				{Component: "input", Unit: "million_tokens", AmountUSD: &amount},
				{Component: "output", Unit: "million_tokens", AmountUSD: &amount},
			}}}
		}
	}
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
	for _, endpoint := range response.Data.Endpoints {
		if strings.Contains(upstream, ":") && endpoint.ModelID != upstream {
			return nil, errors.New("native variant endpoint identity mismatch")
		}
	}
	if operation == "text" && (!slices.Contains(response.Data.Architecture.InputModalities, "text") || !slices.Contains(response.Data.Architecture.OutputModalities, "text")) {
		if len(response.Data.Architecture.InputModalities) == 0 || len(response.Data.Architecture.OutputModalities) == 0 {
			return nil, errors.New("native endpoint has no proved operation")
		}
		return []model.CatalogOffer{}, nil
	}
	offers := []model.CatalogOffer{}
	if len(response.Data.Endpoints) == 0 {
		offers = append(offers, model.CatalogOffer{ID: "native:unknown", Operation: operation, Mode: "sync", MainComponent: main})
	}
	for index, endpoint := range response.Data.Endpoints {
		vectors, err := nativePricingVectors(endpoint)
		if err != nil {
			return nil, err
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
				case "audio", "input_audio_cache":
					if !slices.Contains(response.Data.Architecture.InputModalities, "audio") {
						if _, err := openrouter.ReadEndpointPrice(vector[key]); err != nil {
							return nil, err
						}
						continue
					}
					component = "audio_input"
					if key == "input_audio_cache" {
						component = "audio_cache_read"
					}
				case "audio_output":
					if !slices.Contains(response.Data.Architecture.OutputModalities, "audio") {
						if _, err := openrouter.ReadEndpointPrice(vector[key]); err != nil {
							return nil, err
						}
						continue
					}
					component = "audio_output"
				case "image_output":
					amount, err := openrouter.ReadEndpointPrice(vector[key])
					if err != nil {
						return nil, err
					}
					if amount != 0 && slices.Contains(response.Data.Architecture.OutputModalities, "image") {
						unprojectable = true
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
	if len(offers) == 0 {
		offers = append(offers, model.CatalogOffer{ID: "native:image:unknown", Operation: "image_generation", Mode: "sync", MainComponent: "image_output"})
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

func fetchPricingMetadata(ctx context.Context, channel *model.Channel, endpointURL string) ([]byte, error) {
	key, err := selectChannelUpstreamModelDiscoveryKey(channel)
	if err != nil {
		return nil, errors.New("pricing discovery authentication unavailable")
	}
	headers, err := buildFetchModelsHeaders(channel, key)
	if err != nil {
		return nil, errors.New("pricing discovery headers unavailable")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header = headers
	settings := channel.GetSetting()
	client, err := service.GetHttpClientWithProxySettings(settings.Proxy, settings)
	if err != nil {
		return nil, errors.New("pricing discovery transport unavailable")
	}
	metadataClient := *client
	metadataClient.Timeout = pricingCatalogTimeout
	metadataClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := metadataClient.Do(request)
	if err != nil {
		return nil, errors.New("pricing metadata request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pricing metadata HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, pricingCatalogBodyLimit+1))
	if err != nil || len(body) > pricingCatalogBodyLimit {
		return nil, errors.New("pricing metadata exceeds body limit")
	}
	return body, nil
}
