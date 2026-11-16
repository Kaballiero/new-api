package channels

import (
	"fmt"
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

// The Russian report lists the models applied by auto sync and, as a
// decision, the channels over the auto removal limit with their number of auto
// removals.
func TestModelUpdateNotificationListsAutoSyncChangesInRussian(t *testing.T) {
	require.NoError(t, i18n.Init())
	models := make([]string, 25)
	for i := range models {
		models[i] = fmt.Sprintf("a/m%d", i+1)
	}
	_, content := ModelUpdateNotification(i18n.LangRu, domain.ModelUpdateReport{
		CheckedChannels: 3, ChangedChannels: 3, DetectedAddModels: 1, DetectedRemoveModels: 50, AutoAddedModels: 1, AutoRemovedModels: 2,
		Channels: []domain.ModelUpdateChannelSummary{
			{ChannelName: "OpenRouter", AddCount: 1, RemoveCount: 2, AutoAddedModels: []string{"openai/gpt-5.1-mini"}, AutoRemovedModels: []string{"x-ai/grok-2", "my-manual-model"}},
			{ChannelName: "OpenRouter big", RemoveCount: 23, PendingRemoveModels: models[:23], RemovalLimitCandidates: models[:21]},
			{ChannelName: "OpenRouter huge", RemoveCount: 25, PendingRemoveModels: models, RemovalLimitCandidates: models},
		},
	})
	assert.Equal(t, "Итог проверки моделей upstream: проверено каналов — 3, с изменениями — 3, добавлено — 1, удалено — 50, добавлено автосинхронизацией — 1, ошибок — 0."+
		"\n\nИзменённые каналы (показано 3/3):\n- OpenRouter (+1 / -2)\n- OpenRouter big (+0 / -23)\n- OpenRouter huge (+0 / -25)"+
		"\n\nДобавлено автосинхронизацией (показано 1/1): openai/gpt-5.1-mini (OpenRouter)"+
		"\n\nУдалено автосинхронизацией (показано 2/2): x-ai/grok-2 (OpenRouter), my-manual-model (OpenRouter)"+
		"\n\nТребует решения — к удалению больше 20 моделей, ничего не удалено: OpenRouter big (21), OpenRouter huge (25)", content)
}

// The Russian report names once the channels whose unresolved model mapping
// blocks the auto-apply of every catalog channel, by id and name, instead of a
// catalog model loss.
func TestModelUpdateNotificationNamesUnresolvedMappingChannelsInRussian(t *testing.T) {
	require.NoError(t, i18n.Init())
	unresolved := []domain.UnresolvedMappingChannel{{ChannelID: 3, ChannelName: "OpenRouter json"}, {ChannelID: 7, ChannelName: "OpenRouter cycle"}}
	_, content := ModelUpdateNotification(i18n.LangRu, domain.ModelUpdateReport{
		CheckedChannels: 2, ChangedChannels: 2, DetectedAddModels: 2,
		Channels: []domain.ModelUpdateChannelSummary{
			{ChannelName: "OpenRouter", AddCount: 1, PendingAddModels: []string{"openai/gpt-5.1"}, AutoApplyBlocked: true, UnresolvedMappingChannels: unresolved},
			{ChannelName: "OpenRouter fast", AddCount: 1, PendingAddModels: []string{"openai/gpt-5.1"}, AutoApplyBlocked: true, UnresolvedMappingChannels: unresolved},
		},
		AddModelSamples: []string{"openai/gpt-5.1"},
	})
	assert.Equal(t, "Итог проверки моделей upstream: проверено каналов — 2, с изменениями — 2, добавлено — 2, удалено — 0, добавлено автосинхронизацией — 0, ошибок — 0."+
		"\n\nИзменённые каналы (показано 2/2):\n- OpenRouter (+1 / -0)\n- OpenRouter fast (+1 / -0)"+
		"\n\nПримеры добавленных моделей (показано 1/1): openai/gpt-5.1"+
		"\n\nИзменения не применяются автоматически — битый маппинг моделей в канале (показано 2/2): #3 OpenRouter json, #7 OpenRouter cycle", content)
}
