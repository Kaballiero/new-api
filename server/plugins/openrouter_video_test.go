package plugins_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openRouterVideoFixture(t *testing.T) (*jsplugin.LoadedPlugin, time.Time) {
	t.Helper()
	source, err := builtinplugins.Source("openrouter-video")
	require.NoError(t, err)
	stamp := regexp.MustCompile(`"fetched_at": "([^"]+)"`).FindStringSubmatch(source)
	require.Len(t, stamp, 2)
	now, err := time.Parse(time.RFC3339Nano, stamp[1])
	require.NoError(t, err)
	plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{
		Key: "openrouter-video", Now: func() time.Time { return now },
	})
	require.NoError(t, err)
	return plugin, now
}

func TestOpenRouterVideoNativeEditingPriceUnits(t *testing.T) {
	plugin, _ := openRouterVideoFixture(t)
	catalog := plugin.Meta.PriceCatalog
	require.NotNil(t, catalog)
	require.NoError(t, catalog.Validate(plugin.Meta.Models))
	for _, scenario := range []struct {
		model     string
		operation string
		unit      string
		rates     []float64
	}{
		{"runway/aleph-2", "video_editing", "second", []float64{0.28}},
		{"black-forest-labs/flux-video-upscale", "video_upscaling", "megapixel_second", []float64{0.105, 0.075}},
	} {
		t.Run(scenario.model, func(t *testing.T) {
			offers := catalog.Models[scenario.model]
			require.Len(t, offers, len(scenario.rates))
			rates := make([]float64, 0, len(offers))
			for _, offer := range offers {
				assert.Equal(t, scenario.operation, offer.Operation)
				assert.Equal(t, "video_output", offer.MainComponent)
				require.Len(t, offer.Prices, 1)
				price := offer.Prices[0]
				assert.Equal(t, "video_output", price.Component)
				assert.Equal(t, scenario.unit, price.Unit)
				require.NotNil(t, price.AmountUSD)
				rates = append(rates, *price.AmountUSD)
			}
			assert.ElementsMatch(t, scenario.rates, rates)
		})
	}
}

func TestOpenRouterVideoOrder(t *testing.T) {
	plugin, _ := openRouterVideoFixture(t)
	ctx := map[string]any{
		"model": "customer-alias", "upstreamModel": "google/veo-3.1-lite",
		"baseUrl": "https://openrouter.ai/api", "apiKey": "fixture-key",
		"requestBody": map[string]any{"prompt": "ocean waves", "seconds": 4, "metadata": map[string]any{"resolution": "720p", "generate_audio": false, "seed": 0}},
	}
	value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
	require.NoError(t, err)
	encoded, err := common.Marshal(value)
	require.NoError(t, err)
	var descriptor struct {
		URL     string            `json:"url"`
		Method  string            `json:"method"`
		Body    map[string]any    `json:"body"`
		Headers map[string]string `json:"headers"`
	}
	require.NoError(t, common.Unmarshal(encoded, &descriptor))
	assert.Equal(t, "https://openrouter.ai/api/v1/videos", descriptor.URL)
	assert.Equal(t, "POST", descriptor.Method)
	assert.Equal(t, "Bearer fixture-key", descriptor.Headers["Authorization"])
	assert.Equal(t, map[string]any{"model": "google/veo-3.1-lite", "prompt": "ocean waves", "duration": float64(4), "resolution": "720p", "generate_audio": false, "seed": float64(0)}, descriptor.Body)
	facts, err := plugin.Engine.Call(t.Context(), "extractUsage", ctx)
	require.NoError(t, err)
	encoded, err = common.Marshal(facts)
	require.NoError(t, err)
	var usage map[string]any
	require.NoError(t, common.Unmarshal(encoded, &usage))
	assert.Equal(t, float64(4), usage["requested_seconds"])
	assert.Equal(t, "disabled", usage["audio"])
	assert.Equal(t, "720p", usage["resolution"])
	assert.Equal(t, float64(1), usage["jobs"])
	measured, err := plugin.Engine.Call(t.Context(), "extractUsageOnComplete", map[string]any{}, map[string]any{}, map[string]any{"usage": map[string]any{"cost": 0.12}})
	require.NoError(t, err)
	assert.Empty(t, measured, "purchase cost must not overwrite the client tariff or invent measured seconds")
}

func TestOpenRouterHeyGenReferences(t *testing.T) {
	plugin, _ := openRouterVideoFixture(t)
	for _, tc := range []struct {
		name                   string
		images, videos, audios int
		metadata               bool
		wantError              string
	}{
		{name: "image references", images: 3},
		{name: "maximum images", images: 9},
		{name: "maximum total", images: 9, videos: 3},
		{name: "too many images", images: 10, wantError: "HeyGen supports at most"},
		{name: "too many videos", videos: 4, wantError: "HeyGen supports at most"},
		{name: "too many audios", images: 1, audios: 4, wantError: "HeyGen supports at most"},
		{name: "too many total", images: 9, videos: 3, audios: 1, wantError: "HeyGen supports at most"},
		{name: "metadata cannot bypass cap", images: 10, metadata: true, wantError: "HeyGen supports at most"},
		{name: "audio alone is not reference mode", audios: 1, wantError: "requires an image or video"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			references := make([]any, 0, tc.images+tc.videos+tc.audios)
			for _, media := range []struct {
				kind  string
				count int
			}{{"image_url", tc.images}, {"video_url", tc.videos}, {"audio_url", tc.audios}} {
				for range media.count {
					references = append(references, map[string]any{"type": media.kind, media.kind: map[string]any{"url": "https://example.com/media"}})
				}
			}
			request := map[string]any{"prompt": "animate the scene", "input_references": references}
			if tc.metadata {
				delete(request, "input_references")
				request["metadata"] = map[string]any{"input_references": references}
			}
			value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", map[string]any{"model": "heygen/heygen-video-1", "baseUrl": "https://openrouter.ai/api", "requestBody": request})
			if tc.wantError != "" {
				require.ErrorContains(t, err, tc.wantError)
				return
			}
			require.NoError(t, err)
			encoded, err := common.Marshal(value)
			require.NoError(t, err)
			var descriptor struct {
				Body struct {
					InputReferences []any `json:"input_references"`
				} `json:"body"`
			}
			require.NoError(t, common.Unmarshal(encoded, &descriptor))
			assert.Len(t, descriptor.Body.InputReferences, len(references))
		})
	}
	// Publish the ordinary first-frame rate, but do not turn the reference
	// surcharge (first two free) into an unconditional price per input image.
	offers := plugin.Meta.PriceCatalog.Models["heygen/heygen-video-1"]
	var haveFrame, haveReference bool
	for _, offer := range offers {
		if offer.ID == "duration_seconds_480p:image_to_video" {
			haveFrame = true
			require.Len(t, offer.Prices, 1)
			require.NotNil(t, offer.Prices[0].AmountUSD)
			assert.Equal(t, 0.02, *offer.Prices[0].AmountUSD)
		}
		if offer.ID == "reference_duration_seconds_480p:image_to_video" {
			haveReference = true
			require.Len(t, offer.Prices, 2)
			assert.Equal(t, "additional_reference_input", offer.Prices[1].Component)
			assert.Nil(t, offer.Prices[1].AmountUSD)
		}
	}
	assert.True(t, haveFrame)
	assert.True(t, haveReference)
}

func TestOpenRouterVideoValidation(t *testing.T) {
	plugin, fetchedAt := openRouterVideoFixture(t)
	for _, test := range []struct {
		name      string
		model     string
		request   map[string]any
		wantError string
	}{
		{"unknown model", "missing/video", map[string]any{"prompt": "waves"}, "Unknown OpenRouter"},
		{"unsigned overflow", "google/veo-3.1-lite", map[string]any{"seconds": "18446744073686646784"}, "seconds must"},
		{"fractional duration", "google/veo-3.1-lite", map[string]any{"seconds": 4.5}, "seconds must"},
		{"model duration", "google/veo-3.1-lite", map[string]any{"seconds": 5}, "Unsupported duration"},
		{"metadata bypass", "google/veo-3.1-lite", map[string]any{"metadata": map[string]any{"duration": 90000}}, "seconds must"},
		{"conflicting duration", "google/veo-3.1-lite", map[string]any{"seconds": 4, "duration": 8}, "Conflicting seconds"},
		{"unsupported resolution", "google/veo-3.1-lite", map[string]any{"resolution": "4K"}, "Unsupported resolution"},
		{"conflicting dimensions", "google/veo-3.1-lite", map[string]any{"resolution": "720p", "size": "1280x720"}, "Use size or"},
		{"unsupported seed", "x-ai/grok-imagine-video", map[string]any{"seed": 0}, "seed is not supported"},
		{"invalid image collection", "google/veo-3.1-lite", map[string]any{"images": "https://example.com/image.png"}, "images must"},
		{"unsupported last frame", "x-ai/grok-imagine-video", map[string]any{"frame_images": []any{map[string]any{"frame_type": "last_frame"}}}, "Unsupported or duplicate"},
		{"unsafe option", "google/veo-3.1-lite", map[string]any{"provider": map[string]any{"options": map[string]any{"google-vertex": map[string]any{"sampleCount": 128}}}}, "Unsupported provider option"},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.request["prompt"] = "ocean waves"
			_, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", map[string]any{"upstreamModel": test.model, "baseUrl": "https://openrouter.ai/api", "requestBody": test.request})
			require.ErrorContains(t, err, test.wantError)
		})
	}
	source, err := builtinplugins.Source("openrouter-video")
	require.NoError(t, err)
	stale, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: "openrouter-video", Now: func() time.Time { return fetchedAt.Add(31 * 24 * time.Hour) }})
	require.NoError(t, err)
	_, err = stale.Engine.Call(t.Context(), "buildSubmitRequest", map[string]any{"model": "google/veo-3.1-lite", "requestBody": map[string]any{"prompt": "waves"}})
	require.ErrorContains(t, err, "catalog is stale")
}

func TestOpenRouterVideoDurationCapabilities(t *testing.T) {
	plugin, _ := openRouterVideoFixture(t)
	for _, test := range []struct {
		name      string
		model     string
		request   map[string]any
		want      any
		wantError string
	}{
		{"listed duration", "google/veo-3.1-lite", map[string]any{"seconds": 8}, float64(8), ""},
		{"duration outside listed values", "google/veo-3.1-lite", map[string]any{"seconds": 5}, nil, "Unsupported duration"},
		{"unlisted durations pass seconds through", "black-forest-labs/flux-video-edit", map[string]any{"seconds": 20}, float64(20), ""},
		{"unlisted durations pass metadata through", "black-forest-labs/flux-video-edit", map[string]any{"metadata": map[string]any{"duration": 7}}, float64(7), ""},
		{"unlisted durations allow omission", "black-forest-labs/flux-video-edit", map[string]any{}, nil, ""},
		{"unlisted durations keep integer bounds", "black-forest-labs/flux-video-edit", map[string]any{"seconds": 3601}, nil, "seconds must"},
		{"unlisted durations reject fractions", "black-forest-labs/flux-video-edit", map[string]any{"seconds": 4.5}, nil, "seconds must"},
		{"unlisted durations keep other capabilities", "black-forest-labs/flux-video-edit", map[string]any{"seconds": 20, "resolution": "720p"}, nil, "Unsupported resolution"},
	} {
		t.Run(test.name, func(t *testing.T) {
			test.request["prompt"] = "ocean waves"
			ctx := map[string]any{"upstreamModel": test.model, "baseUrl": "https://openrouter.ai/api", "requestBody": test.request}
			value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
			if test.wantError != "" {
				require.ErrorContains(t, err, test.wantError)
				return
			}
			require.NoError(t, err)
			encoded, err := common.Marshal(value)
			require.NoError(t, err)
			var descriptor struct {
				Body map[string]any `json:"body"`
			}
			require.NoError(t, common.Unmarshal(encoded, &descriptor))
			assert.Equal(t, test.want, descriptor.Body["duration"])
			facts, err := plugin.Engine.Call(t.Context(), "extractUsage", ctx)
			require.NoError(t, err)
			encoded, err = common.Marshal(facts)
			require.NoError(t, err)
			var usage map[string]any
			require.NoError(t, common.Unmarshal(encoded, &usage))
			wantSeconds := test.want
			if wantSeconds == nil {
				wantSeconds = float64(0)
			}
			assert.Equal(t, wantSeconds, usage["requested_seconds"], "usage facts describe the forwarded duration")
		})
	}
}

func TestOpenRouterVideoLifecycle(t *testing.T) {
	plugin, _ := openRouterVideoFixture(t)
	submit, err := plugin.Engine.Call(t.Context(), "parseSubmitResponse", map[string]any{}, map[string]any{"statusCode": 202, "body": map[string]any{"id": "upstream-123", "status": "pending"}})
	require.NoError(t, err)
	encoded, err := common.Marshal(submit)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), `"taskId":"upstream-123"`)
	for _, test := range []struct{ upstream, status string }{
		{"pending", "QUEUED"}, {"in_progress", "IN_PROGRESS"}, {"completed", "SUCCESS"},
		{"failed", "FAILURE"}, {"cancelled", "FAILURE"}, {"expired", "FAILURE"}, {"new-state", "UNKNOWN"},
	} {
		result, err := plugin.Engine.Call(t.Context(), "parseTaskResult", map[string]any{"taskId": "upstream-123"}, map[string]any{"id": "upstream-123", "status": test.upstream, "error": "Bearer secret-value"})
		require.NoError(t, err)
		encoded, err := common.Marshal(result)
		require.NoError(t, err)
		var parsed map[string]any
		require.NoError(t, common.Unmarshal(encoded, &parsed))
		assert.Equal(t, test.status, parsed["status"])
		assert.NotContains(t, string(encoded), "secret-value")
	}
	content, err := plugin.Engine.Call(t.Context(), "buildContentRequest", map[string]any{
		"artifactKey": "video", "baseUrl": "https://openrouter.ai/api", "apiKey": "fixture-key", "upstreamTaskId": "upstream/123",
		"data": map[string]any{"unsigned_urls": []string{"https://attacker.example/steal"}}, "clientRequest": map[string]any{"method": "HEAD"},
	})
	require.NoError(t, err)
	encoded, err = common.Marshal(content)
	require.NoError(t, err)
	assert.Contains(t, string(encoded), "https://openrouter.ai/api/v1/videos/upstream%2F123/content?index=0")
	assert.NotContains(t, string(encoded), "attacker.example")
	assert.Contains(t, string(encoded), `"method":"GET"`)
	view, err := plugin.Engine.CallPath(t.Context(), "protocols", []string{"openai_video", "render"}, map[string]any{}, map[string]any{"data": map[string]any{"usage": map[string]any{"cost": 2}, "polling_url": "private-upstream"}})
	require.NoError(t, err)
	assert.Empty(t, view)
}

func TestOpenRouterSourceMediaDescriptor(t *testing.T) {
	plugin, _ := openRouterVideoFixture(t)
	for _, model := range []string{"runway/aleph-2", "black-forest-labs/flux-video-upscale", "heygen/avatar-iv"} {
		t.Run(model, func(t *testing.T) {
			kind := "video_url"
			if model == "heygen/avatar-iv" {
				kind = "audio_url"
			}
			uri := "data:" + strings.ReplaceAll(kind, "_url", "") + "/fixture;base64,Ynl0ZXM="
			reference := map[string]any{"type": kind, kind: map[string]any{"url": uri}}
			request := map[string]any{"input_references": []any{reference}}
			ctx := map[string]any{"model": model, "baseUrl": "https://openrouter.ai/api", "requestBody": request}
			value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
			require.NoError(t, err)
			encoded, err := common.Marshal(value)
			require.NoError(t, err)
			var descriptor struct {
				Body struct {
					References []map[string]any `json:"input_references"`
				} `json:"body"`
			}
			require.NoError(t, common.Unmarshal(encoded, &descriptor))
			require.Len(t, descriptor.Body.References, 1)
			assert.Equal(t, reference, descriptor.Body.References[0], "host estimator and POST consume the same inline reference")
			reference["duration"] = 9999
			_, err = plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
			require.ErrorContains(t, err, "only type and URL")
			delete(reference, "duration")
			request["provider"] = map[string]any{"options": map[string]any{"provider": map[string]any{"keyframes": []any{}}}}
			_, err = plugin.Engine.Call(t.Context(), "buildSubmitRequest", ctx)
			require.ErrorContains(t, err, "Provider options are unsupported")
		})
	}
}
