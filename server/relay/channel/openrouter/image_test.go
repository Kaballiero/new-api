package openrouter

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/i18n"
	tasksrecord "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	tasksapp "github.com/QuantumNous/new-api/internal/app/tasks"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	_ "github.com/QuantumNous/new-api/relay/billing" // This adaptor test binary does not link the root relay package.
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/taskrecord"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
)

// Initialize translations for this test binary, which does not run main.
func init() {
	if err := i18n.Init(); err != nil {
		panic(err)
	}
}

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
		endpoint.SupportedParameters["input_references"] = ImageCapability{Type: "range", Min: 0, Max: 2}
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
		sourceBased bool
		unlisted    bool
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
		{name: "listed durations stay strict", model: "runway/gen-4.5", prices: map[string]string{"cents_per_second_output": "12"}, order: VideoPriceOrder{Duration: common.GetPointer(4)}, errorText: "duration absent from fresh capabilities"},
		{name: "unlisted durations reserve requested seconds", model: "catalog/new-unlisted-model", prices: map[string]string{"cents_per_second_output": "28"}, order: VideoPriceOrder{Duration: common.GetPointer(20)}, unlisted: true, want: 5.6},
		{name: "unlisted durations keep the task duration cap", model: "catalog/new-unlisted-model", prices: map[string]string{"cents_per_second_output": "28"}, order: VideoPriceOrder{Duration: common.GetPointer(relaycommon.MaxTaskDurationSeconds + 1)}, unlisted: true, errorText: "bounded output duration is unavailable"},
		{name: "unlisted omitted duration has no bound", model: "catalog/new-unlisted-model", prices: map[string]string{"cents_per_second_output": "28"}, unlisted: true, errorText: "bounded output duration is unavailable"},
		{name: "edit omitted unlisted duration keeps 15 second maximum", model: "black-forest-labs/flux-video-edit", prices: map[string]string{"cents_per_second_output": "12"}, unlisted: true, want: 1.8},
		{name: "edit shorter request keeps 15 second maximum", model: "black-forest-labs/flux-video-edit", prices: map[string]string{"cents_per_second_output": "12"}, order: VideoPriceOrder{Duration: common.GetPointer(5)}, unlisted: true, want: 1.8},
		{name: "edit longer request reserves requested seconds", model: "black-forest-labs/flux-video-edit", prices: map[string]string{"cents_per_second_output": "12"}, order: VideoPriceOrder{Duration: common.GetPointer(20)}, unlisted: true, want: 2.4},
		{name: "unknown SKU rejected", model: "runway/gen-4.5", prices: map[string]string{"cents_per_second_output": "12", "unexplained": "9"}, errorText: "unknown video pricing SKU"},
		{name: "avatar quantity unavailable", model: "heygen/avatar-iv", prices: map[string]string{"duration_seconds": "0.1"}, sourceBased: true, errorText: "source media quantity"},
		{name: "aleph quantity unavailable despite floor", model: "runway/aleph-2", prices: map[string]string{"cents_per_second_output": "10", "minimum_cents_per_generation": "50"}, sourceBased: true, errorText: "source media quantity"},
		{name: "upscale quantity unavailable", model: "black-forest-labs/flux-video-upscale", prices: map[string]string{"cents_per_megapixel_second_precise": "1"}, sourceBased: true, errorText: "source media quantity"},
		{name: "source video tokens unavailable", model: "bytedance/seedance-2.0", prices: map[string]string{"video_tokens": "0.1"}, order: VideoPriceOrder{InputReferences: []struct {
			Type string `json:"type"`
		}{{Type: "video_url"}}}, sizes: []string{"1280x720"}, errorText: "source video token quantity"},
		{name: "invalid cents", model: "runway/gen-4.5", prices: map[string]string{"cents_per_second_output": "-12"}, errorText: "invalid video tariff"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.order.Model = tc.model
			model := VideoModelPricing{ID: tc.model, PricingSKUs: tc.prices, SupportedDurations: []int{3, 5}, SupportedSizes: tc.sizes, SupportedResolutions: []string{"480p", "720p"}}
			if tc.sourceBased || tc.unlisted {
				// These discovery records have no finite output durations. Do not
				// fabricate quantities to exercise their monetary SKU.
				model.SupportedDurations = nil
			}
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

func TestHeyGenVideoReservation(t *testing.T) {
	for _, tc := range []struct {
		name, order      string
		images           int
		tariffKey, value string
		want             float64
		errorText        string
	}{
		{name: "text without references", order: `{"duration":5,"resolution":"480p"}`, want: 0.10},
		{name: "first frame uses ordinary output", order: `{"duration":5,"resolution":"480p","frame_images":[{"type":"image_url"}]}`, want: 0.10},
		{name: "one free reference", order: `{"duration":5,"resolution":"480p"}`, images: 1, want: 0.35},
		{name: "two free references", order: `{"duration":5,"resolution":"480p"}`, images: 2, want: 0.35},
		{name: "third reference is additive", order: `{"duration":5,"resolution":"480p"}`, images: 3, want: 0.39},
		{name: "maximum image references", order: `{"duration":5,"resolution":"480p"}`, images: 9, want: 0.63},
		{name: "reference tariff counts output seconds", order: `{"duration":5,"resolution":"480p","input_references":[{"type":"video_url"}]}`, want: 0.35},
		{name: "uppercase capability matches SKU", order: `{"duration":5,"resolution":"2K"}`, want: 0.50},
		{name: "uppercase reference resolution", order: `{"duration":5,"resolution":"2K"}`, images: 3, want: 0.94},
		{name: "omitted duration and resolution reserve maximum", order: `{}`, want: 1.50},
		{name: "omitted reference defaults reserve maximum plus inputs", order: `{}`, images: 3, want: 2.74},
		{name: "explicit zero image tariff", order: `{"duration":5,"resolution":"480p"}`, images: 3, tariffKey: "reference_input_images", value: "0", want: 0.35},
		{name: "missing image tariff with paid references", order: `{}`, images: 3, tariffKey: "reference_input_images", errorText: "missing reference image tariff"},
		{name: "missing image tariff with free references", order: `{"duration":5,"resolution":"480p"}`, images: 2, tariffKey: "reference_input_images", want: 0.35},
		{name: "negative image tariff", order: `{}`, tariffKey: "reference_input_images", value: "-0.04", errorText: "invalid video tariff"},
		{name: "invalid image tariff", order: `{}`, tariffKey: "reference_input_images", value: "NaN", errorText: "invalid video tariff"},
		{name: "unknown monetary SKU", order: `{}`, tariffKey: "unknown_charge", value: "1", errorText: "unknown video pricing SKU"},
		{name: "too many references", order: `{}`, images: 10, errorText: "unsupported HeyGen reference quantities"},
		{name: "mixed frame and references", order: `{"frame_images":[{"type":"image_url"}]}`, images: 1, errorText: "unsupported HeyGen reference quantities"},
		{name: "audio remains unpriced", order: `{"input_references":[{"type":"image_url"},{"type":"audio_url"}]}`, errorText: "audio reference tariff is unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := VideoModelPricing{
				ID: "heygen/heygen-video-1", SupportedDurations: []int{5, 15}, SupportedResolutions: []string{"480p", "768p", "2K"},
				PricingSKUs: map[string]string{
					"duration_seconds_480p": "0.02", "duration_seconds_768p": "0.036", "duration_seconds_2k": "0.1",
					"reference_duration_seconds_480p": "0.07", "reference_duration_seconds_768p": "0.1", "reference_duration_seconds_2k": "0.18",
					"reference_input_images": "0.04",
				},
			}
			if tc.tariffKey != "" {
				if tc.value == "" {
					delete(model.PricingSKUs, tc.tariffKey)
				} else {
					model.PricingSKUs[tc.tariffKey] = tc.value
				}
			}
			var order VideoPriceOrder
			require.NoError(t, common.Unmarshal([]byte(tc.order), &order))
			order.Model = model.ID
			if tc.images > 0 {
				references := "[" + strings.TrimSuffix(strings.Repeat(`{"type":"image_url"},`, tc.images), ",") + "]"
				require.NoError(t, common.Unmarshal([]byte(references), &order.InputReferences))
			}
			cost, err := EstimateVideoOrder(model, order)
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

func TestVideoQuantityContractsWithoutModelFamilies(t *testing.T) {
	for _, tc := range []struct {
		name, model, prices, order string
		want                       float64
		errorText                  string
	}{
		{"USD seconds", "catalog/new-seconds-model", `{"duration_seconds_480p":"0.02"}`, `{"resolution":"480p"}`, 0.1, ""},
		{"cents seconds", "catalog/new-cents-model", `{"cents_per_second_output":"28"}`, `{}`, 1.4, ""},
		{"minimum is a floor", "catalog/new-cents-model", `{"cents_per_second_output":"10","minimum_cents_per_generation":"56"}`, `{}`, 0.56, ""},
		{"higher output exceeds minimum", "catalog/new-cents-model", `{"cents_per_second_output":"28","minimum_cents_per_generation":"56"}`, `{}`, 1.4, ""},
		{"zero remains zero", "catalog/new-zero-model", `{"duration_seconds":"0","minimum_cents_per_generation":"0"}`, `{}`, 0, ""},
		{"unknown image allowance is not guessed", "catalog/new-reference-model", `{"duration_seconds":"0.1","reference_input_images":"0.04"}`, `{}`, 0, "unknown video pricing SKU"},
		{"unknown SKU remains closed", "catalog/new-seconds-model", `{"duration_seconds":"0.1","extra_charge":"1"}`, `{}`, 0, "unknown video pricing SKU"},
		{"different output units cannot be max alternatives", "bytedance/seedance-2.0", `{"duration_seconds":"0.1","video_tokens":"0.001"}`, `{}`, 0, "incompatible video output quantity units"},
		{"token price does not define a pixel meter", "catalog/new-token-model", `{"video_tokens":"0.001"}`, `{}`, 0, "video token quantity contract is unavailable"},
		{"paid output format is not free", "runway/gen-4.5", `{"cents_per_second_output":"28"}`, `{"provider":{"options":{"runway":{"output_format":"prores"}}}}`, 0, "unpriced output format"},
		{"ordinary mp4 output", "runway/gen-4.5", `{"cents_per_second_output":"28"}`, `{"provider":{"options":{"runway":{"output_format":"mp4"}}}}`, 1.4, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := VideoModelPricing{ID: tc.model, SupportedDurations: []int{3, 5}, SupportedResolutions: []string{"480p"}}
			require.NoError(t, common.Unmarshal([]byte(tc.prices), &model.PricingSKUs))
			var order VideoPriceOrder
			require.NoError(t, common.Unmarshal([]byte(tc.order), &order))
			order.Model = model.ID
			cost, err := EstimateVideoOrder(model, order)
			if tc.errorText != "" {
				require.ErrorContains(t, err, tc.errorText)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.want, cost, 1e-12)
		})
	}
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

func TestSharedImageChannelReserve(t *testing.T) {
	for _, source := range []dto.OpenRouterReservationSource{"", dto.OpenRouterReservationUpstream} {
		for _, tc := range []struct {
			name, chatPricing, nativePricing string
			want                             float64
		}{
			{"minimum", `{"prompt":"0","completion":"0.04","image_output":"0"}`, `[{"billable":"output_image","unit":"image","cost_usd":0.04}]`, .25},
			{"larger known estimate", `{"prompt":"0","completion":"0.7","image_output":"0"}`, `[{"billable":"output_image","unit":"image","cost_usd":0.7}]`, .7},
			{"unknown monetary estimate", `[]`, `[]`, .25},
		} {
			t.Run(string(source)+"/"+tc.name, func(t *testing.T) {
				posts := 0
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodGet {
						posts++
						w.WriteHeader(http.StatusMethodNotAllowed)
						return
					}
					switch r.URL.Path {
					case "/v1/models/test/image/endpoints":
						_, _ = fmt.Fprintf(w, `{"data":{"id":"test/image","architecture":{"output_modalities":["text","image"]},"endpoints":[{"tag":"provider","context_length":100000,"max_completion_tokens":1,"pricing":%s}]}}`, tc.chatPricing)
					case "/v1/images/models/test/image/endpoints":
						_, _ = fmt.Fprintf(w, `{"id":"test/image","endpoints":[{"provider_tag":"provider","supported_parameters":{"n":{"type":"range","min":1,"max":1},"resolution":{"type":"enum","values":["1K"]}},"pricing":%s}]}`, tc.nativePricing)
					default:
						w.WriteHeader(http.StatusNotFound)
					}
				}))
				defer server.Close()
				info := &relaycommon.RelayInfo{
					OriginModelName: "shared-image",
					RelayFormat:     types.RelayFormatOpenAI,
					ChannelMeta: &relaycommon.ChannelMeta{
						ChannelId: 42, ChannelType: constant.ChannelTypeOpenRouter,
						ChannelBaseUrl: server.URL, UpstreamModelName: "test/image",
					},
				}
				info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{"test/image": .25}
				if source != "" {
					info.ChannelSetting.OpenRouterReservationSource = map[string]dto.OpenRouterReservationSource{"test/image": source}
				}
				ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
				ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
				estimator := NewRequestEstimator()
				require.NoError(t, estimator.Discover(ctx, info, &imageTestAdaptor{}, "test/image"))
				cost, err := estimator.Estimate(info, []byte(`{"model":"test/image","max_tokens":1,"n":1,"modalities":["text","image"],"provider":{"only":["provider"],"allow_fallbacks":false}}`), "chat/completions")
				require.NoError(t, err)
				assert.InDelta(t, tc.want, cost, 1e-12)

				ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
				native := &ImageAdaptor{Adaptor: &imageTestAdaptor{}}
				var request dto.ImageRequest
				require.NoError(t, common.Unmarshal([]byte(`{"model":"test/image","prompt":"x","n":1,"size":"1K","provider":{"only":["provider"],"allow_fallbacks":false}}`), &request))
				require.NoError(t, native.Prepare(ctx, info, request))
				assert.InDelta(t, tc.want, info.NativeImage.EstimatedCostUSD, 1e-12)
				assert.Equal(t, "provider", info.NativeImage.ProviderTag)
				assert.Equal(t, "shared-image", info.OriginModelName)
				assert.Zero(t, posts, "admission must not submit an inference request")
			})
		}
	}
}

func TestChatImageReserveGuards(t *testing.T) {
	for _, tc := range []struct {
		name, extra, operation string
		wantError              string
		reserve                float64
		noReserve, legacyOnly  bool
		modalities             []string
		noEndpoints, nonOR     bool
		maxCompletion          int
	}{
		{name: "missing reserve", noReserve: true},
		{name: "zero reserve", reserve: 0},
		{name: "negative reserve", reserve: -1},
		{name: "NaN reserve", reserve: math.NaN()},
		{name: "infinite reserve", reserve: math.Inf(1)},
		{name: "legacy Sonar is not an exact image reserve", reserve: .25, legacyOnly: true},
		{name: "missing image capability", reserve: .25},
		{name: "text-only capability", reserve: .25, modalities: []string{"text"}},
		{name: "no endpoints", reserve: .25, modalities: []string{"image"}, noEndpoints: true},
		{name: "non OpenRouter", reserve: .25, modalities: []string{"image"}, nonOR: true},
		{name: "wrong operation", reserve: .25, modalities: []string{"image"}, operation: "embeddings"},
		{name: "zero output quantity", reserve: .25, modalities: []string{"image"}, extra: `,"n":0`},
		{name: "multiple responses", reserve: .25, modalities: []string{"image"}, extra: `,"n":2`},
		{name: "streaming", reserve: .25, modalities: []string{"image"}, extra: `,"stream":true`},
		{name: "audio object", reserve: .25, modalities: []string{"image"}, extra: `,"audio":{"voice":"alloy","format":"wav"}`, wantError: "audio output remains unsupported"},
		{name: "audio string", reserve: .25, modalities: []string{"image"}, extra: `,"audio":"malformed"`, wantError: "audio output remains unsupported"},
		{name: "audio number", reserve: .25, modalities: []string{"image"}, extra: `,"audio":123`, wantError: "audio output remains unsupported"},
		{name: "tools", reserve: .25, modalities: []string{"image"}, extra: `,"tools":[{"type":"function"}]`},
		{name: "fallback models", reserve: .25, modalities: []string{"image"}, extra: `,"models":["test/other"]`},
		{name: "input image", reserve: .25, modalities: []string{"image"}, extra: `,"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/x.png"}}]}]`},
		{name: "provider excludes all", reserve: .25, modalities: []string{"image"}, extra: `,"provider":{"only":["missing"]}`},
		{name: "malformed max price", reserve: .25, modalities: []string{"image"}, extra: `,"provider":{"max_price":{"prompt":-1}}`},
		{name: "output cap exceeds endpoint", reserve: .25, modalities: []string{"image"}, maxCompletion: 1, extra: `,"max_tokens":2`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}}
			if tc.nonOR {
				info.ChannelType = constant.ChannelTypeOpenAI
			}
			if !tc.noReserve {
				info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{"test/image": tc.reserve}
			}
			if tc.legacyOnly {
				info.ChannelSetting.OpenRouterModelReserveUSD = nil
				info.ChannelSetting.OpenRouterSonarResearchReserveUSD = common.GetPointer(tc.reserve)
			}
			estimator := NewRequestEstimator()
			estimator.Models["test/image"] = []PricedEndpoint{{Tag: "provider", ContextLength: 100000, MaxCompletionTokens: tc.maxCompletion, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0"`), "completion": json.RawMessage(`"0"`), "image_output": json.RawMessage(`"1"`)}}}
			if tc.noEndpoints {
				estimator.Models["test/image"] = nil
			}
			estimator.OutputModalities["test/image"] = tc.modalities
			operation := tc.operation
			if operation == "" {
				operation = "chat/completions"
			}
			_, err := estimator.Estimate(info, []byte(`{"model":"test/image","modalities":["image"]`+tc.extra+`}`), operation)
			require.Error(t, err)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
			}
		})
	}

	// Explicit configured-price admission remains valid without a channel minimum.
	info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}}
	estimator := NewRequestEstimator()
	estimator.Models["test/image"] = []PricedEndpoint{{Tag: "provider", ContextLength: 100000, MaxCompletionTokens: 1}}
	estimator.OutputModalities["test/image"] = []string{"image"}
	cost, err := estimator.EstimateWithLocalPrice(info, []byte(`{"model":"test/image","modalities":["image"]}`), "chat/completions", .4)
	require.NoError(t, err)
	assert.InDelta(t, .4, cost, 1e-12)
}

func TestChatImageReserveDoesNotCoverDiscoveryFailure(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		status         int
	}{
		{"authentication", `{}`, http.StatusUnauthorized},
		{"no endpoints", `{"data":{"id":"test/image","architecture":{"output_modalities":["image"]},"endpoints":[]}}`, http.StatusOK},
		{"wrong model", `{"data":{"id":"test/other","architecture":{"output_modalities":["image"]},"endpoints":[{"tag":"provider"}]}}`, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					posts++
				}
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.response)
			}))
			defer server.Close()
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, ChannelMeta: &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter, ChannelBaseUrl: server.URL}}
			info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{"test/image": .25}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			_, err := PrepareRequest(ctx, info, &imageTestAdaptor{}, strings.NewReader(`{"model":"test/image","modalities":["image"],"max_tokens":1}`), "chat/completions")
			require.Error(t, err)
			assert.Nil(t, info.OpenRouterCost, "discovery errors must fail before any reserve")
			assert.Zero(t, posts)
		})
	}
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

func TestEndpointChannelReserveHostedSearchAndChatAudio(t *testing.T) {
	const search = `,"max_tokens":10,"web_search_options":{}`
	const audio = `,"max_tokens":10,"modalities":["text","audio"],"audio":{"voice":"alloy","format":"wav"},"stream":true`
	for _, tc := range []struct {
		name, extra, operation, searchRate, wantError string
		reserve, local                                *float64
		nonstreamInfo                                 bool
		want                                          float64
	}{
		{name: "search without reserve stays closed", extra: search, wantError: "hosted-search quantity is unpriced"},
		{name: "search configured price alone stays closed", extra: search, local: common.GetPointer(5.0), wantError: "hosted-search quantity is unpriced"},
		{name: "search zero reserve with positive rate", extra: search, reserve: common.GetPointer(0.0), wantError: "hosted-search quantity is unpriced"},
		{name: "search zero reserve with configured price", extra: search, reserve: common.GetPointer(0.0), local: common.GetPointer(5.0), wantError: "hosted-search quantity is unpriced"},
		{name: "search reserve adds to smaller token estimate", extra: search, reserve: common.GetPointer(0.5), want: 1.52},
		{name: "search reserve adds to larger token estimate", extra: search, reserve: common.GetPointer(5.0), want: 6.02},
		{name: "search reserve without published search rate", extra: search, reserve: common.GetPointer(5.0), searchRate: "absent", want: 6.02},
		{name: "search reserve adds to token estimate under configured price", extra: search + `,"modalities":["text"]`, reserve: common.GetPointer(0.5), local: common.GetPointer(0.25), want: 1.52},
		{name: "configured price above search sum", extra: search + `,"modalities":["text"]`, reserve: common.GetPointer(0.5), local: common.GetPointer(3.0), want: 3},
		{name: "search reserve invalid search rate", extra: search, reserve: common.GetPointer(5.0), searchRate: `"-1"`, wantError: "invalid OpenRouter endpoint price"},
		{name: "search reserve Responses stays closed", extra: `,"max_output_tokens":10,"web_search_options":{}`, operation: "responses", reserve: common.GetPointer(5.0), wantError: "hosted-search quantity is unpriced"},
		{name: "search reserve Batch stays closed", extra: search, operation: "batch_chat/completions", reserve: common.GetPointer(5.0), wantError: "hosted search is unavailable for Batch"},
		{name: "search reserve adds to streamed token estimate", extra: search + `,"stream":true`, reserve: common.GetPointer(5.0), want: 6.02},
		{name: "search reserve hosted tool stays closed", extra: search + `,"tools":[{"type":"openrouter:web_search"}]`, reserve: common.GetPointer(5.0), wantError: "hosted tool pricing is unavailable"},
		{name: "search reserve plugin stays closed", extra: `,"max_tokens":10,"plugins":[{"id":"web"}]`, reserve: common.GetPointer(5.0), wantError: "unpriced execution dimensions"},
		{name: "search with streamed audio output stays closed", extra: audio + `,"web_search_options":{}`, reserve: common.GetPointer(5.0), wantError: "cannot share an approximate reserve"},
		{name: "search with image output reserve stays closed", extra: `,"max_tokens":10,"modalities":["text","image"],"web_search_options":{}`, reserve: common.GetPointer(5.0), wantError: "cannot share an approximate reserve"},
		{name: "audio channel reserve", extra: audio, reserve: common.GetPointer(0.3), want: 0.3},
		{name: "audio channel reserve requires stream body", extra: `,"max_tokens":10,"modalities":["text","audio"]`, reserve: common.GetPointer(0.3), nonstreamInfo: true, wantError: "requires streaming"},
		{name: "audio channel reserve requires stream relay", extra: audio, reserve: common.GetPointer(0.3), nonstreamInfo: true, wantError: "requires streaming"},
		{name: "audio without reserve or configured price", extra: audio, wantError: "requires configured_price or an explicit approximate channel reserve"},
		{name: "audio zero channel reserve", extra: audio, reserve: common.GetPointer(0.0), wantError: "invalid OpenRouter approximate reserve"},
		{name: "audio channel reserve Responses stays closed", extra: audio, operation: "responses", reserve: common.GetPointer(0.3), wantError: "requires configured_price or an explicit approximate channel reserve"},
		{name: "audio configured price unchanged", extra: audio, local: common.GetPointer(0.25), want: 0.25},
		{name: "audio configured price below channel reserve", extra: audio, local: common.GetPointer(0.25), reserve: common.GetPointer(0.3), want: 0.3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pricing := map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.002"`), "web_search": json.RawMessage(`"0.01"`)}
			if strings.Contains(tc.extra, `"audio"]`) {
				pricing["output_audio"] = json.RawMessage(`"0.00004"`)
			}
			if tc.searchRate == "absent" {
				delete(pricing, "web_search")
			} else if tc.searchRate != "" {
				pricing["web_search"] = json.RawMessage(tc.searchRate)
			}
			estimator := NewRequestEstimator()
			estimator.Models["test/model"] = []PricedEndpoint{{Tag: "provider", ModelID: "test/model", ContextLength: 10000, MaxCompletionTokens: 100, Pricing: pricing}}
			estimator.OutputModalities["test/model"] = []string{"text", "audio"}
			operation := tc.operation
			if operation == "" {
				operation = "chat/completions"
			}
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, IsStream: strings.Contains(tc.extra, `"stream":true`) && !tc.nonstreamInfo}
			info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}
			if tc.reserve != nil {
				info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{"test/model": *tc.reserve}
			}
			info.SetEstimatePromptTokens(1000)
			body := []byte(`{"model":"test/model"` + tc.extra + `}`)
			var cost float64
			var err error
			if tc.local != nil {
				cost, err = estimator.EstimateWithLocalPrice(info, body, operation, *tc.local)
			} else {
				cost, err = estimator.Estimate(info, body, operation)
			}
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.want, cost, 1e-9)
		})
	}
}

func TestEndpointChannelReserveIsAddOnForPricedTokens(t *testing.T) {
	const plain = `,"max_tokens":10`
	const search = `,"max_tokens":10,"web_search_options":{}`
	for _, tc := range []struct {
		name, extra, operation, wantError string
		unpricedReasoning, nativeSearch   bool
		reserve                           *float64
		want                              float64
		wantUse                           ReserveUse
	}{
		{name: "nonstream hold is token estimate", extra: plain, reserve: common.GetPointer(5.0), want: 1.02},
		{name: "native search adds reserve once", extra: plain + `,"stream":true`, nativeSearch: true, reserve: common.GetPointer(5.0), want: 6.02, wantUse: ReserveAddOn},
		{name: "native and requested search add reserve once", extra: search, nativeSearch: true, reserve: common.GetPointer(5.0), want: 6.02, wantUse: ReserveAddOn},
		{name: "native search without reserve unchanged", extra: plain, nativeSearch: true, want: 1.02},
		{name: "stream", extra: plain + `,"stream":true`, reserve: common.GetPointer(5.0), want: 1.02},
		{name: "function tools", extra: plain + `,"tools":[{"type":"function","function":{"name":"f"}}]`, reserve: common.GetPointer(5.0), want: 1.02},
		{name: "provider routing", extra: plain + `,"provider":{"only":["provider"],"order":["provider"]}`, reserve: common.GetPointer(5.0), want: 1.02},
		{name: "fallback models take the most expensive", extra: plain + `,"models":["test/expensive"]`, reserve: common.GetPointer(5.0), want: 2.04},
		{name: "multiple outputs", extra: plain + `,"n":2`, reserve: common.GetPointer(5.0), want: 1.04},
		{name: "image input", extra: plain + `,"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/i.png"}}]}]`, reserve: common.GetPointer(5.0), want: 1.52},
		{name: "batch", extra: plain, operation: "batch_chat/completions", reserve: common.GetPointer(5.0), want: 1.02},
		{name: "search adds reserve to streamed tool request", extra: search + `,"stream":true,"tools":[{"type":"function","function":{"name":"f"}}]`, reserve: common.GetPointer(0.5), want: 1.52, wantUse: ReserveAddOn},
		{name: "search adds reserve with provider routing and n", extra: search + `,"n":2,"provider":{"only":["provider"]}`, reserve: common.GetPointer(0.5), want: 1.54, wantUse: ReserveAddOn},
		{name: "search without reserve stays closed", extra: search + `,"stream":true`, wantError: "hosted-search quantity is unpriced"},
		{name: "unpriced dimension keeps whole reserve", extra: plain, unpricedReasoning: true, reserve: common.GetPointer(5.0), want: 5, wantUse: ReserveWholeHold},
		{name: "unpriced dimension search keeps whole reserve", extra: search, unpricedReasoning: true, reserve: common.GetPointer(5.0), want: 6.02, wantUse: ReserveWholeHold},
		{name: "unpriced dimension search stream stays closed", extra: search + `,"stream":true`, unpricedReasoning: true, reserve: common.GetPointer(5.0), wantError: "one nonstream text chat response"},
		{name: "unpriced dimension search n stays closed", extra: search + `,"n":2`, unpricedReasoning: true, reserve: common.GetPointer(5.0), wantError: "one nonstream text chat response"},
		{name: "unpriced dimension search models stays closed", extra: search + `,"models":["test/expensive"]`, unpricedReasoning: true, reserve: common.GetPointer(5.0), wantError: "endpoint model does not match final model ID"},
		{name: "search reserve stays bound to the reserved model", extra: search + `,"models":["test/expensive"]`, reserve: common.GetPointer(5.0), wantError: "endpoint model does not match final model ID"},
		{name: "unpriced dimension search same model list stays closed", extra: search + `,"models":["test/model"]`, unpricedReasoning: true, reserve: common.GetPointer(5.0), wantError: "one nonstream text chat response"},
		{name: "unpriced dimension stream stays closed", extra: plain + `,"stream":true`, unpricedReasoning: true, reserve: common.GetPointer(5.0), wantError: "one nonstream text chat response"},
		{name: "unpriced dimension tools stay closed", extra: plain + `,"tools":[{"type":"function","function":{"name":"f"}}]`, unpricedReasoning: true, reserve: common.GetPointer(5.0), wantError: "one nonstream text chat response"},
		{name: "unpriced dimension batch stays closed", extra: plain, operation: "batch_chat/completions", unpricedReasoning: true, reserve: common.GetPointer(5.0), wantError: "one nonstream text chat response"},
		{name: "unpriced dimension without reserve stays closed", extra: plain, unpricedReasoning: true, wantError: "unsupported billable dimension internal_reasoning"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pricing := map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.002"`), "image": json.RawMessage(`"0.5"`), "web_search": json.RawMessage(`"0"`)}
			if tc.unpricedReasoning {
				pricing["internal_reasoning"] = json.RawMessage(`"0.003"`)
			}
			if tc.nativeSearch {
				pricing["web_search"] = json.RawMessage(`"0.01"`)
			}
			estimator := NewRequestEstimator()
			estimator.Models["test/model"] = []PricedEndpoint{{Tag: "provider", ModelID: "test/model", ContextLength: 10000, MaxCompletionTokens: 100, Pricing: pricing}}
			estimator.Models["test/expensive"] = []PricedEndpoint{{Tag: "other", ModelID: "test/expensive", ContextLength: 10000, MaxCompletionTokens: 100, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.002"`), "completion": json.RawMessage(`"0.004"`)}}}
			estimator.OutputModalities["test/model"] = []string{"text"}
			operation := tc.operation
			if operation == "" {
				operation = "chat/completions"
			}
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, IsStream: strings.Contains(tc.extra, `"stream":true`)}
			info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}
			if tc.reserve != nil {
				info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{"test/model": *tc.reserve}
			}
			info.SetEstimatePromptTokens(1000)
			cost, err := estimator.Estimate(info, []byte(`{"model":"test/model"`+tc.extra+`}`), operation)
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.want, cost, 1e-9)
			assert.Equal(t, tc.wantUse, estimator.ReserveUse)
		})
	}
}

func TestEndpointChannelReserveInactiveAudioInputRates(t *testing.T) {
	const model = "google/gemini-2.5-flash-lite"
	const search = `,"max_tokens":10,"web_search_options":{}`
	const audioInput = `,"messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"AAAA","format":"wav"}}]}]`
	for _, tc := range []struct {
		name, extra, audioCacheRate, wantError string
		reserve                                *float64
		want                                   float64
	}{
		{name: "text search reserve admits published audio input rates", extra: search, reserve: common.GetPointer(5.0), want: 6.02},
		{name: "text reserve without search adds native search reserve", extra: `,"max_tokens":10`, reserve: common.GetPointer(0.5), want: 1.52},
		{name: "audio input content under search reserve stays closed", extra: search + audioInput, reserve: common.GetPointer(5.0), wantError: "input media duration quantity is unpriced"},
		{name: "invalid inactive audio input rate under reserve", extra: search, reserve: common.GetPointer(5.0), audioCacheRate: `"-1"`, wantError: "invalid OpenRouter endpoint price"},
		{name: "streamed audio output reserve unchanged", extra: `,"max_tokens":10,"modalities":["text","audio"],"audio":{"voice":"alloy","format":"wav"},"stream":true`, reserve: common.GetPointer(0.3), want: 0.3},
		{name: "text without reserve unchanged", extra: `,"max_tokens":10`, want: 1.02},
		{name: "search without reserve stays closed", extra: search, wantError: "hosted-search quantity is unpriced"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pricing := map[string]json.RawMessage{
				"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.002"`), "internal_reasoning": json.RawMessage(`"0.002"`),
				"web_search": json.RawMessage(`"0.01"`), "audio": json.RawMessage(`"0.003"`), "input_audio_cache": json.RawMessage(`"0.0005"`),
				"input_cache_read": json.RawMessage(`"0.0001"`),
			}
			if tc.audioCacheRate != "" {
				pricing["input_audio_cache"] = json.RawMessage(tc.audioCacheRate)
			}
			if strings.Contains(tc.extra, `"audio"]`) {
				pricing["output_audio"] = json.RawMessage(`"0.00004"`)
			}
			estimator := NewRequestEstimator()
			estimator.Models[model] = []PricedEndpoint{{Tag: "google-vertex", ModelID: model, ContextLength: 10000, MaxCompletionTokens: 100, Pricing: pricing}}
			estimator.OutputModalities[model] = []string{"text", "audio"}
			info := &relaycommon.RelayInfo{RelayFormat: types.RelayFormatOpenAI, IsStream: strings.Contains(tc.extra, `"stream":true`)}
			info.ChannelMeta = &relaycommon.ChannelMeta{ChannelType: constant.ChannelTypeOpenRouter}
			if tc.reserve != nil {
				info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{model: *tc.reserve}
			}
			info.SetEstimatePromptTokens(1000)
			cost, err := estimator.Estimate(info, []byte(`{"model":"`+model+`"`+tc.extra+`}`), "chat/completions")
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.want, cost, 1e-9)
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

func TestEndpointCalendarAdmissionWholeReachableTariffs(t *testing.T) {
	for _, scenario := range []struct {
		name, overrides string
		want            float64
	}{
		{"daily opposite rates exclude unreachable base", `[{"utc_start":0,"utc_end":1200,"prompt":"0.002","completion":"0.001"},{"utc_start":1200,"utc_end":0,"prompt":"0.001","completion":"0.05"}]`, 2.01},
		{"overlapping overrides combine in declaration order", `[{"utc_start":0,"utc_end":1200,"prompt":"0.003"},{"utc_start":1000,"utc_end":1400,"completion":"0.2"}]`, 5},
		{"weekend whole day plus wrapping weekday", `[{"utc_days":["saturday","sunday"],"prompt":"0.004","completion":"0.001"},{"utc_start":2300,"utc_end":100,"utc_days":["monday"],"prompt":"0.002","completion":"0.03"}]`, 4.01},
		{"calendar threshold excluded below prompt", `[{"min_prompt_tokens":1001,"utc_start":0,"utc_end":1200,"prompt":"0.01"},{"utc_start":1200,"utc_end":0,"prompt":"0.002","completion":"0.001"}]`, 2.01},
		{"calendar threshold equality retains actual base", `[{"min_prompt_tokens":1000,"utc_start":0,"utc_end":1200,"prompt":"0.003","completion":"0.002"}]`, 2},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			estimator := NewRequestEstimator()
			estimator.Models["test/model"] = []PricedEndpoint{{Tag: "provider", MaxCompletionTokens: 10, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.1"`), "overrides": json.RawMessage(scenario.overrides)}}}
			info := &relaycommon.RelayInfo{}
			info.SetEstimatePromptTokens(1000)
			cost, err := estimator.Estimate(info, []byte(`{"model":"test/model","max_tokens":10}`), "chat/completions")
			require.NoError(t, err)
			assert.InDelta(t, scenario.want, cost, 1e-12)
		})
	}
}

func TestEndpointCalendarAdmissionRejectsMalformedConditions(t *testing.T) {
	for _, declaration := range []string{
		`{"utc_start":1200,"prompt":"0.002"}`,
		`{"utc_start":null,"utc_end":1400,"prompt":"0.002"}`,
		`{"min_prompt_tokens":null,"utc_days":["monday"],"prompt":"0.002"}`,
		`{"utc_start":1260,"utc_end":1400,"prompt":"0.002"}`,
		`{"utc_start":2400,"utc_end":1400,"prompt":"0.002"}`,
		`{"utc_start":1200,"utc_end":1200,"prompt":"0.002"}`,
		`{"utc_start":"1200","utc_end":1400,"prompt":"0.002"}`,
		`{"utc_days":[],"prompt":"0.002"}`,
		`{"utc_days":["Monday"],"prompt":"0.002"}`,
		`{"utc_days":["monday","monday"],"prompt":"0.002"}`,
		`{"utc_days":null,"prompt":"0.002"}`,
		`{"utc_days":["monday"],"prompt":null}`,
		`{"utc_days":["monday"],"prompt":"-1"}`,
		`{"utc_days":["monday"],"unknown_charge":"1"}`,
		`{"utc_days":["monday"],"utc_month":1,"prompt":"0.002"}`,
		`{"prompt":"0.002"}`,
	} {
		t.Run(declaration, func(t *testing.T) {
			estimator := NewRequestEstimator()
			estimator.Models["test/model"] = []PricedEndpoint{{Tag: "provider", MaxCompletionTokens: 10, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.002"`), "overrides": json.RawMessage("[" + declaration + "]")}}}
			info := &relaycommon.RelayInfo{}
			info.SetEstimatePromptTokens(1000)
			_, err := estimator.Estimate(info, []byte(`{"model":"test/model","max_tokens":10}`), "chat/completions")
			require.Error(t, err)
		})
	}
}

func TestEndpointGeminiTextAdmissionReasoningEnvelope(t *testing.T) {
	for _, scenario := range []struct {
		name, model, rate, extra string
		want                     float64
		fail                     bool
	}{
		{"Gemini25 higher reasoning", "google/gemini-2.5-pro", `"0.003"`, "", 1.03, false},
		{"Gemini3 text conditional audio", "google/gemini-3.1-pro-preview", `"0.003"`, `,"overrides":[{"min_prompt_tokens":999,"audio":"0.04","input_audio_cache":"0.03","completion":"0.004"}]`, 1.04, false},
		{"explicit free reasoning", "google/gemini-2.5-pro", `"0"`, "", 1.02, false},
		{"other model has no combined budget proof", "other/reasoner", `"0.003"`, "", 0, true},
		{"public alias is not native proof", "gemini-2.5-pro", `"0.003"`, "", 0, true},
		{"malformed reasoning", "google/gemini-2.5-pro", `null`, "", 0, true},
		{"negative reasoning", "google/gemini-2.5-pro", `"-1"`, "", 0, true},
		{"unknown billable dimension", "google/gemini-2.5-pro", `"0.003"`, `,"unpriced_dimension":"1"`, 0, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var prices map[string]json.RawMessage
			require.NoError(t, common.Unmarshal([]byte(`{"prompt":"0.001","completion":"0.002","audio":"0.02","input_audio_cache":"0.01","internal_reasoning":`+scenario.rate+scenario.extra+`}`), &prices))
			estimator := NewRequestEstimator()
			estimator.Models[scenario.model] = []PricedEndpoint{{ModelID: scenario.model, Tag: "google-ai-studio", MaxCompletionTokens: 10, Pricing: prices}}
			info := &relaycommon.RelayInfo{}
			info.SetEstimatePromptTokens(1000)
			cost, err := estimator.Estimate(info, []byte(`{"model":"`+scenario.model+`","max_tokens":10,"messages":[{"role":"user","content":"hello"}]}`), "chat/completions")
			if scenario.fail {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, scenario.want, cost, 1e-12)
			_, err = estimator.Estimate(info, []byte(`{"model":"`+scenario.model+`","max_tokens":10,"messages":[{"role":"user","content":[{"type":"input_audio","input_audio":{"data":"a","format":"wav"}}]}]}`), "chat/completions")
			require.ErrorContains(t, err, "media duration quantity is unpriced")
		})
	}
}

func TestEndpointSavedNativeCalendarAndGeminiMetadata(t *testing.T) {
	for _, scenario := range []struct {
		model, metadata string
		want            float64
	}{
		{`deepseek/deepseek-v4-pro-0813`, `{"model_id":"deepseek/deepseek-v4-pro-0813","tag":"deepseek","context_length":1048576,"max_prompt_tokens":null,"max_completion_tokens":393216,"pricing":{"prompt":"0.00000066","completion":"0.00000198","input_cache_read":"0.000000022","discount":0,"overrides":[{"utc_days":["saturday","sunday"],"prompt":"0.00000066","completion":"0.00000198","input_cache_read":"0.000000022"},{"utc_start":0,"utc_end":100,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.00000066","completion":"0.00000198","input_cache_read":"0.000000022"},{"utc_start":100,"utc_end":400,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.00000132","completion":"0.00000396","input_cache_read":"0.000000044"},{"utc_start":400,"utc_end":600,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.00000066","completion":"0.00000198","input_cache_read":"0.000000022"},{"utc_start":600,"utc_end":1000,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.00000132","completion":"0.00000396","input_cache_read":"0.000000044"},{"utc_start":1000,"utc_end":0,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.00000066","completion":"0.00000198","input_cache_read":"0.000000022"}]}}`, 0.0013596},
		{`deepseek/deepseek-v4-flash-0731`, `{"model_id":"deepseek/deepseek-v4-flash-0731","tag":"alibaba","context_length":1000000,"max_prompt_tokens":null,"max_completion_tokens":393216,"pricing":{"prompt":"0.000000176","completion":"0.000000528","input_cache_read":"0.0000000176","discount":0,"overrides":[{"utc_start":0,"utc_end":1400,"prompt":"0.000000352","completion":"0.000001056","input_cache_read":"0.0000000352"},{"utc_start":1400,"utc_end":0,"prompt":"0.000000176","completion":"0.000000528","input_cache_read":"0.0000000176"}]}}`, 0.00036256},
		{`tencent/hy3`, `{"model_id":"tencent/hy3","tag":"tencent/fp8","context_length":262144,"max_prompt_tokens":null,"max_completion_tokens":128000,"pricing":{"prompt":"0.0000000825","completion":"0.00000033","input_cache_read":"0.000000020625","discount":0,"overrides":[{"utc_start":0,"utc_end":1600,"prompt":"0.000000132","completion":"0.000000528","input_cache_read":"0.000000033"},{"utc_start":1600,"utc_end":0,"prompt":"0.0000000825","completion":"0.00000033","input_cache_read":"0.000000020625"}]}}`, 0.00013728},
		{`deepseek/deepseek-v4.1-flash`, `{"model_id":"deepseek/deepseek-v4.1-flash","tag":"deepseek","context_length":1048576,"max_prompt_tokens":null,"max_completion_tokens":393216,"pricing":{"prompt":"0.00000015","completion":"0.0000006","input_cache_read":"0.000000003","discount":0,"overrides":[{"utc_days":["saturday","sunday"],"prompt":"0.00000015","completion":"0.0000006","input_cache_read":"0.000000003"},{"utc_start":0,"utc_end":100,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.00000015","completion":"0.0000006","input_cache_read":"0.000000003"},{"utc_start":100,"utc_end":400,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.0000003","completion":"0.0000012","input_cache_read":"0.000000006"},{"utc_start":400,"utc_end":600,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.00000015","completion":"0.0000006","input_cache_read":"0.000000003"},{"utc_start":600,"utc_end":1000,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.0000003","completion":"0.0000012","input_cache_read":"0.000000006"},{"utc_start":1000,"utc_end":0,"utc_days":["monday","tuesday","wednesday","thursday","friday"],"prompt":"0.00000015","completion":"0.0000006","input_cache_read":"0.000000003"}]}}`, 0.000312},
		{`tencent/hy4-preview`, `{"model_id":"tencent/hy4-preview","tag":"tencent/fp8","context_length":1048576,"max_prompt_tokens":null,"max_completion_tokens":64000,"pricing":{"prompt":"0.0000007506","completion":"0.0000022509","input_cache_read":"0.0000000378","discount":0,"overrides":[{"utc_start":0,"utc_end":1600,"prompt":"0.000000834","completion":"0.000002501","input_cache_read":"0.000000042"},{"utc_start":1600,"utc_end":0,"prompt":"0.0000007506","completion":"0.0000022509","input_cache_read":"0.0000000378"}]}}`, 0.00085901},
		{`google/gemini-2.5-pro`, `{"model_id":"google/gemini-2.5-pro","tag":"google-ai-studio","context_length":1048576,"max_prompt_tokens":null,"max_completion_tokens":65536,"pricing":{"prompt":"0.00000125","completion":"0.00001","image":"0.00000125","audio":"0.00000125","input_audio_cache":"0.000000125","web_search":"0.014","internal_reasoning":"0.00001","input_cache_read":"0.000000125","input_cache_write":"0.000000375","discount":0,"overrides":[{"min_prompt_tokens":200000,"prompt":"0.0000025","completion":"0.000015","audio":"0.0000025","input_audio_cache":"0.00000025","input_cache_read":"0.00000025"}]}}`, 0.00135},
	} {
		t.Run(scenario.model, func(t *testing.T) {
			var endpoint PricedEndpoint
			require.NoError(t, common.Unmarshal([]byte(scenario.metadata), &endpoint))
			estimator := NewRequestEstimator()
			estimator.Models[scenario.model] = []PricedEndpoint{endpoint}
			info := &relaycommon.RelayInfo{}
			info.SetEstimatePromptTokens(1000)
			cost, err := estimator.Estimate(info, []byte(`{"model":"`+scenario.model+`","max_tokens":10}`), "chat/completions")
			require.NoError(t, err)
			assert.InDelta(t, scenario.want, cost, 1e-12)
		})
	}
}

func TestEndpointCalendarReservationPreservesRoutingAndCaps(t *testing.T) {
	estimator := NewRequestEstimator()
	estimator.Models["test/model"] = []PricedEndpoint{
		{Tag: "provider-a", MaxCompletionTokens: 10, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.002"`), "overrides": json.RawMessage(`[{"utc_start":0,"utc_end":1200,"prompt":"0.004","completion":"0.003"}]`)}},
		{Tag: "provider-b", MaxCompletionTokens: 20, Pricing: map[string]json.RawMessage{"prompt": json.RawMessage(`"0.002"`), "completion": json.RawMessage(`"0.01"`)}},
	}
	info := &relaycommon.RelayInfo{}
	info.SetEstimatePromptTokens(1000)
	for _, scenario := range []struct {
		name, body string
		want       float64
	}{
		{"whole calendar maximum", `{"model":"test/model","max_tokens":10}`, 4.03},
		{"provider only", `{"model":"test/model","max_tokens":10,"provider":{"only":["provider-b"]}}`, 2.1},
		{"provider ignore", `{"model":"test/model","max_tokens":10,"provider":{"ignore":["provider-a"]}}`, 2.1},
		{"provider max price", `{"model":"test/model","max_tokens":10,"provider":{"max_price":{"prompt":3000}}}`, 2.1},
		{"explicit zero output", `{"model":"test/model","max_tokens":0}`, 4},
		{"incompatible output cap", `{"model":"test/model","max_tokens":11}`, 2.11},
		{"finite default cap", `{"model":"test/model"}`, 4.03},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			cost, err := estimator.Estimate(info, []byte(scenario.body), "chat/completions")
			require.NoError(t, err)
			assert.InDelta(t, scenario.want, cost, 1e-12)
		})
	}
}

func TestEndpointGeminiAudioMetadataStrictRates(t *testing.T) {
	for _, key := range []string{"audio", "input_audio_cache"} {
		for _, rate := range []string{`"0"`, `null`, `"-1"`, `"NaN"`, `"Infinity"`} {
			t.Run(key+"/"+rate, func(t *testing.T) {
				prices := map[string]json.RawMessage{"prompt": json.RawMessage(`"0.001"`), "completion": json.RawMessage(`"0.002"`), key: json.RawMessage(rate)}
				estimator := NewRequestEstimator()
				estimator.Models["google/gemini-2.5-pro"] = []PricedEndpoint{{ModelID: "google/gemini-2.5-pro", Tag: "google-ai-studio", MaxCompletionTokens: 10, Pricing: prices}}
				info := &relaycommon.RelayInfo{}
				info.SetEstimatePromptTokens(1000)
				cost, err := estimator.Estimate(info, []byte(`{"model":"google/gemini-2.5-pro","max_tokens":10}`), "chat/completions")
				if rate != `"0"` {
					require.Error(t, err)
					return
				}
				require.NoError(t, err)
				assert.InDelta(t, 1.02, cost, 1e-12)
			})
		}
	}
}

func TestNativeImageProviderPreparation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, extra, overrides, wantTag string
		wantReserve                     float64
		wantTags                        []string
		wantFallback                    *bool
		fail                            bool
	}{
		{name: "default cheapest reserves maximum", wantTag: "cheap", wantReserve: 0.09, wantFallback: common.GetPointer(false)},
		{name: "only expensive", extra: `"provider":{"only":["expensive"],"allow_fallbacks":false,"sort":"price","options":{}}`, wantTag: "expensive", wantReserve: 0.09, wantFallback: common.GetPointer(false)},
		{name: "ordered expensive", extra: `"provider":{"order":["expensive","cheap"]}`, wantTag: "expensive", wantReserve: 0.09, wantTags: []string{"cheap", "expensive"}},
		{name: "explicit fallback preserved", extra: `"provider":{"order":["expensive","cheap"],"allow_fallbacks":true}`, wantTag: "expensive", wantReserve: 0.09, wantTags: []string{"cheap", "expensive"}, wantFallback: common.GetPointer(true)},
		{name: "nested admin replacement", extra: `"extra_body":{"provider":{"only":["expensive"]}}`, overrides: `{"provider":{"only":["cheap"]}}`, wantTag: "cheap", wantReserve: 0.01},
		{name: "ignore expensive", extra: `"provider":{"ignore":["expensive"]}`, wantTag: "cheap", wantReserve: 0.01},
		{name: "first primary unavailable", extra: `"provider":{"order":["missing","cheap"],"allow_fallbacks":false}`, fail: true},
		{name: "nested provider without admin override", extra: `"extra_body":{"provider":{"only":["expensive"]}}`, wantTag: "expensive", wantReserve: 0.09},
		{name: "only unavailable", extra: `"provider":{"only":["missing"]}`, fail: true},
		{name: "bad provider", extra: `"provider":[]`, fail: true},
		{name: "bad only", extra: `"provider":{"only":"cheap"}`, fail: true},
		{name: "empty identifier", extra: `"provider":{"only":[""]}`, fail: true},
		{name: "null preferences preserved", extra: `"provider":{"only":null,"allow_fallbacks":null}`, wantTag: "cheap", wantReserve: 0.09, wantTags: []string{"cheap", "expensive"}},
		{name: "count excludes incompatible fallback", extra: `"n":2,"provider":{"order":["expensive","cheap"]}`, wantTag: "cheap", wantReserve: 0.02},
		{name: "null unknown field rejected", extra: `"provider":{"unknown":null}`, fail: true},
		{name: "unknown field", extra: `"provider":{"max_price":{}}`, fail: true},
		{name: "options unsafe", extra: `"provider":{"options":{"cheap":{"x":1}}}`, fail: true},
		{name: "throughput sorting", extra: `"provider":{"sort":"throughput"}`, wantTag: "cheap", wantReserve: 0.09, wantTags: []string{"cheap", "expensive"}},
		{name: "partitioned price sorting", extra: `"provider":{"sort":{"by":"price","partition":"none"}}`, wantTag: "cheap", wantReserve: 0.09, wantTags: []string{"cheap", "expensive"}},
		{name: "unknown sorting rejected", extra: `"provider":{"sort":"unknown"}`, fail: true},
		{name: "latency sorting", extra: `"provider":{"sort":"latency"}`, wantTag: "cheap", wantReserve: 0.09, wantTags: []string{"cheap", "expensive"}},
		{name: "capability mismatch", extra: `"provider":{"only":["cheap"]},"resolution":"4K"`, fail: true},
		{name: "legacy admin override", overrides: `{"provider":{"only":["expensive"]}}`, wantTag: "expensive", wantReserve: 0.09},
		{name: "conditional move mapped model", extra: `"extra_body":{"provider":{"only":["expensive"]}}`, overrides: `{"operations":[{"mode":"move","from":"extra_body.provider","to":"provider","conditions":[{"path":"upstream_model","mode":"full","value":"test/image"}]},{"mode":"delete","path":"extra_body"}]}`, wantTag: "expensive", wantReserve: 0.09},
		{name: "copy cleanup", extra: `"extra_body":{"provider":{"only":["expensive"]}}`, overrides: `{"operations":[{"mode":"copy","from":"extra_body.provider","to":"provider"},{"mode":"delete","path":"extra_body"}]}`, wantTag: "expensive", wantReserve: 0.09},
		{name: "literal equal cleanup", extra: `"extra_body":{"provider":{"only":["expensive"]}}`, overrides: `{"provider":{"only":["expensive"]},"operations":[{"mode":"delete","path":"extra_body"}]}`, wantTag: "expensive", wantReserve: 0.09},
		{name: "mismatch cleanup preserves normalized nested", extra: `"extra_body":{"provider":{"only":["expensive"]}}`, overrides: `{"operations":[{"mode":"move","from":"extra_body.provider","to":"provider","conditions":[{"path":"model","mode":"full","value":"wrong"}]},{"mode":"delete","path":"extra_body"}]}`, wantTag: "expensive", wantReserve: 0.09},
		{name: "direct nested conflict", extra: `"provider":{"only":["cheap"]},"extra_body":{"provider":{"only":["expensive"]}}`, overrides: `{"operations":[{"mode":"move","from":"extra_body.provider","to":"provider"},{"mode":"delete","path":"extra_body"}]}`, fail: true},
		{name: "condition mismatch preserves normalized nested", extra: `"extra_body":{"provider":{"only":["expensive"]}}`, overrides: `{"operations":[{"mode":"move","from":"extra_body.provider","to":"provider","conditions":[{"path":"model","mode":"full","value":"wrong"}]}]}`, wantTag: "expensive", wantReserve: 0.09},
		{name: "cleanup cannot hide extra fields", extra: `"extra_body":{"provider":{},"n":100}`, overrides: `{"operations":[{"mode":"delete","path":"extra_body"}]}`, fail: true},
		{name: "unsupported override", overrides: `{"operations":[{"mode":"set","path":"n","value":10}]}`, fail: true},
		{name: "header override", overrides: `{"operations":[{"mode":"set_header","path":"Authorization","value":"secret"}]}`, fail: true},
		{name: "wildcard override", overrides: `{"operations":[{"mode":"set","path":"provider.*","value":{}}]}`, fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts++
					return
				}
				assert.Equal(t, "/v1/images/models/test/image/endpoints", r.URL.Path)
				_, _ = io.WriteString(w, `{"id":"test/image","endpoints":[{"provider_tag":"cheap","supported_parameters":{"n":{"type":"range","min":1,"max":4}},"pricing":[{"billable":"output_image","unit":"image","cost_usd":0.01}]},{"provider_tag":"expensive","pricing":[{"billable":"output_image","unit":"image","cost_usd":0.09}]}]}`)
			}))
			defer server.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			body := `{"model":"test/image","prompt":"x"`
			if tc.extra != "" {
				body += "," + tc.extra
			}
			body += "}"
			var request dto.ImageRequest
			require.NoError(t, common.Unmarshal([]byte(body), &request))
			originalExtra := request.Extra
			info := &relaycommon.RelayInfo{OriginModelName: "alias", ChannelMeta: &relaycommon.ChannelMeta{ChannelId: 42, ChannelBaseUrl: server.URL, UpstreamModelName: "test/image"}}
			if tc.overrides != "" {
				require.NoError(t, common.Unmarshal([]byte(tc.overrides), &info.ParamOverride))
			}
			a := &ImageAdaptor{Adaptor: &imageTestAdaptor{}}
			err := a.Prepare(c, info, request)
			assert.Equal(t, originalExtra, request.Extra)
			if tc.fail {
				require.Error(t, err)
				assert.Nil(t, info.NativeImage)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.wantTag, info.NativeImage.ProviderTag)
				assert.InDelta(t, tc.wantReserve, info.NativeImage.EstimatedCostUSD, 1e-12)
				var wire map[string]json.RawMessage
				require.NoError(t, common.Unmarshal(info.NativeImage.RequestBody, &wire))
				var provider map[string]json.RawMessage
				require.NoError(t, common.Unmarshal(wire["provider"], &provider))
				var only []string
				require.NoError(t, common.Unmarshal(provider["only"], &only))
				if tc.wantTags != nil {
					assert.Equal(t, tc.wantTags, only)
					assert.NotContains(t, wire, "n", "fallbacks with no n capability receive the native default")
				} else {
					assert.Equal(t, []string{tc.wantTag}, only)
				}
				if strings.Contains(tc.extra, `"allow_fallbacks":null`) {
					assert.Equal(t, "null", string(provider["allow_fallbacks"]))
				} else if tc.wantFallback == nil {
					assert.NotContains(t, provider, "allow_fallbacks")
				} else {
					assert.Equal(t, fmt.Sprint(*tc.wantFallback), string(provider["allow_fallbacks"]))
				}
				if tc.name == "only expensive" {
					assert.JSONEq(t, `"price"`, string(provider["sort"]))
					assert.JSONEq(t, `{}`, string(provider["options"]))
				}
				if strings.Contains(tc.name, "sorting") {
					assert.Contains(t, provider, "sort")
				}
				info.ChannelId++
				_, err = a.ConvertImageRequest(c, info, request)
				require.ErrorContains(t, err, "not prepared for this channel")
			}
			assert.Zero(t, posts)
		})
	}
}

// imageTestAdaptor supplies the host header callback without importing the host
// OpenAI adaptor, which itself depends on OpenRouter.
type imageTestAdaptor struct{ channel.Adaptor }

func (*imageTestAdaptor) SetupRequestHeader(_ *gin.Context, header *http.Header, _ *relaycommon.RelayInfo) error {
	header.Set("Authorization", "Bearer selected-key")
	return nil
}

func TestNativeImageProviderSinglePaidRequest(t *testing.T) {
	previousLimit := constant.TaskQueryLimit
	constant.TaskQueryLimit = 100
	t.Cleanup(func() { constant.TaskQueryLimit = previousLimit })
	previousDB, previousLogDB := model.DB, model.LOG_DB
	previousType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
	previousRedis, previousBatch, previousLog := common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled
	dialect := os.Getenv("FX_TASK_DB_DIALECT")
	openDB := func(dsn string) (*gorm.DB, error) {
		switch dialect {
		case "mysql":
			return gorm.Open(mysql.Open(dsn), &gorm.Config{})
		case "postgres":
			return gorm.Open(postgres.Open(dsn), &gorm.Config{})
		default:
			return gorm.Open(sqlite.Open(dsn), &gorm.Config{})
		}
	}
	mainDSN, logDSN := os.Getenv("FX_TASK_MAIN_DSN"), os.Getenv("FX_TASK_LOG_DSN")
	databaseType := common.DatabaseTypeSQLite
	if dialect == "" || dialect == "sqlite" {
		mainDSN, logDSN = ":memory:", ":memory:"
	} else {
		require.NotEmpty(t, mainDSN)
		require.NotEmpty(t, logDSN)
		if dialect == "mysql" {
			databaseType = common.DatabaseTypeMySQL
		} else {
			require.Equal(t, "postgres", dialect)
			databaseType = common.DatabaseTypePostgreSQL
		}
	}
	db, err := openDB(mainDSN)
	require.NoError(t, err)
	logDB, err := openDB(logDSN)
	require.NoError(t, err)
	for _, database := range []*gorm.DB{db, logDB} {
		sqlDB, err := database.DB()
		require.NoError(t, err)
		sqlDB.SetMaxOpenConns(1)
		t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	}
	var version string
	query := "SELECT sqlite_version()"
	if dialect == "mysql" || dialect == "postgres" {
		query = "SELECT version()"
	}
	require.NoError(t, db.Raw(query).Scan(&version).Error)
	t.Logf("real %s database version: %s", databaseType, version)
	for range 2 {
		require.NoError(t, db.AutoMigrate(&model.Task{}, &model.User{}, &model.Token{}, &model.Channel{}))
		require.NoError(t, logDB.AutoMigrate(&model.Log{}))
	}
	model.DB, model.LOG_DB = db, logDB
	common.SetDatabaseTypes(databaseType, databaseType)
	common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = false, false, true
	t.Cleanup(func() {
		model.DB, model.LOG_DB = previousDB, previousLogDB
		common.SetDatabaseTypes(previousType, previousLogType)
		common.RedisEnabled, common.BatchUpdateEnabled, common.LogConsumeEnabled = previousRedis, previousBatch, previousLog
	})
	root := t.TempDir()
	require.NoError(t, os.Chmod(root, 0700))
	t.Cleanup(func() { require.NoError(t, service.InitTaskArtifactStore()) })
	t.Setenv(system_setting.TaskArtifactStoreModeEnv, "filesystem")
	t.Setenv(system_setting.TaskArtifactStoreRootEnv, root)
	require.NoError(t, service.InitTaskArtifactStore())
	var pngData bytes.Buffer
	require.NoError(t, png.Encode(&pngData, image.NewRGBA(image.Rect(0, 0, 1, 1))))
	encoded := base64.StdEncoding.EncodeToString(pngData.Bytes())
	const upstreamModel = "vendor/image"
	for i, tc := range []struct {
		name, cost, pricing, extra, crash, source, requestID, language                                                                                                                                                  string
		reserve                                                                                                                                                                                                         float64
		noReserve, aliasReserve, badImage, transportError, httpError, noEndpoint, noProvider, noCapability, storageDisabled, insufficient, changeChannel, partial, sourceSet, noGeneration, expired, swept, lateHeaders bool
		wantError                                                                                                                                                                                                       string
	}{
		{name: "empty reservation source", reserve: .02, sourceSet: true, wantError: "unsupported OpenRouter image reservation source"},
		{name: "unknown reservation source", reserve: .02, sourceSet: true, source: "invalid", wantError: "unsupported OpenRouter image reservation source"},
		{name: "configured price source", reserve: .02, sourceSet: true, source: "configured_price", wantError: "unsupported OpenRouter image reservation source"},
		{name: "explicit upstream source", reserve: .02, sourceSet: true, source: "upstream", cost: "0.01"},
		{name: "missing generation id preserves request correlation", reserve: .02, noGeneration: true},
		{name: "secret request correlation rejected", reserve: .02, requestID: "selected-key", cost: "0.01"},
		{name: "late headers known cost", reserve: .02, cost: "0.03", lateHeaders: true},
		{name: "late headers after swept lease known cost", reserve: .02, cost: "0.03", lateHeaders: true, swept: true},
		{name: "late headers after swept lease unknown cost", reserve: .02, lateHeaders: true, swept: true, noGeneration: true},
		{name: "expired body lease known cost", reserve: .02, cost: "0.03", expired: true},
		{name: "swept lease known cost", reserve: .02, cost: "0.03", expired: true, swept: true},
		{name: "swept lease zero cost", reserve: .02, cost: "0", expired: true, swept: true},
		{name: "empty pricing opt in", reserve: .02, cost: "0.01"},
		{name: "over reserve", reserve: .02, cost: "0.03"},
		{name: "explicit cost zero", reserve: .02, cost: "0"},
		{name: "known estimate floor", reserve: .02, cost: "0.01", pricing: `[{"billable":"output_image","unit":"image","cost_usd":0.04}]`},
		{name: "reserve floor", reserve: .06, cost: "0.01", pricing: `[{"billable":"output_image","unit":"image","cost_usd":0.04}]`},
		{name: "legacy known price without storage", noReserve: true, storageDisabled: true, cost: "0.01", pricing: `[{"billable":"output_image","unit":"image","cost_usd":0.04}]`},
		{name: "published zero", noReserve: true, cost: "0", pricing: `[{"billable":"output_image","unit":"image","cost_usd":0}]`},
		{name: "no reserve", noReserve: true, wantError: "tariff unavailable"},
		{name: "alias reserve rejected", reserve: .02, aliasReserve: true, wantError: "tariff unavailable"},
		{name: "zero reserve", wantError: "invalid OpenRouter image channel reserve"},
		{name: "negative reserve", reserve: -1, wantError: "invalid OpenRouter image channel reserve"},
		{name: "NaN reserve", reserve: math.NaN(), wantError: "invalid OpenRouter image channel reserve"},
		{name: "infinite reserve", reserve: math.Inf(1), wantError: "invalid OpenRouter image channel reserve"},
		{name: "oversized reserve", reserve: 1e20, wantError: "invalid OpenRouter image channel reserve"},
		{name: "malformed nonempty price", reserve: .02, pricing: `[{"billable":"output_image","unit":"image"}]`, wantError: "tariff unavailable"},
		{name: "negative nonempty price", reserve: .02, pricing: `[{"billable":"output_image","unit":"image","cost_usd":-1}]`, wantError: "tariff unavailable"},
		{name: "no endpoint", reserve: .02, noEndpoint: true, wantError: "no documented image endpoint"},
		{name: "no provider", reserve: .02, noProvider: true, wantError: "no compatible"},
		{name: "unsupported resolution", reserve: .02, noCapability: true, wantError: "is unsupported by this endpoint"},
		{name: "missing resolution", reserve: .02, extra: `"resolution":null`, wantError: "unpriced OpenRouter image requires"},
		{name: "unsupported optional parameter", reserve: .02, extra: `"seed":1`, wantError: "unpriced OpenRouter image parameter seed is unsupported"},
		{name: "missing reference", reserve: .02, extra: `"image":null`, wantError: "requires an input reference"},
		{name: "unsupported options", reserve: .02, extra: `"provider":{"only":["fixture"],"allow_fallbacks":false,"options":{"thinking_mode":true}}`, wantError: "unsupported image provider options"},
		{name: "price sorting closed", reserve: .02, extra: `"provider":{"only":["fixture"],"allow_fallbacks":false,"sort":"price"}`, wantError: "unpriced OpenRouter image requires"},
		{name: "fallbacks closed", reserve: .02, extra: `"provider":{"only":["fixture"]}`, wantError: "unpriced OpenRouter image requires"},
		{name: "no selected provider", reserve: .02, extra: `"provider":{}`, wantError: "unpriced OpenRouter image requires"},
		{name: "wrong provider", reserve: .02, extra: `"provider":{"only":["other"],"allow_fallbacks":false}`, wantError: "no compatible"},
		{name: "stream closed", reserve: .02, extra: `"stream":true`, wantError: "unpriced OpenRouter image requires"},
		{name: "multiple outputs", reserve: .02, extra: `"n":2`, wantError: "count is unsupported"},
		{name: "multiple references", reserve: .02, extra: `"image":["https://example.com/a","https://example.com/b"]`, wantError: "unpriced OpenRouter image requires"},
		{name: "client reserve closed", reserve: .02, extra: `"openrouter_model_reserve_usd":100`, wantError: "is unsupported"},
		{name: "missing cost", reserve: .02},
		{name: "null cost", reserve: .02, cost: "null"},
		{name: "negative cost", reserve: .02, cost: "-1"},
		{name: "string cost", reserve: .02, cost: `"0.01"`},
		{name: "bad image known cost", reserve: .02, cost: "0.03", badImage: true},
		{name: "bad image unknown cost", reserve: .02, badImage: true},
		{name: "transport unknown", reserve: .02, transportError: true},
		{name: "http failure unknown", reserve: .02, httpError: true},
		{name: "http failure known cost", reserve: .02, httpError: true, cost: "0.03"},
		{name: "crash intent", reserve: .02, crash: "intent"},
		{name: "crash hold", reserve: .02, crash: "hold"},
		{name: "crash dispatch", reserve: .02, crash: "dispatch"},
		{name: "image without local storage", reserve: .02, storageDisabled: true, cost: "0.01"},
		{name: "insufficient funds", reserve: .02, insufficient: true, language: "zh-CN", wantError: "预扣费额度失败, 用户剩余额度: %s, 需要预扣费额度: %s"},
		{name: "insufficient funds English", reserve: .02, insufficient: true, language: "en", wantError: "Failed to reserve quota, remaining user quota: %s, required quota: %s"},
		{name: "channel changed", reserve: .02, changeChannel: true, wantError: "cannot be repeated"},
		{name: "settlement partial failure", reserve: .02, cost: "0.03", partial: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.storageDisabled {
				t.Cleanup(func() { require.NoError(t, service.InitTaskArtifactStore()) })
				t.Setenv(system_setting.TaskArtifactStoreModeEnv, "upstream")
				require.NoError(t, service.InitTaskArtifactStore())
			}
			id := 1000 + i
			quota := 10000000
			if tc.insufficient {
				quota = 1
			}
			require.NoError(t, db.Create(&model.User{Id: id, Username: fmt.Sprintf("image-%d", id), AffCode: fmt.Sprint(id), Quota: quota}).Error)
			key := fmt.Sprintf("image-key-%d", id)
			require.NoError(t, db.Create(&model.Token{Id: id, UserId: id, Key: key, RemainQuota: quota, Status: common.TokenStatusEnabled}).Error)
			require.NoError(t, db.Create(&model.Channel{Id: id}).Error)
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					assert.Equal(t, "/v1/images/models/vendor/image/endpoints", r.URL.Path)
					pricing := tc.pricing
					if pricing == "" {
						pricing = "[]"
					}
					provider := "fixture"
					if tc.noProvider {
						provider = ""
					}
					capabilities := `{"resolution":{"type":"enum","values":["1K","2K"]},"aspect_ratio":{"type":"enum","values":["1:1"]},"input_references":{"type":"range","min":1,"max":8},"seed":{"type":"boolean"}}`
					if tc.noCapability {
						capabilities = `{"input_references":{"type":"range","min":1,"max":8},"seed":{"type":"boolean"}}`
					}
					endpoints := fmt.Sprintf(`[{"provider_tag":%q,"supported_parameters":%s,"pricing":%s}]`, provider, capabilities, pricing)
					if tc.noEndpoint {
						endpoints = "[]"
					}
					_, _ = fmt.Fprintf(w, `{"id":"vendor/image","endpoints":%s}`, endpoints)
					return
				}
				posts++
				assert.Equal(t, "/v1/images", r.URL.Path)
				var submitted map[string]any
				require.NoError(t, common.DecodeJson(r.Body, &submitted))
				assert.Equal(t, upstreamModel, submitted["model"])
				assert.Equal(t, map[string]any{"only": []any{"fixture"}, "allow_fallbacks": false}, submitted["provider"])
				// Only the explicit reserve opts into the durable path.
				if !tc.noReserve {
					var task model.Task
					require.NoError(t, db.Where("user_id = ?", id).First(&task).Error)
					assert.True(t, task.PrivateData.Reconciliation.SubmissionPending)
					assert.True(t, task.PrivateData.OpenRouterSyncState().DispatchPrepared)
					assert.Equal(t, constant.TaskPlatformOpenRouterImage, task.Platform)
					assert.Empty(t, task.PrivateData.Key)
					assert.Empty(t, task.PrivateData.OpenRouterSyncState().Headers)
					if tc.lateHeaders {
						task.PrivateData.OpenRouterSyncState().LeaseUntil = 1
						require.NoError(t, tasksapp.NewStore().UpdateOpenRouterSync(&task, task.PrivateData.OpenRouterSyncState().Revision))
						if tc.swept {
							pollSyncMetadataForProvider(t)
						}
					}
				}
				if tc.transportError {
					conn, _, err := w.(http.Hijacker).Hijack()
					require.NoError(t, err)
					_ = conn.Close()
					return
				}
				if !tc.noGeneration {
					w.Header().Set("X-Generation-Id", "gen-image-fixture")
				}
				requestID := "req-image-fixture"
				if tc.requestID != "" {
					requestID = tc.requestID
				}
				w.Header().Set("X-Request-Id", requestID)
				result := fmt.Sprintf(`"data":[{"b64_json":%q}]`, encoded)
				if tc.badImage {
					result = `"data":[{"b64_json":"aGVsbG8="}]`
				}
				if tc.httpError {
					w.WriteHeader(503)
					result = `"error":{"message":"fixture failed"}`
				}
				usage := ""
				if tc.cost != "" {
					usage = `,"usage":{"cost":` + tc.cost + `}`
				}
				_, _ = fmt.Fprint(w, `{`+result+usage+`}`)
			}))
			defer server.Close()
			fields := map[string]json.RawMessage{}
			require.NoError(t, common.Unmarshal([]byte(`{"model":"vendor/image","prompt":"x","resolution":"1K","aspect_ratio":"1:1","image":"data:image/png;base64,`+encoded+`","provider":{"only":["fixture"],"allow_fallbacks":false}}`), &fields))
			if tc.extra != "" {
				var overrides map[string]json.RawMessage
				require.NoError(t, common.Unmarshal([]byte(`{`+tc.extra+`}`), &overrides))
				for key, value := range overrides {
					fields[key] = value
				}
			}
			body, err := common.Marshal(fields)
			require.NoError(t, err)
			var request dto.ImageRequest
			require.NoError(t, common.Unmarshal(body, &request))
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
			if tc.language != "" {
				c.Request.Header.Set("Accept-Language", tc.language)
			}
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelId: id, ChannelType: constant.ChannelTypeOpenRouter, ChannelBaseUrl: server.URL, UpstreamModelName: upstreamModel, ApiKey: "selected-key"}, UserId: id, TokenId: id, TokenKey: key, OriginModelName: "image-alias", RequestId: fmt.Sprint("image-request-", id), RelayMode: relayconstant.RelayModeImagesEdits, Request: &request}
			info.UserSetting.BillingPreference = "wallet_only"
			info.PriceData.BillingGroupRatio = 1.5
			if !tc.noReserve {
				reserveModel := upstreamModel
				if tc.aliasReserve {
					reserveModel = "image-alias"
				}
				info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{reserveModel: tc.reserve}
			}
			if tc.sourceSet {
				info.ChannelSetting.OpenRouterReservationSource = map[string]dto.OpenRouterReservationSource{upstreamModel: dto.OpenRouterReservationSource(tc.source)}
			}
			adaptor := &ImageAdaptor{Adaptor: &imageTestAdaptor{}}
			err = adaptor.Prepare(c, info, request)
			if err == nil && tc.changeChannel {
				info.ChannelId++
			}
			if err == nil && tc.crash != "" {
				info.OpenRouterCost = &relaycommon.OpenRouterCostState{Basis: relaycommon.NewOpenRouterCostBasis(info, tc.reserve)}
				planned, clamp, basisErr := info.OpenRouterCost.Basis.Quota(tc.reserve)
				require.NoError(t, basisErr)
				require.Nil(t, clamp)
				require.NoError(t, taskrecord.BeginOpenRouterSyncTask(c, info, planned))
				if tc.crash != "intent" {
					require.Nil(t, service.PreConsumeBilling(c, planned, info))
					if tc.crash == "dispatch" {
						require.NoError(t, taskrecord.CommitOpenRouterSyncReservation(c, info))
						require.NoError(t, taskrecord.ConfigureOpenRouterSyncSubmission(info, "", nil))
					}
				}
			} else if err == nil {
				response, requestErr := adaptor.DoRequest(c, info, nil)
				err = requestErr
				if tc.wantError == "" && !tc.transportError {
					require.NoError(t, err)
					if tc.expired {
						task, exists, loadErr := tasksapp.NewStore().GetUniqueByOnlyTaskId(info.OpenRouterCost.TaskID)
						require.NoError(t, loadErr)
						require.True(t, exists)
						task.PrivateData.OpenRouterSyncState().LeaseUntil = 1
						require.NoError(t, tasksapp.NewStore().UpdateOpenRouterSync(task, task.PrivateData.OpenRouterSyncState().Revision))
						if tc.swept {
							pollSyncMetadataForProvider(t)
						}
					}
					if tc.partial {
						require.NoError(t, db.Unscoped().Where("id = ?", id).Delete(&model.Token{}).Error)
					}
					if tc.httpError {
						_ = ImageError(c, response.(*http.Response), info)
					} else {
						usage, apiErr := adaptor.DoResponse(c, response.(*http.Response), info)
						if tc.noReserve && apiErr == nil {
							service.PostTextConsumeQuota(c, info, usage.(*dto.Usage), nil)
						}
						if tc.cost == "" || tc.cost == "null" || tc.cost == "-1" || strings.HasPrefix(tc.cost, `"`) || tc.badImage || tc.expired || tc.lateHeaders {
							require.NotNil(t, apiErr)
						} else {
							require.Nil(t, apiErr)
						}
					}
				}
			}
			if tc.wantError != "" {
				if tc.insufficient {
					planned := common.QuotaRound(tc.reserve * common.QuotaPerUnit * 1.5)
					require.EqualError(t, err, fmt.Sprintf(tc.wantError, logger.FormatQuota(quota), logger.FormatQuota(planned)))
				} else {
					require.ErrorContains(t, err, tc.wantError)
				}
				assert.Zero(t, posts)
				var user model.User
				require.NoError(t, db.First(&user, id).Error)
				assert.Equal(t, quota, user.Quota)
				return
			}
			require.NotNil(t, info.OpenRouterCost)
			if tc.noReserve {
				assert.Empty(t, info.OpenRouterCost.TaskID)
				assert.False(t, relaycommon.HasOpenRouterSyncTask(info))
				var user model.User
				require.NoError(t, db.First(&user, id).Error)
				cost, costErr := common.ValidateOpenRouterCost([]byte(tc.cost))
				require.NoError(t, costErr)
				assert.Equal(t, quota-common.QuotaRound(cost*common.QuotaPerUnit*1.5), user.Quota)
				assert.Equal(t, 1, posts)
				return
			}
			require.NotEmpty(t, info.OpenRouterCost.TaskID)
			assert.Equal(t, info.OpenRouterCost.TaskID, recorder.Header().Get("X-Task-Id"))
			task, exists, err := tasksapp.NewStore().GetUniqueByOnlyTaskId(info.OpenRouterCost.TaskID)
			require.NoError(t, err)
			require.True(t, exists)
			if tc.crash != "" {
				task.PrivateData.OpenRouterSyncState().LeaseUntil = 1
				require.NoError(t, tasksapp.NewStore().UpdateOpenRouterSync(task, task.PrivateData.OpenRouterSyncState().Revision))
			}
			if tc.partial {
				assert.True(t, task.PrivateData.Reconciliation.SettlementPending)
				require.NoError(t, db.Create(&model.Token{Id: id, UserId: id, Key: key, RemainQuota: quota - info.FinalPreConsumedQuota, Status: common.TokenStatusEnabled}).Error)
			}
			for range 2 {
				pollSyncMetadataForProvider(t)
			}
			task, exists, err = tasksapp.NewStore().GetUniqueByOnlyTaskId(info.OpenRouterCost.TaskID)
			require.NoError(t, err)
			require.True(t, exists)
			wantCost := info.NativeImage.EstimatedCostUSD
			if tc.crash == "intent" {
				wantCost = 0
			}
			cost, knownErr := common.ValidateOpenRouterCost([]byte(tc.cost))
			known := knownErr == nil && tc.crash == "" && !tc.transportError
			if known {
				if tc.partial {
					assert.True(t, task.PrivateData.Reconciliation.SettlementPending)
					assert.False(t, task.PrivateData.Reconciliation.MainApplied)
					assert.False(t, task.PrivateData.Reconciliation.Finalized)
					assert.Equal(t, common.QuotaRound(cost*common.QuotaPerUnit*1.5), task.PrivateData.Reconciliation.TargetQuota)
				} else {
					wantCost = cost
					assert.True(t, task.PrivateData.Reconciliation.Finalized)
				}
			} else {
				assert.True(t, task.PrivateData.Reconciliation.Required)
				assert.False(t, task.PrivateData.Reconciliation.Finalized)
				assert.False(t, tasksapp.NewBilling().Refund(t.Context(), tasksrecord.ToDomain(task), "unknown must remain held"))
			}
			wantQuota := quota - common.QuotaRound(wantCost*common.QuotaPerUnit*1.5)
			var user model.User
			require.NoError(t, db.First(&user, id).Error)
			assert.Equal(t, wantQuota, user.Quota)
			var token model.Token
			require.NoError(t, db.First(&token, id).Error)
			assert.Equal(t, wantQuota, token.RemainQuota)
			assert.False(t, info.Billing != nil && info.Billing.NeedsRefund() && tc.crash == "")
			assert.Equal(t, "image", task.Action)
			assert.Equal(t, "fixture", task.PrivateData.OpenRouterSyncState().ProviderTag)
			if tc.crash == "" && !tc.transportError {
				wantID := "req-image-fixture"
				if tc.requestID != "" {
					wantID = ""
				}
				assert.Equal(t, wantID, task.PrivateData.OpenRouterSyncState().UpstreamRequestID)
			}
			if known && !tc.partial {
				require.Error(t, taskrecord.FailOpenRouterSyncTask(info, "duplicate_cost"))
			}
			if tc.crash == "" && !tc.transportError && !tc.httpError && known && !tc.badImage && !tc.expired && !tc.lateHeaders {
				assert.True(t, task.PrivateData.OpenRouterSyncState().ResultReady)
				assert.Empty(t, task.PrivateData.OpenRouterSyncState().ResultSHA256)
				assert.Equal(t, "application/json", task.PrivateData.OpenRouterSyncState().ResultMimeType)
				assert.Contains(t, recorder.Body.String(), encoded)
			}
			_, err = adaptor.DoRequest(c, info, nil)
			require.Error(t, err)
			wantPosts := 1
			if tc.crash != "" {
				wantPosts = 0
			}
			assert.Equal(t, wantPosts, posts)
		})
	}
}

func TestNativeImageUnknownReachableTariffFailsClosed(t *testing.T) {
	for _, price := range []string{`[]`, `[{"billable":"output_image","unit":"image"}]`, `[{"billable":"output_image","unit":"image","cost_usd":null}]`, `[{"billable":"output_image","unit":"image","cost_usd":-1}]`, `[{"billable":"output_image","unit":"unknown","cost_usd":0.02}]`, `[{"billable":"unknown","unit":"image","cost_usd":0.02}]`} {
		t.Run(price, func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts++
					return
				}
				_, _ = fmt.Fprintf(w, `{"id":"test/image","endpoints":[{"provider_tag":"known","pricing":[{"billable":"output_image","unit":"image","cost_usd":0.01}]},{"provider_tag":"unknown","pricing":%s}]}`, price)
			}))
			defer server.Close()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL}}
			adaptor := &ImageAdaptor{Adaptor: &imageTestAdaptor{}}
			err := adaptor.Prepare(c, info, dto.ImageRequest{Model: "test/image", Prompt: "x"})
			require.ErrorIs(t, err, errImageTariff)
			assert.Nil(t, info.NativeImage)
			assert.Zero(t, posts)
		})
	}
}

func TestNativeImageExplicitFreeTariff(t *testing.T) {
	var prices []ImagePrice
	require.NoError(t, common.Unmarshal([]byte(`[{"billable":"output_image","unit":"image","cost_usd":0}]`), &prices))
	cost, err := EstimateImageCost(prices, 1, 0, map[string]any{"prompt": "x"})
	require.NoError(t, err)
	assert.Zero(t, cost)
}

func TestNativeImageMultipartRoutingFailsClosed(t *testing.T) {
	for _, field := range []string{"provider", "extra_body"} {
		t.Run(field, func(t *testing.T) {
			var form bytes.Buffer
			writer := multipart.NewWriter(&form)
			require.NoError(t, writer.WriteField(field, `{"only":["expensive"],"allow_fallbacks":false}`))
			require.NoError(t, writer.Close())
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", &form)
			c.Request.Header.Set("Content-Type", writer.FormDataContentType())
			require.NoError(t, c.Request.ParseMultipartForm(1<<20))
			defer c.Request.MultipartForm.RemoveAll()
			// No adaptor/URL is supplied: routing must fail before discovery or POST.
			info := &relaycommon.RelayInfo{}
			err := (&ImageAdaptor{}).Prepare(c, info, dto.ImageRequest{Model: "test/image", Prompt: "x"})
			require.ErrorContains(t, err, "multipart routing fields are unsupported")
			assert.Nil(t, info.NativeImage)
		})
	}
}

func TestNativeImageIncompatibleUnknownTariffExcluded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		_, _ = io.WriteString(w, `{"id":"test/image","endpoints":[{"provider_tag":"unknown","supported_parameters":{"input_references":{"type":"range","min":1,"max":2}},"pricing":[]},{"provider_tag":"known","pricing":[{"billable":"output_image","unit":"image","cost_usd":0.01}]}]}`)
	}))
	defer server.Close()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{ChannelBaseUrl: server.URL}}
	adaptor := &ImageAdaptor{Adaptor: &imageTestAdaptor{}}
	require.NoError(t, adaptor.Prepare(c, info, dto.ImageRequest{Model: "test/image", Prompt: "x"}))
	assert.Equal(t, "known", info.NativeImage.ProviderTag)
	assert.InDelta(t, 0.01, info.NativeImage.EstimatedCostUSD, 1e-12)
}

// Fixture generated with ffmpeg: color=c=blue:s=640x480:r=24, -t 2 -an
// -c:v libx264 -profile:v baseline -bf 0 -pix_fmt yuv420p -use_editlist 0.
func TestSourceMediaVideoReservation(t *testing.T) {
	video, err := os.ReadFile("testdata/source-640x480-2s.mp4")
	require.NoError(t, err)
	videoURI := "data:video/mp4;base64," + base64.StdEncoding.EncodeToString(video)
	var photo bytes.Buffer
	require.NoError(t, png.Encode(&photo, image.NewRGBA(image.Rect(0, 0, 32, 32))))
	imageURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(photo.Bytes())
	audio := make([]byte, 44+16000*2*3)
	copy(audio, "RIFF")
	binary.LittleEndian.PutUint32(audio[4:8], uint32(len(audio)-8))
	copy(audio[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(audio[16:20], 16)
	binary.LittleEndian.PutUint16(audio[20:22], 1)
	binary.LittleEndian.PutUint16(audio[22:24], 1)
	binary.LittleEndian.PutUint32(audio[24:28], 16000)
	binary.LittleEndian.PutUint32(audio[28:32], 32000)
	binary.LittleEndian.PutUint16(audio[32:34], 2)
	binary.LittleEndian.PutUint16(audio[34:36], 16)
	copy(audio[36:], "data")
	binary.LittleEndian.PutUint32(audio[40:44], uint32(len(audio)-44))
	audioURI := "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(audio)
	malformedAudio := bytes.Clone(audio)
	binary.LittleEndian.PutUint32(malformedAudio[28:32], 1)
	malformedAudioURI := "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(malformedAudio)
	longAudio := append(bytes.Clone(audio), make([]byte, 16000*2*28)...)
	binary.LittleEndian.PutUint32(longAudio[4:8], uint32(len(longAudio)-8))
	binary.LittleEndian.PutUint32(longAudio[40:44], uint32(len(longAudio)-44))
	longAudioURI := "data:audio/wav;base64," + base64.StdEncoding.EncodeToString(longAudio)
	reference := func(kind, uri string) any { return map[string]any{"type": kind, kind: map[string]any{"url": uri}} }
	for _, tc := range []struct {
		name, model string
		prices      map[string]string
		fields      map[string]any
		want        float64
		errorText   string
	}{
		{name: "Aleph source seconds and minimum", model: "runway/aleph-2", prices: map[string]string{"cents_per_second_output": "28", "minimum_cents_per_generation": "56"}, want: .56},
		{name: "fresh minimum", model: "runway/aleph-2", prices: map[string]string{"cents_per_second_output": "28", "minimum_cents_per_generation": "70"}, want: .70},
		{name: "FLUX measured nominal binary megapixels", model: "black-forest-labs/flux-video-upscale", prices: map[string]string{"cents_per_megapixel_second_precise": "3.5", "cents_per_megapixel_second_creative": "5"}, fields: map[string]any{"upscale_factor": 1.5, "creativity": 0}, want: float64(640*480) * 1.5 * 1.5 * 2 / 1048576 * .035},
		{name: "FLUX default creative", model: "black-forest-labs/flux-video-upscale", prices: map[string]string{"cents_per_megapixel_second_precise": "3.5", "cents_per_megapixel_second_creative": "5"}, want: float64(640*480) * 4 * 2 / 1048576 * .05},
		{name: "Avatar PCM seconds", model: "heygen/avatar-iv", prices: map[string]string{"duration_seconds": "0.05"}, fields: map[string]any{"resolution": "720p", "input_references": []any{reference("image_url", imageURI), reference("audio_url", audioURI)}}, want: .15},
		{name: "WAV cannot fake byte rate", model: "heygen/avatar-iv", prices: map[string]string{"duration_seconds": "0.05"}, fields: map[string]any{"resolution": "720p", "input_references": []any{reference("image_url", imageURI), reference("audio_url", malformedAudioURI)}}, errorText: "invalid WAV quantities"},
		{name: "audio duration cap", model: "heygen/avatar-iv", prices: map[string]string{"duration_seconds": "0.05"}, fields: map[string]any{"resolution": "720p", "input_references": []any{reference("image_url", imageURI), reference("audio_url", longAudioURI)}}, errorText: "at most 30 seconds"},
		{name: "script not a duration bound", model: "heygen/avatar-iv", prices: map[string]string{"duration_seconds": "0.05"}, fields: map[string]any{"resolution": "720p", "prompt": "hello", "input_references": []any{reference("image_url", imageURI), reference("audio_url", audioURI)}}, errorText: "script/TTS"},
		{name: "URL never fetched", fields: map[string]any{"input_references": []any{reference("video_url", "http://169.254.169.254/latest")}}, errorText: "byte limit"},
		{name: "bad MP4", fields: map[string]any{"input_references": []any{reference("video_url", "data:video/mp4;base64,YmFk")}}, errorText: "MP4"},
		{name: "count cap", fields: map[string]any{"input_references": []any{reference("video_url", videoURI), reference("video_url", videoURI)}}, errorText: "exactly one"},
		{name: "client duration cannot replace source", fields: map[string]any{"duration": 2}, errorText: "source media quantity"},
		{name: "provider multiplier bypass", fields: map[string]any{"provider": map[string]any{"options": map[string]any{"runway": map[string]any{"duration": 9999}}}}, errorText: "provider options"},
		{name: "unknown SKU", prices: map[string]string{"cents_per_second_output": "28", "new_fee": "1"}, errorText: "unknown"},
		{name: "negative tariff", prices: map[string]string{"cents_per_second_output": "-28"}, errorText: "invalid"},
		{name: "explicit free", prices: map[string]string{"cents_per_second_output": "0"}},
		{name: "unknown tariff", prices: map[string]string{}, errorText: "missing"},
		{name: "oversized encoded input", fields: map[string]any{"input_references": []any{reference("video_url", "data:video/mp4;base64,"+strings.Repeat("A", 16_000_000))}}, errorText: "byte limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.model == "" {
				tc.model = "runway/aleph-2"
			}
			if tc.prices == nil {
				tc.prices = map[string]string{"cents_per_second_output": "28"}
			}
			fields := map[string]any{"model": tc.model, "input_references": []any{reference("video_url", videoURI)}}
			for key, value := range tc.fields {
				fields[key] = value
			}
			body, err := common.Marshal(fields)
			require.NoError(t, err)
			model := VideoModelPricing{ID: tc.model, PricingSKUs: tc.prices, SupportedResolutions: []string{"720p", "1080p"}}
			cost, err := estimateSourceMediaVideo(model, body)
			if tc.errorText != "" {
				require.ErrorContains(t, err, tc.errorText)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.want, cost, 1e-12)
		})
	}
	t.Run("default closed explicit approximate opt in", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/v1/videos/models", r.URL.Path)
			_, err := io.WriteString(w, `{"data":[{"id":"runway/aleph-2","pricing_skus":{"cents_per_second_output":"28","minimum_cents_per_generation":"56"}},{"id":"heygen/avatar-iv","supported_resolutions":["720p"],"pricing_skus":{"duration_seconds":"0.05"}}]}`)
			assert.NoError(t, err)
		}))
		defer server.Close()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/videos", nil)
		info := &relaycommon.RelayInfo{ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "runway/aleph-2", ChannelBaseUrl: server.URL}}
		body, err := common.Marshal(map[string]any{"model": "runway/aleph-2", "input_references": []any{reference("video_url", videoURI)}})
		require.NoError(t, err)
		_, err = EstimateVideo(c, info, body)
		require.ErrorContains(t, err, "explicit approximate")
		info.ChannelSetting.OpenRouterModelReserveUSD = map[string]float64{"runway/aleph-2": 1}
		cost, err := EstimateVideo(c, info, body)
		require.NoError(t, err)
		assert.Equal(t, 1.0, cost)
		for _, reserve := range []float64{0, -1, math.NaN(), math.Inf(1)} {
			info.ChannelSetting.OpenRouterModelReserveUSD["runway/aleph-2"] = reserve
			_, err = EstimateVideo(c, info, body)
			require.ErrorContains(t, err, "explicit approximate")
		}
		info.UpstreamModelName = "heygen/avatar-iv"
		info.ChannelSetting.OpenRouterModelReserveUSD["heygen/avatar-iv"] = 1
		avatarBody, err := common.Marshal(map[string]any{"model": "heygen/avatar-iv", "resolution": "720p", "input_references": []any{reference("image_url", imageURI), reference("audio_url", audioURI)}})
		require.NoError(t, err)
		_, err = EstimateVideo(c, info, avatarBody)
		require.ErrorContains(t, err, "inline audio transport is unconfirmed")
		assert.Equal(t, 7, calls, "only discovery GETs, no paid POST")
	})
}

func TestBoundedVideoRejectsConflictingMetadata(t *testing.T) {
	data, err := os.ReadFile("testdata/source-640x480-2s.mp4")
	require.NoError(t, err)
	seconds, width, height, err := probeBoundedVideo(data)
	require.NoError(t, err)
	assert.Equal(t, 2.0, seconds)
	assert.Equal(t, 640, width)
	assert.Equal(t, 480, height)
	for _, tc := range []struct {
		name   string
		mutate func([]byte)
	}{
		{"zero sample description", func(b []byte) { i := bytes.Index(b, []byte("stsc")); clear(b[i+20 : i+24]) }},
		{"oversized sample description", func(b []byte) {
			i := bytes.Index(b, []byte("stsc"))
			binary.BigEndian.PutUint32(b[i+20:i+24], math.MaxUint32)
		}},
		{"unvalidated sample description", func(b []byte) { i := bytes.Index(b, []byte("stsc")); binary.BigEndian.PutUint32(b[i+20:i+24], 2) }},
		{"multiple sample descriptions", func(b []byte) { i := bytes.Index(b, []byte("stsd")); binary.BigEndian.PutUint32(b[i+8:i+12], 2) }},
		{"zero timescale", func(b []byte) { i := bytes.Index(b, []byte("mdhd")); require.Positive(t, i); clear(b[i+16 : i+20]) }},
		{"duration conflict", func(b []byte) { i := bytes.Index(b, []byte("mdhd")); binary.BigEndian.PutUint32(b[i+20:i+24], 1) }},
		{"unbounded sample count", func(b []byte) {
			i := bytes.Index(b, []byte("stts"))
			binary.BigEndian.PutUint32(b[i+12:i+16], math.MaxUint32)
		}},
		{"zero sample delta", func(b []byte) { i := bytes.Index(b, []byte("stts")); clear(b[i+16 : i+20]) }},
		{"too high FPS", func(b []byte) { i := bytes.Index(b, []byte("stts")); binary.BigEndian.PutUint32(b[i+16:i+20], 1) }},
		{"dimensions conflict with bitstream", func(b []byte) {
			i := bytes.Index(b, []byte("avc1"))
			for i >= 0 && i+32 < len(b) {
				if binary.BigEndian.Uint16(b[i+28:i+30]) == 640 {
					binary.BigEndian.PutUint16(b[i+28:i+30], 3000)
					return
				}
				j := bytes.Index(b[i+4:], []byte("avc1"))
				if j < 0 {
					break
				}
				i += 4 + j
			}
			t.Fatal("missing avc1")
		}},
		{"overlapping chunk ranges", func(b []byte) {
			i := bytes.Index(b, []byte("stco"))
			require.Positive(t, i)
			count := binary.BigEndian.Uint32(b[i+8 : i+12])
			require.Greater(t, count, uint32(1))
			copy(b[i+16:i+20], b[i+12:i+16])
		}},
		{"source duration above cap", func(b []byte) {
			mvhd := bytes.Index(b, []byte("mvhd"))
			mdhd := bytes.Index(b, []byte("mdhd"))
			stts := bytes.Index(b, []byte("stts"))
			binary.BigEndian.PutUint32(b[mvhd+16:mvhd+20], 48)
			binary.BigEndian.PutUint32(b[mvhd+20:mvhd+24], 48*31)
			binary.BigEndian.PutUint32(b[mdhd+16:mdhd+20], 48)
			binary.BigEndian.PutUint32(b[mdhd+20:mdhd+24], 48*31)
			binary.BigEndian.PutUint32(b[stts+16:stts+20], 31)
		}},
		{"truncated payload", func(b []byte) { clear(b[len(b)-10:]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := bytes.Clone(data)
			tc.mutate(b)
			_, _, _, err := probeBoundedVideo(b)
			require.Error(t, err)
		})
	}
}

// pollSyncMetadataForProvider tests the durable metadata scenario directly.
// Importing the outer task runtime here would cycle through this provider package.
func pollSyncMetadataForProvider(t *testing.T) {
	t.Helper()
	reconciler := tasksapp.NewReconciler()
	for _, task := range tasksapp.NewStore().GetUnfinishedSyncTaskPage(constant.TaskQueryLimit, 0) {
		require.NoError(t, reconciler.Poll(t.Context(), tasksrecord.ToDomain(task)))
	}
}
