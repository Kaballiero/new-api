package domain

import "errors"

type SystemTaskStatus string

const (
	SystemTaskStatusPending   SystemTaskStatus = "pending"
	SystemTaskStatusRunning   SystemTaskStatus = "running"
	SystemTaskStatusSucceeded SystemTaskStatus = "succeeded"
	SystemTaskStatusFailed    SystemTaskStatus = "failed"

	SystemTaskTypeLogCleanup     = "log_cleanup"
	SystemTaskTypeChannelTest    = "channel_test"
	SystemTaskTypeModelUpdate    = "model_update"
	SystemTaskTypeMidjourneyPoll = "midjourney_poll"
	SystemTaskTypeAsyncTaskPoll  = "async_task_poll"
	SystemTaskTypeModelCostFX    = "model_cost_fx"
	SystemTaskTypePricingCatalog = "pricing_catalog"
)

var ErrSystemTaskLockLost = errors.New("system task lock lost")

type SystemTask struct {
	ID        int64            `json:"id"`
	TaskID    string           `json:"task_id"`
	Type      string           `json:"type"`
	Status    SystemTaskStatus `json:"status"`
	ActiveKey *string          `json:"active_key,omitempty"`
	Payload   string           `json:"payload"`
	State     string           `json:"state"`
	Result    string           `json:"result"`
	Error     string           `json:"error"`
	LockedBy  string           `json:"locked_by"`
	CreatedAt int64            `json:"created_at"`
	UpdatedAt int64            `json:"updated_at"`
}

var ErrTargetTimestamp = errors.New("target timestamp is required")
