package dto

import "github.com/QuantumNous/new-api/internal/system/domain"

type SystemTaskResponse struct {
	ID        int64                   `json:"id"`
	TaskID    string                  `json:"task_id"`
	Type      string                  `json:"type"`
	Status    domain.SystemTaskStatus `json:"status"`
	ActiveKey *string                 `json:"active_key,omitempty"`
	Payload   any                     `json:"payload"`
	State     any                     `json:"state"`
	Result    any                     `json:"result"`
	Error     string                  `json:"error"`
	LockedBy  string                  `json:"locked_by"`
	CreatedAt int64                   `json:"created_at"`
	UpdatedAt int64                   `json:"updated_at"`
}

func TaskResponse(task *domain.SystemTask, decode func(string) any) SystemTaskResponse {
	return SystemTaskResponse{
		ID:        task.ID,
		TaskID:    task.TaskID,
		Type:      task.Type,
		Status:    task.Status,
		ActiveKey: task.ActiveKey,
		Payload:   decode(task.Payload),
		State:     decode(task.State),
		Result:    decode(task.Result),
		Error:     task.Error,
		LockedBy:  task.LockedBy,
		CreatedAt: task.CreatedAt,
		UpdatedAt: task.UpdatedAt,
	}
}
