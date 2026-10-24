package domain

import "errors"

var ErrTaskNotFound = errors.New("task not found")
var ErrTaskQuery = errors.New("failed to query task")
var ErrArtifactNotFound = errors.New("task or artifact not found")
var ErrArtifactsNotReady = errors.New("task artifacts are not ready")
var ErrArtifactStorageUnavailable = errors.New("task artifact storage is unavailable")
var ErrArtifactURL = errors.New("failed to build artifact content URL")
var ErrArtifactContentUnavailable = errors.New("artifact content plugin is unavailable")
var ErrArtifactContent = errors.New("artifact content plugin failed")
var ErrVideoTaskIDRequired = errors.New("task_id is required")
var ErrArtifactGone = errors.New("artifact content is no longer available")

type TaskIncompleteError struct{ Status TaskStatus }

func (e *TaskIncompleteError) Error() string {
	return "task is not completed yet, current status: " + string(e.Status)
}

type TaskAccess struct {
	Capability bool
	UserID     int
	TokenID    int
	Admin      bool
}

// TaskAccessReader preserves owner, dashboard and unique-public-ID queries.
type TaskAccessReader interface {
	GetOwn(userID int, taskID string) (*Task, bool, error)
	GetFirst(taskID string) (*Task, bool, error)
	GetUnique(taskID string) (*Task, bool, error)
	OwnerEnabled(userID int) (bool, error)
}

func ResolveTaskAccess(reader TaskAccessReader, access TaskAccess, taskID string) (*Task, bool, error) {
	if access.Capability {
		task, exists, err := reader.GetUnique(taskID)
		if err != nil || !exists || task == nil {
			return task, exists, err
		}
		enabled, err := reader.OwnerEnabled(task.UserId)
		if err != nil || !enabled {
			return nil, false, err
		}
		return task, true, nil
	}
	if access.TokenID == 0 && access.Admin {
		return reader.GetFirst(taskID)
	}
	return reader.GetOwn(access.UserID, taskID)
}

// StoredArtifactRef is an immutable result descriptor, without filesystem handles.
type StoredArtifactRef struct {
	Backend   string
	Bucket    string
	ObjectKey string
	MimeType  string
	Size      int64
	SHA256    string
}

type ArtifactProjector interface {
	ListArtifacts(task *Task) ([]Artifact, error)
}

func ProjectArtifacts(task *Task, projector ArtifactProjector) ([]Artifact, error) {
	if task != nil && task.PrivateData.OpenRouterSyncState() != nil && task.PrivateData.OpenRouterSyncState().ResultReady {
		sync := task.PrivateData.OpenRouterSyncState()
		if sync.ResultSHA256 == "" {
			return []Artifact{}, nil
		}
		kind := "file"
		if len(sync.ResultMimeType) >= len("audio/") && sync.ResultMimeType[:len("audio/")] == "audio/" {
			kind = "audio"
		}
		return ValidateArtifacts([]Artifact{{Key: "result", Type: kind, MimeType: sync.ResultMimeType}})
	}
	if task == nil || task.Status != TaskStatusSuccess || !HasPluginExecution(task) {
		return []Artifact{}, nil
	}
	artifacts, err := projector.ListArtifacts(task)
	if err != nil {
		return nil, err
	}
	return ValidateArtifacts(artifacts)
}
