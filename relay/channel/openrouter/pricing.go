package openrouter

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type PricedEndpoint struct {
	Tag                 string                     `json:"tag"`
	ContextLength       int                        `json:"context_length"`
	MaxPromptTokens     int                        `json:"max_prompt_tokens"`
	MaxCompletionTokens int                        `json:"max_completion_tokens"`
	Quantization        string                     `json:"quantization"`
	SupportedParameters []string                   `json:"supported_parameters"`
	Pricing             map[string]json.RawMessage `json:"pricing"`
}

// RequestEstimator is request-scoped discovery, reused for all items in a
// batch. It deliberately has no global cache or routing lifecycle.
type RequestEstimator struct{ Models map[string][]PricedEndpoint }

func NewRequestEstimator() *RequestEstimator {
	return &RequestEstimator{Models: make(map[string][]PricedEndpoint)}
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
			ID        string           `json:"id"`
			Endpoints []PricedEndpoint `json:"endpoints"`
		} `json:"data"`
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		return errors.New("OpenRouter pricing discovery exceeds limit")
	}
	if common.Unmarshal(data, &result) != nil || len(result.Data.Endpoints) == 0 {
		return errors.New("OpenRouter model has no priced endpoints")
	}
	if result.Data.ID != "" && result.Data.ID != discoveryModel {
		return errors.New("OpenRouter pricing discovery model mismatch")
	}
	e.Models[model] = result.Data.Endpoints
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
	var request struct {
		Model       string   `json:"model"`
		Models      []string `json:"models"`
		Route       string   `json:"route"`
		Preset      string   `json:"preset"`
		ServiceTier string   `json:"service_tier"`
		Speed       string   `json:"speed"`
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
		request.Provider.Only = nil
		request.Provider.Ignore = nil
		request.Provider.Order = nil
		request.Provider.AllowFallbacks = nil
		request.Provider.Quantizations = nil
		request.Provider.MaxPrice = nil
		request.Provider.RequireParameters = false
		request.ServiceTier = ""
		request.Speed = ""
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
	if raw, ok := fields["modalities"]; ok {
		if common.Unmarshal(raw, &modalities) != nil {
			return 0, errors.New("invalid OpenRouter modalities")
		}
		for _, modality := range modalities {
			if modality != "text" {
				return 0, errors.New("OpenRouter output modality tariff is unavailable")
			}
		}
	}
	imageCount, err := requestInputMediaQuantities(fields)
	if err != nil {
		return 0, err
	}
	prompt := max(info.GetEstimatePromptTokens(), service.CountTextToken(string(body), request.Model))
	count := 1
	if request.N != nil {
		count = *request.N
	}
	if count < 1 || count > dto.MaxImageN {
		return 0, errors.New("invalid OpenRouter output count")
	}
	var maximum decimal.Decimal
	found := false
	for _, model := range models {
		if strings.HasSuffix(model, ":online") {
			return 0, errors.New("OpenRouter online routing includes unpriced hosted search")
		}
		endpoints, ok := e.Models[model]
		if !ok {
			return 0, fmt.Errorf("OpenRouter endpoint discovery missing for %s", model)
		}
		hasFlex := false
		if request.ServiceTier == "flex" {
			for _, endpoint := range endpoints {
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
		// A published long-context override is another complete endpoint
		// tariff. Select applicable thresholds using the same prompt estimate,
		// then maximize whole costs rather than unrelated rate components.
		var tariffs []PricedEndpoint
		for _, endpoint := range endpoints {
			prices := maps.Clone(endpoint.Pricing)
			raw, hasOverrides := prices["overrides"]
			delete(prices, "overrides")
			endpoint.Pricing = prices
			tariffs = append(tariffs, endpoint)
			if !hasOverrides || string(raw) == "null" {
				continue
			}
			var overrides []map[string]json.RawMessage
			if common.Unmarshal(raw, &overrides) != nil {
				return 0, errors.New("invalid OpenRouter endpoint pricing overrides")
			}
			for _, override := range overrides {
				var threshold int
				thresholdRaw, ok := override["min_prompt_tokens"]
				if !ok || common.Unmarshal(thresholdRaw, &threshold) != nil || threshold < 0 || threshold > helper.MaxTokensLimit {
					return 0, errors.New("invalid OpenRouter pricing override token threshold")
				}
				variant := endpoint
				variant.Pricing = maps.Clone(prices)
				for key, value := range override {
					switch key {
					case "min_prompt_tokens":
						continue
					case "prompt", "completion", "input_cache_read", "input_cache_write", "input_cache_write_1h", "request", "image":
					default:
						return 0, fmt.Errorf("OpenRouter pricing override has unsupported dimension %s", key)
					}
					if _, err := readRate(value); err != nil {
						return 0, err
					}
					variant.Pricing[key] = value
				}
				if prompt >= threshold {
					tariffs = append(tariffs, variant)
				}
			}
		}
		for _, endpoint := range tariffs {
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
			if requestedTier == "fast" || request.Speed == "fast" {
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

			if endpoint.MaxPromptTokens > 0 && prompt > endpoint.MaxPromptTokens {
				continue
			}
			output := 0
			if operation != "embeddings" {
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
						return 0, errors.New("OpenRouter endpoint lacks a finite default completion cap")
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
			for key, raw := range request.Provider.MaxPrice {
				rate, err := readRate(endpoint.Pricing[key])
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
			if request.Provider.RequireParameters {
				var fields map[string]json.RawMessage
				_ = common.Unmarshal(body, &fields)
				for key := range fields {
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
			promptRate, err := readRate(endpoint.Pricing["prompt"])
			if err != nil {
				return 0, err
			}
			for _, key := range []string{"input_cache_read", "input_cache_write", "input_cache_write_1h"} {
				if raw, ok := endpoint.Pricing[key]; ok {
					rate, err := readRate(raw)
					if err != nil {
						return 0, err
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
					return 0, err
				}
				cost = cost.Add(rate.Mul(decimal.NewFromInt(int64(output))).Mul(decimal.NewFromInt(int64(count))))
			}
			if raw, ok := endpoint.Pricing["request"]; ok {
				rate, err := readRate(raw)
				if err != nil {
					return 0, err
				}
				cost = cost.Add(rate)
			}
			if imageCount > 0 {
				if raw, ok := endpoint.Pricing["image"]; ok {
					rate, err := readRate(raw)
					if err != nil {
						return 0, err
					}
					cost = cost.Add(rate.Mul(decimal.NewFromInt(int64(imageCount))))
				}
			}
			for key, raw := range endpoint.Pricing {
				switch key {
				case "prompt", "completion", "request", "input_cache_read", "input_cache_write", "input_cache_write_1h", "discount", "image":
					continue
				}
				if key == "audio" || key == "input_audio" || key == "output_audio" || key == "web_search" {
					continue
				}
				if key == "overrides" {
					return 0, errors.New("OpenRouter endpoint has unsupported request-dependent pricing overrides")
				}
				rate, err := readRate(raw)
				if err != nil {
					return 0, err
				}
				if !rate.IsZero() {
					return 0, fmt.Errorf("OpenRouter endpoint has unsupported billable dimension %s", key)
				}
			}
			if !found || cost.GreaterThan(maximum) {
				maximum = cost
			}
			found = true
		}
	}
	if !found {
		return 0, errors.New("OpenRouter request has no compatible priced endpoint")
	}
	result, _ := maximum.Float64()
	if math.IsInf(result, 0) || math.IsNaN(result) {
		return 0, errors.New("OpenRouter estimate overflow")
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
				Content json.RawMessage `json:"content"`
				Type    string          `json:"type"`
			}
			if len(item) == 0 || item[0] != '{' {
				continue
			}
			if common.Unmarshal(item, &message) != nil {
				return 0, errors.New("invalid OpenRouter input item")
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
