package resolve_plugin_origins

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/internal/taskplugins/domain"
	tasksdomain "github.com/QuantumNous/new-api/internal/tasks/domain"
)

type Reader interface {
	GetOwn(int, string) (*tasksdomain.Task, bool, error)
	ChannelEnabled(int) (bool, error)
}
type Usecase struct {
	reader     Reader
	pluginType int
	suno       string
}

func NewUsecase(r Reader, pluginType int, suno string) Usecase {
	return Usecase{reader: r, pluginType: pluginType, suno: suno}
}

type Input struct {
	UserID   int
	Identity domain.Identity
	Value    any
	Present  bool
}
type Result struct {
	Tasks     []*tasksdomain.Task
	ChannelID int
}

func (u Usecase) Execute(ctx context.Context, input Input) (Result, error) {
	raw, present := input.Value, input.Present
	if !present {
		return Result{}, nil
	}
	var values []any
	switch typed := raw.(type) {
	case []any:
		values = typed
	case []string:
		values = make([]any, len(typed))
		for i, id := range typed {
			values[i] = id
		}
	default:
		return Result{}, &domain.OriginError{Code: "invalid_origin_task_ids", Message: "origin task ids are invalid", StatusCode: http.StatusBadRequest}
	}
	if len(values) > 16 {
		return Result{}, &domain.OriginError{Code: "invalid_origin_task_ids", Message: "origin task ids are invalid", StatusCode: http.StatusBadRequest}
	}
	ids := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		id, ok := value.(string)
		if !ok {
			return Result{}, &domain.OriginError{Code: "invalid_origin_task_ids", Message: "origin task ids are invalid", StatusCode: http.StatusBadRequest}
		}
		id = strings.TrimSpace(id)
		if id == "" || utf8.RuneCountInString(id) > 128 {
			return Result{}, &domain.OriginError{Code: "invalid_origin_task_ids", Message: "origin task ids are invalid", StatusCode: http.StatusBadRequest}
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return Result{}, nil
	}

	userID := input.UserID
	platforms := input.Identity.Platforms(u.pluginType, u.suno)
	allowedPlatform := make(map[string]struct{}, len(platforms))
	for _, platform := range platforms {
		allowedPlatform[platform] = struct{}{}
	}

	tasks := make([]*tasksdomain.Task, 0, len(ids))
	channelID := 0
	for _, id := range ids {
		task, exist, err := u.reader.GetOwn(userID, id)
		if err != nil {
			return Result{},
				&domain.OriginError{Code: "origin_task_not_found",
					Message:    "origin task not found or not owned by you",
					StatusCode: http.StatusInternalServerError}
		}
		if !exist || task == nil {
			return Result{},
				&domain.OriginError{Code: "origin_task_not_found",
					Message:    "origin task not found or not owned by you",
					StatusCode: http.StatusBadRequest}
		}
		if _, allowed := allowedPlatform[task.Platform]; !allowed {
			return Result{},
				&domain.OriginError{Code: "origin_task_platform_mismatch",
					Message:    "origin task does not belong to this plugin",
					StatusCode: http.StatusBadRequest}
		}
		if channelID == 0 {
			channelID = task.ChannelId
		} else if task.ChannelId != channelID {
			return Result{},
				&domain.OriginError{Code: "origin_task_channel_conflict",
					Message:    "origin tasks must belong to the same channel",
					StatusCode: http.StatusBadRequest}
		}
		tasks = append(tasks, task)
	}

	enabled, err := u.reader.ChannelEnabled(channelID)
	if err != nil || !enabled {
		return Result{},
			&domain.OriginError{Code: "origin_task_channel_disabled",
				Message:    "origin task channel is disabled",
				StatusCode: http.StatusBadRequest}
	}
	return Result{Tasks: tasks, ChannelID: channelID}, nil

}
