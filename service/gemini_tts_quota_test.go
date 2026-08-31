package service

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCalculateAudioQuotaForGeminiTTSTextInputAudioOutput(t *testing.T) {
	modelRatioSnapshot := ratio_setting.ModelRatio2JSONString()
	audioCompletionRatioSnapshot := ratio_setting.AudioCompletionRatio2JSONString()
	t.Cleanup(func() {
		require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(modelRatioSnapshot))
		require.NoError(t, ratio_setting.UpdateAudioCompletionRatioByJSONString(audioCompletionRatioSnapshot))
	})

	cases := []struct {
		model                string
		modelRatio           float64
		audioCompletionRatio float64
		wantQuota            int
	}{
		{model: "gemini-2.5-flash-preview-tts", modelRatio: 0.25, audioCompletionRatio: 20, wantQuota: 1025},
		{model: "gemini-2.5-pro-preview-tts", modelRatio: 0.5, audioCompletionRatio: 20, wantQuota: 2050},
	}

	modelRatios := ratio_setting.GetModelRatioCopy()
	audioCompletionRatios := ratio_setting.GetAudioCompletionRatioCopy()
	for _, tc := range cases {
		modelRatios[tc.model] = tc.modelRatio
		audioCompletionRatios[tc.model] = tc.audioCompletionRatio
	}

	modelRatioJSON, err := common.Marshal(modelRatios)
	require.NoError(t, err)
	audioCompletionRatioJSON, err := common.Marshal(audioCompletionRatios)
	require.NoError(t, err)

	require.NoError(t, ratio_setting.UpdateModelRatioByJSONString(string(modelRatioJSON)))
	require.NoError(t, ratio_setting.UpdateAudioCompletionRatioByJSONString(string(audioCompletionRatioJSON)))

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			modelRatio, ok, _ := ratio_setting.GetModelRatio(tc.model)
			require.True(t, ok)

			quota, clamp := calculateAudioQuota(QuotaInfo{
				InputDetails:  TokenDetails{TextTokens: 100},
				OutputDetails: TokenDetails{AudioTokens: 200},
				ModelName:     tc.model,
				ModelRatio:    modelRatio,
				GroupRatio:    1,
			})

			assert.Nil(t, clamp)
			assert.Equal(t, tc.wantQuota, quota)
		})
	}
}
