package taskplugins

import (
	"testing"

	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"

	"github.com/stretchr/testify/assert"
)

type completionFactsAdaptor struct{ channel.TaskAdaptor }

func (completionFactsAdaptor) AdjustBillingOnComplete(task *record.Task, result *relaycommon.TaskInfo) int {
	result.TotalTokens = 73
	result.UsageFacts = map[string]any{"final_units": 73}
	task.PrivateData.PluginState = []byte(`{"settled":true}`)
	return 0
}
func TestCompletionHookRetainsFinalUsageFactsAndTaskState(t *testing.T) {
	task := &domain.Task{TaskID: "task-hook-fixture"}
	result := &domain.TaskInfo{TotalTokens: 2}
	quota := (poller{adaptor: completionFactsAdaptor{}}).AdjustBillingOnComplete(task, result)
	assert.Zero(t, quota)
	assert.Equal(t, 73, result.TotalTokens)
	assert.Equal(t, map[string]any{"final_units": 73}, result.UsageFacts)
	assert.JSONEq(t, `{"settled":true}`, string(task.PrivateData.PluginState))
}
