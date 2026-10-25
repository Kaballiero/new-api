package taskplugins

import (
	record "github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/relay/taskview"
)

type TaskProjector struct{}

func (TaskProjector) View(t *domain.Task) (map[string]any, error) {
	view, e := taskview.BuildTaskPluginView(record.FromDomain(t))
	if e != nil {
		return nil, e
	}
	out := map[string]any{"task_id": view.TaskID, "platform": view.Platform, "status": view.Status,
		"progress": view.Progress, "fail_reason": view.FailReason, "created_at": float64(view.CreatedAt)}
	if view.UpdatedAt != 0 {
		out["updated_at"] = float64(view.UpdatedAt)
	}
	if view.FinishedAt != 0 {
		out["finished_at"] = float64(view.FinishedAt)
	}
	if view.Data != nil {
		out["data"] = view.Data
	}
	return out, nil
}
