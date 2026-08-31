package ratio_setting

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func snapshotRatioSettings(t *testing.T) {
	t.Helper()

	modelPrice := ModelPrice2JSONString()
	modelRatio := ModelRatio2JSONString()
	completionRatio := CompletionRatio2JSONString()
	cacheRatio := CacheRatio2JSONString()
	createCacheRatio := CreateCacheRatio2JSONString()
	imageRatio := ImageRatio2JSONString()
	audioRatio := AudioRatio2JSONString()
	audioCompletionRatio := AudioCompletionRatio2JSONString()

	t.Cleanup(func() {
		require.NoError(t, UpdateModelPriceByJSONString(modelPrice))
		require.NoError(t, UpdateModelRatioByJSONString(modelRatio))
		require.NoError(t, UpdateCompletionRatioByJSONString(completionRatio))
		require.NoError(t, UpdateCacheRatioByJSONString(cacheRatio))
		require.NoError(t, UpdateCreateCacheRatioByJSONString(createCacheRatio))
		require.NoError(t, UpdateImageRatioByJSONString(imageRatio))
		require.NoError(t, UpdateAudioRatioByJSONString(audioRatio))
		require.NoError(t, UpdateAudioCompletionRatioByJSONString(audioCompletionRatio))
	})
}

func TestGeminiTTSDefaultRatios(t *testing.T) {
	snapshotRatioSettings(t)
	InitRatioSettings()

	cases := []struct {
		model                string
		modelRatio           float64
		audioCompletionRatio float64
	}{
		{model: "gemini-2.5-flash-preview-tts", modelRatio: 0.25, audioCompletionRatio: 20},
		{model: "gemini-2.5-pro-preview-tts", modelRatio: 0.5, audioCompletionRatio: 20},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			ratio, ok, matchedName := GetModelRatio(tc.model)

			require.True(t, ok, "model ratio must be configured, otherwise the channel falls back to self-use pricing")
			assert.Equal(t, tc.model, matchedName)
			assert.Equal(t, tc.modelRatio, ratio)
			assert.Equal(t, tc.audioCompletionRatio, GetAudioCompletionRatio(tc.model))
			assert.True(t, ContainsAudioCompletionRatio(tc.model))
		})
	}
}

func TestGeminiTTSKeepsDefaultAudioInputRatio(t *testing.T) {
	snapshotRatioSettings(t)
	InitRatioSettings()

	for _, model := range []string{"gemini-2.5-flash-preview-tts", "gemini-2.5-pro-preview-tts"} {
		t.Run(model, func(t *testing.T) {
			assert.False(t, ContainsAudioRatio(model))
			assert.Equal(t, float64(1), GetAudioRatio(model))
		})
	}
}
