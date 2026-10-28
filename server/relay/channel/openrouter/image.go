package openrouter

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/logger"
	httpclient "github.com/QuantumNous/new-api/pkg/httpclient"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/relay/taskrecord"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"

	"github.com/gin-gonic/gin"
	_ "golang.org/x/image/webp"
)

// ImageAdaptor uses OpenRouter's dedicated /images protocol. Embedding the
// selected channel adaptor preserves its authentication and channel settings.
// Chat completions continue through the ordinary OpenAI-compatible adaptor.
type ImageAdaptor struct{ channel.Adaptor }

var errImageTariff = errors.New("OpenRouter image tariff unavailable")

type ImageCapability struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
	Min    int64    `json:"min"`
	Max    int64    `json:"max"`
}
type ImagePrice struct {
	missingCost bool
	Billable    string  `json:"billable"`
	Unit        string  `json:"unit"`
	CostUSD     float64 `json:"cost_usd"`
	Variant     string  `json:"variant"`
}

// UnmarshalJSON distinguishes an explicitly free tariff from unknown pricing.
func (price *ImagePrice) UnmarshalJSON(data []byte) error {
	type priceFields ImagePrice
	var decoded priceFields
	if err := common.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := common.Unmarshal(data, &fields); err != nil {
		return err
	}
	cost, exists := fields["cost_usd"]
	decoded.missingCost = !exists || bytes.Equal(bytes.TrimSpace(cost), []byte("null"))
	*price = ImagePrice(decoded)
	return nil
}

type ImageEndpoint struct {
	PromptOnly          bool                       `json:"-"`
	ProviderTag         string                     `json:"provider_tag"`
	SupportedParameters map[string]ImageCapability `json:"supported_parameters"`
	Pricing             []ImagePrice               `json:"pricing"`
}

func PublishedImageEndpoint(model string, endpoint ImageEndpoint) ImageEndpoint {
	if len(endpoint.Pricing) != 0 || endpoint.ProviderTag != "krea" {
		return endpoint
	}
	rate, known := map[string]float64{"krea/krea-2-large": 0.06, "krea/krea-2-medium": 0.03, "krea/krea-2-medium-turbo": 0.015}[model]
	if known {
		endpoint.Pricing = []ImagePrice{{Billable: "output_image", Unit: "image", CostUSD: rate}}
	}
	return endpoint
}

func PublishedMuseImageEndpoint(model string) (ImageEndpoint, bool) {
	return ImageEndpoint{PromptOnly: true, Pricing: []ImagePrice{{Billable: "output_image", Unit: "image", CostUSD: 0.01}}}, model == "meta/muse-image"
}

type imageReference struct {
	Type     string `json:"type"`
	ImageURL struct {
		URL string `json:"url"`
	} `json:"image_url"`
}
type imageResponse struct {
	Created int64 `json:"created"`
	Data    []struct {
		B64JSON   string `json:"b64_json"`
		MediaType string `json:"media_type,omitempty"`
	} `json:"data"`
	Usage *struct {
		dto.Usage
		Cost *float64 `json:"cost"`
	} `json:"usage"`
	Error json.RawMessage `json:"error,omitempty"`
}

func (a *ImageAdaptor) GetRequestURL(info *relaycommon.RelayInfo) (string, error) {
	return strings.TrimRight(info.ChannelBaseUrl, "/") + "/v1/images", nil
}

func (a *ImageAdaptor) SetupRequestHeader(c *gin.Context, header *http.Header, info *relaycommon.RelayInfo) error {
	if err := a.Adaptor.SetupRequestHeader(c, header, info); err != nil {
		return err
	}
	header.Set("Content-Type", "application/json")
	return nil
}

// prepareImageOverrides confines administrator transformations to routing. Other
// fields must remain visible to capability validation and the cost estimator.
func prepareImageOverrides(request dto.ImageRequest, info *relaycommon.RelayInfo) (dto.ImageRequest, error) {
	raw, err := common.Marshal(request)
	if err != nil {
		return request, err
	}
	fields := map[string]json.RawMessage{}
	if err = common.Unmarshal(raw, &fields); err != nil {
		return request, err
	}
	for key, value := range request.Extra {
		fields[key] = value
	}
	var nestedProvider any
	nestedPresent := false
	if value, exists := fields["extra_body"]; exists {
		var extra map[string]json.RawMessage
		if common.Unmarshal(value, &extra) != nil || extra == nil {
			return request, errors.New("invalid image extra_body")
		}
		for key := range extra {
			if key != "provider" {
				return request, errors.New("unsupported image extra_body field")
			}
		}
		if value, exists := extra["provider"]; exists {
			nestedPresent = true
			if err = common.Unmarshal(value, &nestedProvider); err != nil {
				return request, err
			}
			if direct, exists := fields["provider"]; exists {
				var directProvider any
				if common.Unmarshal(direct, &directProvider) != nil || !reflect.DeepEqual(directProvider, nestedProvider) {
					return request, errors.New("conflicting direct and nested image provider")
				}
			}
			fields["provider"] = value
		}
	}
	for key, value := range info.ParamOverride {
		if key == "provider" {
			continue
		}
		if key != "operations" {
			return request, errors.New("unsupported OpenRouter image parameter override")
		}
		encoded, err := common.Marshal(value)
		if err != nil {
			return request, err
		}
		var operations []map[string]any
		if common.Unmarshal(encoded, &operations) != nil || operations == nil {
			return request, errors.New("invalid image override operations")
		}
		for _, operation := range operations {
			for field := range operation {
				if !slices.Contains([]string{"path", "mode", "value", "from", "to", "keep_origin", "conditions", "logic"}, field) {
					return request, errors.New("unsupported image override operation field")
				}
			}
			mode, _ := operation["mode"].(string)
			path, _ := operation["path"].(string)
			from, _ := operation["from"].(string)
			to, _ := operation["to"].(string)
			switch mode {
			case "set":
				if path != "provider" || from != "" || to != "" {
					return request, errors.New("unsupported image override set path")
				}
			case "move", "copy":
				if from != "extra_body.provider" || to != "provider" || path != "" {
					return request, errors.New("unsupported image override routing path")
				}
			case "delete":
				if path != "extra_body" || from != "" || to != "" {
					return request, errors.New("unsupported image override delete path")
				}
			default:
				return request, errors.New("unsupported image override operation")
			}
		}
	}
	raw, err = common.Marshal(fields)
	if err != nil {
		return request, err
	}
	raw, err = relaycommon.ApplyParamOverride(raw, info.ParamOverride, relaycommon.BuildParamOverrideContext(info))
	if err != nil {
		return request, err
	}
	fields = map[string]json.RawMessage{}
	if err = common.Unmarshal(raw, &fields); err != nil {
		return request, err
	}
	if nestedPresent && len(info.ParamOverride) == 0 {
		var finalProvider any
		value, exists := fields["provider"]
		if !exists || common.Unmarshal(value, &finalProvider) != nil || !reflect.DeepEqual(finalProvider, nestedProvider) {
			return request, errors.New("image extra_body.provider was not preserved by routing overrides")
		}
	}
	if value, exists := fields["extra_body"]; exists {
		var extra map[string]json.RawMessage
		if common.Unmarshal(value, &extra) != nil || len(extra) > 1 || len(extra) == 1 && len(extra["provider"]) == 0 {
			return request, errors.New("image extra_body.provider must be moved and cleaned up")
		}
		delete(fields, "extra_body")
	}
	raw, err = common.Marshal(fields)
	if err != nil {
		return request, err
	}
	var prepared dto.ImageRequest
	err = common.Unmarshal(raw, &prepared)
	return prepared, err
}

type imageRouting struct {
	Only           []string
	Order          []string
	Ignore         []string
	AllowFallbacks bool
	Fields         map[string]json.RawMessage
}

func parseImageRouting(request dto.ImageRequest) (imageRouting, error) {
	routing := imageRouting{AllowFallbacks: true}
	raw, exists := request.Extra["provider"]
	if !exists || string(bytes.TrimSpace(raw)) == "null" {
		return routing, nil
	}
	if common.Unmarshal(raw, &routing.Fields) != nil || routing.Fields == nil {
		return routing, errors.New("invalid image provider routing")
	}
	for key, value := range routing.Fields {
		if !slices.Contains([]string{"only", "order", "ignore", "allow_fallbacks", "sort", "options"}, key) {
			return routing, errors.New("unsupported image provider routing field")
		}
		if string(bytes.TrimSpace(value)) == "null" {
			continue
		}
		switch key {
		case "only", "order", "ignore":
			var list []string
			if common.Unmarshal(value, &list) != nil || list == nil {
				return routing, errors.New("invalid image provider list")
			}
			for _, tag := range list {
				if strings.TrimSpace(tag) == "" {
					return routing, errors.New("invalid image provider identifier")
				}
			}
			switch key {
			case "only":
				routing.Only = list
			case "order":
				routing.Order = list
			case "ignore":
				routing.Ignore = list
			}
		case "allow_fallbacks":
			if string(value) == "null" || common.Unmarshal(value, &routing.AllowFallbacks) != nil {
				return routing, errors.New("invalid image allow_fallbacks")
			}
		case "sort":
			var by string
			objectSort := false
			if common.Unmarshal(value, &by) != nil {
				objectSort = true
				var sort map[string]string
				if common.Unmarshal(value, &sort) != nil {
					return routing, errors.New("unsupported image provider sort")
				}
				for key := range sort {
					if key != "by" && key != "partition" {
						return routing, errors.New("unsupported image provider sort field")
					}
				}
				if partition := sort["partition"]; partition != "" && partition != "model" && partition != "none" {
					return routing, errors.New("unsupported image provider sort partition")
				}
				by = sort["by"]
			}
			if !(objectSort && by == "") && !slices.Contains([]string{"price", "throughput", "latency", "exacto"}, by) {
				return routing, errors.New("unsupported image provider sort")
			}
		case "options":
			var options map[string]json.RawMessage
			if common.Unmarshal(value, &options) != nil || options == nil || len(options) != 0 {
				return routing, errors.New("unsupported image provider options")
			}
		default:
			return routing, errors.New("unsupported image provider routing field")
		}
	}
	return routing, nil
}

// Prepare validates against definitive per-endpoint capabilities before any
// paid request. Metadata GETs use the selected channel's key and transport.
func (a *ImageAdaptor) Prepare(c *gin.Context, info *relaycommon.RelayInfo, request dto.ImageRequest) error {
	if c.Request.MultipartForm != nil {
		for _, key := range []string{"provider", "extra_body"} {
			if _, exists := c.Request.MultipartForm.Value[key]; exists {
				return errors.New("OpenRouter image provider routing requires a JSON request; multipart routing fields are unsupported")
			}
		}
	}

	var err error
	request, err = prepareImageOverrides(request, info)
	if err != nil {
		return err
	}
	routing, err := parseImageRouting(request)
	if err != nil {
		return err
	}
	modelParts := strings.Split(request.Model, "/")
	if len(modelParts) != 2 || modelParts[0] == "" || modelParts[1] == "" {
		return errors.New("invalid OpenRouter image model")
	}
	endpointURL := strings.TrimRight(info.ChannelBaseUrl, "/") + "/v1/images/models/" + url.PathEscape(modelParts[0]) + "/" + url.PathEscape(modelParts[1]) + "/endpoints"
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodGet, endpointURL, nil)
	if err != nil {
		return errors.New("invalid OpenRouter image discovery URL")
	}
	if err = a.SetupRequestHeader(c, &req.Header, info); err != nil {
		return err
	}
	overrides, err := channel.ResolveHeaderOverride(info, c)
	if err != nil {
		return err
	}
	for key, value := range overrides {
		req.Header.Set(key, value)
	}
	client, err := httpclient.GetHttpClientWithProxySettings(info.ChannelSetting.Proxy, info.ChannelSetting)
	if err != nil {
		return errors.New("OpenRouter image discovery transport unavailable")
	}
	metadataClient := *client
	metadataClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := metadataClient.Do(req)
	if err != nil {
		return imageResponseError("OpenRouter image capability discovery failed")
	}
	defer resp.Body.Close()
	captureImageRequestID(c, resp, info)
	if resp.StatusCode != http.StatusOK {
		logger.LogWarn(c, fmt.Sprintf("OpenRouter image capability discovery HTTP %d upstream_request_id=%s", resp.StatusCode, c.GetString(common.UpstreamRequestIdKey)))
		return imageResponseError(fmt.Sprintf("OpenRouter image capability discovery returned HTTP %d", resp.StatusCode))
	}
	var catalog struct {
		ID        string          `json:"id"`
		Endpoints []ImageEndpoint `json:"endpoints"`
	}
	if err = common.DecodeJson(io.LimitReader(resp.Body, 1<<20), &catalog); err != nil || catalog.ID != request.Model {
		return imageResponseError("invalid OpenRouter image capability response")
	}
	if len(catalog.Endpoints) == 0 && request.Model == "meta/muse-image" && len(routing.Fields) == 0 {
		// The official model page documents this exact prompt-only Image API
		// request and $0.01/image, while image endpoint discovery is empty.
		// Do not infer reference/size/count capabilities from chat metadata.
		// https://openrouter.ai/meta/muse-image
		endpoint, _ := PublishedMuseImageEndpoint(request.Model)
		catalog.Endpoints = []ImageEndpoint{endpoint}
	}
	if len(catalog.Endpoints) == 0 {
		return errors.New("OpenRouter image model has no documented image endpoint")
	}
	reserve, optedIn := info.ChannelSetting.OpenRouterModelReserveUSD[request.Model]
	if optedIn && (math.IsNaN(reserve) || math.IsInf(reserve, 0) || reserve <= 0 || reserve > float64(math.MaxInt32)/common.QuotaPerUnit) {
		return errors.New("invalid OpenRouter image channel reserve")
	}
	if source, configured := info.ChannelSetting.OpenRouterReservationSource[request.Model]; configured && source != dto.OpenRouterReservationUpstream {
		return errors.New("unsupported OpenRouter image reservation source")
	}
	var lastErr error
	var selected *relaycommon.NativeImageInfo
	var maximumEstimate float64
	var eligibleTags []string
	var commonPayload map[string]json.RawMessage
	selectedRank := len(routing.Order)
	for _, endpoint := range catalog.Endpoints {
		if len(routing.Fields) > 0 && endpoint.ProviderTag == "" {
			continue
		}
		if routing.Only != nil && !slices.Contains(routing.Only, endpoint.ProviderTag) || slices.Contains(routing.Ignore, endpoint.ProviderTag) {
			continue
		}
		rank := slices.Index(routing.Order, endpoint.ProviderTag)
		if rank < 0 {
			rank = len(routing.Order)
		}
		if !routing.AllowFallbacks && len(routing.Order) > 0 && rank != 0 {
			continue
		}
		endpoint = PublishedImageEndpoint(request.Model, endpoint)
		body, estimate, convertErr := convertImageRequest(c, request, endpoint, reserve)
		if convertErr != nil {
			if errors.Is(convertErr, errImageTariff) {
				return convertErr
			}
			lastErr = convertErr
			continue
		}
		if len(routing.Fields) > 0 {
			var payload map[string]json.RawMessage
			if err = common.Unmarshal(body, &payload); err != nil {
				return err
			}
			delete(payload, "provider")
			// One image is the native default even on endpoints that expose no n
			// parameter. Do not send that optional knob to an incompatible fallback.
			if string(payload["n"]) == "1" {
				delete(payload, "n")
			}
			if commonPayload != nil && !reflect.DeepEqual(commonPayload, payload) {
				return errors.New("OpenRouter image providers require incompatible request payloads")
			}
			commonPayload = payload
			body, err = common.Marshal(payload)
			if err != nil {
				return err
			}
		}
		maximumEstimate = max(maximumEstimate, estimate)
		eligibleTags = append(eligibleTags, endpoint.ProviderTag)
		if selected == nil || rank < selectedRank || rank == selectedRank && estimate < selected.EstimatedCostUSD {
			selectedRank = rank
			selected = &relaycommon.NativeImageInfo{RequestBody: body, EstimatedCostUSD: estimate, ChannelID: info.ChannelId, ProviderTag: endpoint.ProviderTag, Model: request.Model, ReserveUSD: reserve, PricingUnknown: len(endpoint.Pricing) == 0}
		}
	}
	if selected == nil {
		if lastErr == nil {
			lastErr = errors.New("no compatible OpenRouter image provider")
		}
		return lastErr
	}
	selected.EstimatedCostUSD = maximumEstimate
	if len(routing.Fields) > 0 {
		// Preserve the client's fallback/order policy over every endpoint that
		// passed capability and price validation, rather than pinning the first.
		var fields map[string]json.RawMessage
		if err = common.Unmarshal(selected.RequestBody, &fields); err != nil {
			return err
		}
		routing.Fields["only"], err = common.Marshal(eligibleTags)
		if err != nil {
			return err
		}
		fields["provider"], err = common.Marshal(routing.Fields)
		if err != nil {
			return err
		}
		selected.RequestBody, err = common.Marshal(fields)
		if err != nil {
			return err
		}
	}
	info.NativeImage = selected
	return nil
}

// ConvertImageRequest accepts the existing OpenAI image JSON/multipart shapes,
// but sends only supported native parameters. ResponseFormat is local: like
// other base64-only adaptors, results are returned in data[].b64_json.
func ConvertImageRequest(c *gin.Context, request dto.ImageRequest, endpoint ImageEndpoint) ([]byte, float64, error) {
	return convertImageRequest(c, request, endpoint, 0)
}

func convertImageRequest(c *gin.Context, request dto.ImageRequest, endpoint ImageEndpoint, reserve float64) ([]byte, float64, error) {
	routing, err := parseImageRouting(request)
	if err != nil {
		return nil, 0, err
	}
	if routing.Only != nil && !slices.Contains(routing.Only, endpoint.ProviderTag) || slices.Contains(routing.Ignore, endpoint.ProviderTag) || !routing.AllowFallbacks && len(routing.Order) > 0 && routing.Order[0] != endpoint.ProviderTag {
		return nil, 0, errors.New("requested provider is unsupported by this endpoint")
	}

	if strings.TrimSpace(request.Prompt) == "" {
		return nil, 0, errors.New("prompt is required")
	}
	if request.ResponseFormat != "" && request.ResponseFormat != "url" && request.ResponseFormat != "b64_json" {
		return nil, 0, errors.New("unsupported response_format")
	}
	if len(endpoint.Pricing) == 0 && endpoint.ProviderTag == "krea" {
		// Published normal-generation tariffs; style references/moodboards are
		// unsupported here and cannot silently select their higher tariffs.
		// https://openrouter.ai/krea/krea-2-large
		// https://openrouter.ai/krea/krea-2-medium
		// https://openrouter.ai/krea/krea-2-medium-turbo
		endpoint = PublishedImageEndpoint(request.Model, endpoint)
	}
	raw, err := common.Marshal(request)
	if err != nil {
		return nil, 0, err
	}
	fields := map[string]json.RawMessage{}
	if err = common.Unmarshal(raw, &fields); err != nil {
		return nil, 0, err
	}
	for key, value := range request.Extra {
		fields[key] = value
	}
	if c.Request.MultipartForm != nil {
		for key, values := range c.Request.MultipartForm.Value {
			if len(values) != 1 {
				return nil, 0, fmt.Errorf("duplicate image parameter %s", key)
			}
			switch key {
			case "n", "output_compression", "seed":
				fields[key] = json.RawMessage(values[0])
			case "stream":
				fields[key] = json.RawMessage(values[0])
			default:
				fields[key], err = common.Marshal(values[0])
			}
			if err != nil {
				return nil, 0, errors.New("invalid image form parameter")
			}
		}
		if len(c.Request.MultipartForm.File["mask"]) > 0 {
			return nil, 0, errors.New("OpenRouter Image API does not support masks")
		}
	}
	if value, present := fields["response_format"]; present {
		var format string
		if common.Unmarshal(value, &format) != nil || (format != "" && format != "url" && format != "b64_json") {
			return nil, 0, errors.New("unsupported response_format")
		}
	}
	if value, present := fields["size"]; present {
		var size string
		if common.Unmarshal(value, &size) != nil {
			return nil, 0, errors.New("invalid image size")
		}
		if slices.Contains([]string{"512", "768", "1K", "1.5K", "2K", "4K"}, size) {
			if resolution, exists := fields["resolution"]; exists && !bytes.Equal(resolution, value) {
				return nil, 0, errors.New("size and resolution conflict")
			}
			fields["resolution"] = value
			delete(fields, "size")
		}
	}
	payload := map[string]any{"model": request.Model, "prompt": request.Prompt}
	count := uint(1)
	if request.N != nil {
		count = *request.N
	}
	if count == 0 || count > min(uint(dto.MaxImageN), uint(10)) {
		return nil, 0, errors.New("OpenRouter image n must be between 1 and 10")
	}
	if cap, ok := endpoint.SupportedParameters["n"]; ok {
		if cap.Type != "range" || int64(count) < cap.Min || int64(count) > cap.Max {
			return nil, 0, errors.New("image count is unsupported by the selected OpenRouter endpoint")
		}
		payload["n"] = count
	} else if count != 1 {
		return nil, 0, errors.New("image count is unsupported by the selected OpenRouter endpoint")
	}
	references := []imageReference{}
	for key, value := range fields {
		if string(value) == "null" {
			continue
		}
		switch key {
		case "model", "prompt", "n", "response_format", "stream", "provider":
			continue
		case "image", "images", "input_references":
			var input any
			if err = common.Unmarshal(value, &input); err != nil {
				return nil, 0, errors.New("invalid reference image")
			}
			if key == "input_references" {
				var native []imageReference
				if err = common.Unmarshal(value, &native); err != nil {
					return nil, 0, errors.New("invalid input_references")
				}
				references = append(references, native...)
			} else {
				items, ok := input.([]any)
				if !ok {
					items = []any{input}
				}
				for _, item := range items {
					ref := imageReference{Type: "image_url"}
					switch v := item.(type) {
					case string:
						ref.ImageURL.URL = v
					case map[string]any:
						ref.ImageURL.URL, _ = v["image_url"].(string)
					default:
						return nil, 0, errors.New("invalid reference image")
					}
					references = append(references, ref)
				}
			}
		case "user", "session_id":
			var v string
			if common.Unmarshal(value, &v) != nil || len(v) > 256 {
				return nil, 0, fmt.Errorf("invalid %s", key)
			}
			payload[key] = v
		case "size":
			if endpoint.PromptOnly {
				return nil, 0, errors.New("image size is unsupported by the documented prompt-only endpoint")
			}
			var size string
			if common.Unmarshal(value, &size) != nil {
				return nil, 0, errors.New("invalid image size")
			}
			width, height, ok := strings.Cut(size, "x")
			w, e1 := strconv.ParseUint(width, 10, 32)
			h, e2 := strconv.ParseUint(height, 10, 32)
			// Bound explicit pixels before pricing arithmetic. The normalized API's
			// largest tier is 4K and its longest documented aspect ratio is 8:1.
			if !ok || e1 != nil || e2 != nil || w == 0 || h == 0 || w > 4096*8 || h > 4096*8 {
				return nil, 0, errors.New("image size is outside the supported bounds")
			}
			payload[key] = size
		default:
			cap, ok := endpoint.SupportedParameters[key]
			if !ok {
				return nil, 0, fmt.Errorf("OpenRouter image parameter %s is unsupported by this endpoint", key)
			}
			var v any
			if common.Unmarshal(value, &v) != nil {
				return nil, 0, fmt.Errorf("invalid image parameter %s", key)
			}
			switch cap.Type {
			case "enum":
				text, ok := v.(string)
				if !ok || !slices.Contains(cap.Values, text) {
					return nil, 0, fmt.Errorf("unsupported image %s", key)
				}
			case "range":
				number, ok := v.(float64)
				if !ok || math.Trunc(number) != number || number < float64(cap.Min) || number > float64(cap.Max) {
					return nil, 0, fmt.Errorf("image %s is outside supported bounds", key)
				}
			case "boolean":
				// Boolean descriptors advertise presence, not the parameter's wire type.
				if key == "seed" {
					var seed int64
					if common.Unmarshal(value, &seed) != nil {
						return nil, 0, errors.New("seed must be an integer")
					}
					v = seed
				}
			default:
				return nil, 0, fmt.Errorf("unknown capability descriptor for %s", key)
			}
			payload[key] = v
		}
	}
	if c.Request.MultipartForm != nil {
		for key, files := range c.Request.MultipartForm.File {
			if key != "image" && key != "image[]" && !strings.HasPrefix(key, "image[") {
				return nil, 0, errors.New("unsupported image form file")
			}
			for _, file := range files {
				source, err := file.Open()
				if err != nil {
					return nil, 0, errors.New("cannot read input image")
				}
				data, err := io.ReadAll(io.LimitReader(source, file.Size+1))
				source.Close()
				if err != nil || int64(len(data)) != file.Size {
					return nil, 0, errors.New("cannot read input image")
				}
				mediaType := http.DetectContentType(data)
				if !strings.HasPrefix(mediaType, "image/") {
					return nil, 0, errors.New("invalid input image media type")
				}
				ref := imageReference{Type: "image_url"}
				ref.ImageURL.URL = "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
				references = append(references, ref)
			}
		}
	}
	cap, acceptsReferences := endpoint.SupportedParameters["input_references"]
	if strings.HasSuffix(c.Request.URL.Path, "/edits") && len(references) == 0 {
		return nil, 0, errors.New("OpenRouter image editing requires an input reference image")
	}
	if len(references) > 16 || (acceptsReferences && (int64(len(references)) < cap.Min || int64(len(references)) > cap.Max)) || (!acceptsReferences && len(references) > 0) {
		return nil, 0, errors.New("reference image count is unsupported by the selected OpenRouter endpoint")
	}
	for _, ref := range references {
		target, err := url.Parse(ref.ImageURL.URL)
		if ref.Type != "image_url" || err != nil || !((target.Scheme == "https" || target.Scheme == "http") && target.Host != "" || target.Scheme == "data" && strings.HasPrefix(ref.ImageURL.URL, "data:image/")) {
			return nil, 0, errors.New("invalid reference image URL")
		}
	}
	if len(references) > 0 {
		payload["input_references"] = references
	}
	provider := map[string]any{}
	for key, value := range routing.Fields {
		provider[key] = value
	}
	provider["allow_fallbacks"] = false
	if endpoint.ProviderTag != "" {
		provider["only"] = []string{endpoint.ProviderTag}
	}
	payload["provider"] = provider
	var estimate float64
	if len(endpoint.Pricing) == 0 && reserve > 0 {
		resolution, ok := payload["resolution"].(string)
		cap := endpoint.SupportedParameters["resolution"]
		_, sorted := routing.Fields["sort"]
		var stream bool
		if raw, exists := fields["stream"]; exists && common.Unmarshal(raw, &stream) != nil {
			return nil, 0, errors.New("invalid image stream")
		}
		if count != 1 || len(references) > 1 || stream || c.Request.MultipartForm != nil || !ok || cap.Type != "enum" || !slices.Contains(cap.Values, resolution) || endpoint.ProviderTag == "" || len(routing.Only) != 1 || routing.Only[0] != endpoint.ProviderTag || routing.AllowFallbacks || sorted {
			return nil, 0, errors.New("unpriced OpenRouter image requires one nonstream output, at most one reference, explicit resolution and one pinned provider without fallbacks or price sorting")
		}
		for key := range payload {
			if !slices.Contains([]string{"model", "prompt", "n", "resolution", "aspect_ratio", "input_references", "provider", "user", "session_id"}, key) {
				return nil, 0, fmt.Errorf("unpriced OpenRouter image parameter %s is unsupported", key)
			}
		}
		estimate = reserve
	} else {
		estimate, err = EstimateImageCost(endpoint.Pricing, int(count), len(references), payload)
	}
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", errImageTariff, err)
	}
	body, err := common.Marshal(payload)
	return body, max(estimate, reserve), err
}

// EstimateImageCost reserves an estimate; it is never substituted for actual
// usage.cost. Variant tariffs use the highest published rate, since normalized
// dimensions and token counts are provider-specific. Final usage reconciles it.
func EstimateImageCost(prices []ImagePrice, count, references int, payload map[string]any) (float64, error) {
	if len(prices) == 0 {
		return 0, errors.New("OpenRouter image endpoint has no published tariff")
	}
	rates := map[string]ImagePrice{}
	for _, price := range prices {
		if price.missingCost || math.IsNaN(price.CostUSD) || math.IsInf(price.CostUSD, 0) || price.CostUSD < 0 {
			return 0, errors.New("invalid OpenRouter image tariff")
		}
		key := price.Billable + "/" + price.Unit
		if previous, ok := rates[key]; !ok || price.CostUSD > previous.CostUSD {
			rates[key] = price
		}
	}
	outputMegapixels := 16.0
	if size, ok := payload["size"].(string); ok {
		width, height, _ := strings.Cut(size, "x")
		w, _ := strconv.ParseFloat(width, 64)
		h, _ := strconv.ParseFloat(height, 64)
		outputMegapixels = max(outputMegapixels, w*h/1_000_000)
	}
	cost := 0.0
	for _, price := range rates {
		quantity := float64(count)
		switch price.Billable {
		case "output_image", "input_text":
		case "input_image", "input_reference":
			quantity = float64(references)
		case "input_font":
			// Fonts are not accepted by this adapter, so this published optional tariff is unused.
			quantity = 0
		default:
			return 0, errors.New("unknown OpenRouter image billable unit")
		}
		switch price.Unit {
		case "image":
		case "request":
			if quantity > 0 {
				quantity = 1
			}
		case "megapixel":
			quantity *= outputMegapixels // Conservative 4K estimate; actual USD is authoritative.
		case "token":
			if price.Billable == "input_text" {
				prompt, _ := payload["prompt"].(string)
				quantity *= float64(len(prompt) + 1024) // Byte upper estimate plus protocol overhead, not a tokenizer claim.
			} else {
				quantity *= math.Ceil(outputMegapixels * 4096) // Reservation estimate, not an asserted token conversion.
			}
		default:
			return 0, errors.New("unknown OpenRouter image pricing unit")
		}
		cost += quantity * price.CostUSD
	}
	if math.IsNaN(cost) || math.IsInf(cost, 0) || cost < 0 {
		return 0, errors.New("invalid OpenRouter image cost estimate")
	}
	return cost, nil
}

func (a *ImageAdaptor) ConvertImageRequest(_ *gin.Context, info *relaycommon.RelayInfo, _ dto.ImageRequest) (any, error) {
	if info.NativeImage == nil || info.NativeImage.ChannelID != info.ChannelId || info.NativeImage.Model != info.UpstreamModelName {
		return nil, errors.New("OpenRouter image request was not prepared for this channel")
	}
	return json.RawMessage(info.NativeImage.RequestBody), nil
}

func (a *ImageAdaptor) DoRequest(c *gin.Context, info *relaycommon.RelayInfo, _ io.Reader) (any, error) {
	if info.NativeImage == nil || info.NativeImage.ChannelID != info.ChannelId || info.NativeImage.Model != info.UpstreamModelName || info.NativeImage.Sent {
		return nil, errors.New("OpenRouter image paid request cannot be repeated")
	}
	endpoint, err := a.GetRequestURL(info)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, endpoint, bytes.NewReader(info.NativeImage.RequestBody))
	if err != nil {
		return nil, err
	}
	req.GetBody = nil // Paid generations must not be transparently replayed.
	if err = a.SetupRequestHeader(c, &req.Header, info); err != nil {
		return nil, err
	}
	overrides, err := channel.ResolveHeaderOverride(info, c)
	if err != nil {
		return nil, err
	}
	for key, value := range overrides {
		req.Header.Set(key, value)
	}
	req.Header.Set("Content-Type", "application/json")
	if err := ReserveCost(c, info, info.NativeImage.EstimatedCostUSD); err != nil {
		return nil, err
	}
	if relaycommon.HasOpenRouterSyncTask(info) {
		if err := taskrecord.ConfigureOpenRouterSyncSubmission(info, "", nil); err != nil {
			return nil, err
		}
	}
	info.DisablePing = true
	info.OpenRouterCost.Sent = true
	info.NativeImage.Sent = true
	c.Set(common.UpstreamRequestIdKey, "") // Discovery IDs do not identify the paid generation.
	resp, err := channel.DoRequest(c, req, info)
	if err != nil {
		info.NativeImage.OutcomeUnknown = true
		return nil, errors.New("OpenRouter image transport failed; generation outcome is unknown")
	}
	captureImageRequestID(c, resp, info)
	return resp, nil
}

func captureImageRequestID(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) {
	for _, key := range []string{"X-Request-Id", "Request-Id", common.RequestIdKey} {
		if id := resp.Header.Get(key); id != "" && len(id) <= 128 && !strings.Contains(id, "sk-") && (info.ApiKey == "" || !strings.Contains(id, info.ApiKey)) && strings.IndexFunc(id, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:", r))
		}) == -1 {
			c.Set(common.UpstreamRequestIdKey, id)
			break
		}
	}
}

func (a *ImageAdaptor) DoResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (usageResult any, apiErr *types.NewAPIError) {
	defer func() {
		if apiErr != nil && relaycommon.HasOpenRouterSyncTask(info) {
			_ = taskrecord.FailOpenRouterSyncTask(info, "invalid_image_result")
		}
	}()
	if resp == nil || resp.Body == nil {
		info.NativeImage.OutcomeUnknown = true
		return nil, imageResponseError("OpenRouter returned no image response")
	}
	defer httpclient.CloseResponseBodyGracefully(resp)
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, int64(max(constant.MaxRequestBodyMB, 64))*1024*1024+1))
	if err != nil || len(responseBody) > max(constant.MaxRequestBodyMB, 64)*1024*1024 {
		info.NativeImage.OutcomeUnknown = true
		return nil, imageResponseError("OpenRouter image response was interrupted or exceeded the response limit")
	}
	if relaycommon.HasOpenRouterSyncTask(info) {
		observeImageCost(info, responseBody)
	}
	var envelope struct {
		Error json.RawMessage `json:"error"`
	}
	if common.Unmarshal(responseBody, &envelope) == nil && len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		resp.Body = io.NopCloser(bytes.NewReader(responseBody))
		return nil, ImageError(c, resp, info)
	}
	result, usage, err := ParseImageResponse(responseBody)
	if err != nil {
		info.NativeImage.OutcomeUnknown = !info.OpenRouterCost.Final
		return nil, imageResponseError(err.Error())
	}
	cost := *result.Usage.Cost
	info.NativeImage.ActualCostUSD = &cost
	info.OpenRouterCost.Observe(responseBody, true)
	info.OpenRouterCost.CostUSD = cost
	info.OpenRouterCost.Valid = true
	info.OpenRouterCost.Final = true
	encoded, err := common.Marshal(result)
	if err != nil {
		return nil, imageResponseError("OpenRouter image result encoding failed")
	}
	if relaycommon.HasOpenRouterSyncTask(info) {
		if err := taskrecord.CompleteOpenRouterSyncTask(c, info, encoded, "application/json"); err != nil {
			return nil, imageResponseError(err.Error())
		}
	}
	if info.PriceData.UsePrice {
		info.PriceData.AddOtherRatio("n", float64(len(result.Data)))
	}
	// A buffered native request also supports clients asking for the existing
	// OpenAI SSE format; previews are optional and never billed separately.
	if info.IsStream {
		helper.SetEventStreamHeaders(c)
		for index, data := range result.Data {
			event := map[string]any{"type": "image_generation.completed", "b64_json": data.B64JSON, "created_at": result.Created}
			if index == len(result.Data)-1 {
				event["usage"] = usage
			}
			if data.MediaType != "" {
				event["media_type"] = data.MediaType
			}
			if strings.HasSuffix(c.Request.URL.Path, "/edits") {
				event["type"] = "image_edit.completed"
			}
			encoded, _ := common.Marshal(event)
			if writeErr := helper.ResponseChunkData(c, dto.ResponsesStreamResponse{Type: event["type"].(string)}, string(encoded)); writeErr != nil {
				logger.LogWarn(c, "OpenRouter image result delivery failed after completed generation")
				break
			}
		}
		helper.Done(c)
	} else {
		if writeErr := httpclient.IOCopyBytesGracefully(c, resp, encoded); writeErr != nil {
			logger.LogWarn(c, "OpenRouter image result delivery failed after completed generation")
		}
	}
	return usage, nil
}

// Monetary evidence is independent of image decoding and client delivery.
func observeImageCost(info *relaycommon.RelayInfo, body []byte) {
	var envelope struct {
		Usage struct {
			Cost json.RawMessage `json:"cost"`
		} `json:"usage"`
	}
	if common.Unmarshal(body, &envelope) != nil {
		return
	}
	cost, err := common.ValidateOpenRouterCost(envelope.Usage.Cost)
	if err != nil {
		return
	}
	info.OpenRouterCost.CostUSD, info.OpenRouterCost.Valid, info.OpenRouterCost.Final = cost, true, true
	info.NativeImage.ActualCostUSD = &cost
}

func imageResponseError(reason string) *types.NewAPIError {
	return types.NewErrorWithStatusCode(errors.New(reason), types.ErrorCodeBadResponseBody, http.StatusBadGateway, types.ErrOptionWithSkipRetry())
}

// ParseImageResponse distinguishes missing/null cost from an explicit zero.
// No synthetic token or price fallback can turn missing usage into a free call.
func ParseImageResponse(body []byte) (*imageResponse, *dto.Usage, error) {
	var result imageResponse
	if err := common.Unmarshal(body, &result); err != nil {
		return nil, nil, errors.New("invalid OpenRouter image response JSON")
	}
	if len(result.Error) > 0 && string(result.Error) != "null" {
		return nil, nil, errors.New("OpenRouter returned an image generation error")
	}
	if len(result.Data) == 0 || len(result.Data) > dto.MaxImageN {
		return nil, nil, errors.New("OpenRouter returned no valid image result")
	}
	for i := range result.Data {
		data := &result.Data[i]
		decoded, err := base64.StdEncoding.Strict().DecodeString(data.B64JSON)
		if err != nil || len(decoded) == 0 {
			return nil, nil, errors.New("invalid OpenRouter image base64")
		}
		detected := http.DetectContentType(decoded)
		if data.MediaType == "image/svg+xml" {
			var root struct{ XMLName xml.Name }
			if xml.Unmarshal(decoded, &root) != nil || root.XMLName.Local != "svg" {
				return nil, nil, errors.New("invalid OpenRouter SVG image")
			}
		} else {
			config, _, err := image.DecodeConfig(bytes.NewReader(decoded))
			if err != nil || config.Width <= 0 || config.Height <= 0 || config.Width > 4096*8 || config.Height > 4096*8 {
				return nil, nil, errors.New("invalid OpenRouter raster image")
			}
			if data.MediaType != "" && data.MediaType != detected {
				return nil, nil, errors.New("OpenRouter image media type mismatch")
			}
			data.MediaType = detected
		}
	}
	if result.Usage == nil || result.Usage.Cost == nil {
		return nil, nil, errors.New("OpenRouter image cost is missing; financial outcome requires reconciliation")
	}
	cost := *result.Usage.Cost
	if math.IsNaN(cost) || math.IsInf(cost, 0) || cost < 0 {
		return nil, nil, errors.New("invalid OpenRouter image cost")
	}
	usage := result.Usage.Usage
	if usage.PromptTokens < 0 || usage.CompletionTokens < 0 || usage.TotalTokens < 0 || usage.PromptTokens > math.MaxInt32 || usage.CompletionTokens > math.MaxInt32 || usage.TotalTokens > math.MaxInt32 {
		return nil, nil, errors.New("invalid OpenRouter image token usage")
	}
	usage.Cost = cost
	return &result, &usage, nil
}

// ImageError records only a bounded reason, status, and correlation ID. Provider
// metadata.raw and flagged inputs never enter the relay error or backend logs.
func ImageError(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) *types.NewAPIError {
	defer httpclient.CloseResponseBodyGracefully(resp)
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
	info.NativeImage.OutcomeUnknown = readErr != nil || relaycommon.HasOpenRouterSyncTask(info)
	if readErr == nil && len(body) <= 64<<10 && relaycommon.HasOpenRouterSyncTask(info) {
		observeImageCost(info, body)
		info.NativeImage.OutcomeUnknown = !info.OpenRouterCost.Final
	}
	if relaycommon.HasOpenRouterSyncTask(info) {
		_ = taskrecord.FailOpenRouterSyncTask(info, "image_upstream_error")
	}
	var envelope struct {
		Error struct {
			Code    any    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	reason := "OpenRouter image generation failed"
	code := "upstream_error"
	if readErr == nil && common.Unmarshal(body, &envelope) == nil {
		switch value := envelope.Error.Code.(type) {
		case string:
			if len(value) <= 64 && !strings.ContainsAny(value, " \r\n") && !strings.Contains(value, "sk-") && (info.ApiKey == "" || !strings.Contains(value, info.ApiKey)) {
				code = value
			}
		case float64:
			if value >= 100 && value <= 599 && math.Trunc(value) == value {
				code = strconv.FormatFloat(value, 'f', 0, 64)
			}
		}
		if envelope.Error.Message != "" {
			reason = envelope.Error.Message
		}
	} else {
		info.NativeImage.OutcomeUnknown = true
	}
	if info.ApiKey != "" {
		reason = strings.ReplaceAll(reason, info.ApiKey, "[redacted]")
	}
	if request, ok := info.Request.(*dto.ImageRequest); ok && request.Prompt != "" {
		reason = strings.ReplaceAll(reason, request.Prompt, "[redacted prompt]")
	}
	words := strings.Fields(reason)
	for i, word := range words {
		if len(word) > 128 || strings.Contains(word, "sk-") || strings.Contains(word, "data:") || strings.EqualFold(word, "Bearer") {
			words[i] = "[redacted]"
		}
	}
	reason = common.MaskSensitiveInfo(strings.Join(words, " "))
	if len(reason) > 256 {
		reason = reason[:256]
	}
	logger.LogWarn(c, fmt.Sprintf("OpenRouter image upstream HTTP %d code=%s reason=%s upstream_request_id=%s", resp.StatusCode, code, reason, c.GetString(common.UpstreamRequestIdKey)))
	status := resp.StatusCode
	if status < 400 || status > 599 {
		status = http.StatusBadGateway
	}
	return types.NewErrorWithStatusCode(fmt.Errorf("OpenRouter image error (%s): %s", code, reason), types.ErrorCodeBadResponse, status, types.ErrOptionWithSkipRetry())
}
