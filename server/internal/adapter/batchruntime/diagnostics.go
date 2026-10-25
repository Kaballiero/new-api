package batchruntime

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/batch/domain"
	"github.com/QuantumNous/new-api/logger"
)

type Diagnostics struct{}

func (Diagnostics) Error(message string) { common.SysError(message) }
func (Diagnostics) Saturation(job *domain.Job, id string, clamp error) {
	requestID := fmt.Sprintf("%x", sha256.Sum256([]byte(job.PublicID+":"+id)))
	ctx := context.WithValue(context.Background(), common.RequestIdKey, requestID)
	logger.LogWarn(ctx, fmt.Sprintf("batch %s item %s quota saturation: %v", job.PublicID, id, clamp))
}
