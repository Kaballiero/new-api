package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/QuantumNous/new-api/constant"
)

var ErrArtifactPluginUnavailable = errors.New("task artifact plugin unavailable")
var ErrArtifactPlugin = errors.New("task artifact plugin error")
var ArtifactKeyPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._~-]{0,127}$`)

type Artifact struct {
	Key      string
	Type     string
	MimeType string
}
type MediaClientRequest struct {
	Method  string
	Headers map[string]string
}

func HasPluginExecution(task *Task) bool {
	return task != nil && task.PrivateData.Execution != nil && task.PrivateData.Execution.TaskPlugin != nil &&
		strings.TrimSpace(task.PrivateData.Execution.TaskPlugin.Key) != ""
}
func LegacyVideoAvailable(task *Task) bool {
	if task == nil || task.Status != TaskStatusSuccess || HasPluginExecution(task) || task.Platform == string(TaskPlatformSuno) ||
		strings.TrimSpace(task.GetResultURL()) == "" {
		return false
	}
	switch constant.NormalizeTaskAction(task.Action) {
	case TaskActionImageToVideo, TaskActionTextToVideo, TaskActionFirstTailToVideo, TaskActionReferenceToVideo, TaskActionRemix:
		return true
	default:
		return false
	}
}
func FailReasonIsLegacyResultURL(value string) bool {
	value = strings.TrimSpace(value)
	return len(value) >= len("https://") && strings.EqualFold(value[:len("https://")], "https://") ||
		len(value) >= len("http://") && strings.EqualFold(value[:len("http://")], "http://") ||
		len(value) >= len("data:") && strings.EqualFold(value[:len("data:")], "data:")
}
func ValidateArtifacts(artifacts []Artifact) ([]Artifact, error) {
	if len(artifacts) > 64 {
		return nil, fmt.Errorf("%w: too many artifacts", ErrArtifactPlugin)
	}
	seen := make(map[string]struct{}, len(artifacts))
	for i := range artifacts {
		if artifacts[i].Key != strings.TrimSpace(artifacts[i].Key) || artifacts[i].Type != strings.TrimSpace(artifacts[i].Type) {
			return nil, fmt.Errorf("%w: invalid artifact identity", ErrArtifactPlugin)
		}
		if !ArtifactKeyPattern.MatchString(artifacts[i].Key) {
			return nil, fmt.Errorf("%w: invalid artifact key", ErrArtifactPlugin)
		}
		if _, exists := seen[artifacts[i].Key]; exists {
			return nil, fmt.Errorf("%w: duplicate artifact key", ErrArtifactPlugin)
		}
		seen[artifacts[i].Key] = struct{}{}
		switch artifacts[i].Type {
		case "video", "audio", "image", "file":
		default:
			return nil, fmt.Errorf("%w: invalid artifact type", ErrArtifactPlugin)
		}
		if len(artifacts[i].MimeType) > 255 || strings.ContainsAny(artifacts[i].MimeType, "\r\n") {
			return nil, fmt.Errorf("%w: invalid artifact mime type", ErrArtifactPlugin)
		}
	}
	return artifacts, nil
}
