package tasks

import (
	"context"
	"fmt"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/adapter/gorm/tasks/record"
	"github.com/QuantumNous/new-api/internal/tasks/domain"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/service"
)

type ArtifactStorage struct{}

func (ArtifactStorage) ContentURL(taskID, key string) (string, error) {
	return service.BuildTaskArtifactContentURL(taskID, key)
}
func (ArtifactStorage) Resolve(task *domain.Task, key string) (*domain.StoredArtifactRef, error) {
	ref, err := service.GetTaskArtifactStore().Resolve(record.FromDomain(task), key)
	if ref == nil {
		return nil, err
	}
	return &domain.StoredArtifactRef{
		Backend:   ref.Backend,
		Bucket:    ref.Bucket,
		ObjectKey: ref.ObjectKey,
		MimeType:  ref.MimeType,
		Size:      ref.Size,
		SHA256:    ref.SHA256,
	}, err
}
func (ArtifactStorage) Serve(c *gin.Context, task *domain.Task, ref *domain.StoredArtifactRef) error {
	return service.GetTaskArtifactStore().Serve(c, record.FromDomain(task), &service.StoredArtifactRef{
		Backend:   ref.Backend,
		Bucket:    ref.Bucket,
		ObjectKey: ref.ObjectKey,
		MimeType:  ref.MimeType,
		Size:      ref.Size,
		SHA256:    ref.SHA256,
	})
}

type VideoDiagnostics struct{}

func (VideoDiagnostics) QueryError(ctx context.Context, taskID string, err error) {
	logger.LogError(ctx, fmt.Sprintf("Failed to query task %s: %s", taskID, err.Error()))
}
func (VideoDiagnostics) ContentWarning(ctx context.Context, taskID string) {
	logger.LogWarn(ctx, fmt.Sprintf("Failed to resolve plugin video content for task %s", taskID))
}
func (VideoDiagnostics) ProjectionWarning(ctx context.Context, taskID string) {
	logger.LogWarn(ctx, fmt.Sprintf("Failed to project plugin video for task %s", taskID))
}
