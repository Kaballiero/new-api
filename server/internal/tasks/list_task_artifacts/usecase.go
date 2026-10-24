package list_task_artifacts

import "github.com/QuantumNous/new-api/internal/tasks/domain"

type URLs interface {
	ContentURL(taskID, key string) (string, error)
}
type Input struct {
	TaskID  string
	Access  domain.TaskAccess
	OwnOnly bool
}
type Artifact struct {
	Key        string `json:"key"`
	Type       string `json:"type"`
	MimeType   string `json:"mime_type,omitempty"`
	ContentURL string `json:"content_url"`
}
type Result struct {
	TaskID           string
	Artifacts        []Artifact
	LegacyContentURL string
}
type Usecase struct {
	reader    domain.TaskAccessReader
	projector domain.ArtifactProjector
	urls      URLs
}

func NewUsecase(reader domain.TaskAccessReader, projector domain.ArtifactProjector, urls URLs) Usecase {
	return Usecase{reader: reader, projector: projector, urls: urls}
}
func (u Usecase) Execute(in Input) (Result, error) {
	var task *domain.Task
	var exists bool
	var err error
	if in.OwnOnly {
		task, exists, err = u.reader.GetOwn(in.Access.UserID, in.TaskID)
	} else {
		task, exists, err = domain.ResolveTaskAccess(u.reader, in.Access, in.TaskID)
	}
	if err != nil {
		return Result{}, domain.ErrTaskQuery
	}
	if !exists || task == nil {
		return Result{}, domain.ErrArtifactNotFound
	}
	artifacts, err := domain.ProjectArtifacts(task, u.projector)
	if err != nil {
		return Result{}, err
	}
	items := make([]Artifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		contentURL, err := u.urls.ContentURL(task.TaskID, artifact.Key)
		if err != nil {
			return Result{}, domain.ErrArtifactURL
		}
		items = append(items, Artifact{Key: artifact.Key, Type: artifact.Type, MimeType: artifact.MimeType, ContentURL: contentURL})
	}
	result := Result{TaskID: task.TaskID, Artifacts: items}
	if domain.LegacyVideoAvailable(task) {
		result.LegacyContentURL, err = u.urls.ContentURL(task.TaskID, "video")
		if err != nil {
			return Result{}, domain.ErrArtifactURL
		}
	}
	return result, nil
}
