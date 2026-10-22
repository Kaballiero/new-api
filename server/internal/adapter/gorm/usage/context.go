package usage

import (
	"context"
	"encoding/json"

	"github.com/gin-gonic/gin"
)

// logMetadata contains storage-only data, never business policy.
// Cost details must retain the provider's JSON bytes. Affinity is the owner's
// unversioned per-rule audit object; its keys are deliberately not usage policy.
type logMetadata struct {
	CostDetails json.RawMessage
	Affinity    map[string]any
}
type recordingContext struct {
	context.Context
	metadata logMetadata
}

func WithLogMetadata(ctx context.Context, details []byte, affinity map[string]any) context.Context {
	return recordingContext{Context: ctx, metadata: logMetadata{CostDetails: details, Affinity: affinity}}
}
func gatewayContext(ctx context.Context) (*gin.Context, logMetadata, error) {
	metadata, wrapped := ctx.(recordingContext)
	if wrapped {
		ctx = metadata.Context
	}
	c, ok := ctx.(*gin.Context)
	if !ok || c == nil {
		return nil, metadata.metadata, errGatewayContext
	}
	return c, metadata.metadata, nil
}
