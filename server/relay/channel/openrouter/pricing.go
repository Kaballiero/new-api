package openrouter

import (
	"bytes"
	"cmp"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"math"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	tokenusage "github.com/QuantumNous/new-api/gateway/tokenusage"
	"github.com/QuantumNous/new-api/internal/app/pricingruntime"
	pricingdomain "github.com/QuantumNous/new-api/internal/pricing/domain"
	"github.com/QuantumNous/new-api/internal/pricing/estimate_upstream_reserve"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
)

type PricedEndpoint struct {
	// Public numeric status does not define route eligibility.
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

type ReserveUse int

const (
	ReserveNotApplied ReserveUse = iota
	ReserveWholeHold
	ReserveAddOn
)

// RequestEstimator is request-scoped discovery, reused for all items in a
// batch. It deliberately has no global cache or routing lifecycle. Models and
// OutputModalities keep the live discoveries by upstream tariff model.
type RequestEstimator struct {
	Models           map[string][]PricedEndpoint
	OutputModalities map[string][]string
	// ReserveUse and ReserveModel describe the channel reserve policy applied
	// by the last successful estimate.
	ReserveUse   ReserveUse
	ReserveModel string
	// ReserveSource is what the last successful estimate is priced from.
	ReserveSource pricingdomain.ReserveSource
	// OutputReserve bounds the last successful estimate when it is a text
	// reserve priced from endpoint tariffs; nil otherwise.
	OutputReserve *relaycommon.OpenRouterOutputReserve
	reserve       estimate_upstream_reserve.Usecase
	c             *gin.Context
	adaptor       channel.Adaptor
}

func NewRequestEstimator() *RequestEstimator {
	return &RequestEstimator{
		Models:           make(map[string][]PricedEndpoint),
		OutputModalities: make(map[string][]string),
		reserve:          pricingruntime.EstimateUpstreamReserve(),
	}
}

// Estimate takes the complete maximum over eligible endpoints, preserving the
// actual routing payload. Price strings are USD per token/request/image.
func (e *RequestEstimator) Estimate(info *relaycommon.RelayInfo, body []byte, operation string) (float64, error) {
	return e.estimate(info, body, operation, nil, false)
}

// EstimateWithLocalPrice retains all endpoint and request guards. A configured
// price preserves known complete estimates and covers unavailable monetary metadata.
func (e *RequestEstimator) EstimateWithLocalPrice(info *relaycommon.RelayInfo, body []byte, operation string, localUSD float64) (float64, error) {
	return e.estimate(info, body, operation, &localUSD, false)
}

func (e *RequestEstimator) estimate(info *relaycommon.RelayInfo, body []byte, operation string, localUSD *float64, wholeReserve bool) (float64, error) {
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
	requestedOperation := operation
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
	// Audio and hosted search may use an explicit channel hold without a global
	// model price. This is an approximate reserve, never a ceiling on actual spend.
	var reserveUSD *float64
	if info.ChannelMeta != nil {
		reserveUSD = info.ChannelSetting.OpenRouterReserveUSD(request.Model)
	}
	decisions := operation == "decisions"
	rerank := operation == "rerank"
	if rerank {
		if batch || localUSD == nil && (info.ChannelMeta == nil || info.ChannelSetting.OpenRouterReserveUSD(request.Model) == nil) || info.RelayFormat != types.RelayFormatRerank {
			return 0, errors.New("OpenRouter rerank requires configured_price or an explicit approximate channel reserve and Rerank format")
		}
		for _, key := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens", "modalities"} {
			if _, present := fields[key]; present {
				return 0, errors.New("OpenRouter rerank does not support Chat output bounds or modalities")
			}
		}
	}
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
	imageReserveOnly := false
	if imageOutput && localUSD == nil && info.ChannelMeta != nil {
		_, imageReserveOnly = info.ChannelSetting.OpenRouterModelReserveUSD[request.Model]
	}
	if imageOutput && (localUSD == nil && !imageReserveOnly || info.RelayFormat != types.RelayFormatOpenAI || imageReserveOnly && operation != "chat/completions") {
		return 0, errors.New("OpenRouter image output requires configured_price or an explicit channel reserve and OpenAI Chat format")
	}
	if raw, present := fields["audio"]; imageOutput && present && string(bytes.TrimSpace(raw)) != "null" {
		return 0, errors.New("OpenRouter audio output remains unsupported by image reserve")
	}
	if audioOutput && (imageOutput || operation != "chat/completions" || localUSD == nil && reserveUSD == nil || info.RelayFormat != types.RelayFormatOpenAI) {
		return 0, errors.New("OpenRouter audio output requires configured_price or an explicit approximate channel reserve and OpenAI Chat format")
	}
	if audioOutput && (!request.Stream || !info.IsStream) {
		return 0, errors.New("OpenRouter Chat audio output requires streaming")
	}
	speech := operation == "audio/speech"
	if dedicatedAudio {
		// Speech is priced from the catalog unit prices; transcription only by
		// a configured price or a channel reserve.
		if localUSD == nil && reserveUSD == nil && !speech || info.RelayFormat != types.RelayFormatOpenAIAudio {
			return 0, errors.New("OpenRouter audio endpoint requires configured_price or an explicit approximate channel reserve (not a spending limit) and OpenAI Audio format")
		}
		if speech {
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
	var searchReserveUSD *float64
	if raw, present := fields["web_search_options"]; present && string(bytes.TrimSpace(raw)) != "null" {
		if operation != "chat/completions" || reserveUSD == nil || !(*reserveUSD > 0) || info.ChannelType != constant.ChannelTypeOpenRouter {
			return 0, errors.New("OpenRouter hosted-search quantity is unpriced without a positive approximate channel reserve")
		}
		if imageOutput || audioOutput {
			return 0, errors.New("OpenRouter hosted search cannot share an approximate reserve with image or audio output")
		}
		if batch {
			return 0, errors.New("OpenRouter hosted search is unavailable for Batch")
		}
		searchReserveUSD = reserveUSD
	}
	imageBlocks, err := requestInputImages(fields)
	if err != nil {
		return 0, err
	}
	// imageTokens is measured only by the Cohere Embed rule, which applies when
	// every routable model follows it; nil otherwise.
	var imageTokens *int
	promptText := body
	outsideCohereEmbedRule := func(model string) bool { return !slices.Contains(cohereEmbedImageTokenModels, model) }
	if operation == "embeddings" && !slices.ContainsFunc(models, outsideCohereEmbedRule) {
		readable := imageBlocks[:min(len(imageBlocks), cohereEmbedImageHeadersPerRequest)]
		tokens := (len(imageBlocks) - len(readable)) * cohereEmbedImageMaxTokens
		for _, block := range readable {
			tokens += cohereEmbedImageTokens(block)
		}
		imageTokens = &tokens
	}
	if imageTokens != nil && len(imageBlocks) > 0 {
		// Image data is priced as image tokens, not as prompt text. A block not
		// found after the previous cut stays in the text, an upper bound.
		var text []byte
		rest := body
		for _, block := range imageBlocks {
			before, after, found := bytes.Cut(rest, block)
			if !found {
				continue
			}
			text = append(text, before...)
			rest = after
		}
		promptText = append(text, rest...)
	}
	prompt := max(info.GetEstimatePromptTokens(), tokenusage.CountTextToken(string(promptText), request.Model))
	if dedicatedAudio || decisions || rerank {
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
	sonarModel := request.Model == "perplexity/sonar-deep-research"
	if reserveUSD != nil && (math.IsNaN(*reserveUSD) || math.IsInf(*reserveUSD, 0) || *reserveUSD < 0 || *reserveUSD == 0 && localUSD == nil) {
		return 0, errors.New("invalid OpenRouter approximate reserve")
	}
	approximateReserve := reserveUSD != nil && (wholeReserve || localUSD != nil || decisions || rerank || dedicatedAudio || audioOutput || imageReserveOnly || sonarModel)
	addOnReserve := reserveUSD != nil && !approximateReserve
	var approximateReserveUSD, addOnReserveUSD *float64
	if approximateReserve {
		streamAllowed := audioOutput && request.Stream && info.IsStream
		operationAllowed := operation == "chat/completions" || decisions || rerank || dedicatedAudio
		if info.ChannelType != constant.ChannelTypeOpenRouter || batch || !operationAllowed || (request.Stream || info.IsStream) && !streamAllowed || count != 1 || len(request.Models) > 0 || request.Route != "" || len(request.Tools) > 0 || len(imageBlocks) > 0 {
			return 0, errors.New("OpenRouter approximate reserve only supports one nonstream text chat response without routing or tools")
		}
		approximateReserveUSD = reserveUSD
	} else {
		addOnReserveUSD = reserveUSD
	}
	for _, cap := range []*int{request.MaxTokens, request.MaxCompletionTokens, request.MaxOutputTokens} {
		if cap != nil && (*cap < 0 || *cap > helper.MaxTokensLimit) {
			return 0, errors.New("invalid OpenRouter output cap")
		}
	}
	if localUSD != nil {
		if info.RelayFormat != types.RelayFormatOpenAI && !dedicatedAudio && !(decisions && info.RelayFormat == types.RelayFormatSystemOne) && !rerank {
			return 0, errors.New("OpenRouter configured price reserve requires OpenAI Chat format")
		}
		if raw, present := fields["audio"]; present && string(bytes.TrimSpace(raw)) != "null" && !audioOutput {
			return 0, errors.New("OpenRouter audio output remains unsupported by configured price reserve")
		}
	}
	outputLimit := cmp.Or(request.MaxOutputTokens, request.MaxCompletionTokens, request.MaxTokens)
	textOutput := operation != "embeddings" && !dedicatedAudio && !decisions && !rerank
	var outputReserve *relaycommon.OpenRouterOutputReserve
	if textOutput && !approximateReserve {
		limitField := "max_tokens"
		switch info.RelayFormat {
		case types.RelayFormatOpenAIResponses:
			limitField = "max_output_tokens"
		case types.RelayFormatGemini:
			limitField = "maxOutputTokens"
		}
		outputReserve = &relaycommon.OpenRouterOutputReserve{ExplicitLimit: outputLimit != nil, LimitField: limitField}
	}
	var channelBaseURL string
	if info.ChannelMeta != nil {
		channelBaseURL = info.ChannelBaseUrl
	}
	var units map[pricingdomain.PriceDimension]int64
	if speech {
		units, err = speechUnits(body, request.Model)
		if err != nil {
			return 0, err
		}
	}
	estimate, err := e.reserve.Execute(estimate_upstream_reserve.Input{
		Model:          request.Model,
		Models:         models,
		Batch:          batch,
		ChannelBaseURL: channelBaseURL,
		Live:           liveTariffs{estimator: e, info: info},
		Provider: estimate_upstream_reserve.Provider{
			Only:              request.Provider.Only,
			Ignore:            request.Provider.Ignore,
			Order:             request.Provider.Order,
			AllowFallbacks:    request.Provider.AllowFallbacks,
			RequireParameters: request.Provider.RequireParameters,
			Quantizations:     request.Provider.Quantizations,
			MaxPrice:          request.Provider.MaxPrice,
		},
		ServiceTier:           request.ServiceTier,
		Speed:                 routingSpeed,
		Parameters:            fields,
		Modalities:            modalities,
		TextOutput:            textOutput,
		ImageOutput:           imageOutput,
		Audio:                 audioOutput || dedicatedAudio && !speech,
		Decisions:             decisions,
		Rerank:                rerank,
		Units:                 units,
		ImageReserveOnly:      imageReserveOnly,
		ConfiguredPrice:       localUSD != nil,
		PromptTokens:          prompt,
		Count:                 count,
		Images:                len(imageBlocks),
		ImageTokens:           imageTokens,
		OutputLimit:           outputLimit,
		DefaultOutputTokens:   helper.DefaultPreConsumeMaxTokens,
		MaxTokensLimit:        helper.MaxTokensLimit,
		ApproximateReserveUSD: approximateReserveUSD,
		AddOnReserveUSD:       addOnReserveUSD,
		SearchReserveUSD:      searchReserveUSD,
	})
	var unavailable *pricingdomain.EstimateUnavailableError
	if addOnReserve && errors.As(err, &unavailable) {
		return e.estimate(info, body, requestedOperation, localUSD, true)
	}
	if err != nil {
		return 0, err
	}
	if outputReserve != nil {
		outputReserve.InputUSD, outputReserve.PromptPriceUSD = estimate.InputUSD, estimate.PromptPriceUSD
		outputReserve.OutputPriceUSD, outputReserve.OutputTokens = estimate.OutputPriceUSD, estimate.OutputTokens
	}
	if estimate.StaleCatalog {
		logger.LogWarn(e.c, "OpenRouter reserve priced from a stale upstream catalog: model=%s batch=%t", request.Model, batch)
	}
	e.ReserveUse, e.ReserveModel = ReserveNotApplied, ""
	e.ReserveSource = estimate.Source
	e.OutputReserve = outputReserve
	if approximateReserve {
		e.ReserveUse, e.ReserveModel = ReserveWholeHold, request.Model
	} else if addOnReserve && (searchReserveUSD != nil || estimate.IncludesAddOnReserve) {
		e.ReserveUse, e.ReserveModel = ReserveAddOn, request.Model
	}
	return estimate.USD, nil
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

// requestInputImages returns the input image blocks, inspecting protocol
// content, never arbitrary tool schemas, function arguments or user text that
// happen to name media fields.
func requestInputImages(fields map[string]json.RawMessage) ([]json.RawMessage, error) {
	var blocks []json.RawMessage
	for _, field := range []string{"messages", "input"} {
		raw := bytes.TrimSpace(fields[field])
		if len(raw) == 0 || raw[0] != '[' {
			continue
		}
		var items []json.RawMessage
		if common.Unmarshal(raw, &items) != nil {
			return nil, errors.New("invalid OpenRouter input content")
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
				return nil, errors.New("invalid OpenRouter input item")
			}
			if len(message.Images) > 0 {
				return nil, errors.New("OpenRouter assistant image inputs are unpriced")
			}
			message.Content = bytes.TrimSpace(message.Content)
			if len(message.Content) > 0 && message.Content[0] == '[' {
				var content []json.RawMessage
				if common.Unmarshal(message.Content, &content) != nil {
					return nil, errors.New("invalid OpenRouter message content")
				}
				blocks = append(blocks, content...)
			} else if len(message.Content) > 0 && message.Content[0] == '{' {
				blocks = append(blocks, message.Content)
			} else if message.Type != "" {
				blocks = append(blocks, item)
			}
		}
	}
	var images []json.RawMessage
	for _, block := range blocks {
		block = bytes.TrimSpace(block)
		if len(block) == 0 || block[0] != '{' {
			continue
		}
		var content struct {
			Type string `json:"type"`
		}
		if common.Unmarshal(block, &content) != nil {
			return nil, errors.New("invalid OpenRouter content block")
		}
		switch content.Type {
		case "image_url", "input_image", "image":
			images = append(images, block)
		case "input_audio", "audio_url", "video_url", "input_video":
			return nil, errors.New("OpenRouter input media duration quantity is unpriced")
		}
	}
	return images, nil
}

// Cohere Embed image tokens, measured on OpenRouter: a grid of 32 px cells,
// at least 2x2 and at most 49x49 after downscaling, plus 4 tokens per image on
// embed-5-fast and 2 on embed-5-pro. The reserve adds 4 for both.
const (
	cohereEmbedImageCellPixels      = 32
	cohereEmbedImageMinCellsPerSide = 2
	cohereEmbedImageMaxCells        = 49 * 49
	cohereEmbedImageExtraTokens     = 4
	cohereEmbedImageMaxTokens       = cohereEmbedImageMaxCells + cohereEmbedImageExtraTokens
	// Image headers read per request, matching Cohere's 96 inputs per call;
	// images past it count at the per-image maximum.
	cohereEmbedImageHeadersPerRequest = 96
)

var cohereEmbedImageTokenModels = []string{"cohere/embed-5-fast", "cohere/embed-5-pro"}

// cohereEmbedImageTokens bounds the Cohere Embed tokens of one image block from
// above. Only the header of a base64 data URI is decoded; an image whose size
// cannot be read counts at the per-image maximum.
func cohereEmbedImageTokens(block json.RawMessage) int {
	var content struct {
		ImageURL struct {
			URL string `json:"url"`
		} `json:"image_url"`
	}
	if common.Unmarshal(block, &content) != nil {
		return cohereEmbedImageMaxTokens
	}
	mediaType, payload, found := strings.Cut(content.ImageURL.URL, ",")
	if !found || !strings.HasPrefix(mediaType, "data:") || !strings.HasSuffix(mediaType, ";base64") {
		return cohereEmbedImageMaxTokens
	}
	config, _, err := image.DecodeConfig(base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload)))
	if err != nil || config.Width <= 0 || config.Height <= 0 {
		return cohereEmbedImageMaxTokens
	}
	columns := (config.Width + cohereEmbedImageCellPixels - 1) / cohereEmbedImageCellPixels
	rows := (config.Height + cohereEmbedImageCellPixels - 1) / cohereEmbedImageCellPixels
	// Clamping each side to the cell maximum keeps the product from overflowing.
	columns = min(max(columns, cohereEmbedImageMinCellsPerSide), cohereEmbedImageMaxCells)
	rows = min(max(rows, cohereEmbedImageMinCellsPerSide), cohereEmbedImageMaxCells)
	return min(columns*rows, cohereEmbedImageMaxCells) + cohereEmbedImageExtraTokens
}
