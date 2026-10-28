package usage

import (
	"context"
	"encoding/json"
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

type observationContext struct {
	context.Context
	observation LogObservation
}

func WithRequestObservation(ctx context.Context, observation LogObservation) context.Context {
	return observationContext{Context: ctx, observation: observation}
}

func recordingObservation(ctx context.Context) (LogObservation, logMetadata, error) {
	metadata, wrapped := ctx.(recordingContext)
	if wrapped {
		ctx = metadata.Context
	}
	observed, ok := ctx.(observationContext)
	if !ok || observed.observation.Identity == nil || observed.observation.ClientIP == nil {
		return LogObservation{}, metadata.metadata, errGatewayContext
	}
	return observed.observation, metadata.metadata, nil
}
