package channels

import (
	"testing"

	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/channels/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The zh-CN text repeats the notification built before localization.
func TestModelUpdateNotificationKeepsChineseText(t *testing.T) {
	require.NoError(t, i18n.Init())
	channels := make([]domain.ModelUpdateChannelSummary, 0, 9)
	for i := range 9 {
		channels = append(channels, domain.ModelUpdateChannelSummary{ChannelName: "c" + string(rune('A'+i)), AddCount: i + 1, RemoveCount: i})
	}
	failed := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}
	add := []string{"m1", "m2", "m3", "m4", "m5", "m6", "m7", "m8", "m9", "m10", "m11", "m12", "m13", " m1 "}
	subject, content := ModelUpdateNotification(i18n.LangZhCN, domain.ModelUpdateReport{
		CheckedChannels: 20, ChangedChannels: 9, DetectedAddModels: 45, DetectedRemoveModels: 36, AutoAddedModels: 2,
		FailedChannelIDs: failed, Channels: channels, AddModelSamples: add, RemoveModelSamples: []string{"r1", "r2"},
	})
	assert.Equal(t, "上游模型巡检通知", subject)
	assert.Equal(t, "上游模型巡检摘要：检测渠道 20 个，发现变更 9 个，新增 45 个，删除 36 个，自动同步新增 2 个，失败 11 个。"+
		"\n\n变更渠道明细（展示 8/9）："+
		"\n- cA (+1 / -0)\n- cB (+2 / -1)\n- cC (+3 / -2)\n- cD (+4 / -3)\n- cE (+5 / -4)\n- cF (+6 / -5)\n- cG (+7 / -6)\n- cH (+8 / -7)"+
		"\n- 其余 1 个渠道已省略"+
		"\n\n新增模型示例（展示 12/13）：m1, m2, m3, m4, m5, m6, m7, m8, m9, m10, m11, m12（其余 1 个已省略）"+
		"\n\n删除模型示例（展示 2/2）：r1, r2"+
		"\n\n失败渠道 ID（展示 10/11）：1, 2, 3, 4, 5, 6, 7, 8, 9, 10（其余 1 个已省略）", content)

	subject, content = ModelUpdateNotification(i18n.LangEn, domain.ModelUpdateReport{CheckedChannels: 1, FailedChannelIDs: []int{4}})
	assert.Equal(t, "Upstream model check notification", subject)
	assert.Equal(t, "Upstream model check summary: checked 1 channels, 0 changed, 0 added, 0 removed, 0 auto-synced additions, 1 failed.\n\nFailed channel IDs (showing 1/1): 4", content)
}
