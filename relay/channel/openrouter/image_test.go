package openrouter

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNativeImageRequestContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	endpoint := ImageEndpoint{
		ProviderTag: "seed",
		SupportedParameters: map[string]ImageCapability{
			"n":                {Type: "range", Min: 1, Max: 4},
			"resolution":       {Type: "enum", Values: []string{"1K", "2K"}},
			"input_references": {Type: "range", Min: 0, Max: 2},
			"seed":             {Type: "boolean"},
		},
		Pricing: []ImagePrice{{Billable: "output_image", Unit: "image", CostUSD: 0.04}},
	}
	for _, tc := range []struct {
		name, body string
		want       map[string]any
		errorText  string
	}{
		{name: "native endpoint receives only supported parameters", body: `{"model":"seed/image","prompt":"a red square","n":2,"size":"2K","response_format":"url","seed":0}`, want: map[string]any{"model": "seed/image", "prompt": "a red square", "n": float64(2), "resolution": "2K", "seed": float64(0), "provider": map[string]any{"only": []any{"seed"}, "allow_fallbacks": false}}},
		{name: "client stream does not request provider streaming", body: `{"model":"seed/image","prompt":"a red square","stream":true}`, want: map[string]any{"model": "seed/image", "prompt": "a red square", "n": float64(1), "provider": map[string]any{"only": []any{"seed"}, "allow_fallbacks": false}}},
		{name: "unsupported quality", body: `{"model":"seed/image","prompt":"x","quality":"high"}`, errorText: "quality is unsupported"},
		{name: "unsupported masks", body: `{"model":"seed/image","prompt":"x","mask":"https://example.com/mask.png"}`, errorText: "mask is unsupported"},
		{name: "unsupported count", body: `{"model":"seed/image","prompt":"x","n":5}`, errorText: "count is unsupported"},
		{name: "huge count", body: `{"model":"seed/image","prompt":"x","n":18446744073686646784}`, errorText: "between 1 and 10"},
		{name: "zero count", body: `{"model":"seed/image","prompt":"x","n":0}`, errorText: "between 1 and 10"},
		{name: "unsupported resolution", body: `{"model":"seed/image","prompt":"x","size":"4K"}`, errorText: "unsupported image resolution"},
		{name: "conflicting size", body: `{"model":"seed/image","prompt":"x","size":"1K","resolution":"2K"}`, errorText: "conflict"},
		{name: "unbounded pixels", body: `{"model":"seed/image","prompt":"x","size":"4294967295x4294967295"}`, errorText: "supported bounds"},
		{name: "provider bypass", body: `{"model":"seed/image","prompt":"x","provider":{"only":["another"]}}`, errorText: "provider is unsupported"},
		{name: "malformed seed", body: `{"model":"seed/image","prompt":"x","seed":"zero"}`, errorText: "seed must be an integer"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewBufferString(tc.body))
			var request dto.ImageRequest
			require.NoError(t, common.Unmarshal([]byte(tc.body), &request))
			body, estimate, err := ConvertImageRequest(c, request, endpoint)
			if tc.errorText != "" {
				require.ErrorContains(t, err, tc.errorText)
				return
			}
			require.NoError(t, err)
			var actual map[string]any
			require.NoError(t, common.Unmarshal(body, &actual))
			assert.Equal(t, tc.want, actual)
			count := 1.0
			if request.N != nil {
				count = float64(*request.N)
			}
			assert.InDelta(t, count*0.04, estimate, 1e-12)
		})
	}
	t.Run("edit requires an image", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", nil)
		_, _, err := ConvertImageRequest(c, dto.ImageRequest{Model: "seed/image", Prompt: "x"}, endpoint)
		require.ErrorContains(t, err, "requires an input reference image")
	})
	t.Run("reference-only model rejects generation", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
		endpoint.SupportedParameters["input_references"] = ImageCapability{Type: "range", Min: 1, Max: 2}
		_, _, err := ConvertImageRequest(c, dto.ImageRequest{Model: "seed/image", Prompt: "x"}, endpoint)
		require.ErrorContains(t, err, "reference image count")
	})
	t.Run("no published tariff is not a free model", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
		endpoint.Pricing = nil
		_, _, err := ConvertImageRequest(c, dto.ImageRequest{Model: "seed/image", Prompt: "x"}, endpoint)
		require.ErrorContains(t, err, "no published tariff")
	})
}

func TestNativeImageMultipartEditContract(t *testing.T) {
	var input bytes.Buffer
	bitmap := image.NewRGBA(image.Rect(0, 0, 1, 1))
	bitmap.Set(0, 0, color.RGBA{R: 255, A: 255})
	require.NoError(t, png.Encode(&input, bitmap))
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	require.NoError(t, writer.WriteField("model", "seed/image"))
	require.NoError(t, writer.WriteField("prompt", "make it blue"))
	require.NoError(t, writer.WriteField("output_compression", "0"))
	part, err := writer.CreateFormFile("image[]", "input.png")
	require.NoError(t, err)
	_, err = part.Write(input.Bytes())
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	require.NoError(t, c.Request.ParseMultipartForm(1<<20))
	t.Cleanup(func() { require.NoError(t, c.Request.MultipartForm.RemoveAll()) })
	endpoint := ImageEndpoint{SupportedParameters: map[string]ImageCapability{"input_references": {Type: "range", Min: 1, Max: 2}, "output_compression": {Type: "range", Min: 0, Max: 100}}, Pricing: []ImagePrice{{Billable: "output_image", Unit: "image", CostUSD: 0.04}, {Billable: "input_reference", Unit: "request", CostUSD: 0.005}}}
	converted, estimate, err := ConvertImageRequest(c, dto.ImageRequest{Model: "seed/image", Prompt: "make it blue"}, endpoint)
	require.NoError(t, err)
	var payload struct {
		InputReferences []imageReference `json:"input_references"`
		Compression     *int             `json:"output_compression"`
	}
	require.NoError(t, common.Unmarshal(converted, &payload))
	require.Len(t, payload.InputReferences, 1)
	assert.Equal(t, "data:image/png;base64,"+base64.StdEncoding.EncodeToString(input.Bytes()), payload.InputReferences[0].ImageURL.URL)
	require.NotNil(t, payload.Compression)
	assert.Zero(t, *payload.Compression)
	assert.InDelta(t, 0.045, estimate, 1e-12)
	assert.NotContains(t, string(converted), "response_format")
}

func TestNativeImageResponseContract(t *testing.T) {
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	encoded := base64.StdEncoding.EncodeToString(data.Bytes())
	for _, tc := range []struct {
		name, body, errorText string
		cost                  float64
	}{
		{name: "image and authoritative USD", body: fmt.Sprintf(`{"created":123,"data":[{"b64_json":%q,"media_type":"image/png"}],"usage":{"prompt_tokens":0,"completion_tokens":0,"cost":0.04}}`, encoded), cost: 0.04},
		{name: "explicit free price", body: fmt.Sprintf(`{"created":123,"data":[{"b64_json":%q}],"usage":{"cost":0}}`, encoded)},
		{name: "no usage", body: fmt.Sprintf(`{"created":123,"data":[{"b64_json":%q}]}`, encoded), errorText: "cost is missing"},
		{name: "null cost", body: fmt.Sprintf(`{"created":123,"data":[{"b64_json":%q}],"usage":{"cost":null}}`, encoded), errorText: "cost is missing"},
		{name: "negative charge", body: fmt.Sprintf(`{"created":123,"data":[{"b64_json":%q}],"usage":{"cost":-1}}`, encoded), errorText: "invalid OpenRouter image cost"},
		{name: "no image despite HTTP200", body: `{"created":123,"data":[],"usage":{"cost":0.04}}`, errorText: "no valid image"},
		{name: "invalid image despite HTTP200", body: `{"created":123,"data":[{"b64_json":"aGVsbG8="}],"usage":{"cost":0.04}}`, errorText: "invalid OpenRouter raster"},
		{name: "embedded upstream error", body: `{"error":{"code":502,"message":"upstream failure","metadata":{"raw":"secret"}}}`, errorText: "image generation error"},
		{name: "invalid media type", body: fmt.Sprintf(`{"created":123,"data":[{"b64_json":%q,"media_type":"image/jpeg"}],"usage":{"cost":0.04}}`, encoded), errorText: "media type mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, usage, err := ParseImageResponse([]byte(tc.body))
			if tc.errorText != "" {
				require.ErrorContains(t, err, tc.errorText)
				assert.Nil(t, usage)
				return
			}
			require.NoError(t, err)
			require.Len(t, response.Data, 1)
			assert.Equal(t, encoded, response.Data[0].B64JSON)
			assert.Equal(t, tc.cost, usage.Cost)
			assert.Zero(t, usage.TotalTokens, "cost billing must not invent tokens")
		})
	}
	t.Run("native URL uses dedicated API", func(t *testing.T) {
		a := &ImageAdaptor{}
		target, err := a.GetRequestURL(&relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: "https://openrouter.ai/api/"}})
		require.NoError(t, err)
		assert.Equal(t, "https://openrouter.ai/api/v1/images", target)
	})
}

func TestNativeImageTariffEstimate(t *testing.T) {
	prices := []ImagePrice{
		{Billable: "output_image", Unit: "image", CostUSD: 0.04},
		{Billable: "output_image", Unit: "image", CostUSD: 0.08, Variant: "4k"},
		{Billable: "input_reference", Unit: "request", CostUSD: 0.005},
		{Billable: "input_text", Unit: "token", CostUSD: 0.000001},
		{Billable: "input_font", Unit: "image", CostUSD: 0.03},
	}
	cost, err := EstimateImageCost(prices, 2, 2, map[string]any{"prompt": "red"})
	require.NoError(t, err)
	// Highest output variant, one reference request fee, text per output;
	// no fonts are sent. Actual settlement uses usage.cost instead.
	assert.InDelta(t, 0.16+0.005+2*1027*0.000001, cost, 1e-12)
}

func TestNativeImagePublishedTariffFallback(t *testing.T) {
	for _, tc := range []struct {
		model string
		price float64
	}{
		{"krea/krea-2-large", 0.06},
		{"krea/krea-2-medium", 0.03},
		{"krea/krea-2-medium-turbo", 0.015},
	} {
		t.Run(tc.model, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			endpoint := ImageEndpoint{ProviderTag: "krea"}
			request := dto.ImageRequest{Model: tc.model, Prompt: "A red square"}
			_, estimate, err := ConvertImageRequest(c, request, endpoint)
			require.NoError(t, err)
			assert.Equal(t, tc.price, estimate)
			request.Extra = map[string]json.RawMessage{"image_style_references": json.RawMessage(`[]`)}
			_, _, err = ConvertImageRequest(c, request, endpoint)
			require.ErrorContains(t, err, "image_style_references is unsupported")
			request.Extra = nil
			endpoint.ProviderTag = "unpriced-provider"
			_, _, err = ConvertImageRequest(c, request, endpoint)
			require.ErrorContains(t, err, "no published tariff")
		})
	}
}

func TestNativeImagePromptOnlyEndpoint(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	endpoint := ImageEndpoint{PromptOnly: true, Pricing: []ImagePrice{{Billable: "output_image", Unit: "image", CostUSD: 0.01}}}
	request := dto.ImageRequest{Model: "meta/muse-image", Prompt: "A red square"}
	body, estimate, err := ConvertImageRequest(c, request, endpoint)
	require.NoError(t, err)
	assert.Equal(t, 0.01, estimate)
	assert.JSONEq(t, `{"model":"meta/muse-image","prompt":"A red square","provider":{"allow_fallbacks":false}}`, string(body))
	request.Size = "32768x32768"
	_, _, err = ConvertImageRequest(c, request, endpoint)
	require.ErrorContains(t, err, "size is unsupported")
}

func TestNativeImageUpstreamDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, body, id       string
		status, clientStatus int
		unknown              bool
	}{
		{name: "known error", body: `{"error":{"code":429,"message":"rate limited","metadata":{"raw":"secret-key private-prompt"}}}`, id: "req-123:abc", status: 429, clientStatus: 429},
		{name: "sensitive fields", body: `{"error":{"code":"secret-key","message":"secret-key private-prompt https://private.example/image?token=private-query"}}`, id: "secret-key", status: 500, clientStatus: 500},
		{name: "embedded error", body: `{"error":{"code":400,"message":"invalid image"}}`, id: "https://example.com?secret-key", status: 200, clientStatus: 502},
		{name: "unreadable error", body: `not JSON`, status: 502, clientStatus: 502, unknown: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "secret-key"}, Request: &dto.ImageRequest{Prompt: "private-prompt"}, NativeImage: &relaycommon.NativeImageInfo{Sent: true}}
			response := &http.Response{StatusCode: tc.status, Header: http.Header{"X-Request-Id": []string{tc.id}}, Body: io.NopCloser(strings.NewReader(tc.body))}
			captureImageRequestID(c, response, info)
			apiErr := ImageError(c, response, info)
			require.NotNil(t, apiErr)
			assert.Equal(t, tc.clientStatus, apiErr.StatusCode)
			assert.Equal(t, tc.unknown, info.NativeImage.OutcomeUnknown)
			for _, secret := range []string{"secret-key", "private-prompt", "private-query", "metadata"} {
				assert.NotContains(t, apiErr.Error(), secret)
			}
			if tc.name == "known error" {
				assert.Equal(t, tc.id, c.GetString(common.UpstreamRequestIdKey))
			} else {
				assert.Empty(t, c.GetString(common.UpstreamRequestIdKey))
			}
		})
	}
}

func TestVideoOperationMonetaryEstimates(t *testing.T) {
	audioOff := false
	for _, tc := range []struct {
		name, model string
		prices      map[string]string
		order       VideoPriceOrder
		sizes       []string
		want        float64
		errorText   string
	}{
		{name: "cents output plus input images", model: "x-ai/grok-imagine-video", prices: map[string]string{"cents_per_video_output_second_480p": "2", "cents_per_video_output_second_720p": "3", "cents_per_image_input": "1"}, order: VideoPriceOrder{Resolution: "480p", FrameImages: []struct {
			Type string `json:"type"`
		}{{Type: "image_url"}}}, want: 0.11},
		{name: "omitted resolution takes maximum", model: "x-ai/grok-imagine-video", prices: map[string]string{"cents_per_video_output_second_480p": "2", "cents_per_video_output_second_720p": "3"}, want: 0.15},
		{name: "USD seconds audio alternative", model: "google/veo-3.1", prices: map[string]string{"duration_seconds_with_audio": "0.4", "duration_seconds_without_audio": "0.2"}, order: VideoPriceOrder{GenerateAudio: &audioOff}, want: 1},
		{name: "reference conditioned output not source duration", model: "heygen/heygen-video-1", prices: map[string]string{"duration_seconds_480p": "0.2", "reference_duration_seconds_480p": "0.4"}, order: VideoPriceOrder{InputReferences: []struct {
			Type string `json:"type"`
		}{{Type: "video_url"}}}, want: 2},
		{name: "first five images free", model: "minimax/hailuo-3", prices: map[string]string{"duration_seconds": "0.2", "reference_images": "0.1"}, order: VideoPriceOrder{InputReferences: []struct {
			Type string `json:"type"`
		}{{Type: "image_url"}, {Type: "image_url"}, {Type: "image_url"}, {Type: "image_url"}, {Type: "image_url"}, {Type: "image_url"}}}, want: 1.1},
		{name: "token formula exact size", model: "bytedance/seedance-2.0", prices: map[string]string{"video_tokens": "0.00001"}, order: VideoPriceOrder{Size: "1280x720"}, sizes: []string{"640x480", "1280x720"}, want: 1.08},
		{name: "finite size superset avoids guessed resolution map", model: "bytedance/seedance-2.5", prices: map[string]string{"video_tokens": "0.00001"}, order: VideoPriceOrder{Resolution: "480p"}, sizes: []string{"640x480", "1280x720"}, want: 1.08},
		{name: "edit conservative 15 second maximum", model: "black-forest-labs/flux-video-edit", prices: map[string]string{"cents_per_second_output": "12"}, want: 1.8},
		{name: "unknown SKU rejected", model: "runway/gen-4.5", prices: map[string]string{"cents_per_second_output": "12", "unexplained": "9"}, errorText: "unknown video pricing SKU"},
		{name: "avatar quantity unavailable", model: "heygen/avatar-iv", prices: map[string]string{"duration_seconds": "0.1"}, errorText: "source media quantity"},
		{name: "aleph quantity unavailable despite floor", model: "runway/aleph-2", prices: map[string]string{"cents_per_second_output": "10", "minimum_cents_per_generation": "50"}, errorText: "source media quantity"},
		{name: "upscale quantity unavailable", model: "black-forest-labs/flux-video-upscale", prices: map[string]string{"cents_per_megapixel_second_precise": "1"}, errorText: "source media quantity"},
		{name: "source video tokens unavailable", model: "bytedance/seedance-2.0", prices: map[string]string{"video_tokens": "0.1"}, order: VideoPriceOrder{InputReferences: []struct {
			Type string `json:"type"`
		}{{Type: "video_url"}}}, sizes: []string{"1280x720"}, errorText: "source video token quantity"},
		{name: "invalid cents", model: "runway/gen-4.5", prices: map[string]string{"cents_per_second_output": "-12"}, errorText: "invalid video tariff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.order.Model = tc.model
			model := VideoModelPricing{ID: tc.model, PricingSKUs: tc.prices, SupportedDurations: []int{3, 5}, SupportedSizes: tc.sizes, SupportedResolutions: []string{"480p", "720p"}}
			cost, err := EstimateVideoOrder(model, tc.order)
			if tc.errorText != "" {
				require.ErrorContains(t, err, tc.errorText)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.want, cost, 1e-12)
		})
	}
}

func TestVideoFreshDiscoveryBeforeTransport(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/videos/models", r.URL.Path)
		assert.Equal(t, "Bearer selected-key", r.Header.Get("Authorization"))
		_, _ = io.WriteString(w, `{"data":[{"id":"runway/gen-4.5","supported_durations":[5,10],"pricing_skus":{"cents_per_second_output":"30"}}]}`)
	}))
	defer server.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ApiKey: "selected-key", ChannelBaseUrl: server.URL, UpstreamModelName: "runway/gen-4.5"}}
	cost, err := EstimateVideo(c, info, []byte(`{"model":"runway/gen-4.5","prompt":"p"}`))
	require.NoError(t, err)
	assert.InDelta(t, 3, cost, 1e-12, "fresh tariff and maximum supported duration replace embedded catalog prices")
	assert.Equal(t, 1, requests)
}

func TestEndpointRequestReservation(t *testing.T) {
	rate := func(prompt, completion string) map[string]json.RawMessage {
		return map[string]json.RawMessage{"prompt": json.RawMessage(`"` + prompt + `"`), "completion": json.RawMessage(`"` + completion + `"`)}
	}
	endpoints := []PricedEndpoint{
		{Tag: "cheap/base", ContextLength: 10000, MaxCompletionTokens: 100, Pricing: rate("0.001", "0.002")},
		{Tag: "expensive/base", ContextLength: 10000, MaxCompletionTokens: 200, Pricing: rate("0.003", "0.004")},
	}
	estimator := NewRequestEstimator()
	estimator.Models["test/model"] = endpoints
	info := &relaycommon.RelayInfo{}
	info.SetEstimatePromptTokens(1000)
	for _, tc := range []struct {
		name, body string
		want       float64
		fail       bool
	}{
		{"complete endpoint maximum", `{"model":"test/model","max_tokens":10}`, 3.04, false},
		{"only restricts", `{"model":"test/model","max_tokens":10,"provider":{"only":["cheap"]}}`, 1.02, false},
		{"ignore variant", `{"model":"test/model","max_tokens":10,"provider":{"ignore":["expensive"]}}`, 1.02, false},
		{"order retains fallbacks", `{"model":"test/model","max_tokens":10,"provider":{"order":["cheap"]}}`, 3.04, false},
		{"fallback disabled", `{"model":"test/model","max_tokens":10,"provider":{"order":["cheap"],"allow_fallbacks":false}}`, 1.02, false},
		{"omitted cap uses finite metadata", `{"model":"test/model"}`, 3.8, false},
		{"explicit zero cap", `{"model":"test/model","max_tokens":0}`, 3, false},
		{"unpriced hosted tool", `{"model":"test/model","tools":[{"type":"web_search"}]}`, 0, true},
		{"unresolved preset", `{"model":"test/model","preset":"private"}`, 0, true},
		{"fallback models require discovery", `{"model":"test/model","models":["other/model"]}`, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			value, err := estimator.Estimate(info, []byte(tc.body), "chat/completions")
			if tc.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.InDelta(t, tc.want, value, 1e-9)
			}
		})
	}
	estimator.Models["test/model"][0].MaxCompletionTokens = 0
	_, err := estimator.Estimate(info, []byte(`{"model":"test/model"}`), "chat/completions")
	require.Error(t, err)
}

func TestEndpointContextPricingAndRoutingDimensions(t *testing.T) {
	estimator := NewRequestEstimator()
	base := map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.002"`), "web_search": json.RawMessage(`"0.01"`), "overrides": json.RawMessage(`[{"min_prompt_tokens":1000,"prompt":"0.003","completion":"0.006"}]`)}
	estimator.Models["test/model"] = []PricedEndpoint{{Tag: "openai", MaxCompletionTokens: 100, Pricing: base}, {Tag: "openai/fast", MaxCompletionTokens: 100, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.005"`), "completion": json.RawMessage(`"0.01"`)}}}
	estimator.Models["test/model"] = append(estimator.Models["test/model"], PricedEndpoint{Tag: "openai/flex", MaxCompletionTokens: 100, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.0005"`), "completion": json.RawMessage(`"0.001"`)}})
	info := &relaycommon.RelayInfo{}
	for _, tc := range []struct {
		name, body string
		prompt     int
		want       float64
		fail       bool
	}{
		{"below threshold", `{"model":"test/model","max_tokens":10}`, 999, 1.019, false},
		{"threshold inclusive complete tariff", `{"model":"test/model","max_tokens":10}`, 1000, 3.06, false},
		{"base provider excludes service tier", `{"model":"test/model","max_tokens":10,"provider":{"only":["openai"]}}`, 1000, 3.06, false},
		{"flex restricts eligible tier", `{"model":"test/model","max_tokens":10,"service_tier":"flex"}`, 1000, .51, false},
		{"top-level priority includes tier", `{"model":"test/model","max_tokens":10,"service_tier":"priority"}`, 1000, 5.1, false},
		{"priority slug aliases fast", `{"model":"test/model","max_tokens":10,"provider":{"only":["openai/priority"]}}`, 1000, 5.1, false},
		{"max price USD per million", `{"model":"test/model","max_tokens":10,"provider":{"max_price":{"prompt":2000}}}`, 999, 1.019, false},
		{"requested hosted search rejects", `{"model":"test/model","max_tokens":10,"web_search_options":{}}`, 999, 0, true},
		{"oversized override cap rejects", `{"model":"test/model","max_tokens":2147483647}`, 999, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info.SetEstimatePromptTokens(tc.prompt)
			cost, err := estimator.Estimate(info, []byte(tc.body), "chat/completions")
			if tc.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.InDelta(t, tc.want, cost, 1e-9)
			}
		})
	}
}

func TestEndpointMediaQuantitiesUseProtocolContent(t *testing.T) {
	estimator := NewRequestEstimator()
	estimator.Models["test/model"] = []PricedEndpoint{{Tag: "provider", MaxCompletionTokens: 100, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.002"`), "image": json.RawMessage(`"1"`), "web_search": json.RawMessage(`"0.01"`)}}}
	info := &relaycommon.RelayInfo{}
	info.SetEstimatePromptTokens(1000)
	schema := `"tools":[{"type":"function","function":{"name":"inspect_record","parameters":{"type":"object","properties":{"audio_url":{"type":"string"},"input_audio":{"type":"string"},"video_url":{"type":"string"},"web_search_options":{"type":"string"},"image_url":{"type":"string"},"input_image":{"type":"string"}}}}}]`
	for _, tc := range []struct {
		name, body, operation string
		want                  float64
		fail                  bool
	}{
		{"chat function schema", `{"model":"test/model","max_tokens":10,"messages":[{"role":"user","content":"inspect audio_url and image_url"}],` + schema + `}`, "chat/completions", 1.02, false},
		{"Responses function schema", `{"model":"test/model","max_output_tokens":10,"input":[{"role":"user","content":[{"type":"input_text","text":"web_search_options"}]}],` + schema + `}`, "responses", 1.02, false},
		{"Batch function schema", `{"model":"test/model","max_tokens":10,"messages":[{"role":"user","content":"input_audio"}],` + schema + `}`, "batch_chat/completions", 1.02, false},
		{"actual chat image", `{"model":"test/model","max_tokens":10,"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/image.png"}}]}]}`, "chat/completions", 2.02, false},
		{"actual Responses image counted once", `{"model":"test/model","max_output_tokens":10,"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.test/image.png"}]}]}`, "responses", 2.02, false},
		{"actual native Claude Batch image", `{"model":"test/model","max_tokens":10,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://example.test/image.png"}}]}]}`, "batch_messages", 2.02, false},
		{"actual chat audio", `{"model":"test/model","max_tokens":10,"messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"a","format":"wav"}}]}]}`, "chat/completions", 0, true},
		{"actual Responses video", `{"model":"test/model","max_output_tokens":10,"input":[{"role":"user","content":[{"type":"video_url","video_url":{"url":"https://example.test/v.mp4"}}]}]}`, "responses", 0, true},
		{"actual Batch audio", `{"model":"test/model","max_tokens":10,"messages":[{"role":"user","content":[{"type":"audio_url","audio_url":{"url":"https://example.test/a.wav"}}]}]}`, "batch_chat/completions", 0, true},
		{"actual hosted search", `{"model":"test/model","max_tokens":10,"web_search_options":{}}`, "chat/completions", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cost, err := estimator.Estimate(info, []byte(tc.body), tc.operation)
			if tc.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.InDelta(t, tc.want, cost, 1e-9)
			}
		})
	}
}

func TestEndpointOneHourCachePublishedSyncAndBatchPricing(t *testing.T) {
	for _, scenario := range []struct {
		name, model, operation, pricing string
		want                            float64
	}{
		{"sync", "anthropic/claude-fable-5.1", "chat/completions", `{"prompt":"0.00001","completion":"0.00005","input_cache_read":"0.00000025","input_cache_write":"0.0000125","input_cache_write_1h":"0.00002","web_search":"0.01","discount":0}`, .0205},
		{"batch_base_model", "anthropic/claude-fable-5.1", "batch_chat/completions", `{"prompt":"0.000005","completion":"0.000025","input_cache_read":"0.000000125","input_cache_write":"0.00000625","input_cache_write_1h":"0.00001","web_search":"0.01","discount":0}`, .01025},
		{"batch_model_alias", "anthropic/claude-fable-5.1:batch", "batch_messages", `{"prompt":"0.000005","completion":"0.000025","input_cache_read":"0.000000125","input_cache_write":"0.00000625","input_cache_write_1h":"0.00001","web_search":"0.01","discount":0}`, .01025},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var prices map[string]json.RawMessage
			require.NoError(t, common.Unmarshal([]byte(scenario.pricing), &prices))
			estimator := NewRequestEstimator()
			estimator.Models[scenario.model] = []PricedEndpoint{{Tag: "anthropic", MaxCompletionTokens: 100, Pricing: prices}}
			info := &relaycommon.RelayInfo{}
			info.SetEstimatePromptTokens(1000)
			for _, cache := range []string{"", `,"cache_control":{"type":"ephemeral"}`, `,"cache_control":{"type":"ephemeral","ttl":"1h"}`, `,"messages":[{"role":"user","content":[{"type":"text","text":"hour prefix","cache_control":{"type":"ephemeral","ttl":"1h"}},{"type":"text","text":"short prefix","cache_control":{"type":"ephemeral","ttl":"5m"}}]}]`} {
				body := []byte(`{"model":"` + scenario.model + `","max_tokens":10` + cache + `}`)
				cost, err := estimator.Estimate(info, body, scenario.operation)
				require.NoError(t, err)
				assert.InDelta(t, scenario.want, cost, 1e-12)
			}
		})
	}
}

func TestEndpointOneHourCacheOverrideAndWholeEndpointBound(t *testing.T) {
	estimator := NewRequestEstimator()
	estimator.Models["test/model"] = []PricedEndpoint{
		{Tag: "provider-a", MaxCompletionTokens: 10, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.1"`), "input_cache_write": json.RawMessage(`"0.002"`), "input_cache_write_1h": json.RawMessage(`"0.004"`), "overrides": json.RawMessage(`[{"min_prompt_tokens":1000,"input_cache_write_1h":"0.006","completion":"0.15"}]`)}},
		{Tag: "provider-b", MaxCompletionTokens: 10, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.002"`), "completion": json.RawMessage(`"0.2"`), "input_cache_write_1h": json.RawMessage(`"0.003"`)}},
	}
	info := &relaycommon.RelayInfo{}
	for _, scenario := range []struct {
		prompt int
		want   float64
	}{{999, 4.997}, {1000, 7.5}} {
		info.SetEstimatePromptTokens(scenario.prompt)
		cost, err := estimator.Estimate(info, []byte(`{"model":"test/model","max_tokens":10}`), "chat/completions")
		require.NoError(t, err)
		assert.InDelta(t, scenario.want, cost, 1e-12)
	}
}

func TestEndpointOneHourCacheStrictRateAndUnknownDimensions(t *testing.T) {
	for _, scenario := range []struct {
		name, key, rate string
		want            float64
		fail            bool
	}{
		{"optional_absent", "", "", 1.27, false},
		{"explicit_zero", "input_cache_write_1h", `"0"`, 1.27, false},
		{"invalid", "input_cache_write_1h", `"invalid"`, 0, true},
		{"negative", "input_cache_write_1h", `"-0.1"`, 0, true},
		{"null", "input_cache_write_1h", `null`, 0, true},
		{"nan", "input_cache_write_1h", `"NaN"`, 0, true},
		{"infinity", "input_cache_write_1h", `"Infinity"`, 0, true},
		{"unknown_nonzero", "input_cache_write_2h", `"0.003"`, 0, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			prices := map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.002"`), "input_cache_write": json.RawMessage(`"0.00125"`)}
			if scenario.key != "" {
				prices[scenario.key] = json.RawMessage(scenario.rate)
			}
			estimator := NewRequestEstimator()
			estimator.Models["test/model"] = []PricedEndpoint{{Tag: "provider", MaxCompletionTokens: 10, Pricing: prices}}
			info := &relaycommon.RelayInfo{}
			info.SetEstimatePromptTokens(1000)
			cost, err := estimator.Estimate(info, []byte(`{"model":"test/model","max_tokens":10}`), "batch_chat/completions")
			if scenario.fail {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, scenario.want, cost, 1e-12)
		})
	}
}
