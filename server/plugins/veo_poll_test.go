package plugins_test

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/jsplugin"
	builtinplugins "github.com/QuantumNous/new-api/plugins"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoogleVeoSubmitUsesRESTSampleCount(t *testing.T) {
	source, err := builtinplugins.Source("google")
	require.NoError(t, err)
	plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: "google"})
	require.NoError(t, err)
	for _, withImage := range []bool{false, true} {
		request := map[string]any{"prompt": "a blue cube", "duration": 4, "size": "1280x720"}
		wantAction := "text_to_video"
		if withImage {
			request["images"] = []any{"data:image/png;base64,aGVsbG8="}
			wantAction = "image_to_video"
		}
		t.Run(wantAction, func(t *testing.T) {
			value, err := plugin.Engine.Call(t.Context(), "buildSubmitRequest", map[string]any{
				"baseUrl": "https://generativelanguage.googleapis.com", "upstreamModel": "veo-3.1-fast-generate-preview",
				"requestBody": request,
			})
			require.NoError(t, err)
			result := decodePluginValue(t, value)
			assert.Equal(t, wantAction, result["action"])
			body, ok := result["body"].(map[string]any)
			require.True(t, ok)
			params, ok := body["parameters"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, float64(1), params["sampleCount"])
			assert.NotContains(t, params, "numberOfVideos")
			assert.Equal(t, float64(4), params["durationSeconds"])
			assert.Equal(t, "720p", params["resolution"])
		})
	}
}

func TestGoogleVeoVideoURIFromRESTAndLegacyResponses(t *testing.T) {
	source, err := builtinplugins.Source("google")
	require.NoError(t, err)
	plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: "google"})
	require.NoError(t, err)
	const uri = "https://generativelanguage.googleapis.com/v1beta/files/abc:download?alt=media"
	for _, field := range []string{"generatedSamples", "generatedVideos"} {
		t.Run(field, func(t *testing.T) {
			body := map[string]any{"name": "models/veo-3.1-fast-generate-preview/operations/abc", "done": true,
				"response": map[string]any{"generateVideoResponse": map[string]any{
					field: []any{map[string]any{"video": map[string]any{"uri": uri}}},
				}}}
			value, err := plugin.Engine.Call(t.Context(), "parseSubmitResponse", map[string]any{}, map[string]any{"body": body})
			require.NoError(t, err)
			submit := decodePluginValue(t, value)
			immediate, ok := submit["immediate"].(map[string]any)
			require.True(t, ok)
			assert.Equal(t, uri, immediate["remoteUrl"])
			value, err = plugin.Engine.Call(t.Context(), "parseTaskResult", map[string]any{}, body)
			require.NoError(t, err)
			assert.Equal(t, uri, decodePluginValue(t, value)["remoteUrl"])
			value, err = plugin.Engine.Call(t.Context(), "listArtifacts", map[string]any{"taskId": "task_x", "status": "SUCCESS", "data": body})
			require.NoError(t, err)
			encoded, err := common.Marshal(value)
			require.NoError(t, err)
			assert.JSONEq(t, `[{"key":"video","type":"video"}]`, string(encoded))
			value, err = plugin.Engine.Call(t.Context(), "buildContentRequest", map[string]any{
				"artifactKey": "video", "data": body, "clientRequest": map[string]any{"method": "GET"},
			})
			require.NoError(t, err)
			assert.Equal(t, uri, decodePluginValue(t, value)["url"])
		})
	}
}

// Google long-running operations serialize proto3 defaults by omission: a
// still-running operation has no `done` key at all. Treating that as
// unrecognized would count every normal poll as a failure and fail the task
// at the poll-failure threshold while the video is still rendering.
func TestVeoParseTaskResultTreatsMissingDoneAsInProgress(t *testing.T) {
	cases := []struct {
		name       string
		body       map[string]any
		wantStatus string
	}{
		{
			name:       "running operation omits done",
			body:       map[string]any{"name": "operations/abc", "metadata": map[string]any{"@type": "x"}},
			wantStatus: "IN_PROGRESS",
		},
		{
			name:       "explicit done false",
			body:       map[string]any{"name": "operations/abc", "done": false},
			wantStatus: "IN_PROGRESS",
		},
		{
			name:       "body without operation name is unrecognized",
			body:       map[string]any{"foo": "bar"},
			wantStatus: "UNKNOWN",
		},
		{
			name:       "operation error is failure",
			body:       map[string]any{"name": "operations/abc", "done": true, "error": map[string]any{"message": "quota exceeded"}},
			wantStatus: "FAILURE",
		},
	}
	for _, key := range []string{"google", "vertex-ai"} {
		source, err := builtinplugins.Source(key)
		require.NoError(t, err)
		plugin, err := jsplugin.NewRegistry().RegisterFactory(source, jsplugin.Options{Key: key})
		require.NoError(t, err)
		for _, tc := range cases {
			t.Run(key+"/"+tc.name, func(t *testing.T) {
				value, err := plugin.Engine.Call(t.Context(), "parseTaskResult", map[string]any{}, tc.body)
				require.NoError(t, err)
				encoded, err := common.Marshal(value)
				require.NoError(t, err)
				var result map[string]any
				require.NoError(t, common.Unmarshal(encoded, &result))
				assert.Equal(t, tc.wantStatus, result["status"])
			})
		}
	}
}
