package plugins_test

import (
	"regexp"
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
