package estimate_upstream_reserve

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/pricing/domain"

	"github.com/shopspring/decimal"
)

// Provider is the provider routing of an OpenRouter request; the gateway
// lowercases the Only, Ignore and Order slugs and names /priority as /fast.
type Provider struct {
	Only, Ignore, Order []string
	AllowFallbacks      *bool
	RequireParameters   bool
	Quantizations       []string
	MaxPrice            map[string]json.RawMessage
}

// Input is a request the gateway has parsed and measured.
type Input struct {
	Model string
	// Models are the routable models in request order, duplicates kept.
	Models []string
	// Batch prices the :batch variants of the routable models
	// (domain.UpstreamTariffModel).
	Batch bool
	// ChannelBaseURL is the base URL of the channel; only the official
	// OpenRouter API is priced from the upstream catalog.
	ChannelBaseURL string
	// Live discovers the endpoint tariffs the catalog does not price.
	Live        LiveTariffs
	Provider    Provider
	ServiceTier string
	Speed       string
	// Parameters are the top-level request fields checked by
	// provider.require_parameters.
	Parameters map[string]json.RawMessage
	Modalities []string
	TextOutput bool
	// ImageOutput, Audio and Decisions name an output whose quantity endpoint
	// tariffs do not measure; Audio covers Chat audio output and
	// transcription.
	ImageOutput, Audio, Decisions bool
	// Rerank is a rerank request, priced from its Units.
	Rerank bool
	// Units are the measured quantities of an operation priced only from the
	// unit prices of its endpoints (unitCost); nil for an operation priced
	// from the endpoint pricing. Output audio tokens are not among them: they
	// are the output cap of each endpoint, DefaultOutputTokens without one.
	Units map[domain.PriceDimension]int64
	// ImageReserveOnly is an image output covered by the channel reserve alone.
	ImageReserveOnly bool
	// ConfiguredPrice is a request priced by a configured local price.
	ConfiguredPrice bool
	PromptTokens    int
	Count           int
	Images          int
	// ImageTokens is measured only by the Cohere Embed rule; nil otherwise.
	ImageTokens *int
	// OutputLimit is the client's output limit; nil when absent.
	OutputLimit         *int
	DefaultOutputTokens int
	MaxTokensLimit      int
	// Searches are the web searches the request may run on the native search
	// of the endpoint's provider, at most 32; searchCost prices them.
	Searches int
	// ManualSearch is a search on an engine without an endpoint price, which
	// SearchReserveUSD alone covers.
	ManualSearch bool
	// ApproximateReserveUSD is the whole-hold reserve, AddOnReserveUSD the
	// reserve added for hosted search of a priced endpoint and
	// SearchReserveUSD the reserve for requested hosted search that no
	// endpoint price covers (searchCost).
	ApproximateReserveUSD, AddOnReserveUSD, SearchReserveUSD *float64
}

// Estimate is the reserve of a request. InputUSD, PromptPriceUSD,
// OutputPriceUSD and OutputTokens bound a text reserve priced from endpoint
// tariffs and stay zero otherwise.
type Estimate struct {
	USD                                      float64
	InputUSD, PromptPriceUSD, OutputPriceUSD decimal.Decimal
	OutputTokens                             int
	// IncludesAddOnReserve reports that the maximum endpoint cost includes the
	// add-on or the search reserve.
	IncludesAddOnReserve bool
	// Source is what the reserve is priced from (reserveSource).
	Source domain.ReserveSource
	// StaleCatalog reports that a stale catalog document priced a model,
	// whatever the Source.
	StaleCatalog bool
}

type Usecase struct {
	deps    domain.Capabilities
	catalog CatalogTariffs
	now     func() time.Time
	// maxAge bounds the age of the catalog data a reserve is priced from.
	maxAge func() time.Duration
}

func NewUsecase(deps domain.Capabilities, catalog CatalogTariffs, now func() time.Time, maxAge func() time.Duration) Usecase {
	return Usecase{deps: deps, catalog: catalog, now: now, maxAge: maxAge}
}

// Execute prices the reserve from the endpoint tariffs of every routable model
// (loadTariffs). A model priced from the fresh catalog is checked against live
// when a price it needs is missing, when no endpoint of it passes the filters
// or when provider.require_parameters excludes one of them: it is priced again
// from live, so that the client gets the result of live pricing. When live
// fails, a missing price is priced from the document as live would price it,
// with the local cover; a pool the filters emptied or narrowed, of a fresh or a
// stale document, never prices the model, so the reserve is never below live:
// only the local cover covers it, otherwise the live error refuses the request.
func (u Usecase) Execute(in Input) (Estimate, error) {
	tariffs, err := u.loadTariffs(in)
	if err != nil {
		return Estimate{}, err
	}
	for {
		estimate, recheck, err := u.price(in, tariffs)
		if recheck == "" {
			return estimate, err
		}
		tariffs.recheckLive(in.Live, recheck)
	}
}

// price takes the complete maximum over the eligible endpoints of every
// routable model. Price strings are USD per token/request/image. An
// unavailable monetary estimate is a *domain.EstimateUnavailableError; with
// an add-on reserve it is returned at the first such endpoint. The tariff
// model it returns instead is a catalog tariff to price again from live
// (modelTariffs.checksLive); it is empty otherwise.
func (u Usecase) price(in Input, tariffsByModel requestTariffs) (Estimate, string, error) {
	type reservationCandidate struct {
		tariff       domain.NativePricedEndpoint
		output       int
		pricingErr   error
		flexFallback bool
	}
	approximateReserve := in.ApproximateReserveUSD != nil
	addOnReserve := in.AddOnReserveUSD != nil
	outputReserve := in.TextOutput && !approximateReserve
	// localCover is the explicit local/minimum policy that alone covers an
	// unavailable monetary estimate.
	localCover := in.ConfiguredPrice || (in.Decisions || in.Audio || in.Units != nil || in.ImageReserveOnly) && approximateReserve
	var estimate Estimate
	var maximum decimal.Decimal
	found := false
	// Exclusion details explain an empty pool without changing its status or
	// code. A filter is named only when no endpoint passed it.
	var unmatchedQuantizations, unsupportedParameters []string
	quantizationMatched, parametersSupported := false, false
	for _, model := range in.Models {
		if model == "perplexity/sonar-deep-research" && !approximateReserve {
			return Estimate{}, "", errors.New("OpenRouter Sonar research reserve policy is disabled")
		}
		if strings.HasSuffix(model, ":online") {
			return Estimate{}, "", errors.New("OpenRouter online routing includes unpriced hosted search")
		}
		tariffModel := domain.UpstreamTariffModel(model, in.Batch)
		modelTariff := tariffsByModel[tariffModel]
		endpoints, declared := modelTariff.response.Endpoints, modelTariff.response.OutputModalities
		if in.Batch {
			// Batch does not check the output modalities its :batch variant
			// declares.
			declared = nil
		}
		if in.Rerank && !slices.Contains(declared, "rerank") {
			return Estimate{}, "", errors.New("OpenRouter model does not declare rerank output capability")
		}
		if in.Decisions && !slices.Contains(declared, "decisions") {
			return Estimate{}, "", errors.New("OpenRouter model does not declare decisions output capability")
		}
		if in.ImageReserveOnly && !slices.Contains(declared, "image") {
			return Estimate{}, "", errors.New("OpenRouter model does not declare image output capability")
		}
		if (in.Audio || in.Units != nil) && len(declared) == 0 {
			return Estimate{}, "", errors.New("OpenRouter audio model output capability is missing")
		}
		if declared != nil {
			if in.ConfiguredPrice && len(in.Modalities) == 0 && !in.Decisions && !in.Rerank {
				for _, modality := range declared {
					if modality != "text" && modality != "image" {
						return Estimate{}, "", errors.New("OpenRouter configured price reserve requires explicit text/image modalities for this model")
					}
				}
			}
			for _, modality := range in.Modalities {
				if !slices.Contains(declared, modality) {
					return Estimate{}, "", errors.New("OpenRouter model does not support requested output modality")
				}
			}
		}
		var candidates []reservationCandidate
		var flexTariffs []domain.NativePricedEndpoint
		flexPriceUnknown, parametersExcluded := false, false
		for _, endpoint := range endpoints {
			tag := strings.ToLower(endpoint.Tag)
			if base, ok := strings.CutSuffix(tag, "/priority"); ok {
				tag = base + "/fast"
			}

			matches := func(slug string) bool {
				slug = strings.ToLower(slug)
				return tag == slug || strings.HasPrefix(tag, slug+"/")
			}
			allowed := len(in.Provider.Only) == 0 || slices.ContainsFunc(in.Provider.Only, matches)
			if !allowed || slices.ContainsFunc(in.Provider.Ignore, matches) {
				continue
			}
			if in.Provider.AllowFallbacks != nil && !*in.Provider.AllowFallbacks && len(in.Provider.Order) > 0 && !slices.ContainsFunc(in.Provider.Order, matches) {
				continue
			}
			if len(in.Provider.Quantizations) > 0 && !slices.Contains(in.Provider.Quantizations, endpoint.Quantization) {
				unmatchedQuantizations = append(unmatchedQuantizations, cmp.Or(endpoint.Quantization, "unspecified"))
				continue
			}
			quantizationMatched = true
			if endpoint.Tag == "" {
				return Estimate{}, "", errors.New("OpenRouter endpoint tag is missing")
			}
			// Service tiers require explicit opt-in; ordinary variants such as
			// deepinfra/turbo remain in the base provider's eligible pool.
			tier := domain.EndpointServiceTier(tag)
			requestedTier := domain.ServiceTier(in.ServiceTier)
			if in.ServiceTier == "fast" || in.Speed == "fast" {
				requestedTier = domain.ServiceTierPriority
			}
			explicitTier := slices.Contains(in.Provider.Only, tag) || slices.Contains(in.Provider.Order, tag)
			if !explicitTier {
				switch tier {
				case domain.ServiceTierUltrafast:
					if requestedTier != domain.ServiceTierUltrafast {
						continue
					}
				case domain.ServiceTierPriority:
					if requestedTier != domain.ServiceTierPriority && requestedTier != domain.ServiceTierUltrafast && !(requestedTier == "" && len(in.Provider.Order) == 0 && strings.HasSuffix(model, ":nitro")) {
						continue
					}
				case domain.ServiceTierFlex:
					if requestedTier != domain.ServiceTierFlex && !(requestedTier == "" && len(in.Provider.Order) == 0 && strings.HasSuffix(model, ":floor")) {
						continue
					}
				}
			}
			if in.ImageTokens != nil && in.Images > 0 && !matches("cohere") {
				return Estimate{}, "", errors.New("OpenRouter Cohere Embed image tokens are measured only on the Cohere provider")
			}

			if endpoint.ContextLength < 0 || endpoint.MaxPromptTokens < 0 || endpoint.MaxCompletionTokens < 0 {
				return Estimate{}, "", errors.New("invalid OpenRouter endpoint token limits")
			}
			if endpoint.ContextLength > 0 && in.PromptTokens > endpoint.ContextLength || endpoint.MaxPromptTokens > 0 && in.PromptTokens > endpoint.MaxPromptTokens {
				continue
			}
			output := 0
			if in.TextOutput {
				if in.OutputLimit != nil {
					output = *in.OutputLimit
					if output < 0 || output > in.MaxTokensLimit {
						return Estimate{}, "", errors.New("invalid OpenRouter output cap")
					}
				} else {
					// Without a client limit the reserve covers a bounded answer.
					// The upstream request keeps no limit and settles at usage.cost.
					output = in.DefaultOutputTokens
					if endpoint.MaxCompletionTokens > 0 {
						output = min(output, endpoint.MaxCompletionTokens)
					}
				}
				if endpoint.MaxCompletionTokens > 0 && output > endpoint.MaxCompletionTokens {
					continue
				}
				if endpoint.ContextLength > 0 {
					available := max(0, endpoint.ContextLength-in.PromptTokens)
					if in.OutputLimit != nil && output > available {
						continue
					}
					output = min(output, available)
				}
			}
			if in.Provider.RequireParameters {
				var unsupported []string
				for key := range in.Parameters {
					if in.Decisions {
						switch key {
						case "state", "questions", "session_id", "trace", "user":
							continue
						}
					}
					// usage is OpenRouter's usage-accounting control, injected by the
					// adaptor and never listed in supported_parameters. Admitting more
					// endpoints only widens the pool whose maximum cost is reserved.
					switch key {
					case "model", "models", "messages", "input", "prompt", "provider", "stream", "stream_options", "usage":
						continue
					}
					if !slices.Contains(endpoint.SupportedParameters, key) {
						unsupported = append(unsupported, key)
					}
				}
				if len(unsupported) > 0 {
					unsupportedParameters = append(unsupportedParameters, unsupported...)
					parametersExcluded = true
					continue
				}
				parametersSupported = true
			}
			if approximateReserve {
				if in.Model == "perplexity/sonar-deep-research" && tag != "perplexity" {
					return Estimate{}, "", errors.New("OpenRouter Sonar research reserve requires the Perplexity endpoint")
				}
				if endpoint.ModelID != "" && endpoint.ModelID != in.Model {
					return Estimate{}, "", errors.New("OpenRouter reserve endpoint model does not match final model ID")
				}
			}
			tariffs, pricingErr := u.endpointPricingTariffs(endpoint, in.PromptTokens, in.MaxTokensLimit)
			if pricingErr != nil {
				if modelTariff.missesCatalogPrice(pricingErr) {
					return Estimate{}, tariffModel, nil
				}
				if len(in.Provider.MaxPrice) > 0 {
					return Estimate{}, "", pricingErr
				}
				// Keep the discovered route identity for local fallback, without
				// inventing a tariff when its monetary declaration is unavailable.
				tariffs = []domain.NativePricedEndpoint{endpoint}
			}
			for _, tariff := range tariffs {
				excluded := false
				for key, raw := range in.Provider.MaxPrice {
					rate, err := domain.ReadEndpointRate(u.deps, tariff.Pricing[key])
					if modelTariff.missesCatalogPrice(err) {
						return Estimate{}, tariffModel, nil
					}
					if err != nil {
						return Estimate{}, "", err
					}
					limit, err := domain.ReadEndpointRate(u.deps, raw)
					if err != nil {
						return Estimate{}, "", err
					}
					if key == "prompt" || key == "completion" {
						rate = rate.Mul(decimal.NewFromInt(1000000))
					}
					if rate.GreaterThan(limit) {
						excluded = true
					}
				}
				if excluded {
					continue
				}
				candidates = append(candidates, reservationCandidate{
					tariff: tariff, output: output, pricingErr: pricingErr,
					flexFallback: requestedTier == domain.ServiceTierFlex && tier == domain.ServiceTierDefault && !explicitTier,
				})
				if requestedTier == domain.ServiceTierFlex && tier == domain.ServiceTierFlex {
					flexTariffs = append(flexTariffs, tariff)
					flexPriceUnknown = flexPriceUnknown || pricingErr != nil
				}
			}
		}
		poolNarrowed := len(candidates) == 0 || parametersExcluded
		if modelTariff.checksLive() && poolNarrowed {
			return Estimate{}, tariffModel, nil
		}
		if modelTariff.liveErr != nil && poolNarrowed {
			// A pool the filters narrowed may hold fewer endpoints than live,
			// so it never prices the model: only the local cover does, as for
			// an unknown price, and otherwise the live error refuses it.
			if addOnReserve {
				return Estimate{}, "", &domain.EstimateUnavailableError{Cause: modelTariff.liveErr}
			}
			if !localCover {
				return Estimate{}, "", modelTariff.liveErr
			}
			if *in.ApproximateReserveUSD <= 0 {
				return Estimate{}, "", errors.New("OpenRouter unavailable estimate requires a positive reserve")
			}
			found = true
			continue
		}
		// Only flex tariffs surviving the same route, capability, limit and
		// max-price checks may constrain the default fallback reservation.
		for _, candidate := range candidates {
			if candidate.flexFallback && !flexPriceUnknown && u.exceedsFlexPriceBounds(candidate.tariff, flexTariffs) {
				continue
			}
			tariff, output, pricingErr := candidate.tariff, candidate.output, candidate.pricingErr
			var cost, promptPrice, outputPrice decimal.Decimal
			var estimateErr error
			includesReserve := false
			if in.Decisions {
				// State may contain arbitrary structures/media; token estimates are unmeasured.
				estimateErr = &domain.EstimateUnavailableError{Cause: errors.New("OpenRouter decisions monetary quantity is unmeasured")}
			} else if in.Audio {
				// Audio rates may be per character, second, token or song.
				// Token caps and zero token tariffs do not price those quantities.
				estimateErr = &domain.EstimateUnavailableError{Cause: errors.New("OpenRouter audio monetary quantity is unmeasured")}
			} else if in.Units != nil {
				// The output audio tokens are the endpoint output cap, the
				// default output bound without one.
				cost, estimateErr = u.unitCost(tariff, in.Units, cmp.Or(tariff.MaxCompletionTokens, in.DefaultOutputTokens))
			} else if pricingErr != nil {
				estimateErr = &domain.EstimateUnavailableError{Cause: pricingErr}
			} else {
				cost, promptPrice, outputPrice, estimateErr = u.estimateEndpointCost(tariff, model, in.PromptTokens, output, in.Count, in.Images, in.ImageTokens, in.ApproximateReserveUSD, in.ImageOutput)
				if estimateErr == nil {
					search, reserved, err := u.searchCost(in, tariff, modelTariff)
					if modelTariff.missesCatalogPrice(err) {
						return Estimate{}, tariffModel, nil
					}
					if err != nil {
						return Estimate{}, "", err
					}
					cost, includesReserve = cost.Add(search), reserved
				}
			}
			if estimateErr != nil {
				if modelTariff.missesCatalogPrice(estimateErr) {
					return Estimate{}, tariffModel, nil
				}
				var unavailable *domain.EstimateUnavailableError
				if addOnReserve && errors.As(estimateErr, &unavailable) {
					return Estimate{}, "", estimateErr
				}
				if !localCover || !errors.As(estimateErr, &unavailable) {
					return Estimate{}, "", estimateErr
				}
				if *in.ApproximateReserveUSD <= 0 {
					return Estimate{}, "", errors.New("OpenRouter unavailable estimate requires a positive reserve")
				}
				// This endpoint is eligible, but its unknown monetary quantity is
				// covered only by the explicit local/minimum policy.
				found = true
				continue
			}
			if outputReserve {
				responsesOutputPrice := outputPrice.Mul(decimal.NewFromInt(int64(in.Count)))
				estimate.InputUSD = decimal.Max(estimate.InputUSD, cost.Sub(responsesOutputPrice.Mul(decimal.NewFromInt(int64(output)))))
				estimate.PromptPriceUSD = decimal.Max(estimate.PromptPriceUSD, promptPrice)
				estimate.OutputPriceUSD = decimal.Max(estimate.OutputPriceUSD, responsesOutputPrice)
				estimate.OutputTokens = max(estimate.OutputTokens, output)
			}
			if !found || cost.GreaterThan(maximum) {
				maximum = cost
				estimate.IncludesAddOnReserve = includesReserve
			}
			found = true
		}
	}
	if !found {
		var reasons []string
		if len(unmatchedQuantizations) > 0 && !quantizationMatched {
			slices.Sort(unmatchedQuantizations)
			reasons = append(reasons, "no OpenRouter endpoint matches the requested quantizations; endpoints report: "+strings.Join(slices.Compact(unmatchedQuantizations), ", "))
		}
		if len(unsupportedParameters) > 0 && !parametersSupported {
			slices.Sort(unsupportedParameters)
			reasons = append(reasons, "provider.require_parameters excluded endpoints that do not support: "+strings.Join(slices.Compact(unsupportedParameters), ", "))
		}
		if len(reasons) > 0 {
			return Estimate{}, "", fmt.Errorf("%w: %s", domain.ErrNoCompatibleEndpoint, strings.Join(reasons, "; "))
		}
		return Estimate{}, "", domain.ErrNoCompatibleEndpoint
	}
	if approximateReserve {
		maximum = decimal.Max(maximum, decimal.NewFromFloat(*in.ApproximateReserveUSD))
	}
	estimate.USD, _ = maximum.Float64()
	if math.IsInf(estimate.USD, 0) || math.IsNaN(estimate.USD) {
		return Estimate{}, "", errors.New("OpenRouter estimate overflow")
	}
	estimate.Source = reserveSource(in, tariffsByModel)
	estimate.StaleCatalog = tariffsByModel.hasStaleDocument()
	return estimate, "", nil
}

// exceedsFlexPriceBounds excludes only fallback tariffs proven to exceed
// every eligible flex tariff on input or output price. Unknown monetary data
// stays in the reservation pool for strict validation or explicit local cover.
func (u Usecase) exceedsFlexPriceBounds(endpoint domain.NativePricedEndpoint, flexTariffs []domain.NativePricedEndpoint) bool {
	if len(flexTariffs) == 0 {
		return false
	}
	prompt, promptErr := domain.ReadEndpointRate(u.deps, endpoint.Pricing["prompt"])
	completion, completionErr := domain.ReadEndpointRate(u.deps, endpoint.Pricing["completion"])
	if promptErr != nil || completionErr != nil {
		return false
	}
	for _, flex := range flexTariffs {
		flexPrompt, promptErr := domain.ReadEndpointRate(u.deps, flex.Pricing["prompt"])
		flexCompletion, completionErr := domain.ReadEndpointRate(u.deps, flex.Pricing["completion"])
		if promptErr != nil || completionErr != nil {
			return false
		}
		if !prompt.GreaterThan(flexPrompt) && !completion.GreaterThan(flexCompletion) {
			return false
		}
	}
	return true
}

// estimateEndpointCost contains monetary metadata/quantity evaluation only.
// Eligibility is established by the caller before any failure can become fallback.
// promptPrice is the price of one prompt token; outputPrice is the price of one
// output token, zero when output is zero. imageTokens is nil when the image
// token quantity is unmeasured, so a non-zero image_token rate stays unpriced.
func (u Usecase) estimateEndpointCost(endpoint domain.NativePricedEndpoint, model string, prompt, output, count, imageCount int, imageTokens *int, reserveUSD *float64, imageOutput bool) (result, promptPrice, outputPrice decimal.Decimal, err error) {
	defer func() {
		if err != nil {
			err = &domain.EstimateUnavailableError{Cause: err}
		}
	}()
	approximateReserve := reserveUSD != nil
	sonarModel := model == "perplexity/sonar-deep-research"
	if approximateReserve {
		for _, key := range []string{"prompt", "completion", "internal_reasoning", "web_search"} {
			if _, ok := endpoint.Pricing[key]; !ok && !sonarModel && (key == "internal_reasoning" || key == "web_search") {
				continue
			}
			rate, err := domain.ReadEndpointRate(u.deps, endpoint.Pricing[key])
			if err != nil {
				return decimal.Zero, decimal.Zero, decimal.Zero, err
			}
			if *reserveUSD == 0 && (key == "internal_reasoning" || key == "web_search") && !rate.IsZero() {
				return decimal.Zero, decimal.Zero, decimal.Zero, errors.New("OpenRouter unknown billable quantity requires a positive reserve")
			}
		}
	}
	if imageOutput {
		rate, err := domain.ReadEndpointRate(u.deps, endpoint.Pricing["image_output"])
		if err != nil {
			return decimal.Zero, decimal.Zero, decimal.Zero, fmt.Errorf("OpenRouter image output tariff: %w", err)
		}
		if !rate.IsZero() && *reserveUSD == 0 {
			return decimal.Zero, decimal.Zero, decimal.Zero, errors.New("OpenRouter unknown image output quantity requires a positive reserve")
		}
	}
	promptRate, err := domain.ReadEndpointRate(u.deps, endpoint.Pricing["prompt"])
	if err != nil {
		return decimal.Zero, decimal.Zero, decimal.Zero, err
	}
	for _, key := range []string{"input_cache_read", "input_cache_write", "input_cache_write_1h"} {
		if raw, ok := endpoint.Pricing[key]; ok {
			rate, err := domain.ReadEndpointRate(u.deps, raw)
			if err != nil {
				return decimal.Zero, decimal.Zero, decimal.Zero, err
			}
			if rate.GreaterThan(promptRate) {
				promptRate = rate
			}
		}
	}
	cost := promptRate.Mul(decimal.NewFromInt(int64(prompt)))
	if output > 0 {
		rate, err := domain.ReadEndpointRate(u.deps, endpoint.Pricing["completion"])
		if err != nil {
			return decimal.Zero, decimal.Zero, decimal.Zero, err
		}
		if raw, ok := endpoint.Pricing["internal_reasoning"]; ok && strings.HasPrefix(model, "google/gemini-") {
			reasoningRate, err := domain.ReadEndpointRate(u.deps, raw)
			if err != nil {
				return decimal.Zero, decimal.Zero, decimal.Zero, err
			}
			if reasoningRate.GreaterThan(rate) {
				rate = reasoningRate
			}
		}
		cost = cost.Add(rate.Mul(decimal.NewFromInt(int64(output))).Mul(decimal.NewFromInt(int64(count))))
		outputPrice = rate
	}
	if raw, ok := endpoint.Pricing["request"]; ok {
		rate, err := domain.ReadEndpointRate(u.deps, raw)
		if err != nil {
			return decimal.Zero, decimal.Zero, decimal.Zero, err
		}
		cost = cost.Add(rate)
	}
	if imageCount > 0 {
		if raw, ok := endpoint.Pricing["image"]; ok {
			rate, err := domain.ReadEndpointRate(u.deps, raw)
			if err != nil {
				return decimal.Zero, decimal.Zero, decimal.Zero, err
			}
			cost = cost.Add(rate.Mul(decimal.NewFromInt(int64(imageCount))))
		}
	}
	if raw, ok := endpoint.Pricing["image_token"]; ok && imageTokens != nil {
		rate, err := domain.ReadEndpointRate(u.deps, raw)
		if err != nil {
			return decimal.Zero, decimal.Zero, decimal.Zero, err
		}
		cost = cost.Add(rate.Mul(decimal.NewFromInt(int64(*imageTokens))))
	}
	for key, raw := range endpoint.Pricing {
		if key == "image_token" && imageTokens != nil {
			continue
		}
		if imageOutput && (key == "image_output" || key == "audio" || key == "input_audio_cache" || key == "input_audio" || key == "output_audio") {
			// Image output quantities are covered by the explicit hold, not
			// inferred from a text token cap. Audio input/output is absent in
			// this admitted protocol; still validate published inactive rates.
			if _, err := domain.ReadEndpointRate(u.deps, raw); err != nil {
				return decimal.Zero, decimal.Zero, decimal.Zero, err
			}
			continue
		}
		if approximateReserve && (key == "internal_reasoning" || key == "web_search") {
			// Rates were validated above. Quantities remain unknown; the
			// explicit approximate hold does not make them free or bounded.
			continue
		}
		if approximateReserve && (key == "audio" || key == "input_audio" || key == "input_audio_cache") {
			// The gateway rejects every audio input block before estimation,
			// so these published input rates are inactive here.
			if _, err := domain.ReadEndpointRate(u.deps, raw); err != nil {
				return decimal.Zero, decimal.Zero, decimal.Zero, err
			}
			continue
		}
		switch key {
		case "prompt", "completion", "request", "input_cache_read", "input_cache_write", "input_cache_write_1h", "discount", "image":
			continue
		}
		if !approximateReserve && (key == "audio" || key == "input_audio_cache" || key == "internal_reasoning" && strings.HasPrefix(model, "google/gemini-")) {
			if _, err := domain.ReadEndpointRate(u.deps, raw); err != nil {
				return decimal.Zero, decimal.Zero, decimal.Zero, err
			}
			continue
		}
		if !approximateReserve && (key == "input_audio" || key == "output_audio" || key == "web_search") {
			continue
		}
		if key == "overrides" {
			return decimal.Zero, decimal.Zero, decimal.Zero, errors.New("OpenRouter endpoint has unsupported request-dependent pricing overrides")
		}
		rate, err := domain.ReadEndpointRate(u.deps, raw)
		if err != nil {
			return decimal.Zero, decimal.Zero, decimal.Zero, err
		}
		if !rate.IsZero() {
			return decimal.Zero, decimal.Zero, decimal.Zero, fmt.Errorf("OpenRouter endpoint has unsupported billable dimension %s", key)
		}
	}
	result = cost
	value, _ := result.Float64()
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return decimal.Zero, decimal.Zero, decimal.Zero, errors.New("OpenRouter estimate overflow")
	}
	return result, promptRate, outputPrice, nil
}
