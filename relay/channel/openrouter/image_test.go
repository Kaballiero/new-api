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
