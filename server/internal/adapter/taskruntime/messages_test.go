package taskruntime

import (
	"fmt"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/internal/app/accountstorage"
	fixture "github.com/QuantumNous/new-api/internal/contracttest/taskfixture"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskTextUsesRecipientLanguage(t *testing.T) {
	db := fixture.Open(t)
	for i, tc := range []struct{ language, timeout, legacy, mjTimeout, mjFailure, adjustment string }{
		{"", "Task timed out (30 minutes)", "Task timed out (legacy task; no automatic refund, contact the administrator)", "Upstream task timed out (more than 1 hour)", "Image composition failed", "Adaptor billing adjustment"},
		{"en", "Task timed out (30 minutes)", "Task timed out (legacy task; no automatic refund, contact the administrator)", "Upstream task timed out (more than 1 hour)", "Image composition failed", "Adaptor billing adjustment"},
		{"ru", "Время ожидания задачи истекло (30 мин.)", "Время ожидания задачи истекло (задача старой системы; автоматического возврата нет, обратитесь к администратору)", "Время ожидания задачи провайдера истекло (более 1 часа)", "Не удалось создать изображение", "Корректировка оплаты адаптером"},
		{"zh-CN", "任务超时（30分钟）", "任务超时（旧系统遗留任务，不进行退款，请联系管理员）", "上游任务超时（超过1小时）", "构图失败", "adaptor计费调整"},
		{"zh-TW", "任务超时（30分钟）", "任务超时（旧系统遗留任务，不进行退款，请联系管理员）", "上游任务超时（超过1小时）", "构图失败", "adaptor计费调整"},
	} {
		t.Run(fmt.Sprintf("%d_%s", i, tc.language), func(t *testing.T) {
			settings, err := common.Marshal(map[string]any{"language": tc.language})
			require.NoError(t, err)
			id := 100 + i
			require.NoError(t, db.Create(&model.User{Id: id, Username: fmt.Sprintf("recipient_%d", id), AffCode: fmt.Sprintf("recipient_%d", id), Setting: string(settings)}).Error)
			text := TaskText{Language: accountstorage.Cache().GetUserLanguage}
			assert.Equal(t, tc.timeout, text.Timeout(id, 30))
			assert.Equal(t, tc.legacy, text.LegacyTimeout(id))
			assert.Equal(t, tc.mjTimeout, text.MidjourneyTimeout(id))
			assert.Equal(t, tc.mjFailure, text.MidjourneyFailure(id))
			assert.Equal(t, tc.adjustment, text.Adjustment(id, "Adaptor billing adjustment"))
			assert.Equal(t, tc.adjustment, text.Adjustment(id, "adaptor计费调整"))
			assert.Equal(t, "provider raw error: 构图失败", text.Adjustment(id, "provider raw error: 构图失败"))
		})
	}
	for _, language := range []string{"zh-CN", "zh-TW"} {
		// Invalid historical group settings keep their exact numeric formatting too.
		require.Equal(t, "token重算：tokens=1, modelRatio=1.00, groupRatio=-1.00, otherMultiplier=1.0000", i18n.Translate(language, i18n.MsgTasksTokenSettlement, map[string]any{"Tokens": "1", "ModelRatio": "1.00", "GroupRatio": "-1.00", "OtherMultiplier": "1.0000"}))
		data := map[string]any{"Price": "0.02", "Ratio": "1.00", "Action": "IMAGINE", "ID": "mj-fixture"}
		assert.Equal(t, "模型固定价格 0.02，分组倍率 1.00，操作 IMAGINE，ID mj-fixture", i18n.Translate(language, i18n.MsgTasksMJPriceID, data))
		assert.Equal(t, "操作 GENERATE，按次计费", i18n.Translate(language, i18n.MsgTasksPerCall, map[string]any{"Content": "操作 GENERATE"}))
		assert.Equal(t, "操作 GENERATE, 计算参数：seconds: 5.00", i18n.Translate(language, i18n.MsgTasksParameters, map[string]any{"Content": "操作 GENERATE", "Parameters": "seconds: 5.00"}))
	}
}
