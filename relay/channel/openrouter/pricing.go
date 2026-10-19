package openrouter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// EstimateUnavailableError is a monetary metadata failure, not a route or
// transport failure. Only an explicit local reserve policy may cover it.
type EstimateUnavailableError struct{ Cause error }

func (e *EstimateUnavailableError) Error() string {
	return "OpenRouter estimate unavailable: " + e.Cause.Error()
}
func (e *EstimateUnavailableError) Unwrap() error { return e.Cause }

type PricedEndpoint struct {
	Status              *int                       `json:"status"`
	ModelID             string                     `json:"model_id"`
	Tag                 string                     `json:"tag"`
	ContextLength       int                        `json:"context_length"`
	MaxPromptTokens     int                        `json:"max_prompt_tokens"`
	MaxCompletionTokens int                        `json:"max_completion_tokens"`
	Quantization        string                     `json:"quantization"`
	SupportedParameters []string                   `json:"supported_parameters"`
	Pricing             map[string]json.RawMessage `json:"pricing"`
}

// Decode route metadata independently from monetary data. An absent or changed
// pricing shape is not evidence that this endpoint/model is unavailable.
func (e *PricedEndpoint) UnmarshalJSON(data []byte) error {
	type route PricedEndpoint
	value := struct {
		*route
		Pricing json.RawMessage `json:"pricing"`
	}{route: (*route)(e)}
	if err := common.Unmarshal(data, &value); err != nil {
		return err
	}
	e.Pricing = nil
	if len(value.Pricing) > 0 {
		if err := common.Unmarshal(value.Pricing, &e.Pricing); err != nil {
			e.Pricing = nil
		}
	}
	return nil
}

// RequestEstimator is request-scoped discovery, reused for all items in a
// batch. It deliberately has no global cache or routing lifecycle.
type RequestEstimator struct {
	Models           map[string][]PricedEndpoint
	OutputModalities map[string][]string
}

func NewRequestEstimator() *RequestEstimator {
	return &RequestEstimator{Models: make(map[string][]PricedEndpoint), OutputModalities: make(map[string][]string)}
}

func (e *RequestEstimator) Discover(c *gin.Context, info *relaycommon.RelayInfo, adaptor channel.Adaptor, model string) error {
	if _, ok := e.Models[model]; ok {
		return nil
	}
	discoveryModel := strings.TrimSuffix(strings.TrimSuffix(model, ":nitro"), ":floor")
	parts := strings.SplitN(discoveryModel, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return errors.New("invalid OpenRouter model identity")
	}
	endpoint := strings.TrimRight(info.ChannelBaseUrl, "/") + "/v1/models/" + url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]) + "/endpoints"
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	if err = adaptor.SetupRequestHeader(c, &req.Header, info); err != nil {
		return err
	}
	overrides, err := channel.ResolveHeaderOverride(info, c)
	if err != nil {
		return err
	}
	for key, value := range overrides {
		req.Header.Set(key, value)
	}
	client, err := service.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
	if err != nil {
		return errors.New("OpenRouter pricing discovery transport unavailable")
	}
	metadataClient := *client
	metadataClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := metadataClient.Do(req)
	if err != nil {
		return errors.New("OpenRouter pricing discovery failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("OpenRouter pricing discovery HTTP %d", resp.StatusCode)
	}
	var result struct {
		Data struct {
			ID           string `json:"id"`
			Architecture struct {
				OutputModalities []string `json:"output_modalities"`
			} `json:"architecture"`
			Endpoints []PricedEndpoint `json:"endpoints"`
		} `json:"data"`
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return errors.New("OpenRouter pricing discovery exceeds limit")
	}
	if err := common.Unmarshal(data, &result); err != nil {
		return errors.New("invalid OpenRouter endpoint discovery metadata")
	}
	if len(result.Data.Endpoints) == 0 {
		return errors.New("OpenRouter model has no available endpoints")
	}
	if result.Data.ID == "" && info.RelayFormat == types.RelayFormatSystemOne {
		return errors.New("OpenRouter decisions discovery model identity is missing")
	}
	if result.Data.ID != "" && result.Data.ID != discoveryModel {
		return errors.New("OpenRouter pricing discovery model mismatch")
	}
	e.Models[model] = result.Data.Endpoints
	if e.OutputModalities == nil {
		e.OutputModalities = make(map[string][]string)
	}
	e.OutputModalities[model] = result.Data.Architecture.OutputModalities
	return nil
}

func readRate(raw json.RawMessage) (decimal.Decimal, error) {
	value := strings.TrimSpace(string(raw))
	if len(value) > 1 && value[0] == '"' {
		if common.Unmarshal(raw, &value) != nil {
			return decimal.Zero, errors.New("invalid OpenRouter endpoint price")
		}
	}
	finite, parseErr := strconv.ParseFloat(value, 64)
	if parseErr != nil || math.IsInf(finite, 0) || math.IsNaN(finite) || finite < 0 {
		return decimal.Zero, errors.New("invalid OpenRouter endpoint price")
	}
	rate, err := decimal.NewFromString(value)
	if err != nil || rate.IsNegative() || rate.Exponent() < -324 || rate.Exponent() > 308 {
		return decimal.Zero, errors.New("missing or invalid OpenRouter endpoint price")
	}
	return rate, nil
}

func ReadEndpointPrice(raw json.RawMessage) (float64, error) {
	rate, err := readRate(raw)
	if err != nil {
		return 0, err
	}
	amount, _ := rate.Float64()
	if amount == 0 && !rate.IsZero() {
		return 0, errors.New("native endpoint price underflows display precision")
	}
	return amount, nil
}

// Estimate takes the complete maximum over eligible endpoints, preserving the
// actual routing payload. Price strings are USD per token/request/image.
func (e *RequestEstimator) Estimate(info *relaycommon.RelayInfo, body []byte, operation string) (float64, error) {
	return e.estimate(info, body, operation, nil)
}

// EstimateWithLocalPrice retains all endpoint and request guards. A configured
// price preserves known complete estimates and covers unavailable monetary metadata.
func (e *RequestEstimator) EstimateWithLocalPrice(info *relaycommon.RelayInfo, body []byte, operation string, localUSD float64) (float64, error) {
	return e.estimate(info, body, operation, &localUSD)
}

func (e *RequestEstimator) estimate(info *relaycommon.RelayInfo, body []byte, operation string, localUSD *float64) (float64, error) {
	var request struct {
		Model       string          `json:"model"`
		Models      []string        `json:"models"`
		Route       string          `json:"route"`
		Preset      string          `json:"preset"`
		ServiceTier string          `json:"service_tier"`
		Speed       json.RawMessage `json:"speed"`
		Provider    struct {
			Only              []string                   `json:"only"`
			Ignore            []string                   `json:"ignore"`
			Order             []string                   `json:"order"`
			AllowFallbacks    *bool                      `json:"allow_fallbacks"`
			RequireParameters bool                       `json:"require_parameters"`
			Quantizations     []string                   `json:"quantizations"`
			MaxPrice          map[string]json.RawMessage `json:"max_price"`
		} `json:"provider"`
		MaxTokens           *int `json:"max_tokens"`
		MaxCompletionTokens *int `json:"max_completion_tokens"`
		MaxOutputTokens     *int `json:"max_output_tokens"`
		N                   *int `json:"n"`
		Stream              bool `json:"stream"`
		Tools               []struct {
			Type string `json:"type"`
		} `json:"tools"`
		Plugins []json.RawMessage `json:"plugins"`
	}
	if common.Unmarshal(body, &request) != nil {
		return 0, errors.New("invalid OpenRouter final request")
	}
	batch := strings.HasPrefix(operation, "batch_")
	operation = strings.TrimPrefix(operation, "batch_")
	if batch {
		// Provider is projected from the durable batch envelope by admission;
		// individual item preferences never define native Batch routing.
		request.ServiceTier = ""
		request.Speed = nil
	}

	if request.Route != "" && request.Route != "fallback" || request.Preset != "" || len(request.Plugins) > 0 {
		return 0, errors.New("OpenRouter router, preset or plugins have unpriced execution dimensions")
	}
	for _, tool := range request.Tools {
		if tool.Type != "function" && tool.Type != "custom" {
			return 0, errors.New("OpenRouter hosted tool pricing is unavailable")
		}
	}
	for _, slugs := range [][]string{request.Provider.Only, request.Provider.Ignore, request.Provider.Order} {
		for i := range slugs {
			slugs[i] = strings.ToLower(slugs[i])
			if base, ok := strings.CutSuffix(slugs[i], "/priority"); ok {
				slugs[i] = base + "/fast"
			}
		}
	}
	models := append([]string{}, request.Models...)
	if request.Model != "" {
		models = append(models, request.Model)
	}
	if len(models) == 0 {
		return 0, errors.New("OpenRouter request model is required")
	}
	var fields map[string]json.RawMessage
	if common.Unmarshal(body, &fields) != nil {
		return 0, errors.New("invalid OpenRouter request fields")
	}
	var modalities []string
	imageOutput := false
	audioOutput := false
	dedicatedAudio := operation == "audio/speech" || operation == "audio/transcriptions"
	decisions := operation == "decisions"
	if raw, ok := fields["modalities"]; ok {
		if common.Unmarshal(raw, &modalities) != nil {
			return 0, errors.New("invalid OpenRouter modalities")
		}
		for _, modality := range modalities {
			if modality == "image" {
				imageOutput = true
			} else if modality == "audio" {
				audioOutput = true
			} else if modality != "text" {
				return 0, errors.New("OpenRouter output modality tariff is unavailable")
			}
		}
	}
	if imageOutput && (localUSD == nil || info.RelayFormat != types.RelayFormatOpenAI) {
		return 0, errors.New("OpenRouter image output requires configured_price reservation and OpenAI Chat format")
	}
	if audioOutput && (imageOutput || operation != "chat/completions" || localUSD == nil || info.RelayFormat != types.RelayFormatOpenAI) {
		return 0, errors.New("OpenRouter audio output requires configured_price reservation and OpenAI Chat format")
	}
	if audioOutput && (!request.Stream || !info.IsStream) {
		return 0, errors.New("OpenRouter Chat audio output requires streaming")
	}
	if dedicatedAudio {
		if localUSD == nil || info.RelayFormat != types.RelayFormatOpenAIAudio {
			return 0, errors.New("OpenRouter audio endpoint requires configured_price reservation and OpenAI Audio format")
		}
		if operation == "audio/speech" {
			modalities = []string{"speech"}
		} else {
			modalities = []string{"transcription"}
		}
		if format, present := fields["response_format"]; present {
			var value string
			if common.Unmarshal(format, &value) != nil || operation == "audio/transcriptions" && value != "" && value != "json" && value != "verbose_json" {
				return 0, errors.New("OpenRouter transcription requires json or verbose_json response format")
			}
		}
		var provider map[string]json.RawMessage
		if raw, present := fields["provider"]; present && common.Unmarshal(raw, &provider) != nil {
			return 0, errors.New("invalid OpenRouter audio provider options")
		}
		// Speech/transcription do not apply Chat provider routing preferences.
		// Never claim that a Chat filter restricted their discovered price pool.
		for key := range provider {
			if key != "zdr" && key != "data_collection" && key != "options" {
				return 0, errors.New("OpenRouter audio endpoints do not support Chat provider routing filters")
			}
		}
	}
	routingSpeed := ""
	if !dedicatedAudio && len(request.Speed) > 0 && string(request.Speed) != "null" {
		if common.Unmarshal(request.Speed, &routingSpeed) != nil {
			return 0, errors.New("invalid OpenRouter routing speed")
		}
	}
	imageCount, err := requestInputMediaQuantities(fields)
	if err != nil {
		return 0, err
	}
	prompt := max(info.GetEstimatePromptTokens(), service.CountTextToken(string(body), request.Model))
	if dedicatedAudio || decisions {
		prompt = 0
	}
	count := 1
	if request.N != nil {
		count = *request.N
	}
	if count < 1 || count > dto.MaxImageN {
		return 0, errors.New("invalid OpenRouter output count")
	}
	// This server-owned policy accepts an approximate hold, not a spending bound.
	var reserveUSD *float64
	if info.ChannelMeta != nil {
		reserveUSD = info.ChannelSetting.OpenRouterReserveUSD(request.Model)
	}
	if localUSD != nil {
		if math.IsNaN(*localUSD) || math.IsInf(*localUSD, 0) || *localUSD < 0 {
			return 0, errors.New("invalid OpenRouter configured price reserve")
		}
		if reserveUSD != nil && (math.IsNaN(*reserveUSD) || math.IsInf(*reserveUSD, 0) || *reserveUSD <= 0) {
			return 0, errors.New("invalid OpenRouter approximate reserve")
		}
		minimum := *localUSD
		if reserveUSD != nil {
			minimum = max(minimum, *reserveUSD)
		}
		reserveUSD = &minimum
	}
	approximateReserve := reserveUSD != nil
	sonarModel := request.Model == "perplexity/sonar-deep-research"
	if approximateReserve {
		if math.IsNaN(*reserveUSD) || math.IsInf(*reserveUSD, 0) || *reserveUSD < 0 || *reserveUSD == 0 && localUSD == nil {
			return 0, errors.New("invalid OpenRouter approximate reserve")
		}
		streamAllowed := audioOutput && localUSD != nil && request.Stream && info.IsStream
		operationAllowed := operation == "chat/completions" || decisions || dedicatedAudio && localUSD != nil
		if info.ChannelType != constant.ChannelTypeOpenRouter || batch || !operationAllowed || (request.Stream || info.IsStream) && !streamAllowed || count != 1 || len(request.Models) > 0 || request.Route != "" || len(request.Tools) > 0 || imageCount > 0 {
			return 0, errors.New("OpenRouter approximate reserve only supports one nonstream text chat response without routing or tools")
		}
	}
	for _, cap := range []*int{request.MaxTokens, request.MaxCompletionTokens, request.MaxOutputTokens} {
		if cap != nil && (*cap < 0 || *cap > helper.MaxTokensLimit) {
			return 0, errors.New("invalid OpenRouter output cap")
		}
	}
	if localUSD != nil {
		if info.RelayFormat != types.RelayFormatOpenAI && !dedicatedAudio && !(decisions && info.RelayFormat == types.RelayFormatSystemOne) {
			return 0, errors.New("OpenRouter configured price reserve requires OpenAI Chat format")
		}
		if raw, present := fields["audio"]; present && string(bytes.TrimSpace(raw)) != "null" && !audioOutput {
			return 0, errors.New("OpenRouter audio output remains unsupported by configured price reserve")
		}
	}
	var maximum decimal.Decimal
	found := false
	for _, model := range models {
		if model == "perplexity/sonar-deep-research" && !approximateReserve {
			return 0, errors.New("OpenRouter Sonar research reserve policy is disabled")
		}
		if strings.HasSuffix(model, ":online") {
			return 0, errors.New("OpenRouter online routing includes unpriced hosted search")
		}
		endpoints, ok := e.Models[model]
		if !ok {
			return 0, fmt.Errorf("OpenRouter endpoint discovery missing for %s", model)
		}
		if decisions && !slices.Contains(e.OutputModalities[model], "decisions") {
			return 0, errors.New("OpenRouter model does not declare decisions output capability")
		}
		if (audioOutput || dedicatedAudio) && len(e.OutputModalities[model]) == 0 {
			return 0, errors.New("OpenRouter audio model output capability is missing")
		}
		if declared := e.OutputModalities[model]; declared != nil {
			if localUSD != nil && len(modalities) == 0 && !decisions {
				for _, modality := range declared {
					if modality != "text" && modality != "image" {
						return 0, errors.New("OpenRouter configured price reserve requires explicit text/image modalities for this model")
					}
				}
			}
			for _, modality := range modalities {
				if !slices.Contains(declared, modality) {
					return 0, errors.New("OpenRouter model does not support requested output modality")
				}
			}
		}
		hasFlex := false
		if request.ServiceTier == "flex" {
			for _, endpoint := range endpoints {
				if endpoint.Status != nil && *endpoint.Status != 0 {
					continue
				}
				tag := strings.ToLower(endpoint.Tag)
				if !strings.HasSuffix(tag, "/flex") {
					continue
				}
				matches := func(slug string) bool { return tag == slug || strings.HasPrefix(tag, slug+"/") }
				if len(request.Provider.Only) > 0 && !slices.ContainsFunc(request.Provider.Only, matches) || slices.ContainsFunc(request.Provider.Ignore, matches) {
					continue
				}
				if len(request.Provider.Quantizations) > 0 && !slices.Contains(request.Provider.Quantizations, endpoint.Quantization) {
					continue
				}
				hasFlex = true
			}
		}
		for _, endpoint := range endpoints {
			if endpoint.Status != nil && *endpoint.Status != 0 {
				continue
			}
			if endpoint.Tag == "" {
				return 0, errors.New("OpenRouter endpoint tag is missing")
			}
			tag := strings.ToLower(endpoint.Tag)
			if base, ok := strings.CutSuffix(tag, "/priority"); ok {
				tag = base + "/fast"
			}

			matches := func(slug string) bool {
				slug = strings.ToLower(slug)
				return tag == slug || strings.HasPrefix(tag, slug+"/")
			}
			allowed := len(request.Provider.Only) == 0 || slices.ContainsFunc(request.Provider.Only, matches)
			if !allowed || slices.ContainsFunc(request.Provider.Ignore, matches) {
				continue
			}
			if request.Provider.AllowFallbacks != nil && !*request.Provider.AllowFallbacks && len(request.Provider.Order) > 0 && !slices.ContainsFunc(request.Provider.Order, matches) {
				continue
			}
			if len(request.Provider.Quantizations) > 0 && !slices.Contains(request.Provider.Quantizations, endpoint.Quantization) {
				continue
			}
			// Service tiers require explicit opt-in; ordinary variants such as
			// deepinfra/turbo remain in the base provider's eligible pool.
			tier := "default"
			for _, candidate := range []string{"fast", "priority", "flex", "ultrafast"} {
				if strings.HasSuffix(tag, "/"+candidate) {
					tier = candidate
				}
			}
			if tier == "fast" {
				tier = "priority"
			}
			requestedTier := request.ServiceTier
			if requestedTier == "fast" || routingSpeed == "fast" {
				requestedTier = "priority"
			}
			explicitTier := slices.Contains(request.Provider.Only, tag) || slices.Contains(request.Provider.Order, tag)
			if !explicitTier {
				switch tier {
				case "default":
					if requestedTier == "flex" && hasFlex {
						continue
					}
				case "ultrafast":
					if requestedTier != "ultrafast" {
						continue
					}
				case "priority":
					if requestedTier != "priority" && requestedTier != "ultrafast" && !(requestedTier == "" && len(request.Provider.Order) == 0 && strings.HasSuffix(model, ":nitro")) {
						continue
					}
				case "flex":
					if requestedTier != "flex" && !(requestedTier == "" && len(request.Provider.Order) == 0 && strings.HasSuffix(model, ":floor")) {
						continue
					}
				}
			}

			if endpoint.ContextLength < 0 || endpoint.MaxPromptTokens < 0 || endpoint.MaxCompletionTokens < 0 {
				return 0, errors.New("invalid OpenRouter endpoint token limits")
			}
			if endpoint.ContextLength > 0 && prompt > endpoint.ContextLength || endpoint.MaxPromptTokens > 0 && prompt > endpoint.MaxPromptTokens {
				continue
			}
			output := 0
			outputUnknown := false
			if operation != "embeddings" && !dedicatedAudio && !decisions {
				cap := request.MaxOutputTokens
				if cap == nil {
					cap = request.MaxCompletionTokens
				}
				if cap == nil {
					cap = request.MaxTokens
				}
				if cap != nil {
					output = *cap
					if output < 0 || output > helper.MaxTokensLimit {
						return 0, errors.New("invalid OpenRouter output cap")
					}
				} else {
					output = endpoint.MaxCompletionTokens
					if output <= 0 {
						output = 0
						outputUnknown = true
					}
				}
				if endpoint.MaxCompletionTokens > 0 && output > endpoint.MaxCompletionTokens {
					continue
				}
				if endpoint.ContextLength > 0 {
					available := max(0, endpoint.ContextLength-prompt)
					if cap != nil && output > available {
						continue
					}
					output = min(output, available)
				}
			}
			excluded := false
			if request.Provider.RequireParameters {
				var fields map[string]json.RawMessage
				_ = common.Unmarshal(body, &fields)
				for key := range fields {
					if decisions {
						switch key {
						case "state", "questions", "session_id", "trace", "user":
							continue
						}
					}
					switch key {
					case "model", "models", "messages", "input", "prompt", "provider", "stream", "stream_options":
						continue
					}
					if !slices.Contains(endpoint.SupportedParameters, key) {
						excluded = true
						break
					}
				}
				if excluded {
					continue
				}
			}
			if approximateReserve {
				if sonarModel && tag != "perplexity" {
					return 0, errors.New("OpenRouter Sonar research reserve requires the Perplexity endpoint")
				}
				if endpoint.ModelID != "" && endpoint.ModelID != request.Model {
					return 0, errors.New("OpenRouter reserve endpoint model does not match final model ID")
				}
			}
			tariffs, pricingErr := endpointPricingTariffs(endpoint, prompt)
			if pricingErr != nil {
				if len(request.Provider.MaxPrice) > 0 {
					return 0, pricingErr
				}
				// Keep the discovered route identity for local fallback, without
				// inventing a tariff when its monetary declaration is unavailable.
				tariffs = []PricedEndpoint{endpoint}
			}
			for _, tariff := range tariffs {
				excluded := false
				for key, raw := range request.Provider.MaxPrice {
					rate, err := readRate(tariff.Pricing[key])
					if err != nil {
						return 0, err
					}
					limit, err := readRate(raw)
					if err != nil {
						return 0, err
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
				var cost decimal.Decimal
				var estimateErr error
				if decisions {
					// State may contain arbitrary structures/media; token estimates are unmeasured.
					estimateErr = &EstimateUnavailableError{Cause: errors.New("OpenRouter decisions monetary quantity is unmeasured")}
				} else if audioOutput || dedicatedAudio {
					// Audio rates may be per character, second, token or song.
					// Token caps and zero token tariffs do not price those quantities.
					estimateErr = &EstimateUnavailableError{Cause: errors.New("OpenRouter audio monetary quantity is unmeasured")}
				} else if pricingErr != nil {
					estimateErr = &EstimateUnavailableError{Cause: pricingErr}
				} else if outputUnknown {
					estimateErr = &EstimateUnavailableError{Cause: errors.New("OpenRouter endpoint lacks a finite default completion cap")}
				} else {
					cost, estimateErr = estimateEndpointCost(tariff, model, prompt, output, count, imageCount, reserveUSD, imageOutput)
				}
				if estimateErr != nil {
					var unavailable *EstimateUnavailableError
					if (localUSD == nil && !(decisions && reserveUSD != nil)) || !errors.As(estimateErr, &unavailable) {
						return 0, estimateErr
					}
					if *reserveUSD <= 0 {
						return 0, errors.New("OpenRouter unavailable estimate requires a positive reserve")
					}
					// This endpoint is eligible, but its unknown monetary quantity is
					// covered only by the explicit local/minimum policy.
					found = true
					continue
				}
				if !found || cost.GreaterThan(maximum) {
					maximum = cost
				}
				found = true
			}
		}
	}
	if !found {
		return 0, errors.New("OpenRouter request has no compatible priced endpoint")
	}
	if approximateReserve {
		maximum = decimal.Max(maximum, decimal.NewFromFloat(*reserveUSD))
	}
	result, _ := maximum.Float64()
	if math.IsInf(result, 0) || math.IsNaN(result) {
		return 0, errors.New("OpenRouter estimate overflow")
	}
	return result, nil
}

// estimateEndpointCost contains monetary metadata/quantity evaluation only.
// Eligibility is established by the caller before any failure can become fallback.
func estimateEndpointCost(endpoint PricedEndpoint, model string, prompt, output, count, imageCount int, reserveUSD *float64, imageOutput bool) (result decimal.Decimal, err error) {
	defer func() {
		if err != nil {
			err = &EstimateUnavailableError{Cause: err}
		}
	}()
	approximateReserve := reserveUSD != nil
	sonarModel := model == "perplexity/sonar-deep-research"
	if approximateReserve {
		for _, key := range []string{"prompt", "completion", "internal_reasoning", "web_search"} {
			if _, ok := endpoint.Pricing[key]; !ok && !sonarModel && (key == "internal_reasoning" || key == "web_search") {
				continue
			}
			rate, err := readRate(endpoint.Pricing[key])
			if err != nil {
				return decimal.Zero, err
			}
			if *reserveUSD == 0 && (key == "internal_reasoning" || key == "web_search") && !rate.IsZero() {
				return decimal.Zero, errors.New("OpenRouter unknown billable quantity requires a positive reserve")
			}
		}
	}
	if imageOutput {
		rate, err := readRate(endpoint.Pricing["image_output"])
		if err != nil {
			return decimal.Zero, fmt.Errorf("OpenRouter image output tariff: %w", err)
		}
		if !rate.IsZero() && *reserveUSD == 0 {
			return decimal.Zero, errors.New("OpenRouter unknown image output quantity requires a positive reserve")
		}
	}
	promptRate, err := readRate(endpoint.Pricing["prompt"])
	if err != nil {
		return decimal.Zero, err
	}
	for _, key := range []string{"input_cache_read", "input_cache_write", "input_cache_write_1h"} {
		if raw, ok := endpoint.Pricing[key]; ok {
			rate, err := readRate(raw)
			if err != nil {
				return decimal.Zero, err
			}
			if rate.GreaterThan(promptRate) {
				promptRate = rate
			}
		}
	}
	cost := promptRate.Mul(decimal.NewFromInt(int64(prompt)))
	if output > 0 {
		rate, err := readRate(endpoint.Pricing["completion"])
		if err != nil {
			return decimal.Zero, err
		}
		if raw, ok := endpoint.Pricing["internal_reasoning"]; ok && strings.HasPrefix(model, "google/gemini-") {
			reasoningRate, err := readRate(raw)
			if err != nil {
				return decimal.Zero, err
			}
			if reasoningRate.GreaterThan(rate) {
				rate = reasoningRate
			}
		}
		cost = cost.Add(rate.Mul(decimal.NewFromInt(int64(output))).Mul(decimal.NewFromInt(int64(count))))
	}
	if raw, ok := endpoint.Pricing["request"]; ok {
		rate, err := readRate(raw)
		if err != nil {
			return decimal.Zero, err
		}
		cost = cost.Add(rate)
	}
	if imageCount > 0 {
		if raw, ok := endpoint.Pricing["image"]; ok {
			rate, err := readRate(raw)
			if err != nil {
				return decimal.Zero, err
			}
			cost = cost.Add(rate.Mul(decimal.NewFromInt(int64(imageCount))))
		}
	}
	for key, raw := range endpoint.Pricing {
		if imageOutput && (key == "image_output" || key == "audio" || key == "input_audio_cache" || key == "input_audio" || key == "output_audio") {
			// Image output quantities are covered by the explicit hold, not
			// inferred from a text token cap. Audio input/output is absent in
			// this admitted protocol; still validate published inactive rates.
			if _, err := readRate(raw); err != nil {
				return decimal.Zero, err
			}
			continue
		}
		if approximateReserve && (key == "internal_reasoning" || key == "web_search") {
			// Rates were validated above. Quantities remain unknown; the
			// explicit approximate hold does not make them free or bounded.
			continue
		}
		switch key {
		case "prompt", "completion", "request", "input_cache_read", "input_cache_write", "input_cache_write_1h", "discount", "image":
			continue
		}
		if !approximateReserve && (key == "audio" || key == "input_audio_cache" || key == "internal_reasoning" && strings.HasPrefix(model, "google/gemini-")) {
			if _, err := readRate(raw); err != nil {
				return decimal.Zero, err
			}
			continue
		}
		if !approximateReserve && (key == "input_audio" || key == "output_audio" || key == "web_search") {
			continue
		}
		if key == "overrides" {
			return decimal.Zero, errors.New("OpenRouter endpoint has unsupported request-dependent pricing overrides")
		}
		rate, err := readRate(raw)
		if err != nil {
			return decimal.Zero, err
		}
		if !rate.IsZero() {
			return decimal.Zero, fmt.Errorf("OpenRouter endpoint has unsupported billable dimension %s", key)
		}
	}
	result = cost
	value, _ := result.Float64()
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return decimal.Zero, errors.New("OpenRouter estimate overflow")
	}
	return result, nil
}

// ModelsFromRequest returns every possible model rather than assuming the
// primary slug is the only provider routing candidate.
func ModelsFromRequest(body []byte) ([]string, error) {
	var request struct {
		Model  string   `json:"model"`
		Models []string `json:"models"`
	}
	if err := common.Unmarshal(body, &request); err != nil {
		return nil, err
	}
	models := request.Models
	if request.Model != "" {
		models = append(models, request.Model)
	}
	return slices.Compact(models), nil
}

// requestInputMediaQuantities inspects protocol content, never arbitrary tool
// schemas, function arguments or user text that happen to name media fields.
func requestInputMediaQuantities(fields map[string]json.RawMessage) (int, error) {
	if raw, present := fields["web_search_options"]; present && string(bytes.TrimSpace(raw)) != "null" {
		return 0, errors.New("OpenRouter hosted-search quantity is unpriced")
	}
	var blocks []json.RawMessage
	for _, field := range []string{"messages", "input"} {
		raw := bytes.TrimSpace(fields[field])
		if len(raw) == 0 || raw[0] != '[' {
			continue
		}
		var items []json.RawMessage
		if common.Unmarshal(raw, &items) != nil {
			return 0, errors.New("invalid OpenRouter input content")
		}
		for _, item := range items {
			item = bytes.TrimSpace(item)
			var message struct {
				Content json.RawMessage   `json:"content"`
				Type    string            `json:"type"`
				Images  []json.RawMessage `json:"images"`
			}
			if len(item) == 0 || item[0] != '{' {
				continue
			}
			if common.Unmarshal(item, &message) != nil {
				return 0, errors.New("invalid OpenRouter input item")
			}
			if len(message.Images) > 0 {
				return 0, errors.New("OpenRouter assistant image inputs are unpriced")
			}
			message.Content = bytes.TrimSpace(message.Content)
			if len(message.Content) > 0 && message.Content[0] == '[' {
				var content []json.RawMessage
				if common.Unmarshal(message.Content, &content) != nil {
					return 0, errors.New("invalid OpenRouter message content")
				}
				blocks = append(blocks, content...)
			} else if len(message.Content) > 0 && message.Content[0] == '{' {
				blocks = append(blocks, message.Content)
			} else if message.Type != "" {
				blocks = append(blocks, item)
			}
		}
	}
	images := 0
	for _, block := range blocks {
		block = bytes.TrimSpace(block)
		if len(block) == 0 || block[0] != '{' {
			continue
		}
		var content struct {
			Type string `json:"type"`
		}
		if common.Unmarshal(block, &content) != nil {
			return 0, errors.New("invalid OpenRouter content block")
		}
		switch content.Type {
		case "image_url", "input_image", "image":
			images++
		case "input_audio", "audio_url", "video_url", "input_video":
			return 0, errors.New("OpenRouter input media duration quantity is unpriced")
		}
	}
	return images, nil
}
